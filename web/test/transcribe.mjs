// Прогоняет браузерное ядро в Node — для сверки с питоновским.
//
//   cd web/test && npm install
//   node transcribe.mjs <папка-модели> запись.wav [ещё.wav ...]
//
// Печатает по строке текста на файл, в том же порядке. Тот же код,
// что работает в браузере, и тот же onnxruntime-web (на WebAssembly).

import { readFileSync } from "node:fs";
import { join } from "node:path";
import * as ort from "onnxruntime-web";
import { Recognizer, MODEL_FILES } from "../giga/recognizer.js";
import { readWav } from "../giga/audio.js";

const [modelDir, ...wavs] = process.argv.slice(2);
if (!modelDir || wavs.length === 0) {
  console.error("node transcribe.mjs <папка-модели> запись.wav [ещё.wav ...]");
  process.exit(2);
}

ort.env.wasm.numThreads = 1;   // как в браузере без изоляции страницы
const read = (name) => readFileSync(join(modelDir, name));
const recognizer = await Recognizer.create(ort, {
  yaml: read(MODEL_FILES.yaml).toString("utf8"),
  encoder: read(MODEL_FILES.encoder),
  decoder: read(MODEL_FILES.decoder),
  joint: read(MODEL_FILES.joint),
  tokenizer: read(MODEL_FILES.tokenizer),
});

for (const path of wavs) {
  const { samples, rate } = readWav(readFileSync(path));
  if (rate !== recognizer.sampleRate) {
    console.error(`${path}: нужен звук ${recognizer.sampleRate} Гц, а тут ${rate}`);
    process.exit(1);
  }
  const t0 = performance.now();
  const text = await recognizer.transcribe(samples);
  const ms = performance.now() - t0;
  console.error(`${path}: ${(samples.length / rate).toFixed(1)} с звука за ${(ms / 1000).toFixed(2)} с`);
  console.log(text);
}
