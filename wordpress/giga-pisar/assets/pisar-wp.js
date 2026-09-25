// Гига Писарь для WordPress: голосовой ввод в любое поле.
//
// Курсор встал в поле ввода — у его края появляется кнопка микрофона.
// Нажали — запись, нажали ещё раз — текст встаёт туда, где стоял курсор.
// Редакторы (блочный и классический) зовут то же самое через
// window.GigaPisar.toggle(адаптер, кнопка).
//
// Первая диктовка спрашивает согласия скачать пакет распознавания (213 МБ)
// в браузер; дальше он живёт там и работает без сети. Мозг (очистка речи,
// команды «Писарь, …») — только если его включил администратор и открыл
// этому человеку (GigaPisarConfig.brain).

import { Engine } from "./giga/engine.js";
import { Mic } from "./giga/mic.js";
import * as store from "./giga/model-store.js";
import { Brain, CHIPS, parseCommand, stripAddress, actionLabel, listLocalModels, probeLocal, LOCAL_CANDIDATES } from "./giga/brain.js";

const cfg = window.GigaPisarConfig || {};
// По-русски, если русский у сайта или у браузера человека
const ru = [document.documentElement.lang, navigator.language].some((l) => /^ru/i.test(l || ""));
const L = (r, e) => (ru ? r : e);
const mb = (b) => Math.round(b / 1e6);
const clock = (s) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, "0")}`;

// ─────────────────────────── куда вставлять ───────────────────────────
//
// Адаптер цели: где курсор, что выделено, как вставить и как откатить.
//   selection()            → { text, empty }
//   insert(text)           — надиктованное туда, где курсор (пробелы по краям — по смыслу)
//   replace(text, whole)   — правка нейронкой: вместо выделенного (или всего текста, если whole)
//   wholeText()            — весь текст (для кнопок без выделения) или null, если так нельзя
//   undo()                 — вернуть как было до правки нейронкой (или null)
//   anchor                 — элемент, у которого показывать подсказки

const needsSpaceBefore = (ch) => ch && !/\s/.test(ch);
const needsSpaceAfter = (ch) => ch && !/[\s.,!?;:…)\]»"']/.test(ch);

/** <textarea> и <input type=text>. */
function textControlAdapter(el) {
  let sel = { start: el.selectionStart ?? el.value.length, end: el.selectionEnd ?? el.value.length };
  const remember = () => { sel = { start: el.selectionStart, end: el.selectionEnd }; };
  let snapshot = null;
  const current = () => (document.activeElement === el ? { start: el.selectionStart, end: el.selectionEnd } : sel);
  const fire = (data) => el.dispatchEvent(new InputEvent("input", { bubbles: true, inputType: "insertText", data }));
  const put = (text, start, end) => {
    el.setRangeText(text, start, end, "end");
    fire(text);
    remember();
  };
  return {
    el,
    anchor: el,
    remember,
    restoreSelection: () => el.setSelectionRange(sel.start, sel.end),
    selection() {
      const s = current();
      return { text: el.value.slice(s.start, s.end), empty: s.end <= s.start };
    },
    insert(text) {
      const { start, end } = current();
      let t = text;
      if (needsSpaceBefore(el.value[start - 1])) t = " " + t;
      if (needsSpaceAfter(el.value[end])) t += " ";
      put(t, start, end);
    },
    replace(text, whole) {
      snapshot = { value: el.value, ...current() };
      const { start, end } = whole ? { start: 0, end: el.value.length } : current();
      put(text, start, end);
    },
    wholeText() { return el.value; },
    undo: () => {
      if (!snapshot) return false;
      el.value = snapshot.value;
      el.setSelectionRange(snapshot.start, snapshot.end);
      fire(null);
      snapshot = null;
      return true;
    },
  };
}

/** Любой contenteditable (комментарии, редакторы тем и т. п.). */
function editableAdapter(el) {
  let range = null;
  const remember = () => {
    const s = document.getSelection();
    if (s.rangeCount && el.contains(s.getRangeAt(0).commonAncestorContainer)) range = s.getRangeAt(0).cloneRange();
  };
  remember();
  const restore = () => {
    el.focus();
    if (range) {
      const s = document.getSelection();
      s.removeAllRanges();
      s.addRange(range);
    }
  };
  // execCommand — чтобы сработала штатная отмена (Ctrl+Z) и редакторы увидели ввод
  const type = (text) => {
    if (!document.execCommand("insertText", false, text)) {
      const s = document.getSelection();
      const r = s.getRangeAt(0);
      r.deleteContents();
      r.insertNode(document.createTextNode(text));
      r.collapse(false);
    }
    remember();
  };
  let changed = false;
  return {
    el,
    anchor: el,
    remember,
    restoreSelection: restore,
    selection() {
      const t = range ? range.toString() : "";
      return { text: t, empty: !t };
    },
    insert(text) {
      restore();
      let t = text;
      const r = range;
      if (r && r.startContainer.nodeType === 3 && needsSpaceBefore(r.startContainer.data[r.startOffset - 1])) t = " " + t;
      type(t);
    },
    replace(text) {
      restore();
      type(text);
      changed = true;
    },
    wholeText: () => null,        // форматирование не трогаем: только выделенное
    undo: () => {
      if (!changed) return false;
      el.focus();
      document.execCommand("undo");
      changed = false;
      return true;
    },
  };
}

// ─────────────────────────── интерфейс: кнопка, подсказка, окно ───────────────────────────

const MIC = '<svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true"><path fill="currentColor" d="M12 14a3 3 0 0 0 3-3V5a3 3 0 0 0-6 0v6a3 3 0 0 0 3 3zm5-3a5 5 0 0 1-10 0H5a7 7 0 0 0 6 6.92V21h2v-3.08A7 7 0 0 0 19 11h-2z"/></svg>';
const BRAIN = '<svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true"><path fill="currentColor" d="M9 2a3.5 3.5 0 0 0-3.46 3A3.5 3.5 0 0 0 3 8.5c0 .9.34 1.72.9 2.34A3.5 3.5 0 0 0 3 13.5 3.5 3.5 0 0 0 5.6 16.9 3.5 3.5 0 0 0 9 20h1V2H9zm6 0a3.5 3.5 0 0 1 3.46 3A3.5 3.5 0 0 1 21 8.5c0 .9-.34 1.72-.9 2.34A3.5 3.5 0 0 1 21 13.5a3.5 3.5 0 0 1-2.6 3.4A3.5 3.5 0 0 1 15 20h-1V2h1z"/></svg>';
const STOP = '<svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true"><rect x="6" y="6" width="12" height="12" rx="2" fill="currentColor"/></svg>';

const CSS = `
:host { all: initial; }
* { box-sizing: border-box; font: 14px/1.4 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; }
.fab {
  position: fixed; z-index: 2147483000; width: 30px; height: 30px; border-radius: 50%;
  border: 0; padding: 0; display: flex; align-items: center; justify-content: center;
  background: #21a038; color: #fff; cursor: pointer; box-shadow: 0 2px 8px rgba(0,0,0,.25);
  transition: transform .1s;
}
.fab:hover { transform: scale(1.08); }
.fab[data-state="recording"] { background: #d93025; }
.fab[data-state="busy"] { background: #6b7280; cursor: progress; }
.fab.brain { background: #5b4fcf; }
.fab.brain[data-open="1"] { box-shadow: 0 0 0 3px rgba(91,79,207,.35), 0 2px 8px rgba(0,0,0,.25); }
.fab[hidden], .bubble[hidden], .veil[hidden], .panel[hidden], [hidden] { display: none !important; }
.panel {
  position: fixed; z-index: 2147483001; width: 340px; max-width: calc(100vw - 8px); max-height: min(70vh, 560px);
  overflow: auto; padding: 10px 12px; border-radius: 10px;
  background: #1f2328; color: #f3f4f6; box-shadow: 0 6px 24px rgba(0,0,0,.35);
}
.panel .head { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.panel .head b { flex: 1; font-weight: 600; }
.panel .head .who { color: #9ca3af; font-size: 12px; }
.panel .scope { color: #9ca3af; font-size: 12px; margin: 0 0 8px; }
.panel .scope.warn { color: #fde68a; }
.panel .chips { display: flex; flex-wrap: wrap; gap: 6px; }
.panel button {
  border: 1px solid #4b5563; background: #374151; color: #f9fafb; border-radius: 6px;
  padding: 4px 9px; font-size: 13px; cursor: pointer;
}
.panel button:hover { background: #4b5563; }
.panel button:disabled { opacity: .45; cursor: default; }
.panel button.icon { padding: 2px 6px; background: transparent; border-color: transparent; font-size: 14px; }
.panel button.icon:hover { background: #374151; }
.panel button.primary { background: #21a038; border-color: #21a038; }
.panel button.primary:hover { background: #1b8a30; }
.panel .own { margin-top: 10px; border-top: 1px solid #374151; padding-top: 8px; }
.panel .own label { display: block; color: #9ca3af; font-size: 12px; margin-bottom: 4px; }
.panel textarea {
  width: 100%; min-height: 54px; resize: vertical; padding: 6px 8px; border-radius: 6px;
  border: 1px solid #4b5563; background: #111827; color: #f9fafb; font: 13px/1.4 system-ui, sans-serif;
}
.panel .row { display: flex; gap: 6px; margin-top: 6px; align-items: center; flex-wrap: wrap; }
.panel .row .spacer { flex: 1; }
.panel .hist { margin: 8px 0 0; padding: 0; list-style: none; max-height: 180px; overflow: auto; }
.panel .hist li { display: flex; align-items: center; gap: 4px; padding: 3px 4px; border-radius: 5px; cursor: pointer; }
.panel .hist li:hover { background: #2b3138; }
.panel .hist li .t { flex: 1; font-size: 12.5px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.panel .hist li.pinned .t { color: #fde68a; }
.panel .hist li .pin { opacity: .45; }
.panel .hist li.pinned .pin { opacity: 1; }
.panel .status { margin-top: 8px; font-size: 12.5px; color: #9ca3af; min-height: 1.2em; }
.panel .status[data-kind="ok"] { color: #86efac; }
.panel .status[data-kind="error"] { color: #fca5a5; }
.panel .status[data-kind="warn"] { color: #fde68a; }
.panel .foot { display: flex; gap: 6px; margin-top: 10px; align-items: center; }
.panel .foot .spacer { flex: 1; }
.bubble {
  position: fixed; z-index: 2147483001; max-width: 320px; padding: 8px 10px; border-radius: 8px;
  background: #1f2328; color: #f3f4f6; box-shadow: 0 4px 16px rgba(0,0,0,.3);
}
.bubble .text[data-kind="error"] { color: #fca5a5; }
.bubble .text[data-kind="warn"] { color: #fde68a; }
.bubble .text[data-kind="ok"] { color: #86efac; }
.bubble .row { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 6px; }
.bubble button {
  border: 1px solid #4b5563; background: #374151; color: #f9fafb; border-radius: 6px;
  padding: 3px 8px; font-size: 13px; cursor: pointer;
}
.bubble button:hover { background: #4b5563; }
.veil {
  position: fixed; inset: 0; z-index: 2147483002; background: rgba(0,0,0,.45);
  display: flex; align-items: center; justify-content: center; padding: 16px;
}
.dialog {
  background: #fff; color: #1c1f24; border-radius: 12px; padding: 20px; max-width: 440px; width: 100%;
  box-shadow: 0 10px 40px rgba(0,0,0,.35);
}
.dialog h2 { font-size: 18px; font-weight: 600; margin: 0 0 8px; }
.dialog p { margin: 0 0 12px; color: #374151; }
.dialog .status { color: #1c1f24; font-weight: 500; min-height: 1.4em; }
.dialog .status[data-kind="error"] { color: #c5221f; }
.dialog .actions { display: flex; gap: 8px; justify-content: flex-end; flex-wrap: wrap; margin-top: 16px; }
.dialog button, .dialog label.btn {
  border: 1px solid #d1d5db; background: #fff; color: #1c1f24; border-radius: 8px;
  padding: 8px 14px; cursor: pointer; font-weight: 500;
}
.dialog button.primary { background: #21a038; border-color: #21a038; color: #fff; }
.dialog button:disabled { opacity: .5; cursor: default; }
.dialog a { color: #1a7f37; }
@media (prefers-color-scheme: dark) {
  .dialog { background: #1e2125; color: #e8eaed; }
  .dialog p { color: #c4c7cc; }
  .dialog .status { color: #e8eaed; }
  .dialog button, .dialog label.btn { background: #2a2e33; color: #e8eaed; border-color: #444; }
  .dialog a { color: #6ee787; }
}
`;

class UI {
  constructor() {
    const host = document.createElement("giga-pisar");
    host.style.cssText = "position:static!important";
    this.root = host.attachShadow({ mode: "open" });
    this.root.innerHTML = `<style>${CSS}</style>
      <button class="fab" type="button" hidden aria-label="${L("Диктовать", "Dictate")}" title="${L("Голосовой ввод", "Voice input")}">${MIC}</button>
      <button class="fab brain" type="button" hidden aria-label="${L("Мозг", "Brain")}" title="${L("Мозг Писаря: команды, свой промпт, вернуть как было", "Pisar's brain: commands, own prompt, undo")}">${BRAIN}</button>
      <div class="panel" hidden role="dialog" aria-label="${L("Мозг Писаря", "Pisar's brain")}"></div>
      <div class="bubble" hidden role="status" aria-live="polite"><div class="text"></div><div class="row"></div></div>
      <div class="veil" hidden><div class="dialog" role="dialog" aria-modal="true"></div></div>`;
    document.documentElement.append(host);
    this.host = host;
    this.fab = this.root.querySelector(".fab:not(.brain)");
    this.fabBrain = this.root.querySelector(".fab.brain");
    this.panel = this.root.querySelector(".panel");
    this.bubble = this.root.querySelector(".bubble");
    this.bubbleText = this.root.querySelector(".bubble .text");
    this.bubbleRow = this.root.querySelector(".bubble .row");
    this.veil = this.root.querySelector(".veil");
    this.dialog = this.root.querySelector(".dialog");
    // Кнопки не забирают фокус у поля: курсор и выделение остаются на месте.
    this.root.addEventListener("mousedown", (e) => {
      if (this.veil.contains(e.target) || e.target.closest?.("textarea, input, select")) return;
      e.preventDefault();
    });
    this.anchor = null;
    this.hideTimer = null;
    this.brainOn = false;    // показывать ли кнопку мозга (мозг доступен этому человеку)
  }

  contains(node) { return node === this.host; }

  setFab(state) {
    this.fab.dataset.state = state;
    this.fab.innerHTML = state === "recording" ? STOP : MIC;
    this.fab.setAttribute("aria-label", state === "recording" ? L("Стоп", "Stop") : L("Диктовать", "Dictate"));
  }

  /** Кнопка у правого края поля: у однострочного — посередине, у многострочного — внизу.
   *  outside — справа снаружи (у блоков редактора, чтобы не закрывать текст). */
  placeFab(el, outside = false) {
    const r = rectOf(el);
    const visible = r.bottom > 0 && r.top < innerHeight && r.width > 40 && r.height > 12;
    this.fab.hidden = !visible;
    this.fabBrain.hidden = !visible || !this.brainOn;
    if (!visible) { this.hidePanel(); return; }
    const size = 30;
    const top = r.height < 60 ? r.top + (r.height - size) / 2 : r.bottom - size - 6;
    let left = r.right - size - 6;
    const brainOn = !this.fabBrain.hidden;
    // у блока редактора обе кнопки снаружи справа, рядом: слева от микрофона
    // сидит «+» вставки блока, и мозг его закрывал
    const row = outside && r.right + (brainOn ? 2 * size + 16 : size + 10) < innerWidth;
    if (row) left = r.right + 8;
    left = Math.min(left, innerWidth - size - 4);
    this.fab.style.top = `${Math.max(4, top)}px`;
    this.fab.style.left = `${Math.max(4, left)}px`;
    if (row) {                       // мозг — справа от микрофона
      this.fabBrain.style.top = this.fab.style.top;
      this.fabBrain.style.left = `${left + size + 6}px`;
    } else if (outside) {            // места справа нет — мозг под микрофоном
      this.fabBrain.style.top = `${Math.max(4, top) + size + 6}px`;
      this.fabBrain.style.left = this.fab.style.left;
    } else {                         // внутри поля — слева от микрофона
      this.fabBrain.style.top = this.fab.style.top;
      this.fabBrain.style.left = `${Math.max(4, left - size - 6)}px`;
    }
    this.placeBubble();
    this.placePanel();
  }

  /** Панель мозга — под кнопками, правым краем к микрофону. */
  placePanel() {
    if (this.panel.hidden) return;
    const a = rectOf(this.fab.hidden ? (this.anchor || this.fab) : this.fab);
    const p = this.panel.getBoundingClientRect();
    let top = a.bottom + 8;
    if (top + p.height > innerHeight - 4) top = Math.max(4, a.top - p.height - 8);
    const left = Math.min(Math.max(4, a.right - p.width), innerWidth - p.width - 4);
    this.panel.style.top = `${top}px`;
    this.panel.style.left = `${left}px`;
  }

  showPanel() {
    this.panel.hidden = false;
    this.fabBrain.dataset.open = "1";
    this.placePanel();
  }

  hidePanel() {
    this.panel.hidden = true;
    delete this.fabBrain.dataset.open;
  }

  placeBubble() {
    if (this.bubble.hidden) return;
    const a = rectOf(this.anchor && this.anchor.isConnected ? this.anchor : this.fab);
    const b = this.bubble.getBoundingClientRect();
    let top = a.bottom + 6;
    if (top + b.height > innerHeight - 4) top = a.top - b.height - 6;
    const left = Math.min(Math.max(4, a.right - b.width), innerWidth - b.width - 4);
    this.bubble.style.top = `${Math.max(4, top)}px`;
    this.bubble.style.left = `${left}px`;
  }

  /** Подсказка словами. buttons: [{title, onClick}] */
  say(text, kind = "", { anchor = null, buttons = [], hideAfter = 0 } = {}) {
    clearTimeout(this.hideTimer);
    this.bubbleText.textContent = text;
    this.bubbleText.dataset.kind = kind;
    this.bubbleRow.textContent = "";
    for (const b of buttons) {
      const el = document.createElement("button");
      el.type = "button";
      el.textContent = b.title;
      el.addEventListener("click", b.onClick);
      this.bubbleRow.append(el);
    }
    this.bubbleRow.hidden = !buttons.length;
    if (anchor) this.anchor = anchor;
    this.bubble.hidden = false;
    this.placeBubble();
    if (hideAfter) this.hideTimer = setTimeout(() => this.hideBubble(), hideAfter);
  }

  hideBubble() { this.bubble.hidden = true; }

  /** Окно с вопросом. Возвращает промис с id нажатой кнопки (или null — закрыли). */
  ask({ title, html, buttons, file = null }) {
    return new Promise((resolve) => {
      this.dialog.innerHTML = `<h2></h2><div class="body">${html}</div><p class="status" aria-live="polite"></p><div class="actions"></div>`;
      this.dialog.querySelector("h2").textContent = title;
      const actions = this.dialog.querySelector(".actions");
      const close = (v) => { this.veil.hidden = true; document.removeEventListener("keydown", onKey, true); resolve(v); };
      const onKey = (e) => { if (e.key === "Escape" && !this.busyDialog) { e.stopPropagation(); close(null); } };
      if (file) {
        const label = document.createElement("label");
        label.className = "btn";
        label.textContent = file.title;
        const input = document.createElement("input");
        input.type = "file";
        input.hidden = true;
        input.multiple = true;
        input.accept = ".gz,.tgz,.onnx,.model,.yaml";
        input.addEventListener("change", () => { if (input.files.length) close({ files: input.files }); });
        label.append(input);
        actions.append(label);
      }
      for (const b of buttons) {
        const el = document.createElement("button");
        el.type = "button";
        el.textContent = b.title;
        if (b.primary) el.className = "primary";
        // keep: кнопка что-то делает внутри окна и не закрывает его
        el.addEventListener("click", () => (b.keep ? b.onClick(this.dialog) : close(b.id)));
        actions.append(el);
      }
      document.addEventListener("keydown", onKey, true);
      this.veil.hidden = false;
      actions.querySelector(".primary")?.focus();
    });
  }

  /** Окно хода дела (скачивание): показывается, пока работает task(progress). */
  async progress(title, text, task) {
    this.dialog.innerHTML = `<h2></h2><p></p><p class="status" aria-live="polite"></p><div class="actions"></div>`;
    this.dialog.querySelector("h2").textContent = title;
    this.dialog.querySelector("p").textContent = text;
    const status = this.dialog.querySelector(".status");
    this.veil.hidden = false;
    this.busyDialog = true;
    try {
      await task((line) => { status.textContent = line; });
      this.veil.hidden = true;
    } catch (e) {
      status.textContent = e.message;
      status.dataset.kind = "error";
      const ok = document.createElement("button");
      ok.type = "button";
      ok.textContent = L("Закрыть", "Close");
      await new Promise((r) => { ok.onclick = r; this.dialog.querySelector(".actions").append(ok); });
      this.veil.hidden = true;
      throw e;
    } finally {
      this.busyDialog = false;
    }
  }
}

// ─────────────────────────── сам Писарь ───────────────────────────

/** Подпись «что делаю» для команды, на любом языке команды. */
function actionLabelSafe(command) {
  try { return actionLabel(command); } catch { return L("Работаю…", "Working…"); }
}

/** История своих промптов: до 100, новый вытесняет самый старый НЕзакреплённый. */
const promptHistory = {
  MAX: 100,
  key: "giga.prompts",
  list() {
    try { return JSON.parse(localStorage.getItem(this.key) || "[]") || []; } catch { return []; }
  },
  save(items) {
    try { localStorage.setItem(this.key, JSON.stringify(items)); } catch { /* приватное окно */ }
  },
  add(text) {
    let items = this.list().filter((i) => i.text !== text);
    const prev = this.list().find((i) => i.text === text);
    items.unshift({ text, pinned: !!prev?.pinned, ts: Date.now() });
    while (items.length > this.MAX) {
      // самый старый незакреплённый — с конца; если все закреплены, ничего не вытесняем
      let idx = -1;
      for (let i = items.length - 1; i >= 0; i--) if (!items[i].pinned) { idx = i; break; }
      if (idx < 0) break;
      items.splice(idx, 1);
    }
    this.save(items);
  },
  togglePin(i) { const items = this.list(); if (items[i]) { items[i].pinned = !items[i].pinned; this.save(items); } },
  remove(i) { const items = this.list(); items.splice(i, 1); this.save(items); },
};

class Pisar {
  constructor() {
    this.ui = new UI();
    this.engine = new Engine();
    this.mic = new Mic();
    this.state = "idle";               // idle | starting | recording | busy
    this.target = null;                // адаптер поля, куда пишем
    this.field = null;                 // поле с фокусом (для плавающей кнопки)
    this.selAtStart = null;            // выделение в момент нажатия — команда над ним
    this.modelReady = null;            // промис: пакет распознавания в браузере и загружен
    this.brain = cfg.brain ? this.makeBrain(cfg.brain) : null;

    this.ext = null;                   // блок редактора, у которого стоит кнопка (float())
    this.ui.brainOn = !!this.brain;    // права: мозг показывается только тем, кому его открыл сайт
    this.ui.fabBrain.addEventListener("click", () => this.togglePanel());
    // выделение в поле запоминаем, пока фокус ещё там: панель его потом переживёт
    document.addEventListener("selectionchange", () => {
      const a = document.activeElement;
      if (a && a === this.field && this.target?.el === a) {
        this.target.remember?.();
        // панель открыта — обновить «над чем работаем»
        if (!this.ui.panel.hidden && this.state === "idle") {
          const sc = this.ui.panel.querySelector(".scope");
          if (sc) { const scope = this.scopeOf(this.target); sc.textContent = scope.label; sc.classList.toggle("warn", scope.whole === null); }
        }
      }
    });
    document.addEventListener("mousedown", (e) => {
      // клик мимо панели, кнопок и самого поля — закрыть панель (в поле можно менять выделение)
      if (this.ui.panel.hidden) return;
      const path = e.composedPath();
      if (path.includes(this.ui.host) || (this.field && path.includes(this.field)) || (this.ext && path.includes(this.ext.el))) return;
      this.ui.hidePanel();
    }, true);
    this.ui.fab.addEventListener("click", () => {
      if (this.field) this.toggle(this.adapterFor(this.field));
      else if (this.ext) this.toggle(this.ext.adapter, this.ext.el);
      else if (this.state === "recording") this.stop();
    });
    this.follow = () => {
      const el = this.state !== "idle" && this.target ? this.target.anchor : this.field || this.ext?.el;
      if (el && el.isConnected && (!this.ui.fab.hidden || this.state !== "idle")) this.ui.placeFab(el, !this.field);
      else { this.ui.placeBubble(); this.ui.placePanel(); }
    };
    this.watchWindow(window);
    if (cfg.floating !== false) this.watchFields();
    document.addEventListener("keydown", (e) => {
      if (e.key !== "Escape") return;
      if (this.state === "recording") this.cancel();
      else if (!this.ui.panel.hidden) { this.ui.hidePanel(); this.refocus(); }
    }, true);
  }

  makeBrain(provider) {
    // Человек может переключить мозг на свой компьютер — это его выбор, живёт в localStorage.
    const local = cfg.brainLocal && localStorage.getItem("giga.brain.where") === "local";
    const opts = { chosenId: local ? "local" : provider, qwenUrls: cfg.qwenUrls, prompts: cfg.prompts || null };
    this.siteProvider = provider;
    if (provider === "gigachat") {
      const headers = { "Content-Type": "application/json", "X-WP-Nonce": cfg.nonce };
      opts.serverHealth = async () => {
        const r = await fetch(cfg.restUrl + "brain/health", { headers, credentials: "same-origin" });
        return r.ok ? (await r.json()).status : "absent";
      };
      opts.serverChat = async (body, command, mode) => {
        const r = await fetch(cfg.restUrl + "brain", {
          method: "POST", headers, credentials: "same-origin",
          body: JSON.stringify({ body, command, mode }),
        });
        const j = await r.json().catch(() => ({}));
        if (!r.ok) throw new Error(j.message || L(`сервер ответил ${r.status}`, `server answered ${r.status}`));
        return j.text || "";
      };
    }
    // Лениво: модели проверяются и грузятся при первой команде, а не на каждой
    // странице — Qwen в памяти занимает 2,4 ГБ.
    return new Brain(opts);
  }

  // ── плавающая кнопка

  eligible(el) {
    if (!el || this.ui.contains(el)) return false;
    if (el.closest?.("[data-giga-pisar='off'], .block-editor-rich-text__editable, .mce-content-body")) return false;
    if (el.tagName === "TEXTAREA") return !el.readOnly && !el.disabled;
    if (el.tagName === "INPUT") {
      const t = (el.getAttribute("type") || "text").toLowerCase();
      return ["text", "search"].includes(t) && !el.readOnly && !el.disabled;
    }
    return el.isContentEditable && el === findEditableRoot(el);
  }

  adapterFor(el) {
    if (this.target?.el === el) return this.target;
    return el.isContentEditable ? editableAdapter(el) : textControlAdapter(el);
  }

  watchFields() {
    document.addEventListener("focusin", (e) => {
      const el = e.composedPath?.()[0] ?? e.target;
      const root = el?.isContentEditable ? findEditableRoot(el) : el;
      if (!this.eligible(root)) return;
      this.field = root;
      this.ui.placeFab(root);
      this.observe(root);
    });
    document.addEventListener("focusout", () => {
      // фокус ушёл — прячем кнопку, но не посреди записи или распознавания
      setTimeout(() => {
        const a = document.activeElement;
        if (this.state !== "idle" || (a && (a === this.field || this.ui.contains(a)))) return;
        if (!this.ui.panel.hidden) return;         // панель открыта — кнопки нужны
        this.field = null;
        if (this.ext) { this.follow(); return; }   // у блока редактора кнопка остаётся
        this.ui.fab.hidden = true;
        this.ui.fabBrain.hidden = true;
        if (!this.ui.bubbleRow.childElementCount) this.ui.hideBubble();
      }, 150);
    });
  }

  /** Следим за прокруткой окна (и холстов редактора в iframe), чтобы кнопка ехала с полем. */
  watchWindow(win) {
    this.watched ??= new WeakSet();
    if (this.watched.has(win)) return;
    this.watched.add(win);
    win.addEventListener("scroll", () => this.follow(), { capture: true, passive: true });
    win.addEventListener("resize", () => this.follow(), { passive: true });
  }

  /** Плавающая кнопка у блока редактора: { el, adapter } или null — убрать.
   *  el может лежать в iframe холста — координаты пересчитываются. */
  float(target) {
    if (cfg.floating === false) return;
    this.ext = target;
    if (!target) {
      if (this.state === "idle" && !this.field && this.ui.panel.hidden) { this.ui.fab.hidden = true; this.ui.fabBrain.hidden = true; }
      return;
    }
    const win = target.el.ownerDocument.defaultView;
    if (win) this.watchWindow(win);
    if (!this.field && this.state === "idle") {
      this.ui.placeFab(target.el, true);
      this.observe(target.el);
    }
  }

  observe(el) {
    this.resizeObs?.disconnect();
    this.resizeObs = new ResizeObserver(() => this.follow?.());
    this.resizeObs.observe(el);
  }

  // ── запись

  /** Главное действие: нажали кнопку (плавающую или в редакторе). */
  async toggle(adapter, anchor = null) {
    if (this.state === "recording") return this.stop();
    if (this.state !== "idle") return;
    this.target = adapter;
    adapter.remember?.();
    if (anchor) adapter.anchor = anchor;

    if (!(await this.ensureModel())) return;

    const sel = adapter.selection();
    this.selAtStart = !sel.empty && this.brain ? sel : null;
    this.setState("starting");
    this.say(L("Включаю микрофон…", "Starting the microphone…"));
    try {
      await this.mic.start();
    } catch (e) {
      this.setState("idle");
      const denied = e?.name === "NotAllowedError" || e?.name === "SecurityError";
      this.say(denied
        ? L("Нет доступа к микрофону — разрешите его в настройках сайта", "No microphone access — allow it in the site settings")
        : L(`Микрофон не включился: ${e.message}`, `Microphone failed: ${e.message}`), "error", { hideAfter: 8000 });
      return;
    }
    this.setState("recording");
    const hint = this.selAtStart
      ? L("скажите команду над выделенным", "say a command for the selection")
      : L("нажмите ещё раз, когда закончите", "press again when done");
    const tick = () => this.say(`● ${L("Запись", "Recording")} ${clock(this.mic.seconds)} — ${hint}`, "recording");
    tick();
    this.timer = setInterval(tick, 500);
  }

  async stop() {
    clearInterval(this.timer);
    this.setState("busy");
    const samples = await this.mic.stop();
    const seconds = samples.length / 16000;
    if (seconds < 0.3) {
      this.setState("idle");
      this.say(L("Слишком коротко — нажмите и говорите", "Too short — press and speak"), "warn", { hideAfter: 4000 });
      return;
    }
    this.say(L(`Распознаю ${seconds.toFixed(1)} с записи…`, `Recognizing ${seconds.toFixed(1)} s of audio…`), "busy");
    try {
      await this.modelReady;
      const { text } = await this.engine.transcribe(samples);
      if (!text) this.say(L("Ничего не расслышал — попробуйте ещё раз", "Heard nothing — try again"), "warn", { hideAfter: 5000 });
      else await this.handleSpeech(text);
    } catch (e) {
      this.say(L(`Не распознал: ${e.message}`, `Recognition failed: ${e.message}`), "error", { hideAfter: 8000 });
    }
    this.setState("idle");
  }

  async cancel() {
    clearInterval(this.timer);
    await this.mic.cancel();
    this.setState("idle");
    this.say(L("Запись отменена", "Recording cancelled"), "", { hideAfter: 3000 });
  }

  setState(s) {
    this.state = s;
    this.ui.setFab(s === "idle" || s === "starting" ? "idle" : s);
    document.dispatchEvent(new CustomEvent("giga-pisar-state", { detail: { state: s, target: this.target } }));
  }

  /** После окна фокус — обратно в поле, туда же, где был курсор. */
  refocus() {
    const t = this.target;
    const el = t?.el;
    if (!el || !el.isConnected) return;
    el.focus({ preventScroll: true });
    t.restoreSelection?.();
    if (this.eligible(el)) {
      this.field = el;
      this.ui.placeFab(el);
    }
  }

  say(text, kind = "", opts = {}) {
    this.ui.say(text, kind, { anchor: this.target?.anchor, ...opts });
  }

  // ── что делать с распознанным

  async handleSpeech(text) {
    const t = this.target;
    // выделение при нажатии + мозг: это команда над выделенным
    if (this.selAtStart) {
      const cmd = stripAddress(text);
      if (cmd && (await this.brainReady())) {
        await this.runBrain(this.selAtStart.text, cmd, "selection", false, L(`Готово: «${cmd}»`, `Done: “${cmd}”`));
        return;
      }
    }
    const parsed = this.brain ? parseCommand(text) : null;
    if (parsed && (await this.brainReady())) {
      await this.runBrain(parsed.body, parsed.command, "dictation", null, L(`Готово: «${parsed.command}»`, `Done: “${parsed.command}”`), text);
      return;
    }
    t.insert(text);
    this.offerChips(L("Готово", "Done"));
  }

  /** Мозг готов работать? Qwen ещё не скачан — спрашиваем согласия. */
  async brainReady() {
    const b = this.brain;
    if (!b) return false;
    if (b.chosenId === "local") {
      if (b.localState !== "ok") await b.checkLocal();
      if (b.localState === "ok") return true;
      this.say(b.localState === "nokey"
        ? L("Нейронке на вашем компьютере нужен ключ — откройте ⚙ Мозг", "Your local brain needs a key — open ⚙ Brain")
        : L("Нейронка на вашем компьютере не отвечает — запущена ли программа? (⚙ Мозг)", "Your local brain does not answer — is the app running? (⚙ Brain)"),
        "warn", { hideAfter: 8000 });
      return false;
    }
    if (b.chosenId === "gigachat") {
      if (b.server !== "ok") await b.checkServer();
      if (b.server !== "ok") {
        this.say(L("GigaChat на сервере сейчас недоступен — вставляю как есть", "GigaChat is unavailable — inserting as is"), "warn", { hideAfter: 5000 });
        return false;
      }
      return true;
    }
    if (b.qwen === "unknown") await b.checkQwen();
    if (b.qwen === "ready" || b.qwen === "loaded") return true;
    const ok = await this.ui.ask({
      title: L("Скачать нейронку Qwen?", "Download the Qwen model?"),
      html: `<p>${L(
        "Для очистки речи и команд Писарю нужна нейронка Qwen3-4B — 1,9 ГБ. Она скачается один раз, останется в этом браузере и будет работать прямо на вашем компьютере (нужно 8 ГБ памяти).",
        "Speech cleanup and commands need the Qwen3-4B model — 1.9 GB. It is downloaded once, stays in this browser and runs on your computer (8 GB RAM needed).")}</p>`,
      buttons: [{ id: "no", title: L("Не сейчас", "Not now") }, { id: "yes", title: L("Скачать 1,9 ГБ", "Download 1.9 GB"), primary: true }],
    });
    if (ok !== "yes") { this.refocus(); return false; }
    try {
      await this.ui.progress(L("Скачиваю Qwen", "Downloading Qwen"),
        L("Можно продолжать работу — окно закроется само.", "The window closes by itself when done."),
        (line) => new Promise((resolve, reject) => {
          const onChange = () => {
            if (b.qwen === "downloading") line(L(`Скачано ${Math.floor(b.qwenProgress * 100)}%`, `Downloaded ${Math.floor(b.qwenProgress * 100)}%`));
          };
          b.addEventListener("change", onChange);
          b.downloadQwen().then(() => {
            b.removeEventListener("change", onChange);
            if (b.qwen === "loaded" || b.qwen === "ready") resolve();
            else reject(new Error(b.lastError || L("не скачалась", "download failed")));
          });
        }));
      this.refocus();
      return true;
    } catch {
      this.refocus();
      return false;
    }
  }

  /** Правка нейронкой. whole: true — весь текст поля, false — выделенное,
   *  null — надиктованное (вставляется, как обычная диктовка). */
  async runBrain(source, command, mode, whole, done, fallback = null) {
    const t = this.target;
    this.setState("busy");
    try {
      const out = await this.brain.transform(source, command, mode, (stage) => this.say(stage, "busy"));
      if (whole === null) t.insert(out);
      else t.replace(out, whole);
      this.canUndo = !!t.undo;
      this.offerChips(done, true);
      this.panelStatus(done, "ok");
    } catch (e) {
      this.canUndo = false;
      this.panelStatus(L(`Не справился: ${e.message}`, `Failed: ${e.message}`), "error");
      if (fallback) {
        t.insert(fallback);
        this.say(L(`Писарь не справился (${e.message}) — вставил как есть`, `Pisar could not do it (${e.message}) — inserted as is`), "error", { hideAfter: 8000 });
      } else {
        this.say(L(`Писарь не справился (${e.message}). Текст не тронул`, `Pisar could not do it (${e.message}). Text untouched`), "error", { hideAfter: 8000 });
      }
    }
    this.setState("idle");
    if (!this.ui.panel.hidden) this.renderPanel();
  }

  /** После вставки — короткий статус; всё остальное живёт в панели 🧠 и не исчезает. */
  offerChips(text, canUndo = false) {
    const t = this.target;
    const buttons = [];
    if (canUndo && t.undo) {
      buttons.push({
        title: L("Вернуть как было", "Put it back"),
        onClick: () => this.undo(),
      });
    }
    const hint = this.brain && !canUndo ? L(" · кнопка 🧠 — правка нейронкой", " · 🧠 — edit with the brain") : "";
    this.say(text + hint, "ok", { buttons, hideAfter: buttons.length ? 12000 : 4000 });
  }

  undo() {
    const t = this.target;
    if (t?.undo && t.undo()) {
      this.canUndo = false;
      this.say(L("Вернул как было", "Restored"), "ok", { hideAfter: 3000 });
      this.panelStatus(L("Вернул как было", "Restored"), "ok");
      if (!this.ui.panel.hidden) this.renderPanel();
    }
  }

  // ── панель мозга 🧠

  /** Список команд сайта: из настроек (название + команда), без них — вшитые. */
  siteChips() {
    return Array.isArray(cfg.chips) && cfg.chips.length && typeof cfg.chips[0] === "object" ? cfg.chips : CHIPS;
  }

  /** Поле, над которым работает панель: то, где курсор, или блок редактора. */
  panelTarget() {
    if (this.field) return this.adapterFor(this.field);
    if (this.ext) return this.ext.adapter;
    return this.target;
  }

  togglePanel() {
    if (!this.ui.panel.hidden) { this.ui.hidePanel(); this.refocus(); return; }
    if (!this.brain) return;
    const t = this.panelTarget();
    if (!t) return;
    if (t !== this.target) { this.canUndo = false; this.lastStatus = null; }   // другое поле — своя история правок
    this.target = t;
    t.remember?.();
    this.renderPanel();
    this.ui.showPanel();
  }

  panelStatus(text, kind = "") {
    this.lastStatus = { text, kind };          // переживает перерисовку панели
    const el = this.ui.panel.querySelector(".status");
    if (el) { el.textContent = text; el.dataset.kind = kind; }
  }

  /** Что сейчас под мозгом: выделенное, весь текст поля или ничего. */
  scopeOf(t) {
    const sel = t.selection();
    if (!sel.empty) return { text: sel.text, whole: false, label: L(`над выделенным (${sel.text.length} зн.)`, `on the selection (${sel.text.length} chars)`) };
    const whole = t.wholeText();
    if (whole !== null && whole.trim()) return { text: whole, whole: true, label: L("над всем текстом поля", "on the whole field") };
    return { text: "", whole: null, label: whole === null ? L("выделите текст, который поправить", "select the text to change") : L("в поле пусто", "the field is empty") };
  }

  /** Команда мозгу над выделенным / всем текстом из панели. */
  async runFromPanel(command) {
    const t = this.target;
    const scope = this.scopeOf(t);
    if (!scope.text) { this.panelStatus(scope.label, "warn"); return; }
    if (this.state !== "idle") return;
    if (!(await this.brainReady())) { this.panelStatus(L("мозг не готов — см. подсказку", "the brain is not ready"), "warn"); return; }
    this.panelStatus(actionLabelSafe(command), "");
    await this.runBrain(scope.text, command, "selection", scope.whole, L("Готово", "Done"));
  }

  renderPanel() {
    const b = this.brain, t = this.target, panel = this.ui.panel;
    const busy = this.state !== "idle";
    const local = b.chosenId === "local";
    const who = local ? L("нейронка на моём компьютере", "brain on my computer")
      : this.siteProvider === "gigachat" ? L("GigaChat на сервере", "GigaChat on the server") : L("Qwen в браузере", "Qwen in the browser");
    const scope = t ? this.scopeOf(t) : { label: "", whole: null };
    panel.textContent = "";
    const h = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; };

    const head = h("div", "head");
    head.append(h("b", null, L("Мозг Писаря", "Pisar's brain")), h("span", "who", who));
    if (cfg.brainLocal) {
      const gear = h("button", "icon", "⚙"); gear.type = "button"; gear.title = L("Где считает мозг", "Where the brain runs");
      gear.addEventListener("click", () => { this.ui.hidePanel(); this.brainSettings(); });
      head.append(gear);
    }
    const close = h("button", "icon", "✕"); close.type = "button"; close.title = L("Закрыть (Esc)", "Close (Esc)");
    close.addEventListener("click", () => { this.ui.hidePanel(); this.refocus(); });
    head.append(close);
    panel.append(head);

    panel.append(h("p", "scope" + (scope.whole === null ? " warn" : ""), scope.label));

    const chips = h("div", "chips");
    for (const chip of this.siteChips()) {
      const btn = h("button", null, chip.title); btn.type = "button"; btn.disabled = busy; btn.title = chip.command;
      btn.addEventListener("click", () => this.runFromPanel(chip.command));
      chips.append(btn);
    }
    panel.append(chips);

    // свой промпт — только с нейронкой на компьютере человека (его ресурсы, его правила)
    const own = h("div", "own");
    if (local) {
      own.append(h("label", null, L("Свой промпт (что сделать с текстом):", "Own prompt (what to do with the text):")));
      const ta = h("textarea"); ta.placeholder = L("например: переведи на немецкий и сделай список", "e.g. translate to German and make a list");
      ta.value = this.ownPrompt || "";
      ta.addEventListener("input", () => { this.ownPrompt = ta.value; });
      ta.addEventListener("keydown", (e) => { if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); run.click(); } });
      own.append(ta);
      const row = h("div", "row");
      const run = h("button", "primary", L("Выполнить", "Run")); run.type = "button"; run.disabled = busy; run.title = "Ctrl+Enter";
      run.addEventListener("click", () => {
        const text = ta.value.trim();
        if (!text) { this.panelStatus(L("напишите, что сделать", "type what to do"), "warn"); return; }
        promptHistory.add(text);
        this.ownPrompt = text;
        this.renderPanel();
        this.runFromPanel(text);
      });
      row.append(run, h("span", "spacer"), h("span", "who", L(`история: ${promptHistory.list().length}/${promptHistory.MAX}`, `history: ${promptHistory.list().length}/${promptHistory.MAX}`)));
      own.append(row);
      const list = promptHistory.list();
      if (list.length) {
        const ul = h("ul", "hist");
        list.forEach((item, i) => {
          const li = h("li", item.pinned ? "pinned" : ""); li.title = item.text;
          const pin = h("button", "icon pin", "📌"); pin.type = "button";
          pin.title = item.pinned ? L("Открепить", "Unpin") : L("Закрепить — не вытеснится из истории", "Pin — never pushed out of history");
          pin.addEventListener("click", (e) => { e.stopPropagation(); promptHistory.togglePin(i); this.renderPanel(); });
          const txt = h("span", "t", item.text);
          const del = h("button", "icon", "✕"); del.type = "button"; del.title = L("Удалить из истории", "Remove from history");
          del.addEventListener("click", (e) => { e.stopPropagation(); promptHistory.remove(i); this.renderPanel(); });
          li.addEventListener("click", () => { ta.value = item.text; this.ownPrompt = item.text; ta.focus(); });
          li.addEventListener("dblclick", () => { ta.value = item.text; this.ownPrompt = item.text; run.click(); });
          li.append(pin, txt, del);
          ul.append(li);
        });
        own.append(ul);
      }
    } else {
      own.append(h("label", null, cfg.brainLocal
        ? L("Свой промпт и история — с нейронкой на вашем компьютере: ⚙ → «Нейронка на моём компьютере».", "Own prompts and history — with the brain on your computer: ⚙ → “On my computer”.")
        : L("Свой промпт доступен, когда сайт разрешает нейронку на компьютере пользователя.", "Own prompts are available when the site allows a local brain.")));
    }
    panel.append(own);

    const foot = h("div", "foot");
    const undo = h("button", null, L("Вернуть как было", "Put it back")); undo.type = "button"; undo.disabled = busy || !this.canUndo;
    undo.addEventListener("click", () => this.undo());
    foot.append(undo, h("span", "spacer"));
    panel.append(foot);
    const st = h("div", "status", this.lastStatus?.text || "");
    st.dataset.kind = this.lastStatus?.kind || "";
    panel.append(st);
    this.ui.placePanel();
  }

  // ── мозг на компьютере человека

  /** Окно «⚙ Мозг»: на сайте или на моём компьютере; адрес, ключ, модель. */
  async brainSettings() {
    const b = this.brain;
    const site = this.siteProvider === "gigachat" ? "GigaChat" : "Qwen3-4B";
    const siteWhere = this.siteProvider === "gigachat" ? L("на сервере сайта", "on the site's server") : L("в браузере", "in the browser");
    const esc = (v) => String(v || "").replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
    const html = `
      <label style="display:block;margin:4px 0"><input type="radio" name="gp-where" value="site" ${b.chosenId !== "local" ? "checked" : ""}> ${site} — ${siteWhere}</label>
      <label style="display:block;margin:4px 0 10px"><input type="radio" name="gp-where" value="local" ${b.chosenId === "local" ? "checked" : ""}> ${L("Нейронка на моём компьютере", "A model on my computer")}</label>
      <div class="gp-local" style="display:grid;gap:6px">
        <input class="gp-base" type="text" placeholder="http://127.0.0.1:8091" value="${esc(b.local.base)}" spellcheck="false" style="padding:6px 8px;font:inherit">
        <input class="gp-key" type="password" placeholder="${L("ключ доступа (если программа его показала)", "access key (if the app printed one)")}" value="${esc(b.local.key)}" style="padding:6px 8px;font:inherit">
        <select class="gp-model" style="padding:6px 8px;font:inherit"></select>
        <p class="gp-note" style="margin:0;font-size:13px;color:#6b7280">${L(
          "Программа с нейронкой на вашем компьютере: GigaBrain (один файл, brain-local в репозитории Гиги Писаря), Ollama или LM Studio. Страница ходит к ней напрямую, текст на сайт не уходит.",
          "An app with a model on your computer: Ollama, LM Studio or brain-local from the Giga Pisar repo. The page talks to it directly; text never goes to the site.")}</p>
      </div>`;
    const fillModels = (dlg, models, chosen) => {
      const sel = dlg.querySelector(".gp-model");
      sel.innerHTML = "";
      for (const id of models) {
        const o = document.createElement("option");
        o.value = o.textContent = id;
        o.selected = id === chosen;
        sel.append(o);
      }
      sel.hidden = !models.length;
    };
    const check = async (dlg) => {
      const status = dlg.querySelector(".status");
      const base = dlg.querySelector(".gp-base").value.trim();
      const key = dlg.querySelector(".gp-key").value.trim();
      status.dataset.kind = "";
      status.textContent = L("Проверяю…", "Checking…");
      try {
        let found;
        if (base) found = { base, models: await listLocalModels(base, key) };
        else found = await probeLocal(LOCAL_CANDIDATES, key);
        if (!found) throw new Error(L("не найдена ни на одном обычном порту — запущена ли программа?", "not found on any usual port — is the app running?"));
        if (found.needsKey) throw new Error(L("программа просит ключ доступа", "the app asks for an access key"));
        dlg.querySelector(".gp-base").value = found.base;
        fillModels(dlg, found.models, b.local.model);
        status.textContent = L(`Отвечает: ${found.base}, моделей: ${found.models.length}`, `Answers: ${found.base}, models: ${found.models.length}`);
        dlg.querySelector("input[name=gp-where][value=local]").checked = true;
      } catch (e) {
        status.dataset.kind = "error";
        status.textContent = L(`Не отвечает: ${e.message}`, `No answer: ${e.message}`);
      }
    };
    // окно строится сразу, ответ ждём потом — список моделей заполняем, пока оно открыто
    const pending = this.ui.ask({
      title: L("Мозг Писаря", "Pisar's brain"),
      html,
      buttons: [
        { title: L("Найти / проверить", "Find / check"), keep: true, onClick: check },
        { id: "cancel", title: L("Отмена", "Cancel") },
        { id: "save", title: L("Сохранить", "Save"), primary: true },
      ],
    });
    const dlg = this.ui.dialog;
    fillModels(dlg, b.localModels, b.local.model);
    if (b.localState === "unknown" && (b.local.base || b.chosenId === "local")) {
      b.checkLocal().then(() => { if (!this.ui.veil.hidden) fillModels(dlg, b.localModels, b.local.model); });
    }
    const res = await pending;
    if (res !== "save") { this.refocus(); return; }
    const where = dlg.querySelector("input[name=gp-where]:checked")?.value || "site";
    b.setLocal({
      base: dlg.querySelector(".gp-base").value.trim(),
      key: dlg.querySelector(".gp-key").value.trim(),
      model: dlg.querySelector(".gp-model").value || b.local.model || "",
    });
    b.chosenId = where === "local" ? "local" : this.siteProvider;
    b.localState = "unknown";
    try { localStorage.setItem("giga.brain.where", where); } catch { /* приватное окно */ }
    if (!this.ui.panel.hidden) this.renderPanel();
    this.refocus();
    this.say(where === "local"
      ? L("Мозг: нейронка на вашем компьютере", "Brain: the model on your computer")
      : L(`Мозг: ${site} ${siteWhere}`, `Brain: ${site} ${siteWhere}`), "ok", { hideAfter: 4000 });
  }

  // ── пакет распознавания

  /** Пакет в браузере и поднимается в память? Первый раз — спрашиваем согласия. */
  async ensureModel() {
    if (this.modelReady) return true;
    if (!window.isSecureContext || !store.storageAvailable()) {
      this.say(L("Голосовой ввод работает только на сайтах с https", "Voice input needs an https site"), "error", { hideAfter: 6000 });
      return false;
    }
    if (!(await store.hasModel())) {
      const res = await this.ui.ask(cfg.modelUrl ? {
        title: L("Включить голосовой ввод?", "Turn on voice input?"),
        html: `<p>${L(
          "Для диктовки нужно один раз скачать пакет распознавания речи — около 210 МБ. Он сохранится в этом браузере, и дальше голосовой ввод работает без повторных загрузок. Речь распознаётся прямо на вашем компьютере: звук никуда не отправляется.",
          "Dictation needs a one-time download of the speech package — about 210 MB. It stays in this browser. Speech is recognized on your computer: audio is never sent anywhere.")}</p>`,
        buttons: [{ id: "no", title: L("Не сейчас", "Not now") }, { id: "yes", title: L("Скачать и включить", "Download and enable"), primary: true }],
      } : {
        title: L("Включить голосовой ввод?", "Turn on voice input?"),
        html: `<p>${L(
          "Для диктовки нужен пакет распознавания речи (архив около 210 МБ). На этом сайте его ещё не выложили, поэтому скачайте архив и выберите его здесь. Он сохранится в браузере; речь распознаётся на вашем компьютере.",
          "Dictation needs the speech package (a ~210 MB archive). This site does not host it yet: download the archive and pick it here. It stays in the browser; speech is recognized on your computer.")}</p>
          <p><a href="${cfg.archiveUrl}" target="_blank" rel="noopener">${L("Скачать архив gigaam-v3-onnx-int8.tar.gz", "Download gigaam-v3-onnx-int8.tar.gz")}</a></p>
          ${cfg.isAdmin ? `<p><a href="${cfg.settingsUrl}&tab=models">${L("Выложить пакет на сайт для всех", "Host the package on this site")}</a></p>` : ""}`,
        buttons: [{ id: "no", title: L("Отмена", "Cancel") }],
        file: { title: L("Выбрать архив…", "Choose the archive…") },
      });
      if (!res || res === "no") { this.refocus(); return false; }
      try {
        await this.ui.progress(L("Скачиваю пакет распознавания", "Downloading the speech package"),
          L("Один раз, около 210 МБ. Окно закроется само.", "Once, about 210 MB. The window closes by itself."),
          async (line) => {
            const progress = (done, total) => line(total
              ? L(`${Math.floor((done / total) * 100)}% (${mb(done)} из ${mb(total)} МБ)`, `${Math.floor((done / total) * 100)}% (${mb(done)} of ${mb(total)} MB)`)
              : L(`${mb(done)} МБ`, `${mb(done)} MB`));
            if (res.files) await store.importFiles(res.files, progress);
            else await store.downloadModel(cfg.modelUrl, progress);
            line(L("Загружаю в память…", "Loading…"));
            await this.engine.load();
          });
      } catch {
        this.refocus();
        return false;
      }
      this.modelReady = Promise.resolve();
      this.refocus();
      this.say(L("Голосовой ввод включён — нажмите 🎙 и говорите", "Voice input is on — press 🎙 and speak"), "ok", { hideAfter: 5000 });
      return false;          // эту нажатую кнопку считаем включением, запись — следующим нажатием
    }
    // пакет уже в браузере: поднимаем модель, а запись можно начинать сразу
    this.modelReady = this.engine.load().catch((e) => {
      this.modelReady = null;
      throw e;
    });
    this.modelReady.catch(() => {});
    return true;
  }
}

/** Прямоугольник элемента в координатах главного окна (элемент может быть в iframe). */
function rectOf(el) {
  const r = el.getBoundingClientRect();
  const frame = el.ownerDocument?.defaultView?.frameElement;
  if (!frame || el.ownerDocument === document) return r;
  const f = frame.getBoundingClientRect();
  return new DOMRect(r.left + f.left + frame.clientLeft, r.top + f.top + frame.clientTop, r.width, r.height);
}

/** Самый внешний contenteditable, к которому относится элемент. */
function findEditableRoot(el) {
  let root = el;
  while (root.parentElement && root.parentElement.isContentEditable) root = root.parentElement;
  return root;
}

// ─────────────────────────── наружу: для редакторов ───────────────────────────

/** Проверка «что мешает диктовке в этом браузере» — для страницы настроек. */
async function diagnose() {
  const rows = [];
  const add = (name, ok, detail = "") => rows.push({ name, ok, detail });
  add(L("Защищённое соединение (https)", "Secure connection (https)"), window.isSecureContext,
    window.isSecureContext ? "" : L("без https браузер не даёт микрофон и хранилище", "without https there is no microphone or storage"));
  add("WebAssembly", typeof WebAssembly === "object");
  add(L("Запись звука (AudioWorklet)", "Audio capture (AudioWorklet)"), typeof AudioWorkletNode === "function");
  add(L("Хранилище браузера", "Browser storage"), store.storageAvailable(),
    store.storageAvailable() ? "" : L("приватное окно или https нет", "private window or no https"));
  const head = async (url) => {
    try { return (await fetch(url, { method: "HEAD", cache: "no-store" })).status; } catch { return 0; }
  };
  const ort = await head(new URL("./vendor/ort/ort.wasm.min.mjs", import.meta.url).href);
  add(L("Движок распознавания в плагине", "Recognition engine in the plugin"), ort === 200,
    ort === 200 ? "" : L(`vendor/ort не отдаётся (${ort}) — плагин собран без движков? Возьмём с CDN`, `vendor/ort is not served (${ort})`));
  if (cfg.modelUrl) {
    const m = await head(cfg.modelUrl);
    add(L("Пакет распознавания на сервере", "Speech package on the server"), m === 200, m === 200 ? "" : L(`адрес отвечает ${m}`, `the URL answers ${m}`));
  } else {
    add(L("Пакет распознавания на сервере", "Speech package on the server"), false,
      L("не выложен — вкладка «Модели на сервере»", "not hosted — see the Models tab"));
  }
  let inBrowser = false;
  try { inBrowser = await store.hasModel(); } catch { /* нет хранилища */ }
  add(L("Пакет в этом браузере", "Package in this browser"), true,
    inBrowser ? L("скачан", "downloaded") : L("ещё нет — скачается при первом нажатии на микрофон", "not yet — downloaded on the first mic press"));
  try {
    const p = await navigator.permissions.query({ name: "microphone" });
    add(L("Микрофон", "Microphone"), p.state !== "denied",
      { granted: L("разрешён", "allowed"), prompt: L("спросит при первой записи", "will ask on first use"), denied: L("запрещён в настройках сайта", "blocked in site settings") }[p.state]);
  } catch {
    add(L("Микрофон", "Microphone"), true, L("браузер спросит при первой записи", "the browser will ask"));
  }
  add(L("Многопоточность", "Multithreading"), true, crossOriginIsolated ? L("включена", "on") : L("выключена — работает в один поток", "off — single thread"));
  add(L("Мозг для вас", "Brain for you"), true, cfg.brain ? cfg.brain + (cfg.brainLocal ? L(" · можно свой на компьютере", " · or your own local one") : "") : L("выключен", "off"));
  return rows;
}

const pisar = new Pisar();
window.GigaPisar = {
  diagnose,
  /** Окно «⚙ Мозг» (нейронка на компьютере человека); null — сайт этого не разрешил. */
  brainSettings: pisar.brain && cfg.brainLocal ? () => pisar.brainSettings() : null,
  /** Открыть/закрыть панель мозга у текущего поля (null — мозг этому человеку не открыт). */
  brainPanel: pisar.brain ? () => pisar.togglePanel() : null,
  /** Плавающая кнопка у блока редактора: { el, adapter } или null. */
  float: (target) => pisar.float(target),
  /** adapter — как у полей выше: selection/insert/replace/wholeText/undo/anchor. */
  toggle: (adapter, anchor) => pisar.toggle(adapter, anchor),
  get state() { return pisar.state; },
  canBrain: !!cfg.brain,
};
document.dispatchEvent(new CustomEvent("giga-pisar-ready"));
