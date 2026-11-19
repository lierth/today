// The small amount of behaviour HTMX does not cover: drag ordering, inline
// title editing, the undo toast, the quick-add tag strip and the keyboard
// shortcuts. Everything here degrades: with JS off the ↑↓ buttons and the
// forms still work.
(function () {
  "use strict";

  var base = document.querySelector('meta[name="base"]');
  var BASE = base ? base.content : "";

  // ---- drag ordering --------------------------------------------------
  // The list posts one order for the whole day; the server writes it in one
  // transaction. Only `.sortable` lists take part.
  var dragEl = null;
  function listOf(el) {
    return el && el.closest ? el.closest("ul.sortable") : null;
  }
  document.addEventListener("dragstart", function (e) {
    var li = e.target.closest && e.target.closest("li.item");
    var list = listOf(li);
    if (!list) return;
    dragEl = li;
    li.classList.add("dragging");
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = "move";
      // Firefox needs some data set or the drag never starts.
      e.dataTransfer.setData("text/plain", li.dataset.id || "");
    }
  });
  document.addEventListener("dragover", function (e) {
    var list = listOf(e.target);
    if (!list || !dragEl || dragEl.parentNode !== list) return;
    e.preventDefault();
    var li = e.target.closest("li.item");
    if (!li || li === dragEl || li.parentNode !== list) return;
    var rect = li.getBoundingClientRect();
    var after = e.clientY > rect.top + rect.height / 2;
    list.insertBefore(dragEl, after ? li.nextSibling : li);
  });
  document.addEventListener("dragend", function () {
    if (!dragEl) return;
    dragEl.classList.remove("dragging");
    var list = dragEl.parentNode;
    dragEl = null;
    if (!list || !list.classList.contains("sortable")) return;
    var ids = Array.prototype.map.call(
      list.querySelectorAll("li.item[data-id]"),
      function (li) { return li.dataset.id; }
    );
    var body = new URLSearchParams();
    ids.forEach(function (id) { body.append("item", id); });
    fetch(BASE + "/api/items/reorder", {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        "HX-Request": "true",
        "X-Today-Client": "1",
      },
      body: body.toString(),
      credentials: "same-origin",
    })
      .then(function (r) { return r.ok ? r.text() : Promise.reject(r.status); })
      .then(function () { toast("顺序已保存"); })
      .catch(function () { toast("顺序没保存，请刷新"); });
  });

  // ---- inline title editing -------------------------------------------
  // Double-click (or `e` on the focused row) turns the title into an editable
  // line; Enter saves through the same PATCH the forms use, Esc cancels.
  function startEdit(link) {
    if (!link || link.isContentEditable) return;
    var original = link.textContent;
    link.contentEditable = "true";
    link.setAttribute("role", "textbox");
    link.focus();
    var range = document.createRange();
    range.selectNodeContents(link);
    var sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(range);

    function finish(save) {
      link.contentEditable = "false";
      link.removeAttribute("role");
      var value = link.textContent.trim();
      if (!save || value === original) {
        link.textContent = original;
        return;
      }
      if (!value) {
        link.textContent = original;
        return;
      }
      var body = new URLSearchParams();
      body.append("title", value);
      fetch(link.dataset.edit, {
        method: "PATCH",
        headers: {
          "Content-Type": "application/x-www-form-urlencoded",
          "HX-Request": "true",
          "X-Today-Client": "1",
        },
        body: body.toString(),
        credentials: "same-origin",
      }).then(function (r) {
        if (!r.ok) throw new Error(r.status);
        link.textContent = value;
        toast("已改名");
      }).catch(function () {
        link.textContent = original;
        toast("改名没保存");
      });
    }

    link.addEventListener("keydown", function onKey(e) {
      if (e.key === "Enter") {
        e.preventDefault();
        link.removeEventListener("keydown", onKey);
        finish(true);
      } else if (e.key === "Escape") {
        e.preventDefault();
        link.removeEventListener("keydown", onKey);
        finish(false);
      }
    });
    link.addEventListener("blur", function onBlur() {
      link.removeEventListener("blur", onBlur);
      if (link.isContentEditable) finish(true);
    }, { once: true });
  }

  document.addEventListener("dblclick", function (e) {
    var link = e.target.closest && e.target.closest("a.editable");
    if (link) {
      e.preventDefault();
      startEdit(link);
    }
  });

  // ---- undo toast ------------------------------------------------------
  // Actions that can be undone carry data-undo (a URL to POST) and
  // data-undo-label. The toast holds for five seconds.
  var toastEl = null;
  var toastTimer = 0;
  function ensureToast() {
    if (toastEl) return toastEl;
    toastEl = document.createElement("div");
    toastEl.className = "toast";
    toastEl.setAttribute("role", "status");
    toastEl.innerHTML = '<span class="toast-text"></span><button type="button" class="toast-undo">撤销</button>';
    document.body.appendChild(toastEl);
    toastEl.querySelector(".toast-undo").addEventListener("click", function () {
      var url = toastEl.dataset.undo;
      hideToast();
      if (!url) return;
      fetch(url, { method: "POST", credentials: "same-origin",
        headers: { "HX-Request": "true", "X-Today-Client": "1" } })
        .then(function (r) { if (!r.ok) throw new Error(r.status); return r.text(); })
        .then(function (html) {
          // The answer is the whole block: the undo replaced a row, so the
          // list it lived in is what changes. Never the carry block: an undo
          // of a tick does not carry anything, and swapping #carry-swap would
          // overwrite the "previously unfinished" section with a day list.
          var target = document.querySelector("#today-list");
          if (target && html) target.outerHTML = html;
          toast("已撤销");
        })
        .catch(function () { toast("撤销失败"); });
    });
    return toastEl;
  }
  function toast(text, undoURL) {
    var el = ensureToast();
    el.querySelector(".toast-text").textContent = text;
    el.dataset.undo = undoURL || "";
    el.querySelector(".toast-undo").hidden = !undoURL;
    el.classList.add("show");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(hideToast, 5000);
  }
  function hideToast() {
    if (toastEl) toastEl.classList.remove("show");
    clearTimeout(toastTimer);
  }
  window.__todayToast = toast;

  // A successful HTMX swap that carried a message shows the toast; the
  // server marks undoable actions with HX-Trigger headers.
  document.body.addEventListener("htmx:afterRequest", function (e) {
    var trigger = e.detail && e.detail.xhr ? e.detail.xhr.getResponseHeader("HX-Trigger") : null;
    if (!trigger) return;
    try {
      var data = JSON.parse(trigger);
      if (data.todayToast) toast(data.todayToast, data.todayUndo || "");
    } catch (_) { /* a plain string trigger is not ours */ }
  });

  // ---- quick-add tags --------------------------------------------------
  // The parse endpoint answers JSON; the tag strip shows what the parser
  // understood so the person sees exactly what will be saved.
  document.body.addEventListener("htmx:configRequest", function (e) {
    // The app's own marker: checkWriteOrigin accepts it from a caller that has
    // no Sec-Fetch-Site and no Origin (a proxied or very old client).
    e.detail.headers["X-Today-Client"] = "1";
  });

  document.body.addEventListener("htmx:beforeRequest", function (e) {
    if (e.detail && e.detail.elt && e.detail.elt.id === "add-title") {
      parseSeq++;
      e.detail.elt.dataset.seq = String(parseSeq);
    }
  });
  document.body.addEventListener("htmx:afterRequest", function (e) {
    if (!e.detail || !e.detail.elt || e.detail.elt.id !== "add-title") return;
    // A slow response for an older keystroke must not overwrite the tags for
    // what is on screen now.
    if (e.detail.elt.dataset.seq !== String(parseSeq)) return;
    var tags = document.getElementById("quick-tags");
    if (!tags) return;
    var text = "";
    try { text = e.detail.xhr.responseText || ""; } catch (_) { return; }
    var data;
    try { data = JSON.parse(text); } catch (_) { return; }
    renderTags(tags, data, e.detail.elt.value);
  });

  function tag(label, value, inputName) {
    var span = document.createElement("span");
    span.className = "tag";
    span.textContent = label;
    if (inputName) {
      // The hidden input makes the visible tag the saved value. It lives in
      // the form, not in the strip (which sits outside the form): a field
      // outside the form is never submitted.
      var hidden = document.createElement("input");
      hidden.type = "hidden";
      hidden.name = inputName;
      hidden.value = value;
      hidden.dataset.tagInput = inputName;
      var host = document.querySelector("#quick-add");
      if (host) {
        host.appendChild(hidden);
        span._input = hidden;
      }
    }
    var x = document.createElement("button");
    x.type = "button";
    x.textContent = "×";
    x.setAttribute("aria-label", "撤销识别：" + label);
    x.addEventListener("click", function () {
      // Removing a tag removes its hidden field too, and the submit marks the
      // row as explicitly parsed, so the removed fragment is not re-found.
      if (span._input && span._input.parentNode) span._input.parentNode.removeChild(span._input);
      span.remove();
      markParsed();
    });
    span.appendChild(x);
    return span;
  }

  // markParsed tells the server "the fields on this form are what the person
  // saw; do not parse the raw line again".
  function markParsed() {
    var form = document.querySelector("#quick-add");
    if (!form) return;
    var flag = form.querySelector('input[name="parsed"]');
    if (!flag) {
      flag = document.createElement("input");
      flag.type = "hidden";
      flag.name = "parsed";
      form.appendChild(flag);
    }
    flag.value = "1";
    var raw = form.querySelector('input[name="raw"]');
    var title = form.querySelector('input[name="title"]');
    if (!title) {
      title = document.createElement("input");
      title.type = "hidden";
      title.name = "title";
      form.appendChild(title);
    }
    // The strip's tags are the truth; the title is what is left after the
    // parsed fragments were removed.
    if (raw) {
      title.value = form.dataset.parsedTitle || raw.value;
    }
  }

  var parseSeq = 0;

  function renderTags(container, data, raw) {
    // Drop the previous run's hidden fields before showing the new tags.
    var form = document.querySelector("#quick-add");
    if (form) {
      Array.prototype.forEach.call(form.querySelectorAll("[data-tag-input]"), function (el) {
        el.remove();
      });
      form.dataset.parsedTitle = data.Title || raw;
    }
    container.innerHTML = "";
    var any = false;
    if (data.Date) {
      container.appendChild(tag("日期 " + data.Date, data.Date, "date"));
      any = true;
    }
    if (data.Time) {
      container.appendChild(tag("时间 " + data.Time, data.Time, "time"));
      any = true;
    }
    if (data.RRule) {
      container.appendChild(tag("重复 " + prettyRule(data.RRule), data.RRule, "rrule"));
      any = true;
    }
    if (any && data.Title && data.Title !== raw) {
      var t = document.createElement("span");
      t.className = "tag";
      t.textContent = "标题 " + data.Title;
      container.appendChild(t);
    }
    markParsed();
  }

  function prettyRule(rule) {
    var parts = {};
    rule.split(";").forEach(function (p) {
      var kv = p.split("=");
      if (kv.length === 2) parts[kv[0]] = kv[1];
    });
    var names = { MO: "一", TU: "二", WE: "三", TH: "四", FR: "五", SA: "六", SU: "日" };
    if (parts.FREQ === "DAILY") return "每天";
    if (parts.FREQ === "WEEKLY") {
      var days = (parts.BYDAY || "").split(",").filter(Boolean).map(function (d) { return names[d] || d; }).join("");
      var every = parts.INTERVAL ? "每" + parts.INTERVAL + "周" : "每周";
      return days ? every + days : every;
    }
    if (parts.FREQ === "MONTHLY") return "每月";
    return rule;
  }

  // ---- keyboard shortcuts ---------------------------------------------
  // n new, j/k move, x toggle, e edit, / search, ? help. Never while typing.
  document.addEventListener("keydown", function (e) {
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    var t = e.target;
    var typing = t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT" || t.isContentEditable);
    if (e.key === "/") {
      if (typing) return;
      var search = document.querySelector('input[name="q"]');
      if (search) {
        e.preventDefault();
        search.focus();
      }
      return;
    }
    if (typing) return;
    var rows = Array.prototype.slice.call(document.querySelectorAll("#today-list li.item[data-id]"));
    var current = document.querySelector("#today-list li.item.cursor");
    var idx = rows.indexOf(current);
    function focusRow(i) {
      if (!rows.length) return;
      if (current) current.classList.remove("cursor");
      idx = Math.max(0, Math.min(rows.length - 1, i));
      rows[idx].classList.add("cursor");
      rows[idx].scrollIntoView({ block: "nearest" });
    }
    switch (e.key) {
      case "n":
        var add = document.getElementById("add-title");
        if (add) { e.preventDefault(); add.focus(); add.select(); }
        break;
      case "j":
        if (rows.length) { e.preventDefault(); focusRow(idx < 0 ? 0 : idx + 1); }
        break;
      case "k":
        if (rows.length) { e.preventDefault(); focusRow(idx < 0 ? 0 : idx - 1); }
        break;
      case "x":
        if (current) {
          e.preventDefault();
          var check = current.querySelector("button.check");
          if (check) check.click();
        }
        break;
      case "e":
        if (current) {
          e.preventDefault();
          startEdit(current.querySelector("a.editable"));
        }
        break;
      case "?":
        e.preventDefault();
        toast("n 新建 · j/k 上下 · x 勾选 · e 编辑 · / 搜索");
        break;
    }
  });

  // HTMX swaps replace the rows; keep the cursor marker on a live row.
  document.body.addEventListener("htmx:afterSwap", function (e) {
    if (!e.target || !e.target.querySelector) return;
    if (!e.target.querySelector("li.item.cursor")) {
      var first = e.target.querySelector && e.target.querySelector("li.item[data-id]");
      if (first) first.classList.add("cursor");
    }
  });
})();
