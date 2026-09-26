// Мозг Писаря в браузере — как swift/Brain.swift: нейронка причёсывает
// надиктованное по команде «Писарь, исправь» (и любой другой) и выполняет
// команды над выделенным текстом.
//
// Три места, где может жить нейронка:
//   GigaChat на сервере сайта — llama-server за nginx (адрес brain/). Туда
//     уходит только текст, звук остаётся на компьютере.
//   Qwen3-4B в браузере — 1,9 ГБ, скачивается один раз и считает на месте
//     через wllama (llama.cpp на WebAssembly/WebGPU).
//   На компьютере человека — отдельная программа с OpenAI-совместимым
//     API (Ollama, LM Studio, llama-server, наш brain-local/). Страница
//     ходит к ней по http://127.0.0.1:порт; модель — любая, что там стоит.
//     Быстрее браузера и не грузит сервер сайта.
//
// Выбор модели и адрес местного мозга запоминаются в localStorage.

import { CLOUD_SERVICES, cloudService, parseKeys, baseFor, storedKeys } from "./cloud.js";
export { CLOUD_SERVICES, cloudService, parseKeys, baseFor, storedKeys };

const ru = (navigator.language || "ru").toLowerCase().startsWith("ru");
const L = (r, e) => (ru ? r : e);

const WLLAMA_VERSION = "3.6.1";
const QWEN_FILE = "Qwen3-4B-Instruct-2507-Q3_K_M.gguf";

/** Где взять wllama: своя копия (web/vendor, см. fetch-vendor.sh), потом CDN. */
const WLLAMA_SOURCES = [
  {
    module: new URL("../vendor/wllama/index.js", import.meta.url).href,
    wasm: new URL("../vendor/wllama/wllama.wasm", import.meta.url).href,
  },
  {
    module: `https://cdn.jsdelivr.net/npm/@wllama/wllama@${WLLAMA_VERSION}/esm/index.js`,
    wasm: `https://cdn.jsdelivr.net/npm/@wllama/wllama@${WLLAMA_VERSION}/esm/wasm/wllama.wasm`,
  },
];

/** Адрес llama-server с GigaChat на сервере сайта (относительно страницы). */
const SERVER_BASE = new URL("../brain/", import.meta.url).href;

/** Где искать мозг на компьютере человека: наш brain-local, Ollama, LM Studio, llama-server. */
export const LOCAL_CANDIDATES = [
  "http://127.0.0.1:8091", "http://127.0.0.1:11434", "http://127.0.0.1:1234", "http://127.0.0.1:8080",
];

export const BRAIN_MODELS = [
  {
    id: "cloud",
    name: L("Облачный сервис", "Cloud service"),
    where: "cloud",
    details: L("Gemini, Groq, OpenRouter… по своему бесплатному ключу · текст уходит в сервис",
               "Gemini, Groq, OpenRouter… with your own free key · text goes to the service"),
  },
  {
    id: "local",
    name: L("На моём компьютере", "On my computer"),
    where: "local",
    details: L("отдельная программа (Ollama, LM Studio, наш brain-local) · любая модель · быстрее всего",
               "a separate app (Ollama, LM Studio, our brain-local) · any model · fastest"),
  },
  {
    id: "gigachat",
    name: "GigaChat",
    where: "server",
    details: L("родной русский · считает сервер сайта, туда уходит только текст",
               "native Russian · runs on the site's server, only text is sent"),
  },
  {
    id: "qwen",
    name: "Qwen3-4B",
    where: "browser",
    size: 1_930_000_000,
    details: L("1,9 ГБ, скачивается один раз · считает прямо в браузере",
               "1.9 GB, downloaded once · runs right in the browser"),
    // Сперва копия на сайте (быстро и без Hugging Face), потом первоисточник.
    urls: [
      new URL(`../brain-models/${QWEN_FILE}`, import.meta.url).href,
      `https://huggingface.co/unsloth/Qwen3-4B-Instruct-2507-GGUF/resolve/main/${QWEN_FILE}`,
    ],
    file: QWEN_FILE,
  },
];

