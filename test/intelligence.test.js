"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");
const catalog = require("../src/catalog.json");
const counters = require("../src/counters.json");
const { create, validate } = require("../src/engine");
const { observe } = require("../src/observer");

test("reference counter table resolves against the pinned catalog and accepts localized input", () => {
  assert.deepEqual(validate(catalog, counters), []);
  const heroes = Object.keys(counters.heroes);
  assert.equal(heroes.length, 42);
  for (const hero of heroes) assert.equal(new Set(counters.heroes[hero]).size, 5, hero);
  const lookup = create(catalog, counters);
  assert.equal(lookup.itemKey("Şifakıran"), "upgrade_healbane");
  assert.equal(lookup.itemKey("2603935618"), "upgrade_healbane");
  assert.equal(lookup.heroKey("Vindicta"), "hero_hornet");
  assert.deepEqual(lookup.counters("hero_haze"), [
    "upgrade_auto_cleanse", "upgrade_deflecting_armor", "upgrade_metal_skin",
    "upgrade_reduce_debuff_duration", "upgrade_target_stun",
  ]);
});

test("items that answer more of the enemy lineup rank first and name the heroes they counter", () => {
  const engine = create(catalog, counters);
  const top = engine.recommend(["hero_haze", "hero_wraith", "hero_warden"]);
  assert.deepEqual(top.map((item) => item.name), ["Plated Armor", "Metal Skin", "Unstoppable"]);
  assert.equal(top[0].reason, "Counters 3 enemies");
  assert.equal(top[0].evidence, "Haze · Wraith · Warden");
  assert.equal(top[0].category, "vitality");
  assert.equal(top[2].evidence, "Wraith · Warden");
  assert.equal(engine.recommend(["hero_haze"], 5).length, 5);
});

test("heroes without a reference build and duplicates contribute nothing", () => {
  const engine = create(catalog, counters);
  assert.deepEqual(engine.recommend([]), []);
  assert.deepEqual(engine.recommend(["hero_synth", "missing"]), []);
  assert.deepEqual(engine.recommend(["hero_haze", "hero_haze"]), engine.recommend(["hero_haze"]));
});

test("enemy purchases from an allied hero's counter list raise a threat, yours first", () => {
  const engine = create(catalog, counters);
  const allies = ["hero_ghost", "hero_inferno"];
  const threats = engine.threats([
    { hero: "hero_haze", item: "upgrade_metal_skin" },
    { hero: "hero_haze", item: "upgrade_healbane" },
    { hero: "hero_haze", item: "upgrade_healbane" },
    { hero: "hero_wraith", item: "upgrade_rupture" },
    { hero: "hero_ghost", item: "upgrade_healbane" },
  ], allies, "hero_inferno");
  assert.deepEqual(threats.map((threat) => threat.text), [
    "Haze bought Healbane — counters you (Infernus), Lady Geist",
    "Wraith bought Decay — counters Lady Geist",
  ]);
  assert.equal(threats[0].targetsYou, true);
  assert.deepEqual(engine.threats([{ hero: "hero_haze", item: "missing" }], allies, null), []);
});

test("ambiguous labels are skipped and missing items cannot be offered", () => {
  const fixture = structuredClone(catalog);
  const [a, b] = fixture.items;
  b[3] = a[2];
  assert.equal(create(fixture, counters).itemKey(a[2]), null);
  fixture.items = fixture.items.filter((row) => row[1] !== "upgrade_auto_cleanse");
  assert.ok(!create(fixture, counters).recommend(["hero_haze"]).some((item) => item.key === "upgrade_auto_cleanse"));
  assert.deepEqual(validate(fixture, counters), ["upgrade_auto_cleanse"]);
});

function panel({id, type = "Panel", classes = [], children = [], visible = true, collapsed = false, tokens = {}, text = ""} = {}) {
  const value = {
    id, paneltype: type, visible, text, tokens,
    actuallayoutwidth: collapsed ? 0 : 120,
    actuallayoutheight: collapsed ? 0 : 120,
    IsValid: () => true,
    GetParent: () => value.parent || null,
    BHasClass: (name) => classes.includes(name),
    Children: () => children,
    FindChildTraverse: (name) => children.find((child) => child.id === name)
      || children.map((child) => child.FindChildTraverse(name)).find(Boolean) || null,
    FindChildrenWithClassTraverse: (name) => children.flatMap((child) => [
      ...(child.BHasClass(name) ? [child] : []), ...child.FindChildrenWithClassTraverse(name),
    ]),
  };
  for (const child of children) child.parent = value;
  return value;
}

function buildHud({realLocal = true, enemyVisible = true} = {}) {
  const player = (heroId, classes, visible = true) => panel({
    type: "CitadelHudTopBarPlayer", classes, visible, tokens: {"{i:hero_id}": heroId},
  });
  const purchase = (hero, text, { visible = true, collapsed = false } = {}) => panel({
    classes: ["recentPurchase"], visible, tokens: {"{s:recent_hero_name}": hero},
    children: [
      panel({ id: "RecentPurchaseHeroImage", collapsed }),
      panel({ classes: ["recentModPurchaseName"], text }),
    ],
  });
  return panel({ children: [
    panel({ id: "TopBar", children: [
      panel({ id: "TeamFriendly", children: [player("7", realLocal ? ["LocalPlayer"] : []), player("1", [])] }),
      panel({ id: "TeamEnemy", children: [player("13", [], enemyVisible)] }),
    ] }),
    panel({ id: "CitadelHudHeroShop", children: [
      panel({ id: "RecentPurchasesContainer", children: [
        purchase("Haze", "Glass Cannon"),
        purchase("Wraith", "Ricochet"),
        purchase("Haze", "Burst Fire", { visible: false }),
        purchase("Haze", "Ricochet", { collapsed: true }),
        purchase("Haze", "Unrecognized Item"),
      ] }),
    ] }),
  ] });
}

