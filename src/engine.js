"use strict";

var DCIEngine = (function () {
  // A match-data pick must raise the estimated win rate against the lineup by this much (hundredths of a point).
  var DATA_MIN_IMPACT = 50;

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

  function create(catalog, counters, stats, patches) {
    var heroes = index(catalog.heroes);
    var items = index(catalog.items);
    var tags = counters.tags || {};
    var table = Object.create(null);
    Object.keys(counters.heroes).forEach(function (hero) {
      if (!heroes.keys[hero]) return;
      table[hero] = counters.heroes[hero].filter(function (item) { return items.keys[item]; });
    });

    var columns = Object.create(null);
    var measured = Object.create(null);
    var minutes = (stats && stats.minutes) || {};
    // Changes from patches the counter table has not been reviewed for, per hero and per item.
    var changed = Object.create(null);
    ((patches && patches.patches) || []).forEach(function (patch) {
      ["heroes", "items"].forEach(function (group) {
        Object.keys(patch[group] || {}).forEach(function (key) {
          var row = group === "heroes" ? heroes.keys[key] : items.keys[key];
          if (!row) return;
          (changed[key] = changed[key] || []).push({
            name: row[2], patch: patch.title, date: patch.date,
            lines: patch[group][key].lines || [], more: patch[group][key].more || 0,
          });
        });
      });
    });
    var lane = Object.create(null);
    if (stats && stats.lane) {
      Object.keys(stats.lane).forEach(function (hero) {
        if (heroes.keys[hero]) {
          lane[hero] = stats.lane[hero].filter(function (pick) { return items.keys[pick[0]]; });
        }
      });
    }
    if (stats) {
      stats.items.forEach(function (item, position) {
        if (items.keys[item]) columns[item] = position;
      });
      Object.keys(stats.heroes).forEach(function (hero) {
        if (heroes.keys[hero]) measured[hero] = stats.heroes[hero];
      });
    }

    function unique(list) {
      return list.filter(function (hero, position) {
        return heroes.keys[hero] && list.indexOf(hero) === position;
      });
    }

    function names(keys) {
      return keys.map(function (key) { return heroes.keys[key][2]; }).join(" · ");
    }

    function lift(item, hero) {
      var row = measured[hero];
      var value = row && item in columns ? row[columns[item]] : null;
      return typeof value === "number" ? value : null;
    }

    // Estimated change in the buyer's win rate against the whole lineup, or null without data.
    function impact(item, enemies) {
      var total = null;
      enemies.forEach(function (hero) {
        var value = lift(item, hero);
        if (value !== null) total = (total || 0) + value;
      });
      return total;
    }

    function entry(key, enemies, extra) {
      var row = items.keys[key];
      var result = {
        key: key,
        name: row[2],
        cost: row[4],
        category: row[5],
        tag: tags[key] || "",
        impact: impact(key, enemies),
        minute: typeof minutes[key] === "number" ? minutes[key] : null,
      };
      Object.keys(extra).forEach(function (field) { result[field] = extra[field]; });
      return result;
    }

    // Rank the reference counter items by how many enemy heroes they answer; match data breaks ties.
    // Items in exclude (already owned) are skipped so the list always shows something new.
    function recommend(enemyHeroes, limit, exclude) {
      var enemies = unique(enemyHeroes);
      var picks = Object.create(null);
      enemies.forEach(function (hero) {
        (table[hero] || []).forEach(function (item, position) {
          if (exclude && exclude.indexOf(item) !== -1) return;
          var pick = picks[item] || (picks[item] = { heroes: [], rank: 0 });
          pick.heroes.push(hero);
          pick.rank += position;
        });
      });

      return Object.keys(picks).map(function (key) {
        var pick = picks[key];
        var count = pick.heroes.length;
        var tag = tags[key];
        return entry(key, enemies, {
          source: "reference",
          score: count,
          rank: pick.rank,
          heroes: pick.heroes,
          reason: (tag ? tag + " · " : "") + (count === 1 ? "counters 1 enemy" : "counters " + count + " enemies"),
          evidence: names(pick.heroes),
        });
      }).sort(function (a, b) {
        return b.score - a.score || (b.impact || 0) - (a.impact || 0) || a.rank - b.rank ||
          a.cost - b.cost || (a.key < b.key ? -1 : 1);
      }).slice(0, limit || 3);
    }

    // The item with the largest measured win-rate gain against the lineup that is not already shown.
    function dataPick(enemyHeroes, exclude) {
      var enemies = unique(enemyHeroes);
      var best = null;
      Object.keys(columns).forEach(function (key) {
        if (exclude && exclude.indexOf(key) !== -1) return;
        var total = impact(key, enemies);
        if (total === null || total < DATA_MIN_IMPACT) return;
        if (best && (total < best.total || (total === best.total && key > best.key))) return;
        best = { key: key, total: total };
      });
      if (!best) return null;
      var strongest = enemies.filter(function (hero) { return lift(best.key, hero) > 0; })
        .sort(function (a, b) { return lift(best.key, b) - lift(best.key, a); }).slice(0, 3);
      var tag = tags[best.key];
      return entry(best.key, enemies, {
        source: "data",
        score: 0,
        heroes: strongest,
        reason: "Match data" + (tag ? " · " + tag : ""),
        evidence: "Strongest vs " + names(strongest),
      });
    }

    // The best early item against the enemies in the local player's lane (first ten minutes).
    function lanePick(laneHeroes, exclude) {
      var enemies = unique(laneHeroes);
      var totals = Object.create(null);
      enemies.forEach(function (hero) {
        (lane[hero] || []).forEach(function (pick) {
          if (exclude && exclude.indexOf(pick[0]) !== -1) return;
          totals[pick[0]] = (totals[pick[0]] || 0) + pick[1];
        });
      });
      var best = Object.keys(totals).sort(function (a, b) {
        return totals[b] - totals[a] || items.keys[a][4] - items.keys[b][4] || (a < b ? -1 : 1);
      })[0];
      if (!best) return null;
      var against = enemies.filter(function (hero) {
        return (lane[hero] || []).some(function (pick) { return pick[0] === best; });
      });
      var tag = tags[best];
      var result = entry(best, enemies, {
        source: "lane",
        score: 0,
        heroes: against,
        reason: "Lane phase" + (tag ? " · " + tag : ""),
        evidence: "Early vs " + names(against),
      });
      result.impact = totals[best];
      result.minute = null; // the usual buy minute is for the whole match, not for the lane
      return result;
    }

    // Patch changes for these heroes and items, heroes first, each key once.
    function patchNotes(keys) {
      var notes = [];
      keys.filter(function (key, position) { return keys.indexOf(key) === position; }).forEach(function (key) {
        (changed[key] || []).forEach(function (note) { notes.push(note); });
      });
      return notes;
    }

    function patchedHeroes(enemyHeroes) {
      return unique(enemyHeroes).filter(function (hero) { return !!changed[hero]; });
    }

    // The first suggestion the player can pay for now, or -1.
    function buyNow(rows, souls) {
      if (typeof souls !== "number" || !(souls >= 0)) return -1;
      for (var i = 0; i < rows.length; i++) {
        if (rows[i].cost <= souls) return i;
      }
      return -1;
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
      impact: function (item, enemies) { return impact(item, unique(enemies)); },
      recommend: recommend,
      dataPick: dataPick,
      lanePick: lanePick,
      buyNow: buyNow,
      patchNotes: patchNotes,
      patchedHeroes: patchedHeroes,
      threats: threats,
    };
  }

  function validate(catalog, counters, stats) {
    var items = index(catalog.items).keys;
    var heroes = index(catalog.heroes).keys;
    var missing = [];
    function check(key, known) {
      if (!known[key] && missing.indexOf(key) === -1) missing.push(key);
    }
    Object.keys(counters.heroes).forEach(function (hero) {
      check(hero, heroes);
      counters.heroes[hero].forEach(function (item) { check(item, items); });
    });
    Object.keys(counters.tags || {}).forEach(function (item) { check(item, items); });
    if (stats) {
      stats.items.forEach(function (item) { check(item, items); });
      Object.keys(stats.lane || {}).forEach(function (hero) {
        check(hero, heroes);
        stats.lane[hero].forEach(function (pick) { check(pick[0], items); });
      });
      Object.keys(stats.heroes).forEach(function (hero) {
        check(hero, heroes);
        if (stats.heroes[hero].length !== stats.items.length) missing.push(hero + " (row length)");
      });
    }
    return missing;
  }

  function formatImpact(value) {
    if (value === null || value === undefined) return "";
    if (Math.round(Math.abs(value) / 10) === 0) return "±0 WR";
    return (value > 0 ? "+" : "−") + (Math.abs(value) / 100).toFixed(1) + " WR";
  }

  // "+0.6 WR · ~14m": the measured change and the typical buy minute, whichever are known.
  function formatStats(item) {
    var parts = [];
    var impact = formatImpact(item.impact);
    if (impact) parts.push(impact);
    if (typeof item.minute === "number") parts.push("~" + item.minute + "m");
    return parts.join(" · ");
  }

  // Tooltip text for patch changes: a title naming the patch, then up to three changes per hero
  // or item and how many more the notes list.
  function formatNotes(notes) {
    if (!notes.length) return { title: "", text: "" };
    var patches = [];
    notes.forEach(function (note) {
      if (patches.indexOf(note.patch) === -1) patches.push(note.patch);
    });
    var title = (patches.length === 1 ? "Changed in " + patches[0] : "Changed in " + patches.length + " patches") +
      " · counters not reviewed for it yet";
    var lines = [];
    notes.forEach(function (note) {
      note.lines.forEach(function (line, index) {
        lines.push((index === 0 ? note.name + ": " : "    ") + line);
      });
      if (note.more) lines.push("    +" + note.more + (note.more === 1 ? " more change" : " more changes"));
    });
    return { title: title, text: lines.join("\n") };
  }

  return { create: create, validate: validate, formatImpact: formatImpact, formatStats: formatStats,
    formatNotes: formatNotes };
})();

if (typeof module !== "undefined") module.exports = DCIEngine;