// ─────────────────────────── обращение «Писарь, …» ───────────────────────────

const ADDRESS = "(?:гига[\\s,—-]+)?п[еиэ]сар[ьяюе]?(?![а-яё])[\\s,.:!—-]*";

/** Ищет в тексте обращение к Писарю. Всё после него — команда.
 *  Берём ПОСЛЕДНЕЕ вхождение: если в самом тексте шла речь про Писаря,
 *  сработает только хвостовое обращение. */
export function parseCommand(text) {
  const re = new RegExp(ADDRESS, "giu");
  let m, last = null;
  while ((m = re.exec(text))) {
    // обращение — отдельное слово, а не хвост другого («описарь» не считается)
    const prev = text[m.index - 1];
    if (!prev || !/[а-яёa-z]/i.test(prev)) last = m;
    if (m[0].length === 0) re.lastIndex++;
  }
  if (!last) return null;
  const command = text.slice(last.index + last[0].length).trim();
  let body = text.slice(0, last.index).trim();
  // висячие запятые и тире перед обращением («…текст, Писарь исправь»)
  while (body && ",—-–".includes(body[body.length - 1])) body = body.slice(0, -1).trimEnd();
  if (!command || !body) return null;
  return { body, command };
}

/** Обращение «Писарь,» в начале команды над выделением — необязательное. */
export function stripAddress(text) {
  return text.replace(new RegExp("^\\s*" + ADDRESS, "iu"), "").trim();
}

/** Что показать в статусе, пока нейронка думает. */
export function actionLabel(command) {
  const c = command.toLowerCase();
  if (c.startsWith("причеши")) return L("Причёсываю…", "Polishing…");
  if (c.includes("перевед") || c.includes("англ")) return L("Перевожу…", "Translating…");
  if (c.includes("сократ") || c.includes("короче")) return L("Сокращаю…", "Shortening…");
  if (c.includes("мысль")) return L("Собираю мысль…", "Composing…");
  if (c.includes("сглад") || c.includes("мягче")) return L("Сглаживаю…", "Smoothing…");
  if (c.includes("делов") || c.includes("официальн")) return L("Делаю деловым…", "Making it formal…");
  if (c.includes("исправ") || c.includes("ошибк")) return L("Исправляю…", "Fixing…");
  return L("Причёсываю…", "Polishing…");
}

/** Функции мозга — кнопки над текстом. Команды вшиты сюда и в тот же
 *  промпт, что и голосовые («Писарь, …»); администратор WordPress
 *  выбирает, какие из них показывать (по id). */
export const CHIPS = [
  { id: "tidy", title: L("Причесать", "Tidy up"),
    command: "причеши текст: убери слова-паразиты и повторы, поправь пунктуацию и очевидные ошибки; смысл, порядок мыслей и лексику не меняй" },
  { id: "fix", title: L("Исправить ошибки", "Fix mistakes"),
    command: "исправь только орфографию, пунктуацию и ошибки распознавания; слова, порядок и стиль не меняй" },
  { id: "short", title: L("Сократить", "Make it shorter"),
    command: "сократи, сохранив суть" },
  { id: "compose", title: L("Собрать мысль", "Compose"),
    command: "собери мысль: из сбивчивой речи сделай связный текст в том же порядке мыслей, ничего не добавляя от себя" },
  { id: "smooth", title: L("Сгладить", "Soften"),
    command: "сгладь тон: сделай мягче и вежливее, убери резкость; смысл не меняй" },
  { id: "formal", title: L("Деловой стиль", "Formal"),
    command: "перепиши в деловом стиле: нейтрально, чётко, без разговорных слов; смысл не меняй" },
  { id: "english", title: L("Перевести на английский", "Translate to English"),
    command: "переведи на английский" },
];

