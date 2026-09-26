// Страница Гиги Писаря: модель — в хранилище браузера, диктовка — в одно поле.

import { Engine } from "./giga/engine.js";
import { attachDictation } from "./giga/dictation.js";
import * as store from "./giga/model-store.js";
import { Brain, BRAIN_MODELS, CHIPS, cloudForm } from "./giga/brain.js";

const ru = (navigator.language || "ru").toLowerCase().startsWith("ru");
const L = (r, e) => (ru ? r : e);
const $ = (id) => document.getElementById(id);
const mb = (bytes) => Math.round(bytes / 1e6);

// Модель рядом со страницей — если тот, кто её выложил, положил и модель.
const SERVER_MODEL = ["model/", "gigaam-v3-onnx-int8.tar.gz"];

if (!ru) {
  document.documentElement.lang = "en";
  for (const el of document.querySelectorAll("[data-en]")) el.textContent = el.dataset.en;
  for (const el of document.querySelectorAll("[data-en-placeholder]")) el.placeholder = el.dataset.enPlaceholder;
}

const engine = new Engine();
const brain = new Brain();
let busy = false;
let chipButtons = [];
const dictation = attachDictation({
  field: $("text"), button: $("dictate"), status: $("status"), engine, brain,
  revert: $("revert"),
  onBusy: (b) => { busy = b; renderChips(); },
});
const say = dictation.say;
dictation.setEnabled(false);
$("archive-link").href = store.ARCHIVE_URL;

async function showStorage() {
  const size = await store.modelSize();
  $("storage").textContent = size ? L(`Модель в браузере: ${mb(size)} МБ.`, `Model stored in the browser: ${mb(size)} MB.`) : "";
  $("model-delete").hidden = !size;
}

async function loadModel() {
  $("model").hidden = true;
  say(L("Загружаю модель в память…", "Loading the model…"), "busy");
  try {
    const { threads } = await engine.load();
    dictation.setEnabled(true);
    say(L("Готово — поставьте курсор в поле, нажмите «Диктовать» и говорите",
          "Ready — place the cursor, press Dictate and speak"), "ok");
    console.info(`Гига Писарь: модель загружена, потоков: ${threads}`);
  } catch (e) {
    say(L(`Модель не загрузилась: ${e.message}`, `The model failed to load: ${e.message}`), "error");
    $("model").hidden = false;
  }
  showStorage();
}

async function importWith(task) {
  dictation.setEnabled(false);
  for (const b of $("model").querySelectorAll("button, label.button")) b.classList.add("disabled");
  try {
    await task((done, total) => {
      const pct = total ? Math.min(100, Math.floor((done / total) * 100)) : 0;
      say(L(`Сохраняю модель в браузер: ${pct}% (${mb(done)} из ${mb(total)} МБ)`,
            `Saving the model to the browser: ${pct}% (${mb(done)} of ${mb(total)} MB)`), "busy");
    });
  } catch (e) {
    say(L(`Не получилось: ${e.message}`, `Failed: ${e.message}`), "error");
    return;
  } finally {
    for (const b of $("model").querySelectorAll(".disabled")) b.classList.remove("disabled");
  }
  await loadModel();
}

async function showModelPanel() {
  $("model").hidden = false;
  say(L("Модель ещё не загружена — выберите архив выше", "The model is not loaded yet — choose the archive above"), "warn");
  const url = await store.findServerModel(SERVER_MODEL);
  if (url) {
    $("model-server").hidden = false;
    $("model-server").onclick = () => importWith((p) => store.downloadModel(url, p));
  }
}

$("model-file").addEventListener("change", (e) => {
  const files = e.target.files;
  if (files?.length) importWith((p) => store.importFiles(files, p));
  e.target.value = "";
});

// Архив можно просто бросить на карточку модели.
const drop = $("model");
drop.addEventListener("dragover", (e) => { e.preventDefault(); drop.classList.add("drop"); });
drop.addEventListener("dragleave", () => drop.classList.remove("drop"));
drop.addEventListener("drop", (e) => {
  e.preventDefault();
  drop.classList.remove("drop");
  if (e.dataTransfer.files.length) importWith((p) => store.importFiles(e.dataTransfer.files, p));
});

