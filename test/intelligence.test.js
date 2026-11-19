"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");
const catalog = require("../src/catalog.json");
const counters = require("../src/counters.json");
const stats = require("../src/stats.json");
const { create, validate, formatImpact, formatStats } = require("../src/engine");
const { observe } = require("../src/observer");

test("reference counter table resolves against the pinned catalog and accepts localized input", () => {
  assert.deepEqual(validate(catalog, counters, stats), []);
  const heroes = Object.keys(counters.heroes);
  assert.equal(heroes.length, catalog.heroes.length, "every hero has reference counters");
  for (const hero of heroes) {
    assert.equal(new Set(counters.heroes[hero]).size, 5, hero);
    for (const item of counters.heroes[hero]) assert.ok(counters.tags[item], `${item} has a tag`);
  }
  assert.equal(Object.keys(stats.heroes).length, catalog.heroes.length, "match data covers every hero");
  assert.match(stats.window.from, /^\d{4}-\d{2}-\d{2}$/);
  const lookup = create(catalog, counters);
  assert.equal(lookup.itemKey("Şifakıran"), "upgrade_healbane");
  assert.equal(lookup.itemKey("2603935618"), "upgrade_healbane");
  assert.equal(lookup.heroKey("Vindicta"), "hero_hornet");
  assert.deepEqual(lookup.counters("hero_haze"), [
    "upgrade_auto_cleanse", "upgrade_deflecting_armor", "upgrade_metal_skin",
    "upgrade_reduce_debuff_duration", "upgrade_target_stun",
  ]);
});

// Synthetic match data, in hundredths of a win-rate point.
const fixture = {
  window: {from: "2026-10-20", to: "2026-11-19"},
  minutes: {upgrade_metal_skin: 14, upgrade_glass_cannon: 22, upgrade_close_range: 3},
  lane: {
    hero_haze: [["upgrade_close_range", 120], ["upgrade_vex_barrier", 40]],
    hero_wraith: [["upgrade_vex_barrier", 90]],
  },
  items: ["upgrade_metal_skin", "upgrade_glass_cannon", "upgrade_deflecting_armor", "upgrade_hollow_point_rounds"],
  heroes: {
    hero_haze: [120, 40, -10, null],
    hero_wraith: [30, 20, 0, 30],
    hero_hornet: [null, 64, null, 0],
  },
};
const lineup = ["hero_haze", "hero_wraith", "hero_warden"];

test("items that answer more of the enemy lineup rank first and name the heroes they counter", () => {
  const engine = create(catalog, counters);
  const top = engine.recommend(lineup);
  assert.deepEqual(top.map((item) => item.name), ["Plated Armor", "Metal Skin", "Unstoppable"]);
  assert.equal(top[0].reason, "Deflects bullets · counters 3 enemies");
  assert.equal(top[0].evidence, "Haze · Wraith · Warden");
  assert.equal(top[0].category, "vitality");
  assert.equal(top[0].impact, null);
  assert.equal(top[2].evidence, "Wraith · Warden");
  assert.equal(engine.recommend(["hero_haze"], 5).length, 5);
});

test("match data breaks coverage ties and reports the estimated win-rate change", () => {
  const engine = create(catalog, counters, fixture);
  const top = engine.recommend(lineup);
  assert.deepEqual(top.map((item) => item.name), ["Metal Skin", "Plated Armor", "Unstoppable"]);
  assert.equal(top[0].impact, 150);
  assert.equal(top[1].impact, -10);
  assert.equal(engine.impact("upgrade_metal_skin", ["hero_warden"]), null, "no row for Warden");
});

test("the match-data pick skips shown items and needs a meaningful gain", () => {
  const engine = create(catalog, counters, fixture);
  const pick = engine.dataPick(lineup, ["upgrade_metal_skin"]);
  assert.equal(pick.name, "Glass Cannon");
  assert.equal(pick.source, "data");
  assert.equal(pick.impact, 60);
  assert.equal(pick.reason, "Match data");
  assert.equal(pick.evidence, "Strongest vs Haze · Wraith");
  assert.equal(engine.dataPick(lineup, ["upgrade_metal_skin", "upgrade_glass_cannon"]), null);
  assert.equal(engine.dataPick(["hero_unknown"], []), null);
  assert.equal(create(catalog, counters).dataPick(lineup, []), null, "no data, no pick");
});

test("owned items are skipped and the first affordable suggestion is the one to buy", () => {
  const engine = create(catalog, counters, fixture);
  const all = engine.recommend(lineup).map((item) => item.key);
  const rest = engine.recommend(lineup, 3, ["upgrade_metal_skin"]).map((item) => item.key);
  assert.equal(all[0], "upgrade_metal_skin");
  assert.ok(!rest.includes("upgrade_metal_skin"), "an owned item is not suggested again");
  const rows = engine.recommend(lineup); // Metal Skin 3,200 · Plated Armor 6,400 · Unstoppable 6,400
  assert.equal(engine.buyNow(rows, 3199), -1);
  assert.equal(engine.buyNow(rows, 3200), 0);
  assert.equal(engine.buyNow(rows.slice(1), 7000), 0);
  assert.equal(engine.buyNow(rows, null), -1, "no badge when souls are unknown");
  assert.equal(rows[0].minute, 14);
});