export const DEFAULT_PROMPTS = {};
const SELECTION_PROMPT = DEFAULT_PROMPTS.selection =
  "Ты редактируешь текст, который пользователь выделил в своём документе, " +
  "и выполняешь над ним команду пользователя. Сохраняй смысл и разбиение " +
  "на абзацы, ничего не добавляй от себя и не комментируй. Тон и стиль " +
  "сохраняй, если только команда не велит их изменить: команда важнее. " +
  "Команда дана в конце этой инструкции, в сам текст не входит, и " +
  "упоминать её в ответе нельзя. Верни ТОЛЬКО готовый текст, без кавычек " +
  "вокруг него.";

const DICTATION_PROMPT = DEFAULT_PROMPTS.dictation =
  "Ты обрабатываешь надиктованный голосом текст перед вставкой. Правила: " +
  "убери слова-паразиты и оговорки (э, ну, типа, вот, как бы), убери повторы " +
  "и самоисправления, расставь знаки препинания, исправь очевидные ошибки " +
  "распознавания. Сохраняй смысл и лексику, ничего не добавляй от себя и " +
  "не комментируй. Живой тон автора сохраняй, если только команда не велит " +
  "его изменить: команда важнее тона. Выполни команду пользователя: она " +
  "дана в конце этой инструкции, в сам текст не входит, и упоминать её " +
  "в ответе нельзя. Верни ТОЛЬКО готовый текст, без кавычек вокруг него.";

/** Если модель всё же подумала вслух, оставляем только ответ. */
export function stripThinking(s) {
  const i = s.indexOf("</think>");
  return i >= 0 ? s.slice(i + "</think>".length) : s;
}

export function messagesFor(body, command, mode, prompts = DEFAULT_PROMPTS) {
  // Команда — в системную инструкцию, текст — отдельным сообщением целиком:
  // иначе нейронка норовит обработать команду как часть текста.
  const base = (mode === "selection" ? prompts.selection : prompts.dictation) || DEFAULT_PROMPTS[mode === "selection" ? "selection" : "dictation"];
  return [
    { role: "system", content: base + `\n\nКоманда пользователя к тексту: ${command}.` },
    { role: "user", content: body },
  ];
}

/** Настройки облачного сервиса человека: сервис, адрес, ключ, модель. */
export function cloudSettings() {
  try { return JSON.parse(localStorage.getItem("giga.cloud") || "{}") || {}; } catch { return {}; }
}
export function saveCloudSettings(v) {
  try { localStorage.setItem("giga.cloud", JSON.stringify(v)); } catch { /* приватное окно */ }
}

/** Настройки местного мозга человека: адрес, ключ, модель. */
export function localSettings() {
  try { return JSON.parse(localStorage.getItem("giga.local") || "{}") || {}; } catch { return {}; }
}
export function saveLocalSettings(v) {
  try { localStorage.setItem("giga.local", JSON.stringify(v)); } catch { /* приватное окно */ }
}

const normBase = (u) => String(u || "").trim().replace(/\/+$/, "").replace(/\/v1$/, "");
const authHeaders = (key) => (key ? { Authorization: `Bearer ${key}` } : {});

/** Спрашивает у OpenAI-совместимого сервера список моделей. Бросает, если не отвечает. */
export async function listLocalModels(base, key = "", timeoutMs = 3000) {
  const ctl = new AbortController();
  const t = setTimeout(() => ctl.abort(), timeoutMs);
  try {
    const r = await fetch(normBase(base) + "/v1/models", { headers: authHeaders(key), signal: ctl.signal, cache: "no-store" });
    if (r.status === 401 || r.status === 403) throw new Error(L("нужен ключ доступа", "an access key is required"));
    if (!r.ok) throw new Error(L(`ответил ${r.status}`, `answered ${r.status}`));
    const j = await r.json();
    return (j.data || j.models || []).map((m) => m.id || m.name || m.model).filter(Boolean);
  } finally {
    clearTimeout(t);
  }
}

