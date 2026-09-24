// Распознавание со стороны страницы: держит воркер с моделью
// и отдаёт наружу простое transcribe(samples) → текст.

const ORT_VERSION = "1.30.0";

/** Где искать onnxruntime-web: своя копия (web/vendor, см. fetch-ort.sh), потом CDN. */
export const DEFAULT_ORT_URLS = [
  new URL("../vendor/ort/ort.wasm.min.mjs", import.meta.url).href,
  `https://cdn.jsdelivr.net/npm/onnxruntime-web@${ORT_VERSION}/dist/ort.wasm.min.mjs`,
];

export class Engine {
  constructor({ ortUrls = DEFAULT_ORT_URLS } = {}) {
    this.ortUrls = ortUrls;
    this.worker = null;
    this.pending = new Map();
    this.nextId = 1;
    this.ready = null;       // Promise<{threads, ms}> — модель в памяти
  }

  /** Поднимает модель из хранилища браузера в воркер. Повторный вызов — тот же промис. */
  load() {
    if (this.ready) return this.ready;
    this.worker = new Worker(new URL("./worker.js", import.meta.url), { type: "module" });
    this.ready = new Promise((resolve, reject) => {
      this.worker.onmessage = ({ data: m }) => {
        if (m.type === "ready") resolve(m);
        else if (m.type === "result" || (m.type === "error" && m.id)) {
          const p = this.pending.get(m.id);
          this.pending.delete(m.id);
          if (m.type === "result") p?.resolve(m);
          else p?.reject(new Error(m.message));
        } else if (m.type === "error") {
          reject(new Error(m.message));
        }
      };
      this.worker.onerror = (e) => reject(new Error(e.message || "воркер распознавания упал"));
    });
    this.ready.catch(() => this.unload());
    this.worker.postMessage({ type: "load", ortUrls: this.ortUrls });
    return this.ready;
  }

  /** Звук (Float32Array, 16 кГц, моно) → { text, ms }. */
  transcribe(samples) {
    if (!this.worker) return Promise.reject(new Error("модель не загружена"));
    const id = this.nextId++;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.worker.postMessage({ type: "transcribe", id, samples }, [samples.buffer]);
    });
  }

  /** Выгружает модель из памяти (например, перед удалением из хранилища). */
  unload() {
    this.worker?.terminate();
    this.worker = null;
    this.ready = null;
    for (const p of this.pending.values()) p.reject(new Error("модель выгружена"));
    this.pending.clear();
  }
}
