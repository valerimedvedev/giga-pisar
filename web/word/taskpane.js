// Гига Писарь в Word: та же диктовка и тот же мозг, что у плагина WordPress,
// только текст живёт в документе Word (doc.js), а панель — в боковой области.
//
// Две кнопки: 🎙 Запись и 🧠 Мозг. Панель мозга: где считает, над чем работает
// (выделенное или абзац с курсором), команды сайта, свой промпт с историей
// до 100 (📌 не вытесняется), «Вернуть как было». Настройки — ⚙: выбор мозга
// (GigaChat на сервере, Qwen в браузере, GigaBrain на компьютере), команды
// и промпты, пакет распознавания.

/* global Office */

import { Engine } from "../giga/engine.js";
import { Mic } from "../giga/mic.js";
import * as store from "../giga/model-store.js";
import { Brain, BRAIN_MODELS, CHIPS, DEFAULT_PROMPTS, parseCommand, stripAddress, actionLabel } from "../giga/brain.js";
import * as doc from "./doc.js";

const $ = (id) => document.getElementById(id);
const mb = (b) => Math.round(b / 1e6);
const gb = (b) => (b / 1e9).toFixed(1).replace(".", ",");
const clock = (s) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, "0")}`;
const SERVER_MODEL = ["../model/", "../gigaam-v3-onnx-int8.tar.gz"];   // модель рядом со страницей, если выложена

// ─────────────────────────── что помнит Word ───────────────────────────

const local = {
  get(k, def) { try { return JSON.parse(localStorage.getItem(k)) ?? def; } catch { return def; } },
  set(k, v) { try { localStorage.setItem(k, JSON.stringify(v)); } catch { /* нет места */ } },
};
const chips = () => local.get("giga.word.chips", null) ?? CHIPS.map(({ title, command }) => ({ title, command }));
const prompts = () => ({ ...DEFAULT_PROMPTS, ...local.get("giga.word.prompts", {}) });

/** История своих промптов — как в плагине: до 100, закреплённые не вытесняются. */
const history = {
  MAX: 100,
  list() { return local.get("giga.prompts", []); },
  save(l) { local.set("giga.prompts", l); },
  add(text) {
    const t = text.trim();
    if (!t) return;
    let l = this.list();
    const old = l.find((h) => h.text === t);
    l = l.filter((h) => h.text !== t);
    l.unshift({ text: t, pinned: !!old?.pinned, ts: Date.now() });
    while (l.length > this.MAX) {
      const i = l.map((h) => h.pinned).lastIndexOf(false);
      if (i < 0) break;
      l.splice(i, 1);
    }
    this.save(l);
  },
  togglePin(t) { this.save(this.list().map((h) => (h.text === t ? { ...h, pinned: !h.pinned } : h))); },
  remove(t) { this.save(this.list().filter((h) => h.text !== t)); },
};

// ─────────────────────────── состояние ───────────────────────────

const engine = new Engine();
const mic = new Mic();
let brain = new Brain({ prompts: prompts() });
let state = "idle";            // idle | starting | recording | busy
let modelReady = false;
let undoHandle = null;
let scopeAtStart = null;       // выделение в момент нажатия «Запись» → команда голосом над ним
let timer = null;

const say = (text, kind = "") => { $("status").textContent = text; $("status").dataset.kind = kind; };

function label() {
  const b = $("dictate");
  b.textContent = state === "recording" ? "⏹ Стоп" : "🎙 Запись";
  b.dataset.state = state;
  b.disabled = !modelReady || state === "starting" || state === "busy";
  $("brain-btn").classList.toggle("off", brain.chosenId === "off");
  for (const c of $("chips").querySelectorAll("button")) c.disabled = state !== "idle" || !brain.ready;
  $("run-prompt").disabled = state !== "idle" || !brain.ready;
  $("revert").disabled = !undoHandle || state !== "idle";
}

// ─────────────────────────── диктовка ───────────────────────────

async function startRecording() {
  scopeAtStart = null;
  try {
    const s = await doc.readScope();
    if (s.kind === "selection") scopeAtStart = s;
  } catch { /* документ не ответил — просто диктуем */ }
  state = "starting"; label();
  say("Включаю микрофон…");
  try {
    await mic.start();
  } catch (e) {
    state = "idle"; label();
    const denied = e?.name === "NotAllowedError" || e?.name === "SecurityError";
    say(denied ? "Нет доступа к микрофону: разрешите его Word (Windows) — на Mac микрофон в надстройках Word пока не работает"
               : `Микрофон не включился: ${e.message}`, "error");
    return;
  }
  state = "recording"; label();
  const hint = scopeAtStart && brain.ready ? "скажите команду над выделенным" : "нажмите «Стоп», когда закончите";
  const tick = () => say(`● Запись ${clock(mic.seconds)} — ${hint}`, "recording");
  tick();
  timer = setInterval(tick, 500);
}

async function stopRecording() {
  clearInterval(timer);
  state = "busy"; label();
  const samples = await mic.stop();
  const seconds = samples.length / 16000;
  if (seconds < 0.3) { state = "idle"; label(); say("Слишком коротко — нажмите и говорите", "warn"); return; }
  say(`Распознаю ${seconds.toFixed(1)} с записи…`, "busy");
  try {
    const { text, ms } = await engine.transcribe(samples);
    if (!text) say("Ничего не расслышал — попробуйте ещё раз", "warn");
    else await handleSpeech(text, `Готово: ${seconds.toFixed(1)} с речи за ${(ms / 1000).toFixed(1)} с`);
  } catch (e) {
    say(`Не распознал: ${e.message}`, "error");
  }
  state = "idle"; label();
}

async function handleSpeech(text, done) {
  // Было выделение и мозг готов — это команда над ним.
  if (scopeAtStart && brain.ready) {
    const cmd = stripAddress(text);
    if (cmd) {
      try {
        await transformScope(scopeAtStart, cmd, "selection");
        say(`Готово: «${cmd}» над выделенным`, "ok");
      } catch (e) {
        say(`Писарь не справился (${e.message}). Выделенное не тронул`, "error");
      }
      return;
    }
  }
  // «…текст, Писарь, команда» — сперва текст в мозг.
  const parsed = parseCommand(text);
  if (parsed && brain.ready) {
    try {
      say(actionLabel(parsed.command), "busy");
      const out = await brain.transform(parsed.body, parsed.command, "dictation", (s) => say(s, "busy"));
      await doc.insertAtCursor(out);
      say(`Готово: «${parsed.command}»`, "ok");
    } catch (e) {
      await doc.insertAtCursor(text);
      say(`Писарь не справился (${e.message}) — вставил как есть`, "error");
    }
    return;
  }
  await doc.insertAtCursor(text);
  say(parsed && !brain.ready ? "Похоже на команду Писарю — включите мозг (⚙)" : done, parsed && !brain.ready ? "warn" : "ok");
}

async function cancelRecording() {
  clearInterval(timer);
  await mic.cancel();
  state = "idle"; label();
  say("Запись отменена");
}

// ─────────────────────────── мозг ───────────────────────────

/** Область → нейронка → на то же место; старое — в «Вернуть как было». */
async function transformScope(scope, command, mode) {
  say(actionLabel(command), "busy");
  const out = await brain.transform(scope.text, command, mode, (s) => say(s, "busy"));
  undoHandle = await doc.replaceScope(scope.kind, out);
  label();
}

async function runCommand(command, own = false) {
  const cmd = stripAddress(command);
  if (!cmd || state !== "idle" || !brain.ready) return;
  if (own) { history.add(command); renderHistory(); }
  state = "busy"; label();
  try {
    const scope = await doc.readScope();
    if (!scope.text.trim()) { say("Поставьте курсор в абзац с текстом или выделите текст", "warn"); }
    else {
      await transformScope(scope, cmd, "selection");
      say(scope.kind === "selection" ? "Готово над выделенным. Не понравилось — «Вернуть как было»" : "Готово над абзацем. Не понравилось — «Вернуть как было»", "ok");
    }
  } catch (e) {
    say(`Писарь не справился (${e.message}). Текст не тронул`, "error");
  }
  state = "idle"; label();
}

async function revert() {
  if (!undoHandle || state !== "idle") return;
  try { await doc.undo(undoHandle); say("Вернул как было", "ok"); } catch (e) { say(`Не вернул: ${e.message}. Попробуйте Ctrl+Z`, "error"); }
  undoHandle = null; label();
}

function whereLabel() {
  const m = brain.chosen;
  if (!m) return "Мозг выключен";
  if (m.id === "local") return `Мозг: на компьютере${brain.local.model ? " · " + brain.local.model : ""}`;
  if (m.id === "gigachat") return "Мозг: GigaChat на сервере";
  return "Мозг: Qwen3-4B в Word";
}

async function renderScope() {
  try {
    const s = await doc.readScope();
    $("scope").textContent = s.kind === "selection" ? `Работает над выделенным (${s.text.length} зн.)` : `Работает над абзацем с курсором (${s.text.trim().split(/\s+/).filter(Boolean).length} сл.). Выделите текст, чтобы править только его.`;
  } catch { $("scope").textContent = "Документ не отвечает"; }
}

function renderChips() {
  const box = $("chips");
  box.textContent = "";
  for (const c of chips()) {
    const b = document.createElement("button");
    b.type = "button";
    b.textContent = c.title;
    b.title = c.command;
    b.addEventListener("click", () => runCommand(c.command));
    box.append(b);
  }
  label();
}

function renderHistory() {
  const l = history.list();
  $("history-box").hidden = !l.length;
  $("history-title").textContent = `История: ${l.length} из ${history.MAX} · 📌 не вытесняется · клик — подставить, двойной — выполнить`;
  const ul = $("history");
  ul.textContent = "";
  for (const h of l) {
    const li = document.createElement("li");
    if (h.pinned) li.classList.add("pinned");
    const span = document.createElement("span");
    span.textContent = h.text;
    span.title = h.text;
    span.addEventListener("click", () => { $("prompt").value = h.text; });
    span.addEventListener("dblclick", () => runCommand(h.text, true));
    const pin = document.createElement("button");
    pin.type = "button"; pin.textContent = h.pinned ? "📌" : "📍"; pin.title = h.pinned ? "Открепить" : "Закрепить";
    pin.addEventListener("click", () => { history.togglePin(h.text); renderHistory(); });
    const del = document.createElement("button");
    del.type = "button"; del.textContent = "✕"; del.title = "Удалить из истории";
    del.addEventListener("click", () => { history.remove(h.text); renderHistory(); });
    li.append(span, pin, del);
    ul.append(li);
  }
}

function togglePanel(open = $("panel").hidden) {
  if (open && brain.chosenId === "off") { openSettings(); return; }   // мозг ещё не выбран — сперва настройки
  $("panel").hidden = !open;
  $("brain-btn").setAttribute("aria-expanded", String(open));
  if (open) { $("settings").hidden = true; renderScope(); renderHistory(); renderChips(); $("brain-where").textContent = whereLabel(); }
}

// ─────────────────────────── настройки мозга ───────────────────────────

function modelState(m) {
  if (m.where === "local") return { ok: `отвечает на ${brain.local.base}`, absent: "не найден — запустите GigaBrain", nokey: "просит ключ доступа", unknown: "ищу на компьютере…" }[brain.localState];
  if (m.where === "server") return { ok: "доступен на сервере", loading: "сервер поднимает модель…", absent: "на сервере не установлен", unknown: "проверяю сервер…" }[brain.server];
  return { absent: `не скачан (${gb(m.size)} ГБ)`, downloading: `скачиваю: ${Math.floor(brain.qwenProgress * 100)}%`, ready: "скачан", loaded: "скачан и загружен", unknown: "проверяю…" }[brain.qwen];
}

function renderBrain() {
  const box = $("brain-models");
  box.textContent = "";
  const options = [{ id: "off", name: "Выключен", details: "текст вставляется как распознан" }, ...BRAIN_MODELS];
  for (const m of options) {
    const row = document.createElement("label");
    row.className = "brain-row";
    const radio = document.createElement("input");
    radio.type = "radio"; radio.name = "brain"; radio.value = m.id; radio.checked = brain.chosenId === m.id;
    radio.addEventListener("change", () => { brain.choose(m.id); label(); });
    const text = document.createElement("span");
    text.className = "brain-text";
    const title = document.createElement("strong");
    title.textContent = m.name;
    const details = document.createElement("span");
    details.className = "hint";
    details.textContent = m.id === "off" ? m.details : `${m.details} · ${modelState(m)}`;
    text.append(title, details);
    row.append(radio, text);
    if (m.where === "local") {
      const form = document.createElement("div");
      form.className = "local-form";
      form.addEventListener("click", (e) => e.stopPropagation());
      const input = (ph, val, type = "text") => { const i = document.createElement("input"); i.type = type; i.placeholder = ph; i.value = val || ""; i.spellcheck = false; return i; };
      const base = input("http://127.0.0.1:8091", brain.local.base);
      const key = input("ключ доступа из окна GigaBrain", brain.local.key, "password");
      const model = document.createElement("select");
      for (const id of brain.localModels) { const o = document.createElement("option"); o.value = o.textContent = id; o.selected = id === brain.local.model; model.append(o); }
      model.hidden = !brain.localModels.length;
      model.addEventListener("change", () => brain.setLocal({ model: model.value }));
      const check = document.createElement("button");
      check.type = "button"; check.textContent = "Найти / проверить";
      check.addEventListener("click", async () => { brain.setLocal({ base: base.value.trim(), key: key.value.trim() }); brain.localState = "unknown"; renderBrain(); await brain.checkLocal(); });
      form.append(base, key, model, check);
      row.append(form);
    }
    if (m.where === "browser") {
      const act = document.createElement("span");
      act.className = "brain-actions";
      const btn = (t, fn) => { const b = document.createElement("button"); b.type = "button"; b.textContent = t; b.addEventListener("click", (e) => { e.preventDefault(); fn(); }); act.append(b); };
      if (brain.qwen === "absent") btn("Скачать", () => { brain.choose("qwen"); brain.downloadQwen(); });
      if (brain.qwen === "downloading") btn("Остановить", () => brain.cancelDownload());
      if (brain.qwen === "ready" || brain.qwen === "loaded") btn("Удалить", async () => { if (confirm("Удалить Qwen из Word?")) await brain.deleteQwen(); });
      row.append(act);
    }
    box.append(row);
  }
  const m = brain.chosen;
  let note = "";
  if (brain.lastError) note = `Мозг: ${brain.lastError}`;
  else if (m?.id === "gigachat" && brain.server !== "ok") note = "GigaChat сейчас недоступен — выберите Qwen или мозг на компьютере.";
  else if (m?.id === "local" && brain.localState === "absent") note = "На компьютере мозг не найден. Запустите GigaBrain и нажмите «Найти / проверить».";
  else if (m?.id === "local" && brain.localState === "nokey") note = "Введите ключ доступа из окна GigaBrain и нажмите «Найти / проверить».";
  else if (m?.id === "qwen" && brain.qwen === "absent") note = "Нажмите «Скачать» — Qwen загрузится один раз (1,9 ГБ).";
  else if (m) note = "Мозг готов. Скажите в конце диктовки «Писарь, исправь» или откройте 🧠.";
  $("brain-status").textContent = note;
  $("brain-where").textContent = whereLabel();
  label();
}

function renderChipsEditor() {
  const box = $("chips-editor");
  box.textContent = "";
  for (const c of chips()) addChipRow(c);
}
function addChipRow(c = { title: "", command: "" }) {
  const row = document.createElement("div");
  row.className = "chip-edit";
  const t = document.createElement("input"); t.type = "text"; t.placeholder = "Название кнопки"; t.value = c.title;
  const cmd = document.createElement("input"); cmd.type = "text"; cmd.placeholder = "Команда нейронке"; cmd.value = c.command;
  const del = document.createElement("button"); del.type = "button"; del.textContent = "Убрать"; del.addEventListener("click", () => row.remove());
  row.append(t, cmd, del);
  $("chips-editor").append(row);
}
function readChipsEditor() {
  return [...$("chips-editor").querySelectorAll(".chip-edit")]
    .map((r) => { const [t, c] = r.querySelectorAll("input"); return { title: t.value.trim(), command: c.value.trim() }; })
    .filter((c) => c.title && c.command).slice(0, 10);
}

function openSettings() {
  $("panel").hidden = true;
  $("brain-btn").setAttribute("aria-expanded", "false");
  $("settings").hidden = false;
  renderBrain();
  renderChipsEditor();
  $("prompt-dictation").value = prompts().dictation;
  $("prompt-selection").value = prompts().selection;
  showStorage();
  brain.refresh();
}

// ─────────────────────────── пакет распознавания ───────────────────────────

async function showStorage() {
  const size = await store.modelSize();
  $("storage").textContent = size ? `Пакет распознавания в Word: ${mb(size)} МБ.` : "Пакет не скачан.";
  $("model-delete").hidden = !size;
}

async function loadModel() {
  $("model").hidden = true;
  say("Загружаю модель в память…", "busy");
  try {
    await engine.load();
    modelReady = true;
    say("Готово — поставьте курсор в документ, нажмите «Запись» и говорите", "ok");
  } catch (e) {
    say(`Модель не загрузилась: ${e.message}`, "error");
    $("model").hidden = false;
  }
  label();
}

async function importWith(task) {
  try {
    await task((done, total) => {
      const pct = total ? Math.min(100, Math.floor((done / total) * 100)) : 0;
      say(`Сохраняю пакет: ${pct}% (${mb(done)} из ${mb(total)} МБ)`, "busy");
    });
  } catch (e) { say(`Не получилось: ${e.message}`, "error"); return; }
  await loadModel();
}

async function showModelPanel() {
  $("model").hidden = false;
  say("Для диктовки нужен пакет распознавания — скачайте его один раз", "warn");
  const url = await store.findServerModel(SERVER_MODEL.map((p) => new URL(p, location.href).href));
  if (url) { $("model-server").hidden = false; $("model-server").onclick = () => importWith((p) => store.downloadModel(url, p)); }
}

// ─────────────────────────── связка ───────────────────────────

function wire() {
  $("dictate").addEventListener("click", () => { if (state === "idle") startRecording(); else if (state === "recording") stopRecording(); });
  document.addEventListener("keydown", (e) => { if (e.key === "Escape" && state === "recording") cancelRecording(); });
  $("brain-btn").addEventListener("click", () => togglePanel());
  $("settings-btn").addEventListener("click", openSettings);
  $("settings-close").addEventListener("click", () => { $("settings").hidden = true; togglePanel(true); });
  $("run-prompt").addEventListener("click", () => { const p = $("prompt").value.trim(); if (p) runCommand(p, true); });
  $("prompt").addEventListener("keydown", (e) => { if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); $("run-prompt").click(); } });
  $("revert").addEventListener("click", revert);
  $("chip-add").addEventListener("click", () => addChipRow());
  $("chips-save").addEventListener("click", () => { local.set("giga.word.chips", readChipsEditor()); renderChips(); say("Команды сохранены", "ok"); });
  $("chips-reset").addEventListener("click", () => { localStorage.removeItem("giga.word.chips"); renderChipsEditor(); renderChips(); });
  $("prompts-save").addEventListener("click", () => {
    local.set("giga.word.prompts", { dictation: $("prompt-dictation").value.trim(), selection: $("prompt-selection").value.trim() });
    brain.prompts = prompts();
    say("Промпты сохранены", "ok");
  });
  $("prompts-reset").addEventListener("click", () => { localStorage.removeItem("giga.word.prompts"); brain.prompts = prompts(); $("prompt-dictation").value = DEFAULT_PROMPTS.dictation; $("prompt-selection").value = DEFAULT_PROMPTS.selection; });
  $("model-file").addEventListener("change", (e) => { if (e.target.files?.length) importWith((p) => store.importFiles(e.target.files, p)); e.target.value = ""; });
  $("model-delete").addEventListener("click", async () => {
    if (!confirm("Удалить пакет распознавания из Word?")) return;
    engine.unload(); modelReady = false; await store.deleteModel(); showStorage(); showModelPanel(); label();
  });
  brain.addEventListener("change", () => { if (!$("settings").hidden) renderBrain(); $("brain-where").textContent = whereLabel(); label(); });
}

async function main() {
  wire();
  label();
  if (!window.isSecureContext || !store.storageAvailable()) { say("Надстройка должна открываться по https", "error"); return; }
  if (await store.hasModel()) loadModel().then(() => brain.refresh());
  else { showModelPanel(); brain.refresh(); }
  // выделение меняется — область в панели тоже
  try {
    Office.context.document.addHandlerAsync(Office.EventType.DocumentSelectionChanged, () => { if (!$("panel").hidden) renderScope(); });
  } catch { /* старый Word */ }
}

if (typeof Office !== "undefined" && Office.onReady) Office.onReady(() => main());
else main();   // вне Word (проверка страницы в браузере)