/** Ищет мозг на компьютере: первый адрес из списка, который отдаёт модели. */
export async function probeLocal(candidates = LOCAL_CANDIDATES, key = "") {
  for (const base of candidates) {
    try {
      const models = await listLocalModels(base, key, 1500);
      return { base, models };
    } catch (e) {
      if (/ключ|key/.test(e.message)) return { base, models: [], needsKey: true };
    }
  }
  return null;
}

const store = {
  get(k) { try { return localStorage.getItem(k); } catch { return null; } },
  set(k, v) { try { localStorage.setItem(k, v); } catch { /* приватное окно */ } },
};

// ─────────────────────────── сам мозг ───────────────────────────

export class Brain extends EventTarget {
  /**
   * Всё необязательно — по умолчанию настройки страницы web/:
   * @param chosenId     модель задана снаружи (иначе — выбор человека из localStorage)
   * @param serverBase   адрес llama-server с GigaChat (OpenAI-совместимый)
   * @param serverChat   (body, command, mode) => Promise<текст> — свой путь к серверу
   *                     вместо прямого llama-server (плагин WordPress ходит через REST)
   * @param serverHealth () => Promise<"ok"|"loading"|"absent">
   * @param qwenUrls     откуда качать Qwen, по порядку
   */
  constructor({ chosenId = null, serverBase = SERVER_BASE, serverChat = null, serverHealth = null,
                qwenUrls = null, prompts = null, local = null } = {}) {
    super();
    this.chosenId = chosenId ?? (store.get("giga.brain") || "off");
    this.prompts = { ...DEFAULT_PROMPTS, ...(prompts || {}) };
    this.local = { ...localSettings(), ...(local || {}) };   // { base, key, model }
    this.localState = "unknown";   // unknown | ok | absent | nokey
    this.localModels = [];
    this.cloud = { service: "gemini", ...cloudSettings() };   // { service, base, key, model }
    this.cloudState = "unknown";   // unknown | ok | absent | nokey
    this.cloudModels = [];
    this.serverBase = serverBase;
    this.serverChatFn = serverChat;
    this.serverHealthFn = serverHealth;
    this.qwenUrls = qwenUrls ?? BRAIN_MODELS[1].urls;
    this.server = "unknown";       // unknown | ok | loading | absent
    this.qwen = "unknown";         // unknown | absent | downloading | ready | loaded
    this.qwenProgress = 0;
    this.lastError = null;
    this.wllama = null;
    this.wllamaLoading = null;
    this.abort = null;
  }

  get chosen() { return BRAIN_MODELS.find((m) => m.id === this.chosenId) ?? null; }

  /** Можно ли прямо сейчас отдать текст выбранной модели. */
  get ready() {
    if (this.chosenId === "gigachat") return this.server === "ok";
    if (this.chosenId === "qwen") return this.qwen === "ready" || this.qwen === "loaded";
    if (this.chosenId === "local") return this.localState === "ok";
    if (this.chosenId === "cloud") return this.cloudState === "ok";
    return false;
  }

  /** Адрес и модель облачного сервиса: свои или из пресета. */
  get cloudBase() { return normBase(this.cloud.base || cloudService(this.cloud.service)?.base || ""); }
  get cloudModel() { return this.cloud.model || cloudService(this.cloud.service)?.model || ""; }

  setCloud(v) {
    this.cloud = { ...this.cloud, ...v };
    saveCloudSettings(this.cloud);
    this.changed();
  }

