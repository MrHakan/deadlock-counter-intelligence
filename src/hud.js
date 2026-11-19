"use strict";

(function () {
  var context = $.GetContextPanel();
  var engine = DCIEngine.create(DCI_CATALOG, DCI_COUNTERS, typeof DCI_STATS === "undefined" ? null : DCI_STATS);
  var ROWS = 4;
  var text = {
    roster: "Enemy lineup",
    waiting: "Waiting for enemy lineup.",
    noMatch: "No reference builds for this lineup.",
    error: "Counter item data unavailable.",
    alertTitle: "Enemy counter purchase",
    more: " more",
  };
  var ALERT_SECONDS = 8;
  var view = null;
  var previous = "";
  var reportedError = false;
  var match = "";
  var alerted = Object.create(null);
  var alertRemaining = 0;

  function findView() {
    var root = context.FindChildTraverse("DCIVisibility");
    if (!root) return null;
    var rows = [];
    for (var i = 0; i < ROWS; i++) {
      var row = root.FindChildTraverse("DCIRow" + i);
      rows.push({
        panel: row,
        name: row.FindChildTraverse("DCIName" + i),
        price: row.FindChildTraverse("DCIPrice" + i),
        reason: row.FindChildTraverse("DCIReason" + i),
        evidence: row.FindChildTraverse("DCIEvidence" + i),
        impact: row.FindChildTraverse("DCIImpact" + i),
      });
    }
    return {
      root: root, rows: rows, source: root.FindChildTraverse("DCISource"),
      empty: root.FindChildTraverse("DCIEmpty"),
      alert: root.FindChildTraverse("DCIAlert"),
      alertTitle: root.FindChildTraverse("DCIAlertTitle"),
      alertText: root.FindChildTraverse("DCIAlertText"),
    };
  }

  function showEmpty(message) {
    view.source.visible = false;
    view.empty.text = message;
    view.empty.visible = true;
    view.rows.forEach(function (row) { row.panel.visible = false; });
  }

  function display(recommendations) {
    view.empty.visible = false;
    view.source.text = text.roster;
    view.source.visible = true;

    view.rows.forEach(function (row, index) {
      var item = recommendations[index];
      row.panel.visible = !!item;
      if (!item) return;
      ["weapon", "vitality", "spirit"].forEach(function (category) {
        row.panel.SetHasClass("DCI" + category.charAt(0).toUpperCase() + category.slice(1),
          item.category === category);
      });
      row.panel.SetHasClass("DCIShared", item.score > 1);
      row.panel.SetHasClass("DCIDataPick", item.source === "data");
      row.name.text = item.name;
      row.price.text = String(item.cost).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
      row.reason.text = item.reason;
      row.evidence.text = item.evidence;
      row.impact.text = DCIEngine.formatImpact(item.impact);
      row.impact.visible = row.impact.text !== "";
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

    var shopOpen = view.root.BHasClass("gShopOpen");
    if (!shopOpen && !view.root.BHasClass("gScoreboardOpen")) {
      if (previous) showEmpty(text.waiting);
      previous = "";
      return 1;
    }

    var observed = DCIObserver.observe(context, engine, $.Localize, shopOpen);
    if (observed.heroes.length) {
      var lineup = observed.heroes.slice().sort().join(",");
      if (lineup !== match) {
        match = lineup;
        alerted = Object.create(null);
      }
      notify(engine.threats(observed.purchases, observed.allies || [], observed.self));
    }

    var recommendations = engine.recommend(observed.heroes, ROWS - 1);
    var extra = engine.dataPick(observed.heroes, recommendations.map(function (item) { return item.key; }));
    if (extra) recommendations.push(extra);
    var emptyMessage = observed.heroes.length ? text.noMatch : text.waiting;
    var signature = recommendations.length ? JSON.stringify(recommendations) : emptyMessage;
    if (signature !== previous) {
      if (recommendations.length) display(recommendations);
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

  $.Schedule(0, function () { tick(0); });
})();