const engine = create(catalog, counters);
const localize = (token, panel) => panel.tokens[token] || token;

test("observer reads only visible enemy purchases and only with the shop open", () => {
  const hud = buildHud();
  const teams = { heroes: ["hero_haze"], allies: ["hero_wraith", "hero_inferno"], self: "hero_wraith" };
  assert.deepEqual(observe(hud, engine, localize, true), {
    ...teams, purchases: [{ hero: "hero_haze", item: "upgrade_glass_cannon" }],
  });
  assert.deepEqual(observe(hud, engine, localize, false), { ...teams, purchases: [] });
  hud.FindChildTraverse("CitadelHudHeroShop").visible = false;
  assert.deepEqual(observe(hud, engine, localize, true), { ...teams, purchases: [] });
});

test("observer requires a visible local player and visible enemy cards", () => {
  const none = { heroes: [], allies: [], self: null, purchases: [] };
  assert.deepEqual(observe(buildHud({ realLocal: false }), engine, localize, true), none);
  assert.deepEqual(observe(buildHud({ enemyVisible: false }), engine, localize, true), none);
});

test("HUD shows lineup counters and an alert that expires and fires once per purchase", () => {
  const fs = require("node:fs");
  const path = require("node:path");
  const vm = require("node:vm");
  const nodes = {};
  const modes = new Set(["gScoreboardOpen"]);
  const scheduled = [];
  const allies = ["hero_inferno", "hero_ghost"];
  let observed = {heroes: ["hero_hornet"], allies, self: "hero_inferno", purchases: []};
  const root = {
    IsValid: () => true,
    BHasClass: (name) => modes.has(name),
    FindChildTraverse: (id) => id === "DCIVisibility" ? root : (nodes[id] ||= {
      FindChildTraverse: root.FindChildTraverse,
      SetHasClass(name, active) { this[name] = active; },
    }),
  };
  const filename = path.resolve(__dirname, "../src/hud.js");
  vm.runInNewContext(fs.readFileSync(filename, "utf8"), {
    $: {
      GetContextPanel: () => root,
      Schedule: (delay, callback) => scheduled.push({delay, callback}),
      Msg: (line) => assert.fail(line),
    },
    DCIEngine: {create},
    DCI_CATALOG: catalog,
    DCI_COUNTERS: counters,
    DCIObserver: {observe: () => observed},
  }, {filename});
  const step = () => scheduled.shift().callback();

  step();
  assert.equal(nodes.DCISource.text, "Enemy lineup");
  assert.equal(nodes.DCIName0.text, "Knockdown");
  assert.equal(nodes.DCIRow0.DCISpirit, true);
  assert.equal(nodes.DCIPrice0.text, "3,200");
  assert.equal(nodes.DCIEvidence0.text, "Vindicta");
  assert.equal(nodes.DCIEmpty.visible, false);
  assert.equal(nodes.DCIAlert?.DCIAlertActive, undefined);

  modes.add("gShopOpen");
  const healbane = {hero: "hero_hornet", item: "upgrade_healbane"};
  observed = {...observed, purchases: [healbane, {hero: "hero_hornet", item: "upgrade_rupture"}]};
  step();
  assert.equal(nodes.DCIAlert.DCIAlertActive, true);
  assert.equal(nodes.DCIAlert.DCIAlertYou, true);
  assert.equal(nodes.DCIAlertText.text, "Vindicta bought Healbane — counters you (Infernus), Lady Geist (+1 more)");

  modes.clear();
  for (let i = 0; i < 8; i++) step(); // 0.5 s in the shop + 7 × 1 s idle ticks
  assert.equal(nodes.DCIAlert.DCIAlertActive, true, "still visible after 7.5 s");
  step();
  assert.equal(nodes.DCIAlert.DCIAlertActive, false, "hidden after 8 s");
  assert.equal(nodes.DCIEmpty.text, "Waiting for enemy lineup.");

  modes.add("gShopOpen");
  step();
  assert.equal(nodes.DCIAlert.DCIAlertActive, false, "the same purchase alerts only once");

  observed = {heroes: ["hero_hornet", "hero_synth"], allies, self: "hero_inferno", purchases: [healbane]};
  step();
  assert.equal(nodes.DCIAlert.DCIAlertActive, true, "a new enemy lineup starts a new match");
  assert.equal(nodes.DCIAlertText.text, "Vindicta bought Healbane — counters you (Infernus), Lady Geist");

  observed = {heroes: ["hero_synth"], allies, self: "hero_inferno", purchases: []};
  step();
  assert.equal(nodes.DCIRow0.visible, false);
  assert.equal(nodes.DCIEmpty.text, "No reference builds for this lineup.");
});
