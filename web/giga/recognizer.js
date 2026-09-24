// Распознавание речи в браузере: звук → признаки → энкодер →
// жадное декодирование RNN-T → текст.
//
// Повторяет питоновское ядро server/giga_core.py и swift/Recognizer.swift
// шаг в шаг. Расхождения проверяются сверкой — см. scripts/сверка-web.py.
//
// Сам onnxruntime сюда передают снаружи (onnxruntime-web в браузере,
// тот же пакет в Node для сверки), поэтому модуль ни от чего не зависит.

import { Features, DEFAULT_FEATURES } from "./features.js";
import { Tokenizer } from "./tokenizer.js";
import { silences, chunkBounds } from "./audio.js";

export const MODEL_NAME = "v3_e2e_rnnt";
export const MODEL_FILES = {
  yaml: `${MODEL_NAME}.yaml`,
  encoder: `${MODEL_NAME}_encoder.onnx`,
  decoder: `${MODEL_NAME}_decoder.onnx`,
  joint: `${MODEL_NAME}_joint.onnx`,
  tokenizer: `${MODEL_NAME}_tokenizer.model`,
};

const MAX_CHUNK = 24.0;            // предел одного прохода модели — 25 секунд
const MAX_SYMBOLS_PER_FRAME = 3;   // столько букв максимум с одного кадра

/** Разбор нужных строк yaml. Полноценный разбор здесь избыточен:
 *  нужны считаные числа, и все ключи в этом файле уникальны по смыслу. */
export function parseModelConfig(text) {
  const value = (key) => {
    for (const line of text.split("\n")) {
      const s = line.trim();
      if (s.startsWith(key + ":")) return s.slice(key.length + 1).trim();
    }
    return null;
  };
  const int = (key, def) => {
    const v = value(key);
    const n = v === null ? NaN : parseInt(v, 10);
    return Number.isFinite(n) ? n : def;
  };
  const c = value("center");
  return {
    features: {
      sampleRate: int("sample_rate", DEFAULT_FEATURES.sampleRate),
      nMels: int("features", DEFAULT_FEATURES.nMels),
      nFFT: int("n_fft", DEFAULT_FEATURES.nFFT),
      winLength: int("win_length", DEFAULT_FEATURES.winLength),
      hopLength: int("hop_length", DEFAULT_FEATURES.hopLength),
      center: c === null ? DEFAULT_FEATURES.center : c === "true",
    },
    predHidden: int("pred_hidden", 320),
    predLayers: int("pred_rnn_layers", 1),
  };
}

export class Recognizer {
  /**
   * @param ort       модуль onnxruntime-web
   * @param files     { yaml: string, encoder, decoder, joint, tokenizer: Uint8Array|ArrayBuffer }
   * @param options   настройки сессий onnxruntime (необязательно)
   */
  static async create(ort, files, options = {}) {
    const opts = { executionProviders: ["wasm"], graphOptimizationLevel: "all", ...options };
    const cfg = parseModelConfig(files.yaml);
    const bytes = (b) => (b instanceof Uint8Array ? b : new Uint8Array(b));
    // По одной: энкодер весит за 300 МБ, и держать в памяти сразу
    // три копии моделей незачем.
    const encoder = await ort.InferenceSession.create(bytes(files.encoder), opts);
    const decoder = await ort.InferenceSession.create(bytes(files.decoder), opts);
    const joint = await ort.InferenceSession.create(bytes(files.joint), opts);
    return new Recognizer(ort, cfg, encoder, decoder, joint, new Tokenizer(files.tokenizer));
  }

  constructor(ort, cfg, encoder, decoder, joint, tokenizer) {
    this.ort = ort;
    this.cfg = cfg;
    this.encoder = encoder;
    this.decoder = decoder;
    this.joint = joint;
    this.tokenizer = tokenizer;
    this.features = new Features(cfg.features);
  }

  get sampleRate() {
    return this.cfg.features.sampleRate;
  }

