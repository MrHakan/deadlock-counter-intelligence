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

  function enemyHeroes(top, engine, localize) {
    if (!shown(top)) return [];
    var enemyTeam = top.FindChildTraverse("TeamEnemy");
    var friendlyTeam = top.FindChildTraverse("TeamFriendly");
    if (!valid(enemyTeam) || !valid(friendlyTeam)) return [];

    function cards(team) {
      return team.FindChildrenWithClassTraverse("LocalPlayer").some(function (panel) {
        return panel.paneltype === "CitadelHudTopBarPlayer" && shown(panel);
      });
    }
    if (!cards(friendlyTeam) || cards(enemyTeam)) return [];

    var result = [];
    var pending = [{ panel: enemyTeam, depth: 0 }];
    for (var i = 0; i < pending.length && i < 96; i++) {
      var current = pending[i];
      if (current.panel.paneltype === "CitadelHudTopBarPlayer") {
        if (!shown(current.panel)) continue;
        var id = resolve(localize, "{i:hero_id}", current.panel);
        var name = resolve(localize, "{s:hero_name}", current.panel);
        var hero = engine.heroKey(id) || engine.heroKey(name);
        if (hero && result.indexOf(hero) === -1) result.push(hero);
      } else if (current.depth < 6) {
        current.panel.Children().forEach(function (child) {
          pending.push({ panel: child, depth: current.depth + 1 });
        });
      }
    }
    return result;
  }

  function observe(root, engine, localize, shopOpen) {
    var heroes = enemyHeroes(root.FindChildTraverse("TopBar"), engine, localize);
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
    return { heroes: heroes, purchases: purchases };
  }

  return { observe: observe };
})();

if (typeof module !== "undefined") module.exports = DCIObserver;
