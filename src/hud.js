"use strict";

(function () {
  var context = $.GetContextPanel();
  var engine = DCIEngine.create(DCI_CATALOG, DCI_LANGUAGE);
  var turkish = DCI_LANGUAGE === "turkish";
  var text = turkish ? {
    roster: "Kadro",
    shop: "Son alımlar",
    waiting: "Rakip kadrosu bekleniyor.",
    noMatch: "Bu kadro için öneri yok.",
    error: "Karşı item verisi okunamadı.",
  } : {
    roster: "Lineup",
    shop: "Recent purchases",
    waiting: "Waiting for enemy lineup.",
    noMatch: "No suggestions for this lineup.",
    error: "Counter item data unavailable.",
  };
  var view = null;
  var previous = "";
  var reportedError = false;

  function findView() {
    var root = context.FindChildTraverse("DCIVisibility");
    if (!root) return null;
    var rows = [];
    for (var i = 0; i < 3; i++) {
      var row = root.FindChildTraverse("DCIRow" + i);
      rows.push({
        panel: row,
        name: row.FindChildTraverse("DCIName" + i),
        price: row.FindChildTraverse("DCIPrice" + i),
        reason: row.FindChildTraverse("DCIReason" + i),
        evidence: row.FindChildTraverse("DCIEvidence" + i),
      });
    }
    return { root: root, rows: rows, source: root.FindChildTraverse("DCISource"),
      empty: root.FindChildTraverse("DCIEmpty") };
  }

  function showEmpty(message) {
    view.source.visible = false;
    view.empty.text = message;
    view.empty.visible = true;
    view.rows.forEach(function (row) { row.panel.visible = false; });
  }

  function display(recommendations) {
    view.empty.visible = false;
    var fromShop = recommendations.some(function (item) { return item.purchaseBased; });
    view.source.text = fromShop ? text.shop : text.roster;
    view.source.visible = true;

    view.rows.forEach(function (row, index) {
      var item = recommendations[index];
      row.panel.visible = !!item;
      if (!item) return;
      row.panel.SetHasClass("DCIVitality", item.category === "vitality");
      row.panel.SetHasClass("DCISpirit", item.category === "spirit");
      row.panel.SetHasClass("DCIPurchase", item.purchaseBased);
      row.name.text = item.name;
      row.price.text = String(item.cost).replace(/\B(?=(\d{3})+(?!\d))/g, turkish ? "." : ",");
      row.reason.text = item.reason;
      row.evidence.text = item.evidence;
    });
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
    var recommendations = engine.recommend(observed.heroes, observed.purchases);
    var emptyMessage = observed.heroes.length ? text.noMatch : text.waiting;
    var signature = recommendations.length ? JSON.stringify(recommendations) : emptyMessage;
    if (signature !== previous) {
      if (recommendations.length) display(recommendations);
      else showEmpty(emptyMessage);
      previous = signature;
    }
    return 0.5;
  }

  function tick() {
    if (!context || !context.IsValid()) return;
    var delay = 1;
    try {
      delay = refresh();
    } catch (error) {
      if (!reportedError) $.Msg("[DCI] HUD update failed: " + error);
      reportedError = true;
      previous = "";
      if (view && view.root.IsValid()) showEmpty(text.error);
    } finally {
      $.Schedule(delay, tick);
    }
  }

  $.Schedule(0, tick);
})();
