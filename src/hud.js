"use strict";

(function () {
  var context = $.GetContextPanel();
  var engine = DCIEngine.create(DCI_CATALOG, DCI_COUNTERS, typeof DCI_STATS === "undefined" ? null : DCI_STATS,
    typeof DCI_PATCHES === "undefined" ? null : DCI_PATCHES);
  var ROWS = 4;
  var text = {
    roster: "Enemy lineup",
    lane: "Lane phase",
    waiting: "Waiting for enemy lineup.",
    noMatch: "No reference builds for this lineup.",
    error: "Counter item data unavailable.",
    alertTitle: "Enemy counter purchase",
    more: " more",
  };
  var ALERT_SECONDS = 8;
  var LANE_SECONDS = 600;
  var view = null;
  var previous = "";
  var reportedError = false;
  var match = "";
  var alerted = Object.create(null);
  var alertRemaining = 0;
  var owned = Object.create(null); // items the local player bought this match
  var inputs = "";
  var rows = [];
  var badgeNotes = DCIEngine.formatNotes([]); // tooltip of the PATCHED badge
  var rowNotes = [];                          // tooltip of each row's ! icon

  function setShown(panel, shown) {
    panel.SetHasClass("DCIHidden", !shown);
  }

  function findView() {
    var root = context.FindChildTraverse("DCIVisibility");
    if (!root) return null;
    var rows = [];
    for (var i = 0; i < ROWS; i++) {
      var row = root.FindChildTraverse("DCIRow" + i);
      rows.push({
        panel: row,
        name: row.FindChildTraverse("DCIName" + i),
        icon: row.FindChildTraverse("DCIIcon" + i),
        price: row.FindChildTraverse("DCIPrice" + i),
        reason: row.FindChildTraverse("DCIReason" + i),
        evidence: row.FindChildTraverse("DCIEvidence" + i),
        impact: row.FindChildTraverse("DCIImpact" + i),
        buy: row.FindChildTraverse("DCIBuy" + i),
        patch: row.FindChildTraverse("DCIPatch" + i),
      });
    }
    var found = {
      root: root, rows: rows, source: root.FindChildTraverse("DCISource"),
      empty: root.FindChildTraverse("DCIEmpty"),
      alert: root.FindChildTraverse("DCIAlert"),
      alertTitle: root.FindChildTraverse("DCIAlertTitle"),
      alertText: root.FindChildTraverse("DCIAlertText"),
      badge: root.FindChildTraverse("DCIPatchBadge"),
      tip: root.FindChildTraverse("DCITip"),
      tipTitle: root.FindChildTraverse("DCITipTitle"),
      tipText: root.FindChildTraverse("DCITipText"),
    };
    // Hovering a patch icon shows what changed in a tooltip at the top of the panel.
    hover(found, found.badge, function () { return badgeNotes; });
    rows.forEach(function (row, index) {
      hover(found, row.patch, function () { return rowNotes[index]; });
    });
    return found;
  }

  // A GlobalClassListener puts the global classes it lists on its parent panel, not on itself, so
  // the shop and scoreboard modes are read from the parent (and from the listener, in case).
  function mode(name) {
    var parent = view.root.GetParent && view.root.GetParent();
    return view.root.BHasClass(name) || !!(parent && parent.IsValid() && parent.BHasClass(name));
  }

  function hover(found, panel, notes) {
    if (!panel || !panel.SetPanelEvent) return;
    panel.SetPanelEvent("onmouseover", function () {
      var content = notes();
      if (!content || !content.text) return;
      found.tipTitle.text = content.title;
      found.tipText.text = content.text;
      setShown(found.tip, true);
    });
    panel.SetPanelEvent("onmouseout", function () { setShown(found.tip, false); });
  }

  function showEmpty(message) {
    setShown(view.source, false);
    setShown(view.badge, false);
    setShown(view.tip, false);
    view.empty.text = message;
    setShown(view.empty, true);
    view.rows.forEach(function (row) { setShown(row.panel, false); });
  }

  function display(recommendations, laning, buy, souls) {
    setShown(view.empty, false);
    setShown(view.badge, badgeNotes.text !== "");
    view.source.text = laning ? text.lane : text.roster;
    setShown(view.source, true);

    view.rows.forEach(function (row, index) {
      var item = recommendations[index];
      setShown(row.panel, !!item);
      if (!item) return;
      var known = typeof souls === "number";
      row.panel.SetHasClass("DCIBuyNow", index === buy);
      row.panel.SetHasClass("DCIExpensive", known && item.cost > souls);
      setShown(row.buy, index === buy);
      ["weapon", "vitality", "spirit"].forEach(function (category) {
        row.panel.SetHasClass("DCI" + category.charAt(0).toUpperCase() + category.slice(1),
          item.category === category);
      });
      row.panel.SetHasClass("DCIShared", item.score > 1);
      row.panel.SetHasClass("DCIDataPick", item.source === "data");
      row.panel.SetHasClass("DCILanePick", item.source === "lane");
      row.name.text = item.name;
      if (row.icon && row.icon.SetImage && item.icon) row.icon.SetImage(item.icon);
      if (row.icon) row.panel.SetHasClass("DCIHasIcon", !!item.icon);
      row.price.text = String(item.cost).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
      row.reason.text = item.reason;
      row.evidence.text = item.evidence;
      row.impact.text = DCIEngine.formatStats(item);
      // The badge covers changed enemy heroes; a row's icon means the item itself changed.
      rowNotes[index] = DCIEngine.formatNotes(engine.patchNotes([item.key]));
      setShown(row.patch, rowNotes[index].text !== "");
      setShown(row.impact, row.impact.text !== "");
      row.impact.SetHasClass("DCIImpactUp", item.impact >= 10);
      row.impact.SetHasClass("DCIImpactDown", item.impact <= -10);
    });
  }

  function setAlert(active) {
    view.alert.SetHasClass("DCIAlertActive", active);
  }

  function notify(threats) {
    var fresh = threats.filter(function (threat) { return !alerted[threat.key]; });
    if (!fresh.length) return;
    fresh.forEach(function (threat) { alerted[threat.key] = true; });
    view.alertTitle.text = text.alertTitle;
    view.alertText.text = fresh[0].text + (fresh.length > 1 ? " (+" + (fresh.length - 1) + text.more + ")" : "");
    view.alert.SetHasClass("DCIAlertYou", fresh[0].targetsYou);
    setAlert(true);
    alertRemaining = ALERT_SECONDS;
  }

  function refresh() {
    if (!view || !view.root.IsValid()) {
      view = findView();
      previous = "";
    }
    if (!view) return 1;

    var shopOpen = mode("gShopOpen");
    if (!shopOpen && !mode("gScoreboardOpen")) {
      setShown(view.tip, false);
      if (previous) showEmpty(text.waiting);
      previous = "";
      return 1;
    }

    var observed = DCIObserver.observe(context, engine, function (token, panel) {
      return $.Localize(token, panel);
    }, shopOpen);
    if (observed.heroes.length) {
      var lineup = observed.heroes.slice().sort().join(",");
      if (lineup !== match) {
        match = lineup;
        alerted = Object.create(null);
        owned = Object.create(null);
      }
      notify(engine.threats(observed.purchases, observed.allies || [], observed.self));
      (observed.owned || []).concat(observed.mine || []).forEach(function (item) { owned[item] = true; });
    }

    // Suggestions change only with the lineup, the lane phase and owned items; souls only move the
    // "buy now" badge, so the ranking is not recomputed on every tick.
    var lane = observed.lane || [];
    var laning = lane.length > 0 && typeof observed.clock === "number" && observed.clock < LANE_SECONDS;
    var exclude = Object.keys(owned).sort();
    var key = observed.heroes.join(",") + "|" + (laning ? lane.join(",") : "") + "|" + exclude.join(",");
    if (key !== inputs) {
      inputs = key;
      var patched = engine.patchedHeroes(observed.heroes);
      badgeNotes = DCIEngine.formatNotes(engine.patchNotes(patched));
      if (view.badge) view.badge.text = patched.length > 1 ? patched.length + " PATCHED" : "PATCHED";
      rows = engine.recommend(observed.heroes, ROWS - 1, exclude);
      var shown = rows.map(function (item) { return item.key; }).concat(exclude);
      var extra = laning ? engine.lanePick(lane, shown) : null;
      if (!extra) extra = engine.dataPick(observed.heroes, shown);
      if (extra) rows.push(extra);
    }
    var souls = typeof observed.souls === "number" ? observed.souls : null;
    var buy = engine.buyNow(rows, souls);
    var affordable = rows.map(function (item) { return souls !== null && item.cost <= souls ? 1 : 0; }).join("");
    var emptyMessage = observed.heroes.length ? text.noMatch : text.waiting;
    var signature = rows.length ? inputs + "|" + laning + "|" + buy + "|" + affordable : emptyMessage;
    if (signature !== previous) {
      if (rows.length) display(rows, laning, buy, souls);
      else showEmpty(emptyMessage);
      previous = signature;
    }
    return 0.5;
  }

  function tick(elapsed) {
    if (!context || !context.IsValid()) return;
    var delay = 1;
    try {
      if (alertRemaining > 0 && view && view.root.IsValid()) {
        alertRemaining -= elapsed;
        if (alertRemaining <= 0) setAlert(false);
      }
      delay = refresh();
    } catch (error) {
      if (!reportedError) $.Msg("[DCI] HUD update failed: " + error);
      reportedError = true;
      previous = "";
      if (view && view.root.IsValid()) showEmpty(text.error);
    } finally {
      $.Schedule(delay, function () { tick(delay); });
    }
  }

  $.Msg("[DCI] Counter items HUD loaded");
  $.Schedule(0, function () { tick(0); });
})();