test("the lane pick sums early lifts against the lane opponents", () => {
  const engine = create(catalog, counters, fixture);
  const pick = engine.lanePick(["hero_haze", "hero_wraith"], []);
  assert.equal(pick.key, "upgrade_vex_barrier", "40 + 90 beats 120");
  assert.equal(pick.impact, 130);
  assert.equal(pick.source, "lane");
  assert.equal(pick.reason, "Lane phase · Barrier when CC'd");
  assert.equal(pick.evidence, "Early vs Haze · Wraith");
  assert.equal(pick.minute, null, "a lane pick is for the first ten minutes");
  assert.equal(engine.lanePick(["hero_haze"], ["upgrade_close_range"]).key, "upgrade_vex_barrier");
  assert.equal(engine.lanePick(["hero_warden"], []), null);
  assert.equal(formatStats({impact: 120, minute: 3}), "+1.2 WR · ~3m");
  assert.equal(formatStats({impact: null, minute: null}), "");
});

test("win-rate changes are formatted for the HUD", () => {
  assert.equal(formatImpact(null), "");
  assert.equal(formatImpact(0), "±0 WR");
  assert.equal(formatImpact(4), "±0 WR");
  assert.equal(formatImpact(64), "+0.6 WR");
  assert.equal(formatImpact(-134), "−1.3 WR");
});

test("heroes without a reference build and duplicates contribute nothing", () => {
  const engine = create(catalog, counters);
  assert.deepEqual(engine.recommend([]), []);
  assert.deepEqual(engine.recommend(["missing", "hero_unknown"]), []);
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
  assert.deepEqual(validate(fixture, {heroes: counters.heroes}), ["upgrade_auto_cleanse"]);
});