$("model-delete").addEventListener("click", async () => {
  if (!confirm(L("Удалить модель из браузера? Для диктовки её придётся загрузить заново.",
                 "Remove the model from the browser? You will need to load it again to dictate."))) return;
  engine.unload();
  dictation.setEnabled(false);
  await store.deleteModel();
  await showStorage();
  showModelPanel();
});

// ─────────────────────────── мозг ───────────────────────────

const gb = (bytes) => (bytes / 1e9).toFixed(1).replace(".", ru ? "," : ".");

// Кнопки над текстом: создаются один раз, гаснут, пока мозг не готов или занят.
chipButtons = CHIPS.map((chip) => {
  const b = document.createElement("button");
  b.type = "button";
  b.textContent = chip.title;
  b.addEventListener("mousedown", (e) => e.preventDefault());   // выделение в поле не теряем
  b.addEventListener("click", () => dictation.runCommand(chip.command));
  $("chips").insertBefore(b, $("revert"));
  return b;
});

function renderChips() {
  $("chips").hidden = brain.chosenId === "off";
  for (const b of chipButtons) b.disabled = busy || !brain.ready;
}

/** Строка про модель: где она и что с ней. Кнопки — только нужные. */
function modelState(m) {
  if (m.where === "local") {
    return {
      ok: L(`отвечает на ${brain.local.base}`, `answers at ${brain.local.base}`),
      absent: L("не найден — запустите GigaBrain (см. brain-local)", "not found — start the app (see brain-local)"),
      nokey: L("просит ключ доступа", "asks for an access key"),
      unknown: L("ищу на компьютере…", "looking on this computer…"),
    }[brain.localState];
  }
  if (m.where === "cloud") {
    return {
      ok: L(`${brain.cloud.service}: ключ принят`, `${brain.cloud.service}: key accepted`),
      absent: L("не отвечает", "does not answer"),
      nokey: L("нужен ключ API", "an API key is needed"),
      unknown: L("не проверен", "not checked"),
    }[brain.cloudState];
  }
  if (m.where === "server") {
    return {
      server: L("доступен на сервере", "available on the server"),
      loading: L("сервер поднимает модель…", "the server is loading the model…"),
      absent: L("на сервере не установлен", "not installed on the server"),
      unknown: L("проверяю сервер…", "checking the server…"),
    }[brain.server === "ok" ? "server" : brain.server];
  }
  return {
    absent: L(`не скачан (${gb(m.size)} ГБ)`, `not downloaded (${gb(m.size)} GB)`),
    downloading: L(`скачиваю: ${Math.floor(brain.qwenProgress * 100)}% из ${gb(m.size)} ГБ`,
                   `downloading: ${Math.floor(brain.qwenProgress * 100)}% of ${gb(m.size)} GB`),
    ready: L("скачан в браузер", "downloaded to the browser"),
    loaded: L("скачан и загружен в память", "downloaded and loaded"),
    unknown: L("проверяю…", "checking…"),
  }[brain.qwen];
}

