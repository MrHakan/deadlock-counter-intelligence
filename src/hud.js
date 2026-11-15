"use strict";

(function () {
  var context = $.GetContextPanel();
  var languageIsTurkish = DCI_LANGUAGE === "turkish";
  var engine = DCIEngine.create(DCI_CATALOG, DCI_LANGUAGE);
  var listener = null;
  var previous = "";
  var reportedError = false;

  function refresh() {
    if (!listener || !listener.IsValid()) {
      listener = context.FindChildTraverse("DCIVisibility");
      previous = "";
    }
    if (!listener) return 1;

    var shopOpen = listener.BHasClass("gShopOpen");
    if (!shopOpen && !listener.BHasClass("gScoreboardOpen")) {
      previous = "";
      return 1;
    }

    var observed = DCIObserver.observe(context, engine, $.Localize, shopOpen);
    var recommendations = engine.recommend(observed.heroes, observed.purchases);
    var signature = JSON.stringify([observed.heroes.length, observed.purchases.length, recommendations]);
    if (signature === previous) return 0.5;

    listener.FindChildTraverse("DCIStatus").text = observed.purchases.length
      ? (languageIsTurkish ? "Görünen son alımlar" : "Visible recent purchases")
      : observed.heroes.length
        ? (languageIsTurkish ? "Kahramanlara göre · Item gözlemi yok" : "Hero-based · No item observations")
        : (languageIsTurkish ? "Rakip bilgisi bekleniyor" : "Waiting for enemy information");

    for (var i = 0; i < 3; i++) {
      var row = listener.FindChildTraverse("DCIRow" + i);
      var recommendation = recommendations[i];
      row.visible = !!recommendation;
      if (recommendation) {
        row.FindChildTraverse("DCIName" + i).text = recommendation.name + "  ·  " + recommendation.cost;
        row.FindChildTraverse("DCIEvidence" + i).text = recommendation.evidence;
        row.FindChildTraverse("DCIReason" + i).text = recommendation.reason;
      }
    }
    previous = signature;
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
      if (listener && listener.IsValid()) {
        for (var i = 0; i < 3; i++) listener.FindChildTraverse("DCIRow" + i).visible = false;
        listener.FindChildTraverse("DCIStatus").text =
          languageIsTurkish ? "HUD verisi okunamadı" : "HUD data unavailable";
      }
    } finally {
      $.Schedule(delay, tick);
    }
  }

  $.Schedule(0, tick);
})();