  /** Проверяет ключ: список моделей (у Cloudflare списка нет — верим ключу). */
  async checkCloud() {
    const svc = cloudService(this.cloud.service);
    const base = this.cloudBase;
    if (!base || base.includes("ACCOUNT_ID") || !this.cloud.key) { this.cloudState = "nokey"; this.changed(); return; }
    try {
      if (svc && !svc.listsModels) { this.cloudState = "ok"; this.changed(); return; }
      this.cloudModels = await listLocalModels(base, this.cloud.key, 8000);
      if (this.cloudModels.length && !this.cloudModels.includes(this.cloudModel)) {
        // у некоторых сервисов список огромный — пресет оставляем, если он там есть под другим именем
        const want = this.cloudModel;
        if (!this.cloudModels.some((m) => m.endsWith(want))) this.cloud.model = this.cloudModels[0];
      }
      this.cloudState = "ok";
      saveCloudSettings(this.cloud);
    } catch (e) {
      this.cloudState = /ключ|key/.test(e.message || "") ? "nokey" : "absent";
      this.lastError = e.message;
    }
    this.changed();
  }

  /** Меняет адрес/ключ/модель местного мозга и запоминает их. */
  setLocal(v) {
    this.local = { ...this.local, ...v };
    saveLocalSettings(this.local);
    this.changed();
  }

  /** Есть ли мозг на компьютере: по сохранённому адресу, а без него — по обычным портам. */
  async checkLocal() {
    const key = this.local.key || "";
    try {
      if (this.local.base) {
        this.localModels = await listLocalModels(this.local.base, key);
      } else {
        const found = await probeLocal(LOCAL_CANDIDATES, key);
        if (!found) throw new Error("absent");
        if (found.needsKey) { this.localState = "nokey"; this.local.base = found.base; this.changed(); return; }
        this.local.base = found.base;
        this.localModels = found.models;
        saveLocalSettings(this.local);
      }
      // модель: выбранная, если она есть на сервере, иначе первая из списка
      if (!this.localModels.includes(this.local.model)) {
        this.local.model = this.localModels[0] || this.local.model || "";
      }
      this.localState = "ok";
    } catch (e) {
      this.localState = /ключ|key/.test(e.message || "") ? "nokey" : "absent";
    }
    this.changed();
  }

  choose(id) {
    this.chosenId = id;
    store.set("giga.brain", id);
    this.changed();
    if (id === "qwen" && this.qwen === "ready") this.warmUp();
  }

  changed() { this.dispatchEvent(new Event("change")); }

  /** Что с моделями: сервер отвечает? Qwen уже в браузере? */
  async refresh() {
    await Promise.all([this.checkServer(), this.checkQwen(), this.checkLocal(), this.chosenId === "cloud" ? this.checkCloud() : null]);
    this.changed();
    if (this.chosenId === "qwen" && this.qwen === "ready") this.warmUp();
  }

  async checkServer() {
    try {
      if (this.serverHealthFn) {
        this.server = await this.serverHealthFn();
      } else {
        const r = await fetch(this.serverBase + "health", { cache: "no-store" });
        this.server = r.ok ? "ok" : r.status === 503 ? "loading" : "absent";
      }
    } catch {
      this.server = "absent";
    }
    // сервер ещё поднимает модель — спросим снова чуть позже
    if (this.server === "loading") setTimeout(() => this.checkServer().then(() => this.changed()), 5000);
  }

  // ── Qwen в браузере

  async getWllama() {
    if (this.wllama) return this.wllama;
    let lastError;
    for (const src of WLLAMA_SOURCES) {
      try {
        const { Wllama, LoggerWithoutDebug } = await import(src.module);
        this.wllama = new Wllama({ default: src.wasm }, {
          logger: LoggerWithoutDebug, suppressNativeLog: true, allowOffline: true, parallelDownloads: 3,
        });
        return this.wllama;
      } catch (e) {
        lastError = e;
      }
    }
    throw new Error(`не загрузился wllama: ${lastError?.message ?? lastError}`);
  }