  /** Распознаёт запись любой длины (Float32Array, 16 кГц, моно): если не влезает
   *  в один проход модели, режется по паузам между фразами и склеивается. */
  async transcribe(samples) {
    const rate = this.sampleRate;
    const total = samples.length / rate;
    if (total <= MAX_CHUNK + 1) return this.transcribeWave(samples);

    const bounds = chunkBounds(total, silences(samples, rate), MAX_CHUNK);
    const parts = [];
    for (const [a, b] of bounds) {
      const from = Math.min(samples.length, Math.floor(a * rate));
      const to = Math.min(samples.length, Math.floor(b * rate));
      if (to > from) parts.push(await this.transcribeWave(samples.subarray(from, to)));
    }
    return parts.filter((s) => s).join(" ");
  }

  /** Распознаёт одну волну (не длиннее предела модели). */
  async transcribeWave(wave) {
    const { Tensor } = this.ort;
    const { values, frames } = this.features.compute(wave);
    if (frames <= 0) return "";

    const enc = this.encoder;
    const encOut = await enc.run({
      [enc.inputNames[0]]: new Tensor("float32", values, [1, this.cfg.features.nMels, frames]),
      [enc.inputNames[1]]: new Tensor("int64", BigInt64Array.of(BigInt(this.features.outLen(wave.length))), [1]),
    });
    const encoded = encOut[enc.outputNames[0]];
    const lenT = encOut[enc.outputNames[1]];
    const encD = encoded.dims[1], encT = encoded.dims[2];
    const encLen = Math.min(Number(lenT.data[0] ?? encT), encT);

    const ids = await this.greedyRNNT(encoded.data, encD, encT, encLen);
    return this.tokenizer.decode(ids);
  }

  /** Жадное декодирование RNN-T для одной записи.
   *
   *  Выход декодера зависит только от последней буквы и его состояния,
   *  а они меняются лишь когда буква выдана. Поэтому его результат
   *  держим до следующей буквы, а не считаем заново на каждом кадре:
   *  текст тот же, что у питона, а прогонов в разы меньше. */
  async greedyRNNT(encoded, encD, encT, encLen) {
    const { Tensor } = this.ort;
    const { predLayers, predHidden } = this.cfg;
    const blank = this.tokenizer.blankId;
    const dec = this.decoder, joint = this.joint;
    const stateShape = [predLayers, 1, predHidden];
    const zeros = new Float32Array(predLayers * predHidden);

    const hyp = [];
    let label = blank;
    let h = zeros, c = zeros;
    let started = false;           // до первой буквы состояние декодера нулевое
    let decOut = null;             // [g, h', c'] для текущих label/h/c

    const runDecoder = async () => {
      const out = await dec.run({
        [dec.inputNames[0]]: new Tensor("int64", BigInt64Array.of(BigInt(started ? label : blank)), [1, 1]),
        [dec.inputNames[1]]: new Tensor("float32", started ? h : zeros, stateShape),
        [dec.inputNames[2]]: new Tensor("float32", started ? c : zeros, stateShape),
      });
      return dec.outputNames.map((n) => out[n].data);
    };

    for (let t = 0; t < encLen; t++) {
      // кадр энкодера: элемент (0, d, t) лежит по адресу d*encT + t
      const frame = new Float32Array(encD);
      for (let d = 0; d < encD; d++) frame[d] = encoded[d * encT + t];
      const frameT = new Tensor("float32", frame, [1, encD, 1]);

      for (let s = 0; s < MAX_SYMBOLS_PER_FRAME; s++) {
        if (!decOut) decOut = await runDecoder();

        // g приходит как [1,1,320], джойнту нужен [1,320,1] —
        // числа те же, меняется только объявленная форма
        const out = await joint.run({
          [joint.inputNames[0]]: frameT,
          [joint.inputNames[1]]: new Tensor("float32", decOut[0], [1, predHidden, 1]),
        });
        const logits = out[joint.outputNames[0]].data;
        let best = 0, bestValue = -Infinity;
        for (let i = 0; i < logits.length; i++) {
          if (logits[i] > bestValue) { bestValue = logits[i]; best = i; }
        }
        if (best === blank) break;

        hyp.push(best);
        label = best;
        h = decOut[1];
        c = decOut[2];
        started = true;
        decOut = null;             // состояние сменилось — декодер нужен заново
      }
    }
    return hyp;
  }
}
