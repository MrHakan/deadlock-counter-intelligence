"use strict";

var DCIObserver = (function () {
  function valid(panel) {
    return panel && (!panel.IsValid || panel.IsValid());
  }

  function shown(panel) {
    if (!valid(panel) || typeof panel.actuallayoutwidth !== "number" || !(panel.actuallayoutwidth > 0) ||
        typeof panel.actuallayoutheight !== "number" || !(panel.actuallayoutheight > 0)) return false;
    for (var node = panel; valid(node); node = node.GetParent()) {
      if (node.visible !== true) return false;
    }
    return true;
  }

  function resolve(localize, token, panel) {
    try {
      var value = localize(token, panel);
      return typeof value === "string" && value.indexOf("{") === -1 ? value : "";
    } catch (error) {
      return "";
    }
  }

  // Souls as the top bar shows them: "12,345", "12.345" or "12.3k", possibly inside markup.
  function parseSouls(text) {
    if (typeof text !== "string") return null;
    var plain = text.replace(/<[^>]*>/g, "").replace(/\s/g, "");
    if (/^[0-9]+([.,][0-9]+)?k$/i.test(plain)) return Math.round(parseFloat(plain.slice(0, -1).replace(",", ".")) * 1000);
    return /^[0-9][0-9.,]*$/.test(plain) ? parseInt(plain.replace(/[.,]/g, ""), 10) : null;
  }

  // Match time in seconds from "12:34", "1:02:03" or "-0:30" (before the match starts).
  function parseClock(text) {
    var match = /^(-)?(?:([0-9]+):)?([0-9]{1,2}):([0-9]{2})$/.exec(typeof text === "string" ? text.trim() : "");
    if (!match) return null;
    var seconds = (parseInt(match[2] || "0", 10) * 60 + parseInt(match[3], 10)) * 60 + parseInt(match[4], 10);
    return match[1] ? -seconds : seconds;
  }

  // Item names exposed by the icons in a player's purchased-items panel. The icons are created
  // by the game at run time, so several common property names are tried; anything that is not
  // a known item is ignored.
  var ITEM_PROPERTIES = ["abilityname", "itemname", "ability_name", "item_name"];

  function ownedItems(card, engine) {
    var mods = card && card.FindChildTraverse("mod_universal");
    if (!valid(mods)) return [];
    var found = [];
    var pending = [mods];
    for (var i = 0; i < pending.length && i < 160; i++) {
      var panel = pending[i];
      for (var p = 0; p < ITEM_PROPERTIES.length; p++) {
        var name = "";
        try {
          name = panel[ITEM_PROPERTIES[p]];
          if (typeof name !== "string" && panel.GetAttributeString) name = panel.GetAttributeString(ITEM_PROPERTIES[p], "");
        } catch (error) {
          name = "";
        }
        var key = typeof name === "string" && /^upgrade_[a-z0-9_]+$/.test(name) ? engine.itemKey(name) : null;
        if (key && found.indexOf(key) === -1) found.push(key);
      }
      if (panel.Children) {
        panel.Children().forEach(function (child) { pending.push(child); });
      }
    }
    return found;
  }

  function readTeam(team, engine, localize) {
    var result = { heroes: [], self: null, lane: [], card: null };
    var pending = [{ panel: team, depth: 0 }];
    for (var i = 0; i < pending.length && i < 96; i++) {
      var current = pending[i];
      if (current.panel.paneltype === "CitadelHudTopBarPlayer") {
        if (!shown(current.panel)) continue;
        var id = resolve(localize, "{i:hero_id}", current.panel);
        var name = resolve(localize, "{s:hero_name}", current.panel);
        var hero = engine.heroKey(id) || engine.heroKey(name);
        if (!hero) continue;
        if (result.heroes.indexOf(hero) === -1) result.heroes.push(hero);
        if (current.panel.BHasClass("LocalPlayer")) {
          result.self = hero;
          result.card = current.panel;
        }
        // The game marks the players who share the local player's lane.
        if (current.panel.BHasClass("IsInLocalPlayerLane") && result.lane.indexOf(hero) === -1) result.lane.push(hero);
      } else if (current.depth < 6) {
        current.panel.Children().forEach(function (child) {
          pending.push({ panel: child, depth: current.depth + 1 });
        });
      }
    }
    return result;
  }

  function lineup(top, engine, localize) {
    var none = { heroes: [], allies: [], self: null, lane: [], souls: null, owned: [], clock: null };
    if (!shown(top)) return none;
    var enemyTeam = top.FindChildTraverse("TeamEnemy");
    var friendlyTeam = top.FindChildTraverse("TeamFriendly");
    if (!valid(enemyTeam) || !valid(friendlyTeam)) return none;

    function cards(team) {
      return team.FindChildrenWithClassTraverse("LocalPlayer").some(function (panel) {
        return panel.paneltype === "CitadelHudTopBarPlayer" && shown(panel);
      });
    }
    if (!cards(friendlyTeam) || cards(enemyTeam)) return none;

    var enemyInfo = readTeam(enemyTeam, engine, localize);
    if (!enemyInfo.heroes.length) return none;
    var friends = readTeam(friendlyTeam, engine, localize);
    var clockLabel = top.FindChildTraverse("GameTime");
    var clock = parseClock(valid(clockLabel) ? clockLabel.text : "");
    if (clock === null) clock = parseClock(resolve(localize, "{s:game_clock}", top));
    var souls = null;
    if (friends.card) {
      souls = parseSouls(resolve(localize, "{s:gold}", friends.card));
      var soulsLabel = souls === null && friends.card.FindChildTraverse("SoulsValue");
      if (valid(soulsLabel)) souls = parseSouls(soulsLabel.text);
    }
    return {
      heroes: enemyInfo.heroes,
      allies: friends.heroes,
      self: friends.self,
      lane: enemyInfo.lane,
      souls: souls,
      owned: friends.card ? ownedItems(friends.card, engine) : [],
      clock: clock,
    };
  }

  function observe(root, engine, localize, shopOpen) {
    var teams = lineup(root.FindChildTraverse("TopBar"), engine, localize);
    var heroes = teams.heroes;
    var purchases = [];
    var mine = [];
    if (shopOpen && heroes.length) {
      var shop = root.FindChildTraverse("CitadelHudHeroShop");
      var recent = valid(shop) && shop.FindChildTraverse("RecentPurchasesContainer");
      if (shown(recent)) {
        recent.Children().slice(0, 24).forEach(function (row) {
          if (!row.BHasClass("recentPurchase") || !shown(row)) return;

          var icon = row.FindChildTraverse("RecentPurchaseHeroImage");
          var label = row.Children().filter(function (child) {
            return child.BHasClass("recentModPurchaseName");
          })[0];
          if (!shown(icon) || !shown(label)) return;

          var hero = engine.heroKey(resolve(localize, "{s:recent_hero_name}", row));
          var item = engine.itemKey(label.text);
          if (hero && item && heroes.indexOf(hero) !== -1) purchases.push({ hero: hero, item: item });
          else if (hero && item && hero === teams.self && mine.indexOf(item) === -1) mine.push(item);
        });
      }
    }
    teams.purchases = purchases;
    teams.mine = mine;
    return teams;
  }

  return { observe: observe, parseSouls: parseSouls, parseClock: parseClock };
})();

if (typeof module !== "undefined") module.exports = DCIObserver;