  /** Запись о скачанном Qwen в кеше wllama (или null). */
  async qwenEntry() {
    const w = await this.getWllama();
    const file = BRAIN_MODELS[1].file;
    const list = await w.cacheManager.list();
    return list.find((e) => e.name.endsWith("_" + file) && e.size > 0 && e.size === e.metadata?.originalSize) ?? null;
  }

  async checkQwen() {
    if (this.qwen === "downloading" || this.qwen === "loaded") return;
    try {
      this.qwen = (await this.qwenEntry()) ? "ready" : "absent";
    } catch (e) {
      this.qwen = "absent";
      this.lastError = e.message;
    }
  }

  /** Качает Qwen в хранилище браузера и сразу поднимает его в память. */
  async downloadQwen() {
    if (this.qwen === "downloading") return;
    this.qwen = "downloading";
    this.qwenProgress = 0;
    this.lastError = null;
    this.changed();
    try { await navigator.storage?.persist?.(); } catch { /* не страшно */ }
    const w = await this.getWllama();
    this.abort = new AbortController();
    let lastError;
    for (const url of this.qwenUrls) {
      // своя копия на сайте есть не всегда — проверяем, прежде чем качать
      if (url.startsWith(location.origin)) {
        const probe = await fetch(url, { method: "HEAD" }).catch(() => null);
        if (!probe?.ok) continue;
      }
      try {
        await w.loadModelFromUrl(url, {
          ...this.loadParams(),
          signal: this.abort.signal,
          progressCallback: ({ loaded, total }) => {
            const p = total ? loaded / total : 0;
            if (p - this.qwenProgress < 0.002 && p < 1) return;   // не чаще, чем раз в 0,2%
            this.qwenProgress = p;
            this.changed();
          },
        });
        this.qwen = "loaded";
        this.abort = null;
        this.changed();
        return;
      } catch (e) {
        lastError = e;
        if (e?.name === "AbortError" || e?.name === "WllamaAbortError") break;
      }
    }
    this.abort = null;
    await this.checkQwen();
    this.lastError = lastError?.name === "AbortError" || lastError?.name === "WllamaAbortError"
      ? L("скачивание остановлено", "download stopped")
      : L(`не скачалась: ${lastError?.message ?? "нет источника"}`, `download failed: ${lastError?.message ?? "no source"}`);
    this.changed();
  }

  cancelDownload() { this.abort?.abort(); }

  loadParams() {
    return { n_ctx: 4096, n_batch: 512 };
  }

  /** Поднимает скачанный Qwen в память заранее, чтобы первая команда не ждала. */
  warmUp() {
    if (this.qwen !== "ready") return this.wllamaLoading ?? Promise.resolve();
    this.wllamaLoading ??= (async () => {
      const entry = await this.qwenEntry();
      if (!entry) throw new Error(L("Qwen не найден в хранилище", "Qwen is not in storage"));
      const w = await this.getWllama();
      await w.loadModelFromUrl(entry.metadata.originalURL, { ...this.loadParams(), useCache: true });
      this.qwen = "loaded";
      this.changed();
    })().catch((e) => {
      this.lastError = e.message;
      this.changed();
      throw e;
    }).finally(() => { this.wllamaLoading = null; });
    return this.wllamaLoading;
  }

  async deleteQwen() {
    const w = await this.getWllama();
    await w.exit().catch(() => {});
    const entry = await this.qwenEntry();
    if (entry) await w.cacheManager.delete(entry.name);
    this.wllama = null;
    this.qwen = "absent";
    this.changed();
  }

  // ── работа

