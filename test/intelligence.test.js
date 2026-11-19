"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");
const catalog = require("../src/catalog.json");
const counters = require("../src/counters.json");
const stats = require("../src/stats.json");
const { create, validate, formatImpact, formatStats, formatNotes } = require("../src/engine");
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
    SetHasClass: (name, active) => {
      if (active && !classes.includes(name)) classes.push(name);
      if (!active && classes.includes(name)) classes.splice(classes.indexOf(name), 1);
    },
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

function buildHud({realLocal = true, enemyVisible = true, swapped = false, ambiguous = false} = {}) {
  const player = (heroId, classes, visible = true, extra = {}) => panel({
    type: "CitadelHudTopBarPlayer", classes, visible, tokens: {"{i:hero_id}": heroId, ...extra.tokens},
    children: extra.children || [],
  });
  // The local player's card: souls in {s:gold} and purchased item icons under mod_universal.
  const local = {
    tokens: {"{s:gold}": "99k"},
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
  const balance = (id, text) => panel({ id, children: [
    panel({ children: [panel({ id: "hudCurGoldLabel", text: "9,999" })] }),
    panel({ id: "CurrentGoldAmount", children: [panel({ id: "hudCurGoldLabel", text })] }),
  ] });
  const friend = panel({ id: swapped ? "TeamEnemy" : "TeamFriendly", children: [
    player("7", realLocal ? ["LocalPlayer"] : [], true, local), player("1", []),
  ] });
  const enemy = panel({ id: swapped ? "TeamFriendly" : "TeamEnemy", children: [
    player("13", ambiguous ? ["IsInLocalPlayerLane", "LocalPlayer"] : ["IsInLocalPlayerLane"], enemyVisible, {
      children: [panel({ classes: ["HeroName"], type: "Label", text: "Haze" })],
    }),
  ] });
  return panel({ children: [
    panel({ id: "TopBar", children: [
      panel({ id: "GameTime", text: "4:12" }),
      friend, enemy,
    ] }),
    balance("gold_and_ap_container", "3,100"),
    panel({ id: "CitadelHudHeroShop", children: [
      balance("GoldAPContainer", "3,400"),
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
    lane: ["hero_haze"], souls: 3400, owned: ["upgrade_healbane"], clock: 252, enemyItems: [],
  };
  assert.deepEqual(observe(hud, engine, localize, true), {
    ...teams, purchases: [{ hero: "hero_haze", item: "upgrade_glass_cannon" }], mine: ["upgrade_ricochet"],
  });
  assert.deepEqual(observe(hud, engine, localize, false), { ...teams, souls: 3100, purchases: [], mine: [] });
  hud.FindChildTraverse("CitadelHudHeroShop").visible = false;
  assert.deepEqual(observe(hud, engine, localize, true), { ...teams, souls: 3100, purchases: [], mine: [] });
});

test("observer reads the items enemies own from their top-bar cards", () => {
  const hud = buildHud();
  const enemy = hud.FindChildTraverse("TeamEnemy").Children()[0];
  enemy.Children().push(panel({ id: "mod_universal", children: [
    panel({ props: {abilityname: "upgrade_spirit_sap"} }),
    panel({ props: {abilityname: "citadel_ability_unknown"} }),
  ] }));
  assert.deepEqual(observe(hud, engine, localize, false).enemyItems, [{ hero: "hero_haze", item: "upgrade_spirit_sap" }]);
});

test("observer requires a visible local player and visible enemy cards", () => {
  const none = { heroes: [], allies: [], self: null, lane: [], souls: null, owned: [], clock: null, enemyItems: [],
    purchases: [], mine: [] };
  assert.deepEqual(observe(buildHud({ realLocal: false }), engine, localize, true), none);
  assert.deepEqual(observe(buildHud({ enemyVisible: false }), engine, localize, true), none);
  assert.deepEqual(observe(buildHud({ ambiguous: true }), engine, localize, true), none);
});

test("observer resolves the local side and reads rendered names when hero tokens are unresolved", () => {
  const hud = buildHud({ swapped: true });
  const enemy = hud.FindChildTraverse("TeamFriendly").Children()[0];
  enemy.tokens["{i:hero_id}"] = "{i:hero_id}";
  const observed = observe(hud, engine, localize, false);
  assert.deepEqual(observed.heroes, ["hero_haze"]);
  assert.deepEqual(observed.lane, ["hero_haze"]);
  assert.equal(observed.self, "hero_wraith");
  assert.deepEqual(observed.allies, ["hero_wraith", "hero_inferno"]);
  hud.FindChildTraverse("gold_and_ap_container").visible = false;
  assert.equal(observe(hud, engine, localize, false).souls, null, "total hero net worth is never a cash balance");
});

test("the bundled scripts produce item rows from stock-shaped cards in scoreboard and shop", () => {
  const fs = require("node:fs");
  const path = require("node:path");
  const vm = require("node:vm");
  const root = buildHud({ swapped: true });
  root.FindChildTraverse("TeamFriendly").Children()[0].tokens["{i:hero_id}"] = "{i:hero_id}";
  const controls = ["DCISource", "DCIEmpty", "DCIAlert", "DCIAlertTitle", "DCIAlertText",
    "DCIPatchBadge", "DCITip", "DCITipTitle", "DCITipText"].map((id) => panel({ id }));
  const rows = Array.from({ length: 4 }, (_, i) => panel({ id: "DCIRow" + i, classes: ["DCIHidden"],
    children: ["Name", "Price", "Reason", "Evidence", "Impact", "Buy", "Patch", "Category"].map((name) =>
      panel({ id: "DCI" + name + i, classes: name === "Buy" || name === "Patch" ? ["DCIHidden"] : [],
        props: { style: {} } })),
  }));
  const view = panel({ id: "DCIVisibility", children: controls.concat(rows) });
  const hud = panel({ id: "Hud", classes: ["gScoreboardOpen"], children: [view] });
  view.parent = hud;
  root.Children().push(view);
  const scheduled = [];
  const bundle = ["engine.js", "observer.js", "hud.js"]
    .map((name) => fs.readFileSync(path.resolve(__dirname, "../src", name), "utf8")).join("\n");
  vm.runInNewContext(bundle, {
    $: {
      GetContextPanel: () => root, Schedule: (_, callback) => scheduled.push(callback),
      Localize(token, node) {
        assert.equal(typeof this.Schedule, "function");
        return localize(token, node);
      },
      Msg: (line) => { if (!line.endsWith("HUD loaded")) assert.fail(line); },
    },
    DCI_CATALOG: catalog, DCI_COUNTERS: counters, DCI_STATS: fixture,
  });
  const node = (id) => view.FindChildTraverse(id);
  scheduled.shift()();
  assert.equal(node("DCIEmpty").BHasClass("DCIHidden"), true);
  assert.equal(node("DCIRow0").BHasClass("DCIHidden"), false);
  assert.equal(node("DCIName0").text, "Metal Skin");
  assert.equal(node("DCICategory0").style.backgroundImage, 'url("s2r://panorama/images/items/vitality/metal_skin_psd.vtex")',
    "the row shows the game's own item icon");
  assert.equal(node("DCIRow0").BHasClass("DCIHasIcon"), true);
  assert.equal(node("DCIBuy0").BHasClass("DCIHidden"), true, "scoreboard balance is below 3,200");

  hud.SetHasClass("gScoreboardOpen", false);
  hud.SetHasClass("gShopOpen", true);
  scheduled.shift()();
  assert.equal(node("DCIName0").text, "Metal Skin");
  assert.equal(node("DCIBuy0").BHasClass("DCIHidden"), false, "shop balance covers the first row");

  root.FindChildTraverse("TeamFriendly").visible = false;
  scheduled.shift()();
  assert.equal(node("DCIEmpty").text, "Waiting for enemy lineup.");
  assert.equal(node("DCIEmpty").BHasClass("DCIHidden"), false);
  assert.equal(node("DCIRow0").BHasClass("DCIHidden"), true);
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
  const dispatched = [];
  const root = {
    IsValid: () => true,
    BHasClass: () => false, // the game puts the listener's classes on its parent
    GetParent: () => ({ IsValid: () => true, BHasClass: (name) => modes.has(name) }),
    FindChildTraverse: (id) => id === "DCIVisibility" ? root : (nodes[id] ||= {
      FindChildTraverse: root.FindChildTraverse,
      SetHasClass(name, active) { this[name] = active; },
      SetPanelEvent(event, handler) { (this.events ||= {})[event] = handler; },
    }),
  };
  const filename = path.resolve(__dirname, "../src/hud.js");
  vm.runInNewContext(fs.readFileSync(filename, "utf8"), {
    $: {
      GetContextPanel: () => root,
      Schedule: (delay, callback) => scheduled.push({delay, callback}),
      Msg: (line) => { if (!line.endsWith("HUD loaded") && !line.startsWith("[DCI] Quick buy: queued")) assert.fail(line); },
      DispatchEvent: (...args) => dispatched.push(args),
    },
    DCIEngine: {create, formatImpact, formatStats, formatNotes},
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
  assert.equal(nodes.DCIEvidence0.text, "vs", "a portrait replaces the hero's name");
  assert.equal(nodes.DCIFaces0.DCIHidden, false);
  assert.equal(nodes.DCIFace0_0.DCIHidden, false);
  assert.equal(nodes.DCIFace0_1.DCIHidden, true, "one hero, one portrait");
  assert.equal(nodes.DCIRow0.DCIHasFaces, true);

  // The queue button hands the item to the game's quick-buy queue.
  nodes.DCIQueue0.events.onactivate();
  assert.deepEqual(dispatched, [["CitadelQuickbuyAddItem", catalog.items.find((row) => row[2] === "Knockdown")[0]]]);
  assert.equal(nodes.DCIImpact0.DCIHidden, true, "no measurement for Knockdown");
  assert.equal(nodes.DCIName3.text, "Glass Cannon");
  assert.equal(nodes.DCIRow3.DCIDataPick, true);
  assert.equal(nodes.DCIRow3.DCIWeapon, true);
  assert.equal(nodes.DCIReason3.text, "Match data");
  assert.equal(nodes.DCIImpact3.text, "+0.6 WR · ~22m");
  assert.equal(nodes.DCIImpact3.DCIImpactUp, true);
  assert.equal(nodes.DCIEmpty.DCIHidden, true);
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
  assert.equal(nodes.DCIImpact2.DCIHidden, true);
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

  // On the scoreboard, items enemies already own count too, and those against you stay listed.
  observed = {heroes: ["hero_hornet", "hero_haze"], allies, self: "hero_inferno", purchases: [],
    enemyItems: [{hero: "hero_haze", item: "upgrade_healbane"}, {hero: "hero_haze", item: "upgrade_metal_skin"}]};
  step();
  assert.equal(nodes.DCIThreats.text, "Countering you: Healbane (Haze)");
  assert.equal(nodes.DCIThreats.DCIHidden, false);
  assert.equal(nodes.DCIAlertText.text, "Haze bought Healbane — counters you (Infernus), Lady Geist");

  observed = {heroes: ["hero_unknown"], allies, self: "hero_inferno", purchases: []};
  step();
  assert.equal(nodes.DCIRow0.DCIHidden, true);
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
    BHasClass: () => false, // the game puts the listener's classes on its parent
    GetParent: () => ({ IsValid: () => true, BHasClass: (name) => modes.has(name) }),
    FindChildTraverse: (id) => id === "DCIVisibility" ? root : (nodes[id] ||= {
      FindChildTraverse: root.FindChildTraverse,
      SetHasClass(name, active) { this[name] = active; },
      SetPanelEvent(event, handler) { (this.events ||= {})[event] = handler; },
    }),
  };
  const filename = path.resolve(__dirname, "../src/hud.js");
  vm.runInNewContext(fs.readFileSync(filename, "utf8"), {
    $: { GetContextPanel: () => root, Schedule: (delay, callback) => scheduled.push(callback), Msg: (line) => { if (!line.endsWith("HUD loaded")) assert.fail(line); } },
    DCIEngine: {create, formatImpact, formatStats, formatNotes},
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
  assert.equal(nodes.DCIBuy0.DCIHidden, false, "Metal Skin costs 3,200");
  assert.equal(nodes.DCIRow0.DCIBuyNow, true);
  assert.equal(nodes.DCIRow1.DCIExpensive, true);
  assert.equal(nodes.DCIBuy1.DCIHidden, true);

  observed = {...observed, owned: ["upgrade_metal_skin"]};
  step();
  assert.deepEqual(names(), ["Plated Armor", "Unstoppable", "Dispel Magic", "Reactive Barrier"]);
  assert.equal(nodes.DCIBuy2.DCIHidden, false, "Dispel Magic is the first affordable suggestion");
  assert.equal(nodes.DCIBuy0.DCIHidden, true);

  observed = {...observed, clock: 700, owned: []};
  step(); // laning is over: the fourth row returns to match data; owned items stay hidden this match
  assert.equal(nodes.DCISource.text, "Enemy lineup");
  assert.equal(nodes.DCIName3.text, "Glass Cannon");
  assert.equal(nodes.DCIRow3.DCIDataPick, true);
  assert.equal(nodes.DCIRow3.DCILanePick, false);
  assert.equal(nodes.DCIName0.text, "Plated Armor", "Metal Skin was bought earlier in this match");

  observed = {...observed, souls: 7000};
  step();
  assert.equal(nodes.DCIBuy0.DCIHidden, false, "more souls move the badge to the top pick");
  assert.equal(nodes.DCIRow0.DCIExpensive, false);
});

test("HUD flags patched enemies and items and shows what changed on hover", () => {
  const fs = require("node:fs");
  const path = require("node:path");
  const vm = require("node:vm");
  const patches = {reviewed: "2026-10-30", patches: [{
    title: "Minor Update - 11-04-2026", date: "2026-11-05",
    heroes: {hero_haze: {lines: ["Sleep Dagger - Cooldown increased from 25s to 30s"], more: 2}},
    items: {upgrade_metal_skin: {lines: ["Duration reduced from 3.5s to 3s"], more: 0}},
  }, {
    title: "an update without patch notes", build: 6864,
    heroes: {hero_inferno: {lines: ["Afterburn - Damage 10 → 12"], more: 0}}, items: {},
  }]};
  const nodes = {};
  const modes = new Set(["gShopOpen"]);
  const scheduled = [];
  let observed = {heroes: lineup, allies: [], self: null, purchases: [], mine: [], lane: [], clock: null, souls: null, owned: []};
  const root = {
    IsValid: () => true,
    BHasClass: () => false, // the game puts the listener's classes on its parent
    GetParent: () => ({ IsValid: () => true, BHasClass: (name) => modes.has(name) }),
    FindChildTraverse: (id) => id === "DCIVisibility" ? root : (nodes[id] ||= {
      FindChildTraverse: root.FindChildTraverse,
      SetHasClass(name, active) { this[name] = active; },
      SetPanelEvent(event, handler) { (this.events ||= {})[event] = handler; },
    }),
  };
  const filename = path.resolve(__dirname, "../src/hud.js");
  vm.runInNewContext(fs.readFileSync(filename, "utf8"), {
    $: { GetContextPanel: () => root, Schedule: (delay, callback) => scheduled.push(callback), Msg: (line) => { if (!line.endsWith("HUD loaded")) assert.fail(line); } },
    DCIEngine: {create, formatImpact, formatStats, formatNotes},
    DCI_CATALOG: catalog, DCI_COUNTERS: counters, DCI_STATS: fixture, DCI_PATCHES: patches,
    DCIObserver: {observe: () => observed},
  }, {filename});
  scheduled.shift()();

  assert.equal(nodes.DCIPatchBadge.DCIHidden, false, "Haze was changed after the table was reviewed");
  assert.equal(nodes.DCIPatchBadge.text, "PATCHED");
  assert.equal(nodes.DCITip.DCIHidden, undefined, "no tooltip until hovered");
  nodes.DCIPatchBadge.events.onmouseover();
  assert.equal(nodes.DCITip.DCIHidden, false);
  assert.equal(nodes.DCITipTitle.text, "Changed in Minor Update - 11-04-2026 · counters not reviewed for it yet");
  assert.equal(nodes.DCITipText.text, "Haze: Sleep Dagger - Cooldown increased from 25s to 30s\n    +2 more changes");
  nodes.DCIPatchBadge.events.onmouseout();
  assert.equal(nodes.DCITip.DCIHidden, true);

  // A row's icon means the item itself changed: Metal Skin did; Plated Armor, which also counters
  // the changed Haze, did not, and the badge already covers Haze.
  assert.equal(nodes.DCIName0.text, "Metal Skin");
  assert.equal(nodes.DCIPatch0.DCIHidden, false);
  nodes.DCIPatch0.events.onmouseover();
  assert.equal(nodes.DCITipText.text, "Metal Skin: Duration reduced from 3.5s to 3s");
  nodes.DCIPatch0.events.onmouseout();
  assert.equal(nodes.DCIName1.text, "Plated Armor");
  assert.equal(nodes.DCIPatch1.DCIHidden, true);

  observed = {...observed, heroes: ["hero_hornet"]};
  scheduled.shift()();
  assert.equal(nodes.DCIPatchBadge.DCIHidden, true);

  // Changes the game data shows without patch notes are flagged the same way.
  observed = {...observed, heroes: ["hero_inferno"]};
  scheduled.shift()();
  assert.equal(nodes.DCIPatchBadge.DCIHidden, false);
  nodes.DCIPatchBadge.events.onmouseover();
  assert.equal(nodes.DCITipTitle.text, "Changed in an update without patch notes · counters not reviewed for it yet");
  assert.equal(nodes.DCITipText.text, "Infernus: Afterburn - Damage 10 → 12");
});
