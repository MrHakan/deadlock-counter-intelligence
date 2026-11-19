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

  // Souls as the game shows them: "12,345", "12.345" or "12.3k", possibly inside markup.
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
    var result = { heroes: [], self: null, lane: [], card: null, localPlayers: 0 };
    var pending = [{ panel: team, depth: 0 }];
    for (var i = 0; i < pending.length && i < 96; i++) {
      var current = pending[i];
      if (current.panel.paneltype === "CitadelHudTopBarPlayer") {
        if (!shown(current.panel)) continue;
        var local = current.panel.BHasClass("LocalPlayer");
        if (local) {
          result.localPlayers++;
          result.card = current.panel;
        }
        var id = resolve(localize, "{i:hero_id}", current.panel);
        var name = resolve(localize, "{s:hero_name}", current.panel);
        var hero = engine.heroKey(id) || engine.heroKey(name);
        if (!hero) {
          var labels = current.panel.FindChildrenWithClassTraverse("HeroName");
          if (valid(labels[0])) hero = engine.heroKey(labels[0].text);
        }
        if (!hero) continue;
        if (result.heroes.indexOf(hero) === -1) result.heroes.push(hero);
        if (local) result.self = hero;
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

    // Resolve sides from the local card even when the native team panels are swapped.
    var first = readTeam(friendlyTeam, engine, localize);
    var second = readTeam(enemyTeam, engine, localize);
    if (first.localPlayers + second.localPlayers !== 1) return none;
    var friends = first.localPlayers ? first : second;
    var enemies = first.localPlayers ? second : first;
    if (!enemies.heroes.length) return none;
    var clockLabel = top.FindChildTraverse("GameTime");
    var clock = parseClock(valid(clockLabel) ? clockLabel.text : "");
    if (clock === null) clock = parseClock(resolve(localize, "{s:game_clock}", top));
    return {
      heroes: enemies.heroes,
      allies: friends.heroes,
      self: friends.self,
      lane: enemies.lane,
      souls: null,
      owned: friends.card ? ownedItems(friends.card, engine) : [],
      clock: clock,
    };
  }

  function spendableSouls(container) {
    if (!valid(container)) return null;
    var amount = container.FindChildTraverse("CurrentGoldAmount");
    var label = valid(amount) && amount.FindChildTraverse("hudCurGoldLabel");
    // The top bar shows total net worth. Both the HUD and shop have a separate spendable balance.
    return shown(label) ? parseSouls(label.text) : null;
  }

  function observe(root, engine, localize, shopOpen) {
    var teams = lineup(root.FindChildTraverse("TopBar"), engine, localize);
    var heroes = teams.heroes;
    var purchases = [];
    var mine = [];
    if (heroes.length) {
      var shop = shopOpen && root.FindChildTraverse("CitadelHudHeroShop");
      if (valid(shop)) teams.souls = spendableSouls(shop.FindChildTraverse("GoldAPContainer"));
      if (teams.souls === null) teams.souls = spendableSouls(root.FindChildTraverse("gold_and_ap_container"));
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
