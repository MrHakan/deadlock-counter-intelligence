"use strict";

var DCIEngine = (function () {
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

  function names(keys, rows) {
    return keys.map(function (key) { return rows[key][2]; }).join(" · ");
  }

  function create(catalog, counters) {
    var heroes = index(catalog.heroes);
    var items = index(catalog.items);
    var table = Object.create(null);
    Object.keys(counters.heroes).forEach(function (hero) {
      if (!heroes.keys[hero]) return;
      table[hero] = counters.heroes[hero].filter(function (item) { return items.keys[item]; });
    });

    function unique(list) {
      return list.filter(function (hero, position) {
        return heroes.keys[hero] && list.indexOf(hero) === position;
      });
    }

    // Rank the reference counter items by how many enemy heroes they answer.
    function recommend(enemyHeroes, limit) {
      var picks = Object.create(null);
      unique(enemyHeroes).forEach(function (hero) {
        (table[hero] || []).forEach(function (item, position) {
          var pick = picks[item] || (picks[item] = { heroes: [], rank: 0 });
          pick.heroes.push(hero);
          pick.rank += position;
        });
      });

      return Object.keys(picks).map(function (key) {
        var row = items.keys[key];
        var pick = picks[key];
        return {
          key: key,
          name: row[2],
          cost: row[4],
          category: row[5],
          score: pick.heroes.length,
          rank: pick.rank,
          heroes: pick.heroes,
          reason: pick.heroes.length === 1 ? "Counters 1 enemy" : "Counters " + pick.heroes.length + " enemies",
          evidence: names(pick.heroes, heroes.keys),
        };
      }).sort(function (a, b) {
        return b.score - a.score || a.rank - b.rank || a.cost - b.cost || (a.key < b.key ? -1 : 1);
      }).slice(0, limit || 3);
    }

    // Enemy purchases that appear in the reference counter list of an allied hero.
    function threats(purchases, allyHeroes, self) {
      var allies = unique(allyHeroes);
      var seen = Object.create(null);
      var result = [];
      purchases.forEach(function (purchase) {
        var buyer = heroes.keys[purchase.hero];
        var item = items.keys[purchase.item];
        var key = purchase.hero + "/" + purchase.item;
        if (!buyer || !item || seen[key] || allies.indexOf(purchase.hero) !== -1) return;
        seen[key] = true;
        var targets = allies.filter(function (hero) {
          return (table[hero] || []).indexOf(purchase.item) !== -1;
        });
        if (!targets.length) return;
        var targetsYou = targets.indexOf(self) !== -1;
        if (targetsYou) targets = [self].concat(targets.filter(function (hero) { return hero !== self; }));
        var labels = targets.map(function (hero) { return heroes.keys[hero][2]; });
        if (targetsYou) labels[0] = "you (" + labels[0] + ")";
        result.push({
          key: key,
          buyer: buyer[2],
          item: item[2],
          targets: targets,
          targetsYou: targetsYou,
          text: buyer[2] + " bought " + item[2] + " — counters " + labels.join(", "),
        });
      });
      return result.sort(function (a, b) { return b.targetsYou - a.targetsYou; });
    }

    return {
      heroKey: heroes.lookup,
      itemKey: items.lookup,
      counters: function (hero) { return (table[hero] || []).slice(); },
      recommend: recommend,
      threats: threats,
    };
  }

  function validate(catalog, counters) {
    var items = index(catalog.items).keys;
    var heroes = index(catalog.heroes).keys;
    var missing = [];
    Object.keys(counters.heroes).forEach(function (hero) {
      if (!heroes[hero]) missing.push(hero);
      counters.heroes[hero].forEach(function (item) {
        if (!items[item] && missing.indexOf(item) === -1) missing.push(item);
      });
    });
    return missing;
  }

  return { create: create, validate: validate };
})();

if (typeof module !== "undefined") module.exports = DCIEngine;
