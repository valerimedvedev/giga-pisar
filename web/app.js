// Страница Гиги Писаря: модель — в хранилище браузера, диктовка — в одно поле.

import { Engine } from "./giga/engine.js";
import { attachDictation } from "./giga/dictation.js";
import * as store from "./giga/model-store.js";

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
const dictation = attachDictation({ field: $("text"), button: $("dictate"), status: $("status"), engine });
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

// ─────────────────────────── старт ───────────────────────────

if (!window.isSecureContext || !store.storageAvailable()) {
  say(L("Откройте страницу по https или через localhost — иначе браузер не даст ни микрофон, ни хранилище",
        "Open this page over https or localhost — otherwise the browser allows neither the microphone nor storage"), "error");
} else if (await store.hasModel()) {
  loadModel();
} else {
  showModelPanel();
  showStorage();
}