function panel({id, type = "Panel", classes = [], children = [], visible = true, collapsed = false, tokens = {}, text = "",
  props = {}} = {}) {
  const value = {
    ...props, id, paneltype: type, visible, text, tokens,
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
  const player = (heroId, classes, visible = true, extra = {}) => panel({
    type: "CitadelHudTopBarPlayer", classes, visible, tokens: {"{i:hero_id}": heroId, ...extra.tokens},
    children: extra.children || [],
  });
  // The local player's card: souls in {s:gold} and purchased item icons under mod_universal.
  const local = {
    tokens: {"{s:gold}": "3.4k"},
    children: [panel({ id: "mod_universal", children: [
      panel({ props: {abilityname: "upgrade_healbane"} }),
      panel({ props: {abilityname: "citadel_ability_unknown"} }),
    ] })],
  };
  const purchase = (hero, text, { visible = true, collapsed = false } = {}) => panel({
    classes: ["recentPurchase"], visible, tokens: {"{s:recent_hero_name}": hero},
    children: [
      panel({ id: "RecentPurchaseHeroImage", collapsed }),
      panel({ classes: ["recentModPurchaseName"], text }),
    ],
  });
  return panel({ children: [
    panel({ id: "TopBar", children: [
      panel({ id: "GameTime", text: "4:12" }),
      panel({ id: "TeamFriendly", children: [player("7", realLocal ? ["LocalPlayer"] : [], true, local), player("1", [])] }),
      panel({ id: "TeamEnemy", children: [player("13", ["IsInLocalPlayerLane"], enemyVisible)] }),
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
  const teams = {
    heroes: ["hero_haze"], allies: ["hero_wraith", "hero_inferno"], self: "hero_wraith",
    lane: ["hero_haze"], souls: 3400, owned: ["upgrade_healbane"], clock: 252,
  };
  assert.deepEqual(observe(hud, engine, localize, true), {
    ...teams, purchases: [{ hero: "hero_haze", item: "upgrade_glass_cannon" }], mine: ["upgrade_ricochet"],
  });
  assert.deepEqual(observe(hud, engine, localize, false), { ...teams, purchases: [], mine: [] });
  hud.FindChildTraverse("CitadelHudHeroShop").visible = false;
  assert.deepEqual(observe(hud, engine, localize, true), { ...teams, purchases: [], mine: [] });
});

test("observer requires a visible local player and visible enemy cards", () => {
  const none = { heroes: [], allies: [], self: null, lane: [], souls: null, owned: [], clock: null, purchases: [], mine: [] };
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
    DCIEngine: {create, formatImpact, formatStats},
    DCI_CATALOG: catalog,
    DCI_COUNTERS: counters,
    DCI_STATS: fixture,
    DCIObserver: {observe: () => observed},
  }, {filename});
  const step = () => scheduled.shift().callback();

  step();
  assert.equal(nodes.DCISource.text, "Enemy lineup");
  assert.equal(nodes.DCIName0.text, "Knockdown");
  assert.equal(nodes.DCIRow0.DCISpirit, true);
  assert.equal(nodes.DCIPrice0.text, "3,200");
  assert.equal(nodes.DCIReason0.text, "Stun, grounds flyers · counters 1 enemy");
  assert.equal(nodes.DCIEvidence0.text, "Vindicta");
  assert.equal(nodes.DCIImpact0.visible, false, "no measurement for Knockdown");
  assert.equal(nodes.DCIName3.text, "Glass Cannon");
  assert.equal(nodes.DCIRow3.DCIDataPick, true);
  assert.equal(nodes.DCIRow3.DCIWeapon, true);
  assert.equal(nodes.DCIReason3.text, "Match data");
  assert.equal(nodes.DCIImpact3.text, "+0.6 WR · ~22m");
  assert.equal(nodes.DCIImpact3.DCIImpactUp, true);
  assert.equal(nodes.DCIEmpty.visible, false);
  assert.equal(nodes.DCIAlert?.DCIAlertActive, undefined);

  observed = {...observed, heroes: lineup};
  step();
  assert.deepEqual([0, 1, 2, 3].map((i) => nodes[`DCIName${i}`].text),
    ["Metal Skin", "Plated Armor", "Unstoppable", "Glass Cannon"]);
  assert.equal(nodes.DCIImpact0.text, "+1.5 WR · ~14m");
  assert.equal(nodes.DCIImpact1.text, "−0.1 WR");
  assert.equal(nodes.DCIImpact1.DCIImpactDown, true);
  assert.equal(nodes.DCIImpact1.DCIImpactUp, false);
  assert.equal(nodes.DCIRow1.DCIDataPick, false);
  assert.equal(nodes.DCIImpact2.visible, false);
  observed = {...observed, heroes: ["hero_hornet"]};

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

  observed = {heroes: ["hero_unknown"], allies, self: "hero_inferno", purchases: []};
  step();
  assert.equal(nodes.DCIRow0.visible, false);
  assert.equal(nodes.DCIEmpty.text, "No reference builds for this lineup.");
});

test("HUD marks what to buy now, hides owned items and uses lane counters until minute 10", () => {
  const fs = require("node:fs");
  const path = require("node:path");
  const vm = require("node:vm");
  const nodes = {};
  const modes = new Set(["gScoreboardOpen"]);
  const scheduled = [];
  let observed = {
    heroes: lineup, allies: ["hero_inferno"], self: "hero_inferno", purchases: [], mine: [],
    lane: ["hero_haze", "hero_wraith"], clock: 200, souls: 3500, owned: [],
  };
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
    $: { GetContextPanel: () => root, Schedule: (delay, callback) => scheduled.push(callback), Msg: (line) => assert.fail(line) },
    DCIEngine: {create, formatImpact, formatStats},
    DCI_CATALOG: catalog,
    DCI_COUNTERS: counters,
    DCI_STATS: fixture,
    DCIObserver: {observe: () => observed},
  }, {filename});
  const step = () => scheduled.shift()();
  const names = () => [0, 1, 2, 3].map((i) => nodes[`DCIName${i}`].text);

  step(); // minute 3:20, 3,500 souls
  assert.equal(nodes.DCISource.text, "Lane phase");
  assert.deepEqual(names(), ["Metal Skin", "Plated Armor", "Unstoppable", "Reactive Barrier"]);
  assert.equal(nodes.DCIRow3.DCILanePick, true);
  assert.equal(nodes.DCIReason3.text, "Lane phase · Barrier when CC'd");
  assert.equal(nodes.DCIBuy0.visible, true, "Metal Skin costs 3,200");
  assert.equal(nodes.DCIRow0.DCIBuyNow, true);
  assert.equal(nodes.DCIRow1.DCIExpensive, true);
  assert.equal(nodes.DCIBuy1.visible, false);

  observed = {...observed, owned: ["upgrade_metal_skin"]};
  step();
  assert.deepEqual(names(), ["Plated Armor", "Unstoppable", "Dispel Magic", "Reactive Barrier"]);
  assert.equal(nodes.DCIBuy2.visible, true, "Dispel Magic is the first affordable suggestion");
  assert.equal(nodes.DCIBuy0.visible, false);

  observed = {...observed, clock: 700, owned: []};
  step(); // laning is over: the fourth row returns to match data; owned items stay hidden this match
  assert.equal(nodes.DCISource.text, "Enemy lineup");
  assert.equal(nodes.DCIName3.text, "Glass Cannon");
  assert.equal(nodes.DCIRow3.DCIDataPick, true);
  assert.equal(nodes.DCIRow3.DCILanePick, false);
  assert.equal(nodes.DCIName0.text, "Plated Armor", "Metal Skin was bought earlier in this match");

  observed = {...observed, souls: 7000};
  step();
  assert.equal(nodes.DCIBuy0.visible, true, "more souls move the badge to the top pick");
  assert.equal(nodes.DCIRow0.DCIExpensive, false);
});
