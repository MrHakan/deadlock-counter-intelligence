"use strict";

var DCIEngine = (function () {
  var rules = [
    {
      counter: "upgrade_healbane",
      category: "vitality",
      heroes: ["hero_atlas", "hero_ghost", "hero_kelvin"],
      items: ["upgrade_healing_booster", "upgrade_health_nova", "upgrade_healbuff", "upgrade_health_stealing_magic"],
      reason: "Spirit damage reduces incoming healing.",
    },
    {
      counter: "upgrade_metal_skin",
      category: "vitality",
      heroes: ["hero_haze", "hero_wraith"],
      items: ["upgrade_ricochet", "upgrade_glass_cannon", "upgrade_intensifying_clip", "upgrade_burst_fire"],
      reason: "Briefly blocks bullet damage on use.",
    },
    {
      counter: "upgrade_spirit_bubble",
      category: "vitality",
      heroes: ["hero_bebop", "hero_gigawatt"],
      items: ["upgrade_magic_burst", "upgrade_boundless_spirit", "upgrade_spirit_burn"],
      reason: "Adds a shield against spirit damage.",
    },
    {
      counter: "upgrade_target_stun",
      category: "spirit",
      heroes: ["hero_hornet", "hero_orion"],
      items: [],
      reason: "Stuns airborne targets for longer.",
    },
  ];

  function normalize(text) {
    return typeof text === "string" ? text.trim().toLowerCase().replace(/[’‘]/g, "'") : "";
  }

  function index(rows) {
    var keys = Object.create(null);
    var aliases = Object.create(null);
    rows.forEach(function (row) {
      keys[row[1]] = row;
      [row[0].toString(), row[1], row[2], row[3]].map(normalize).filter(Boolean)
        .forEach(function (alias) {
          aliases[alias] = alias in aliases && aliases[alias] !== row[1] ? null : row[1];
        });
    });
    return {
      lookup: function (value) { return aliases[normalize(value)] || null; },
      keys: keys,
    };
  }

  function create(catalog) {
    var heroes = index(catalog.heroes);
    var items = index(catalog.items);

    function recommend(enemyHeroes, purchases) {
      var enemies = Object.create(null);
      enemyHeroes.forEach(function (hero) {
        if (heroes.keys[hero]) enemies[hero] = true;
      });

      return rules.map(function (rule) {
        var counter = items.keys[rule.counter];
        if (!counter) return null;

        var threats = rule.heroes.filter(function (hero) { return enemies[hero]; });
        var signals = Object.create(null);
        purchases.forEach(function (purchase) {
          if (enemies[purchase.hero] && items.keys[purchase.item] && rule.items.indexOf(purchase.item) !== -1) {
            signals[purchase.hero + "/" + purchase.item] = purchase;
          }
        });
        var distinct = Object.keys(signals).sort().map(function (key) { return signals[key]; });
        var score = threats.length + 3 * distinct.length;
        if (!score) return null;

        return {
          key: rule.counter,
          name: counter[2],
          cost: counter[4],
          category: rule.category,
          purchaseBased: distinct.length > 0,
          score: score,
          reason: rule.reason,
          evidence: distinct.length
            ? heroes.keys[distinct[0].hero][2] + " · " + items.keys[distinct[0].item][2]
            : heroes.keys[threats[0]][2],
        };
      }).filter(Boolean).sort(function (a, b) {
        return b.score - a.score || a.cost - b.cost || (a.key < b.key ? -1 : 1);
      }).slice(0, 3);
    }

    return { heroKey: heroes.lookup, itemKey: items.lookup, recommend: recommend };
  }

  function validateRules(catalog) {
    var items = index(catalog.items).keys;
    var heroes = index(catalog.heroes).keys;
    return rules.reduce(function (missing, rule) {
      return missing.concat([rule.counter].concat(rule.items).filter(function (key) {
        return !items[key];
      }), rule.heroes.filter(function (key) {
        return !heroes[key];
      }));
    }, []);
  }

  return { create: create, validateRules: validateRules };
})();

if (typeof module !== "undefined") module.exports = DCIEngine;