function renderBrain() {
  const box = $("brain-models");
  box.textContent = "";
  const options = [{ id: "off", name: L("Выключен", "Off"), details: L("текст вставляется как распознан", "text is inserted as recognized") }, ...BRAIN_MODELS];
  for (const m of options) {
    const row = document.createElement("label");
    row.className = "brain-row";
    const radio = document.createElement("input");
    radio.type = "radio";
    radio.name = "brain";
    radio.value = m.id;
    radio.checked = brain.chosenId === m.id;
    radio.addEventListener("change", () => brain.choose(m.id));
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
      // адрес, ключ, модель — свои у каждого человека, живут в localStorage
      const form = document.createElement("div");
      form.className = "local-form";
      form.addEventListener("click", (e) => e.stopPropagation());
      const input = (ph, val, type = "text") => {
        const i = document.createElement("input");
        i.type = type; i.placeholder = ph; i.value = val || ""; i.spellcheck = false;
        return i;
      };
      const base = input("http://127.0.0.1:8091", brain.local.base);
      const key = input(L("ключ (если задан)", "key (if set)"), brain.local.key, "password");
      const model = document.createElement("select");
      for (const id of brain.localModels) {
        const o = document.createElement("option");
        o.value = o.textContent = id;
        o.selected = id === brain.local.model;
        model.append(o);
      }
      model.hidden = !brain.localModels.length;
      model.addEventListener("change", () => brain.setLocal({ model: model.value }));
      const check = document.createElement("button");
      check.type = "button";
      check.textContent = L("Проверить", "Check");
      check.addEventListener("click", async () => {
        brain.setLocal({ base: base.value.trim(), key: key.value.trim() });
        brain.localState = "unknown";
        renderBrain();
        await brain.checkLocal();
      });
      form.append(base, key, model, check);
      row.append(form);
    }
    if (m.where === "cloud") row.append(cloudForm(brain, renderBrain));
    if (m.where === "browser") {
      const act = document.createElement("span");
      act.className = "brain-actions";
      const btn = (label, fn) => {
        const b = document.createElement("button");
        b.type = "button";
        b.textContent = label;
        b.addEventListener("click", (e) => { e.preventDefault(); fn(); });
        act.append(b);
      };
      if (brain.qwen === "absent") btn(L("Скачать", "Download"), () => { brain.choose("qwen"); brain.downloadQwen(); });
      if (brain.qwen === "downloading") btn(L("Остановить", "Stop"), () => brain.cancelDownload());
      if (brain.qwen === "ready" || brain.qwen === "loaded") {
        btn(L("Удалить", "Remove"), async () => {
          if (confirm(L("Удалить Qwen из браузера?", "Remove Qwen from the browser?"))) await brain.deleteQwen();
        });
      }
      row.append(act);
    }
    box.append(row);
  }

  let note = "";
  const m = brain.chosen;
  if (brain.lastError) note = L(`Мозг: ${brain.lastError}`, `Brain: ${brain.lastError}`);
  else if (m?.id === "gigachat" && brain.server !== "ok") note = L("GigaChat сейчас недоступен — выберите Qwen, он считает прямо в браузере.", "GigaChat is unavailable now — choose Qwen, it runs in the browser.");
  else if (m?.id === "local" && brain.localState === "absent") note = L("На компьютере мозг не найден. Запустите GigaBrain (brain-local/dist) или Ollama и нажмите «Проверить».", "No local brain found. Install Ollama or our brain-local (see README) and press Check.");
  else if (m?.id === "local" && brain.localState === "nokey") note = L("Введите ключ доступа, который показала программа при запуске, и нажмите «Проверить».", "Enter the access key the app printed at start and press Check.");
  else if (m?.id === "cloud" && brain.cloudState !== "ok") note = L("Выберите сервис, вставьте свой ключ API и нажмите «Проверить и сохранить». Текст будет уходить в этот сервис.", "Pick a service, paste your API key and press Check. Text will be sent to that service.");
  else if (m?.id === "qwen" && brain.qwen === "absent") note = L("Нажмите «Скачать» — Qwen загрузится один раз и останется в браузере.", "Press Download — Qwen is fetched once and stays in the browser.");
  else if (m) note = L("Мозг готов. Скажите в конце диктовки «Писарь, исправь».", "The brain is ready. Say “Pisar, fix it” at the end of dictation.");
  $("brain-status").textContent = note;
  renderChips();
}

brain.addEventListener("change", renderBrain);
renderBrain();

// ─────────────────────────── старт ───────────────────────────

if (!window.isSecureContext || !store.storageAvailable()) {
  say(L("Откройте страницу по https или через localhost — иначе браузер не даст ни микрофон, ни хранилище",
        "Open this page over https or localhost — otherwise the browser allows neither the microphone nor storage"), "error");
} else if (await store.hasModel()) {
  loadModel().then(() => brain.refresh());
} else {
  showModelPanel();
  showStorage();
  brain.refresh();
}