  /** Прогоняет текст через выбранную модель. Бросает, если не вышло.
   *  onStage(текст) — что показать человеку, пока ждём. */
  async transform(body, command, mode = "dictation", onStage = () => {}) {
    const messages = messagesFor(body, command, mode, this.prompts);
    let out;
    if (this.chosenId === "local") {
      if (this.localState !== "ok") await this.checkLocal();
      if (this.localState !== "ok") {
        throw new Error(this.localState === "nokey"
          ? L("мозгу на компьютере нужен ключ доступа", "the local brain needs an access key")
          : L("мозг на компьютере не отвечает — запущена ли программа?", "the local brain does not answer — is the app running?"));
      }
      onStage(actionLabel(command));
      out = await this.chatOpenAI(normBase(this.local.base), messages, { key: this.local.key, model: this.local.model });
    } else if (this.chosenId === "cloud") {
      if (this.cloudState !== "ok") await this.checkCloud();
      if (this.cloudState !== "ok") {
        throw new Error(this.cloudState === "nokey"
          ? L("облачному сервису нужен ключ API (⚙ Мозг)", "the cloud service needs an API key")
          : L("облачный сервис не отвечает", "the cloud service does not answer"));
      }
      onStage(actionLabel(command));
      out = await this.chatOpenAI(this.cloudBase, messages, { key: this.cloud.key, model: this.cloudModel, llamaExtras: false, extras: cloudService(this.cloud.service)?.extras });
    } else if (this.chosenId === "gigachat") {
      onStage(actionLabel(command));
      out = this.serverChatFn ? await this.serverChatFn(body, command, mode) : await this.chatServer(messages);
    } else if (this.chosenId === "qwen") {
      if (this.qwen !== "loaded") {
        onStage(L("Запускаю нейронку…", "Starting the brain…"));
        await this.warmUp();
      }
      onStage(actionLabel(command));
      const w = await this.getWllama();
      const r = await w.createChatCompletion({
        messages, temperature: 0.3, max_tokens: 2048,
        chat_template_kwargs: { enable_thinking: false },
      });
      out = r.choices?.[0]?.message?.content ?? "";
    } else {
      throw new Error(L("мозг не выбран", "no brain selected"));
    }
    const text = stripThinking(out).trim();
    if (!text) throw new Error(L("нейронка ничего не ответила", "the brain returned nothing"));
    return text;
  }

  chatServer(messages) {
    return this.chatOpenAI(this.serverBase.replace(/\/+$/, ""), messages);
  }

  /** Запрос к любому OpenAI-совместимому серверу (llama-server, Ollama, LM Studio). */
  async chatOpenAI(base, messages, { key = "", model = "", llamaExtras = true, extras = null } = {}) {
    const ctl = new AbortController();
    const timer = setTimeout(() => ctl.abort(), 120_000);
    try {
      const r = await fetch(base + "/v1/chat/completions", {
        method: "POST",
        headers: { "Content-Type": "application/json", ...authHeaders(key) },
        body: JSON.stringify({
          ...(model ? { model } : {}),
          ...(extras || {}),
          messages, temperature: 0.3, max_tokens: 2048, stream: false,
          // поле llama.cpp; облачные сервисы незнакомые поля отвергают
          ...(llamaExtras ? { chat_template_kwargs: { enable_thinking: false } } : {}),
        }),
        signal: ctl.signal,
      });
      if (r.status === 401 || r.status === 403) throw new Error(L("ключ не принят", "the key was rejected"));
      if (r.status === 429) throw new Error(L("слишком много запросов подряд, подождите минуту", "too many requests, wait a minute"));
      if (!r.ok) throw new Error(L(`сервер ответил ${r.status}`, `server answered ${r.status}`));
      const j = await r.json();
      return j.choices?.[0]?.message?.content ?? "";
    } catch (e) {
      if (e.name === "AbortError") throw new Error(L("сервер не ответил за 2 минуты", "no answer from the server in 2 minutes"));
      throw e;
    } finally {
      clearTimeout(timer);
    }
  }
}

