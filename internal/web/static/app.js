// Progressive enhancement for the dashboard sync controls. CSP-safe: loaded from
// /static/app.js (same-origin), no inline script. With JS off, the server-rendered
// run status (R3) stays correct on manual refresh.
(function () {
  "use strict";

  function syncButtons() {
    return Array.prototype.slice.call(document.querySelectorAll("[data-sync-btn]"));
  }

  function setButtonsDisabled(disabled) {
    syncButtons().forEach(function (b) {
      b.disabled = disabled;
    });
  }

  function init() {
    var forms = document.querySelectorAll("[data-sync-form]");
    if (!forms.length) {
      return;
    }

    forms.forEach(function (form) {
      form.addEventListener("submit", function () {
        var btn = form.querySelector("[data-sync-btn]");
        if (btn) {
          btn.disabled = true;
          btn.textContent = "Starting…";
        }
      });
    });

    var statusEl = document.getElementById("run-status");
    var wasRunning = false;

    function poll() {
      fetch("/actions/status", { headers: { Accept: "application/json" } })
        .then(function (r) {
          return r.ok ? r.json() : null;
        })
        .then(function (s) {
          if (!s) {
            return;
          }
          var running = !!s.running;
          setButtonsDisabled(running);
          if (statusEl) {
            statusEl.textContent = running ? "Running…" : "Idle";
          }
          if (wasRunning && !running) {
            // a run just finished: reload to show the fresh server-rendered status
            window.location.reload();
            return;
          }
          wasRunning = running;
        })
        .catch(function () {
          /* ignore transient polling errors */
        });
    }

    poll();
    setInterval(poll, 2000);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
