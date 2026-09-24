// Запись с микрофона — как в swift/Mic.swift: берём звук в РОДНОЙ частоте
// устройства, а в 16 кГц для модели пересчитываем сами, когда запись
// закончена. Микрофон открыт только между start() и stop().
//
// Обработку браузера (шумодав, эхоподавление, автогромкость) выключаем:
// модель училась на живом звуке, а шумодав съедает тихие согласные.

const TARGET_RATE = 16000;
const TAIL_MS = 200;   // хвост после «Стоп»: последний слог не обрезается

// Процессор в звуковом потоке: сводит каналы в моно и отдаёт куски наружу.
const WORKLET = `
class GigaTap extends AudioWorkletProcessor {
  process(inputs) {
    const chans = inputs[0];
    if (chans && chans.length) {
      const n = chans[0].length;
      const mono = new Float32Array(n);
      for (const ch of chans) for (let i = 0; i < n; i++) mono[i] += ch[i];
      if (chans.length > 1) for (let i = 0; i < n; i++) mono[i] /= chans.length;
      this.port.postMessage(mono, [mono.buffer]);
    }
    return true;
  }
}
registerProcessor("giga-tap", GigaTap);
`;

export class Mic {
  constructor() {
    this.recording = false;
    this.chunks = [];
    this.rate = 0;
  }

  /** Открывает микрофон и начинает писать. Бросает, если доступа нет. */
  async start() {
    if (this.recording) return;
    if (!navigator.mediaDevices?.getUserMedia) {
      throw new Error("браузер не даёт микрофон: страница должна открываться по https или с localhost");
    }
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: {
        channelCount: { ideal: 1 },
        echoCancellation: false,
        noiseSuppression: false,
        autoGainControl: false,
      },
    });
    const ctx = new AudioContext();                  // родная частота устройства
    try {
      const url = URL.createObjectURL(new Blob([WORKLET], { type: "text/javascript" }));
      try { await ctx.audioWorklet.addModule(url); } finally { URL.revokeObjectURL(url); }
      const source = ctx.createMediaStreamSource(stream);
      const tap = new AudioWorkletNode(ctx, "giga-tap", { numberOfOutputs: 1 });
      this.chunks = [];
      tap.port.onmessage = ({ data }) => { if (this.recording) this.chunks.push(data); };
      source.connect(tap);
      tap.connect(ctx.destination);                  // тишина, но так узел точно крутится
      if (ctx.state === "suspended") await ctx.resume();
      this.stream = stream;
      this.ctx = ctx;
      this.rate = ctx.sampleRate;
      this.recording = true;
      this.startedAt = performance.now();
    } catch (e) {
      stream.getTracks().forEach((t) => t.stop());
      ctx.close();
      throw e;
    }
  }

  /** Сколько секунд уже записано. */
  get seconds() {
    return this.recording ? (performance.now() - this.startedAt) / 1000 : 0;
  }

  /** Останавливает запись и отдаёт звук: Float32Array, 16 кГц, моно. */
  async stop() {
    if (!this.recording) return new Float32Array(0);
    await new Promise((r) => setTimeout(r, TAIL_MS));
    this.recording = false;
    this.stream.getTracks().forEach((t) => t.stop());
    await this.ctx.close();
    this.stream = this.ctx = null;
    return resample(concat(this.chunks), this.rate, TARGET_RATE);
  }

  /** Бросает запись, ничего не распознавая. */
  async cancel() {
    if (!this.recording) return;
    this.recording = false;
    this.stream.getTracks().forEach((t) => t.stop());
    await this.ctx.close();
    this.stream = this.ctx = null;
    this.chunks = [];
  }
}

function concat(chunks) {
  const n = chunks.reduce((s, c) => s + c.length, 0);
  const out = new Float32Array(n);
  let off = 0;
  for (const c of chunks) { out.set(c, off); off += c.length; }
  return out;
}

/** Пересчёт частоты штатным ресемплером браузера (OfflineAudioContext). */
export async function resample(x, from, to) {
  if (x.length === 0 || from === to) return x;
  const length = Math.ceil((x.length * to) / from);
  const off = new OfflineAudioContext(1, length, to);
  const buf = off.createBuffer(1, x.length, from);
  buf.copyToChannel(x, 0);
  const src = off.createBufferSource();
  src.buffer = buf;
  src.connect(off.destination);
  src.start();
  const out = await off.startRendering();
  return out.getChannelData(0).slice();
}