/** Форма облачного сервиса: пресет, адрес, ключ, модель, «Проверить». */
export function cloudForm(brain, rerender) {
  const form = document.createElement("div");
  form.className = "local-form";
  form.addEventListener("click", (e) => e.stopPropagation());
  const select = document.createElement("select");
  for (const s of CLOUD_SERVICES) {
    const o = document.createElement("option");
    o.value = s.id; o.textContent = s.name + (s.browser ? "" : " (только через сервер)"); o.selected = s.id === brain.cloud.service;
    select.append(o);
  }
  const note = document.createElement("span");
  note.className = "hint";
  const base = document.createElement("input"); base.type = "text"; base.spellcheck = false; base.placeholder = "https://…/v1";
  const key = document.createElement("input"); key.type = "password"; key.placeholder = "ключ API";
  const model = document.createElement("input"); model.type = "text"; model.spellcheck = false; model.placeholder = "модель";
  const link = document.createElement("a"); link.target = "_blank"; link.rel = "noopener";
  const fill = () => {
    const svc = cloudService(select.value) || CLOUD_SERVICES[0];
    const same = svc.id === brain.cloud.service;
    const entry = storedKeys()[svc.id];          // из набора ключей, если импортирован
    base.value = same ? (brain.cloud.base || baseFor(svc, entry)) : baseFor(svc, entry);
    model.value = same ? (brain.cloud.model || svc.model) : svc.model;
    key.value = same ? (brain.cloud.key || entry?.key || "") : (entry?.key || "");
    note.textContent = svc.note;
    link.href = svc.keyUrl; link.textContent = "получить ключ: " + svc.keyUrl.replace("https://", "");
  };
  select.addEventListener("change", fill);
  fill();
  const check = document.createElement("button");
  check.type = "button"; check.textContent = "Проверить и сохранить";
  check.addEventListener("click", async () => {
    brain.setCloud({ service: select.value, base: base.value.trim(), key: key.value.trim(), model: model.value.trim() });
    brain.cloudState = "unknown"; brain.lastError = null;
    rerender?.();
    await brain.checkCloud();
  });
  // набор ключей giga-keys.json: вставить текст или выбрать файл — ключи всех сервисов запомнятся
  const keysBox = document.createElement("textarea");
  keysBox.rows = 2; keysBox.placeholder = "Набор ключей: вставьте содержимое giga-keys.json (страница keys.html делает его)"; keysBox.spellcheck = false;
  const keysRow = document.createElement("div");
  keysRow.style.cssText = "display:flex;gap:6px;flex-wrap:wrap;align-items:center";
  const importBtn = document.createElement("button"); importBtn.type = "button"; importBtn.textContent = "Импорт набора";
  const fileLabel = document.createElement("label"); fileLabel.className = "button"; fileLabel.textContent = "Файл…";
  const fileInput = document.createElement("input"); fileInput.type = "file"; fileInput.accept = ".json,application/json"; fileInput.hidden = true;
  fileLabel.append(fileInput);
  const keysMsg = document.createElement("span"); keysMsg.className = "hint";
  const importText = (text) => {
    try {
      const { keys, default: def } = parseKeys(text);
      localStorage.setItem("giga.cloudKeys", JSON.stringify({ format: "giga-pisar-keys/1", default: def, services: keys }));
      const id = def || (keys[select.value] ? select.value : Object.keys(keys)[0]);
      const svc = cloudService(id);
      brain.setCloud({ service: id, base: baseFor(svc, keys[id]), key: keys[id].key, model: svc.model });
      select.value = id; fill();
      keysMsg.textContent = `Ключи: ${Object.keys(keys).join(", ")} — включён ${svc.name}`;
      keysBox.value = "";
    } catch (e) { keysMsg.textContent = "Не разобрал: " + e.message; }
  };
  importBtn.addEventListener("click", () => keysBox.value.trim() && importText(keysBox.value));
  fileInput.addEventListener("change", () => { const f = fileInput.files?.[0]; if (f) f.text().then(importText); fileInput.value = ""; });
  keysRow.append(importBtn, fileLabel, keysMsg);
  form.append(select, note, link, base, key, model, check, keysBox, keysRow);
  return form;
}
