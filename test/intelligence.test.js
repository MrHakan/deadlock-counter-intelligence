"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");
const catalog = require("../src/catalog.json");
const { create, validateRules } = require("../src/engine");
const { observe } = require("../src/observer");

test("rules resolve against the pinned bilingual shop inventory", () => {
  assert.deepEqual(validateRules(catalog), []);
  const tr = create(catalog, "turkish");
  assert.equal(tr.itemKey("Şifakıran"), "upgrade_healbane");
  assert.equal(tr.itemKey("2603935618"), "upgrade_healbane");
  assert.equal(tr.heroKey("Vindicta"), "hero_hornet");
});

test("visible purchase changes the order and removing it restores the hero baseline", () => {
  const engine = create(catalog, "english");
  const rivals = ["hero_ghost", "hero_atlas", "hero_haze", "hero_wraith", "hero_bebop"];
  const shot = { hero: "hero_haze", item: "upgrade_glass_cannon" };
  const baseline = engine.recommend(rivals, []);
  assert.equal(baseline[0].name, "Healbane");
  const updated = engine.recommend(rivals, [shot, shot]);
  assert.equal(updated[0].name, "Metal Skin");
  assert.equal(updated[0].score, 5);
  assert.equal(updated[0].evidence, "Haze · Glass Cannon");
  assert.deepEqual(engine.recommend(rivals, []), baseline);
});

test("unknown items and purchases from outside the enemy roster contribute nothing", () => {
  const engine = create(catalog, "english");
  const rivals = ["hero_haze"];
  assert.deepEqual(engine.recommend(rivals, [
    { hero: "hero_wraith", item: "upgrade_glass_cannon" },
    { hero: "hero_haze", item: "missing" },
  ]), engine.recommend(rivals, []));
  assert.deepEqual(engine.recommend([], [{ hero: "hero_haze", item: "upgrade_glass_cannon" }]), []);
});

test("ambiguous labels are skipped and missing counters cannot be offered", () => {
  const fixture = structuredClone(catalog);
  const [a, b] = fixture.items;
  b[3] = a[2];
  assert.equal(create(fixture).itemKey(a[2]), null);
  fixture.items = fixture.items.filter((row) => row[1] !== "upgrade_healbane");
  assert.deepEqual(create(fixture).recommend(["hero_ghost"], []), []);
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
      panel({ id: "TeamFriendly", children: [player("7", realLocal ? ["LocalPlayer"] : [])] }),
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

const engine = create(catalog);
const localize = (token, panel) => panel.tokens[token] || token;

test("observer reads only visible enemy purchases and only with the shop open", () => {
  const hud = buildHud();
  assert.deepEqual(observe(hud, engine, localize, true), {
    heroes: ["hero_haze"], purchases: [{ hero: "hero_haze", item: "upgrade_glass_cannon" }],
  });
  assert.deepEqual(observe(hud, engine, localize, false), { heroes: ["hero_haze"], purchases: [] });
  hud.FindChildTraverse("CitadelHudHeroShop").visible = false;
  assert.deepEqual(observe(hud, engine, localize, true), { heroes: ["hero_haze"], purchases: [] });
});

test("observer requires a visible local player and visible enemy cards", () => {
  assert.deepEqual(observe(buildHud({ realLocal: false }), engine, localize, true), { heroes: [], purchases: [] });
  assert.deepEqual(observe(buildHud({ enemyVisible: false }), engine, localize, true), { heroes: [], purchases: [] });
});
