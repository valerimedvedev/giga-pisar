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
import { Brain, CHIPS, parseCommand, stripAddress } from "./giga/brain.js";

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
.fab[hidden], .bubble[hidden], .veil[hidden], [hidden] { display: none !important; }
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
      <div class="bubble" hidden role="status" aria-live="polite"><div class="text"></div><div class="row"></div></div>
      <div class="veil" hidden><div class="dialog" role="dialog" aria-modal="true"></div></div>`;
    document.documentElement.append(host);
    this.host = host;
    this.fab = this.root.querySelector(".fab");
    this.bubble = this.root.querySelector(".bubble");
    this.bubbleText = this.root.querySelector(".bubble .text");
    this.bubbleRow = this.root.querySelector(".bubble .row");
    this.veil = this.root.querySelector(".veil");
    this.dialog = this.root.querySelector(".dialog");
    // Кнопки не забирают фокус у поля: курсор и выделение остаются на месте.
    this.root.addEventListener("mousedown", (e) => { if (!this.veil.contains(e.target)) e.preventDefault(); });
    this.anchor = null;
    this.hideTimer = null;
  }

  contains(node) { return node === this.host; }

  setFab(state) {
    this.fab.dataset.state = state;
    this.fab.innerHTML = state === "recording" ? STOP : MIC;
    this.fab.setAttribute("aria-label", state === "recording" ? L("Стоп", "Stop") : L("Диктовать", "Dictate"));
  }

  /** Кнопка у правого края поля: у однострочного — посередине, у многострочного — внизу. */
  placeFab(el) {
    const r = el.getBoundingClientRect();
    const visible = r.bottom > 0 && r.top < innerHeight && r.width > 40 && r.height > 12;
    this.fab.hidden = !visible;
    if (!visible) return;
    const size = 30;
    const top = r.height < 60 ? r.top + (r.height - size) / 2 : r.bottom - size - 6;
    const left = Math.min(r.right - size - 6, innerWidth - size - 4);
    this.fab.style.top = `${Math.max(4, top)}px`;
    this.fab.style.left = `${Math.max(4, left)}px`;
    this.placeBubble();
  }

  placeBubble() {
    if (this.bubble.hidden) return;
    const a = (this.anchor && this.anchor.isConnected ? this.anchor : this.fab).getBoundingClientRect();
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
        el.addEventListener("click", () => close(b.id));
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

    this.ui.fab.addEventListener("click", () => {
      if (this.field) this.toggle(this.adapterFor(this.field));
    });
    if (cfg.floating !== false) this.watchFields();
    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape" && this.state === "recording") this.cancel();
    }, true);
  }

  makeBrain(provider) {
    const opts = { chosenId: provider, qwenUrls: cfg.qwenUrls };
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
        this.field = null;
        this.ui.fab.hidden = true;
        if (!this.ui.bubbleRow.childElementCount) this.ui.hideBubble();
      }, 150);
    });
    const follow = () => {
      const el = this.state !== "idle" && this.target ? this.target.anchor : this.field;
      if (el && el.isConnected && !this.ui.fab.hidden) this.ui.placeFab(el);
      else if (!this.ui.bubble.hidden) this.ui.placeBubble();
    };
    addEventListener("scroll", follow, { capture: true, passive: true });
    addEventListener("resize", follow, { passive: true });
    this.follow = follow;
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
      this.offerChips(done, true);
    } catch (e) {
      if (fallback) {
        t.insert(fallback);
        this.say(L(`Писарь не справился (${e.message}) — вставил как есть`, `Pisar could not do it (${e.message}) — inserted as is`), "error", { hideAfter: 8000 });
      } else {
        this.say(L(`Писарь не справился (${e.message}). Текст не тронул`, `Pisar could not do it (${e.message}). Text untouched`), "error", { hideAfter: 8000 });
      }
    }
    this.setState("idle");
  }

  /** После вставки: кнопки мозга (если он открыт) и «Вернуть как было». */
  offerChips(text, canUndo = false) {
    const t = this.target;
    const buttons = [];
    if (this.brain) {
      for (const chip of CHIPS) {
        buttons.push({
          title: chip.title,
          onClick: async () => {
            const sel = t.selection();
            const whole = sel.empty ? t.wholeText() : null;
            if (sel.empty && whole === null) {
              this.say(L("Выделите текст, который поправить", "Select the text to change"), "warn", { hideAfter: 4000 });
              return;
            }
            if (!(await this.brainReady())) return;
            await this.runBrain(sel.empty ? whole : sel.text, chip.command, "selection", sel.empty,
              L("Готово", "Done"));
          },
        });
      }
    }
    if (canUndo && t.undo) {
      buttons.push({
        title: L("Вернуть как было", "Put it back"),
        onClick: () => { if (t.undo()) this.say(L("Вернул как было", "Restored"), "ok", { hideAfter: 3000 }); },
      });
    }
    this.say(text, "ok", { buttons, hideAfter: buttons.length ? 12000 : 3000 });
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

/** Самый внешний contenteditable, к которому относится элемент. */
function findEditableRoot(el) {
  let root = el;
  while (root.parentElement && root.parentElement.isContentEditable) root = root.parentElement;
  return root;
}

// ─────────────────────────── наружу: для редакторов ───────────────────────────

const pisar = new Pisar();
window.GigaPisar = {
  /** adapter — как у полей выше: selection/insert/replace/wholeText/undo/anchor. */
  toggle: (adapter, anchor) => pisar.toggle(adapter, anchor),
  get state() { return pisar.state; },
  canBrain: !!cfg.brain,
};
document.dispatchEvent(new CustomEvent("giga-pisar-ready"));
