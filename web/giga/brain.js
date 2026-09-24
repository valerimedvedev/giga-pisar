// Мозг Писаря в браузере — как swift/Brain.swift: нейронка причёсывает
// надиктованное по команде «Писарь, исправь» (и любой другой) и выполняет
// команды над выделенным текстом.
//
// Две модели, как в приложении:
//   GigaChat — родной русский, 6,5 ГБ. В браузер не влезает (WebAssembly
//     даёт странице не больше 4 ГБ памяти), поэтому крутится на сервере
//     сайта (llama-server за nginx, адрес brain/). Туда уходит только текст,
//     звук остаётся на компьютере.
//   Qwen3-4B — 1,9 ГБ, скачивается в браузер один раз и считает на месте
//     через wllama (llama.cpp на WebAssembly/WebGPU).
//
// Выбор модели запоминается в localStorage.

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

export const BRAIN_MODELS = [
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
  if (c.includes("перевед") || c.includes("англ")) return L("Перевожу…", "Translating…");
  if (c.includes("сократ") || c.includes("короче")) return L("Сокращаю…", "Shortening…");
  if (c.includes("мысль")) return L("Собираю мысль…", "Composing…");
  if (c.includes("сглад") || c.includes("мягче")) return L("Сглаживаю…", "Smoothing…");
  if (c.includes("исправ") || c.includes("ошибк")) return L("Исправляю…", "Fixing…");
  return L("Причёсываю…", "Polishing…");
}

/** Кнопки над текстом — те же, что в менюшке приложения. */
export const CHIPS = [
  { title: L("Причесать", "Tidy up"),
    command: "причеши текст: убери слова-паразиты и повторы, поправь пунктуацию и очевидные ошибки; смысл, порядок мыслей и лексику не меняй" },
  { title: L("Сократить", "Make it shorter"), command: "сократи, сохранив суть" },
  { title: L("Перевести на английский", "Translate to English"), command: "переведи на английский" },
];

const SELECTION_PROMPT =
  "Ты редактируешь текст, который пользователь выделил в своём документе, " +
  "и выполняешь над ним команду пользователя. Сохраняй смысл и разбиение " +
  "на абзацы, ничего не добавляй от себя и не комментируй. Тон и стиль " +
  "сохраняй, если только команда не велит их изменить: команда важнее. " +
  "Команда дана в конце этой инструкции, в сам текст не входит, и " +
  "упоминать её в ответе нельзя. Верни ТОЛЬКО готовый текст, без кавычек " +
  "вокруг него.";

const DICTATION_PROMPT =
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

function messagesFor(body, command, mode) {
  // Команда — в системную инструкцию, текст — отдельным сообщением целиком:
  // иначе нейронка норовит обработать команду как часть текста.
  return [
    { role: "system", content: (mode === "selection" ? SELECTION_PROMPT : DICTATION_PROMPT) +
      `\n\nКоманда пользователя к тексту: ${command}.` },
    { role: "user", content: body },
  ];
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
                qwenUrls = null } = {}) {
    super();
    this.chosenId = chosenId ?? (store.get("giga.brain") || "off");
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
    return false;
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
    await Promise.all([this.checkServer(), this.checkQwen()]);
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
    const messages = messagesFor(body, command, mode);
    let out;
    if (this.chosenId === "gigachat") {
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

  async chatServer(messages) {
    const ctl = new AbortController();
    const timer = setTimeout(() => ctl.abort(), 120_000);
    try {
      const r = await fetch(this.serverBase + "v1/chat/completions", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          messages, temperature: 0.3, max_tokens: 2048,
          chat_template_kwargs: { enable_thinking: false },
        }),
        signal: ctl.signal,
      });
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
