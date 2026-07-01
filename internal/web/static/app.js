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

  function setStatusBadge(running, current) {
    var el = document.getElementById("run-status");
    if (!el) return;
    var span = document.createElement("span");
    span.className = "badge " + (running ? "badge-active" : "badge-muted");
    span.textContent = running ? "● Running" : "Idle";
    el.textContent = "";
    el.appendChild(span);
  }

  function renderProgress(p) {
    var box = document.getElementById("sync-progress");
    if (!box) return;
    if (!p || !p.total) {
      box.hidden = true;
      return;
    }
    box.hidden = false;
    var bar = document.getElementById("progress-bar");
    if (bar) {
      bar.max = p.total;
      bar.value = p.done;
    }
    var text = document.getElementById("progress-text");
    if (text) {
      var parts = ["Processing " + p.done + "/" + p.total];
      if (p.requested) parts.push("requested " + p.requested);
      if (p.notFound) parts.push("not found " + p.notFound);
      var line = parts.join(" · ");
      if (p.current) line += " — " + p.current;
      text.textContent = line;
    }
  }

  // ── Book detail drawer ──────────────────────────────────────────────
  // Clicking a book (grid card / list row) slides in a detail panel fetched
  // from /book/<source>/<id>?drawer=1. No-JS fallback: the same link renders a
  // full page. CSP-safe (external script, no inline handlers).
  var drawer;
  function ensureDrawer() {
    if (drawer) return drawer;
    drawer = document.createElement("div");
    drawer.className = "bb-drawer";
    drawer.innerHTML =
      '<div class="bb-backdrop" data-drawer-close></div>' +
      '<aside class="bb-panel"><button class="bb-close" data-drawer-close aria-label="Close">✕</button>' +
      '<div class="bb-panel-body"></div></aside>';
    document.body.appendChild(drawer);
    return drawer;
  }
  function openDrawer(href) {
    var d = ensureDrawer();
    var body = d.querySelector(".bb-panel-body");
    body.innerHTML = '<p class="muted drawer-msg">Loading…</p>';
    d.classList.add("open");
    document.body.classList.add("bb-noscroll");
    fetch(href + (href.indexOf("?") < 0 ? "?" : "&") + "drawer=1", { headers: { "X-Requested-With": "fetch" } })
      .then(function (r) { return r.ok ? r.text() : null; })
      .then(function (html) { if (html !== null) body.innerHTML = html; })
      .catch(function () { body.innerHTML = '<p class="muted drawer-msg">Couldn’t load this book.</p>'; });
  }
  function closeDrawer() {
    if (!drawer) return;
    drawer.classList.remove("open");
    document.body.classList.remove("bb-noscroll");
  }
  document.addEventListener("click", function (e) {
    var link = e.target.closest ? e.target.closest("[data-book]") : null;
    if (link) { e.preventDefault(); openDrawer(link.getAttribute("href")); return; }
    if (e.target.closest && e.target.closest("[data-drawer-close]")) { closeDrawer(); }
  });
  document.addEventListener("keydown", function (e) { if (e.key === "Escape") closeDrawer(); });

  function init() {
    var forms = document.querySelectorAll("[data-sync-form]");

    forms.forEach(function (form) {
      form.addEventListener("submit", function () {
        var btn = form.querySelector("[data-sync-btn]");
        if (btn) {
          btn.disabled = true;
          btn.textContent = "Starting…";
        }
      });
    });

    var stopForm = document.querySelector("[data-stop-form]");
    if (stopForm) {
      stopForm.addEventListener("submit", function () {
        var b = stopForm.querySelector("[data-stop-btn]");
        if (b) {
          b.disabled = true;
          b.textContent = "Stopping…";
        }
      });
    }

    // Only the dashboard has the status/progress hooks; bail elsewhere.
    if (!document.getElementById("run-status")) return;

    var wasRunning = false;

    function poll() {
      fetch("/actions/status", { headers: { Accept: "application/json" } })
        .then(function (r) {
          return r.ok ? r.json() : null;
        })
        .then(function (s) {
          if (!s) return;
          var running = !!s.running;
          setButtonsDisabled(running);
          setStatusBadge(running, "");
          renderProgress(running ? s.progress : null);
          var stopForm = document.querySelector("[data-stop-form]");
          if (stopForm) {
            stopForm.hidden = !running;
          }
          if (wasRunning && !running) {
            window.location.reload(); // run finished: show fresh server-rendered state
            return;
          }
          wasRunning = running;
        })
        .catch(function () {
          /* ignore transient polling errors */
        });
    }

    poll();
    setInterval(poll, 1500);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
