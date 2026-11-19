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

  function readTeam(team, engine, localize) {
    var result = { heroes: [], self: null };
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
        if (current.panel.BHasClass("LocalPlayer")) result.self = hero;
      } else if (current.depth < 6) {
        current.panel.Children().forEach(function (child) {
          pending.push({ panel: child, depth: current.depth + 1 });
        });
      }
    }
    return result;
  }

  function lineup(top, engine, localize) {
    var none = { heroes: [], allies: [], self: null };
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

    var enemies = readTeam(enemyTeam, engine, localize).heroes;
    if (!enemies.length) return none;
    var friends = readTeam(friendlyTeam, engine, localize);
    return { heroes: enemies, allies: friends.heroes, self: friends.self };
  }

  function observe(root, engine, localize, shopOpen) {
    var teams = lineup(root.FindChildTraverse("TopBar"), engine, localize);
    var heroes = teams.heroes;
    var purchases = [];
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
        });
      }
    }
    return { heroes: heroes, allies: teams.allies, self: teams.self, purchases: purchases };
  }

  return { observe: observe };
})();

if (typeof module !== "undefined") module.exports = DCIObserver;
