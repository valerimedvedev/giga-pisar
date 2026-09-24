// Воркер распознавания: модель живёт здесь, чтобы страница не замирала,
// пока энкодер считает. Страница присылает звук (16 кГц, моно) — воркер
// возвращает текст. В сеть ничего не уходит: onnxruntime считает на месте.
//
// Сообщения:
//   → { type: "load", ortUrls: [адрес ort.wasm.min.mjs, ...] }
//   ← { type: "ready", threads, ms }         модель в памяти
//   → { type: "transcribe", id, samples }    Float32Array, 16 кГц
//   ← { type: "result", id, text, ms }
//   ← { type: "error", id?, message }

import { Recognizer } from "./recognizer.js";
import { readModel } from "./model-store.js";

let recognizer = null;
let loading = null;
let queue = Promise.resolve();

/** onnxruntime-web: сперва своя копия рядом со страницей, потом CDN. */
async function loadOrt(urls) {
  let lastError;
  for (const url of urls) {
    try {
      const ort = await import(url);
      return { ort: ort.default?.InferenceSession ? ort.default : ort, url };
    } catch (e) {
      lastError = e;
    }
  }
  throw new Error(`не загрузился onnxruntime-web: ${lastError?.message ?? lastError}`);
}

async function load(ortUrls) {
  const t0 = performance.now();
  const { ort, url } = await loadOrt(ortUrls);

  // Потоки WebAssembly есть только у «изолированной» страницы (заголовки
  // COOP/COEP, см. web/serve.py) — и только когда onnxruntime лежит у нас:
  // с чужого адреса браузер не даст ему завести свои потоки.
  const local = new URL(url, self.location.href).origin === self.location.origin;
  const cores = self.navigator?.hardwareConcurrency || 4;
  const threads = self.crossOriginIsolated && local ? Math.max(1, Math.min(8, cores)) : 1;
  ort.env.wasm.numThreads = threads;
  ort.env.logLevel = "error";

  const files = await readModel();
  recognizer = await Recognizer.create(ort, files);
  return { threads, ms: performance.now() - t0 };
}

self.onmessage = async ({ data: msg }) => {
  if (msg.type === "load") {
    loading ??= load(msg.ortUrls);
    try {
      const info = await loading;
      self.postMessage({ type: "ready", ...info });
    } catch (e) {
      loading = null;
      self.postMessage({ type: "error", message: String(e?.message ?? e) });
    }
  } else if (msg.type === "transcribe") {
    // Записи распознаются строго по очереди: сессии onnxruntime общие.
    queue = queue.then(async () => {
      try {
        if (!loading) throw new Error("модель не загружена");
        await loading;                       // запись пришла раньше модели — ждём
        const t0 = performance.now();
        const text = await recognizer.transcribe(msg.samples);
        self.postMessage({ type: "result", id: msg.id, text, ms: performance.now() - t0 });
      } catch (e) {
        self.postMessage({ type: "error", id: msg.id, message: String(e?.message ?? e) });
      }
    });
  }
};
