// Нарезка длинных записей на куски по паузам и чтение wav.
// Повторяет swift/Audio.swift.

/** Середины пауз (в секундах) — кандидаты в точки разреза.
 *  Повторяет ffmpeg silencedetect: тише порога дольше заданного времени. */
export function silences(x, rate, noiseDb = -35, minSeconds = 0.3) {
  const threshold = Math.pow(10.0, noiseDb / 20.0);
  const minRun = Math.floor(minSeconds * rate);
  const points = [];
  let start = -1;
  for (let i = 0; i < x.length; i++) {
    if (Math.abs(x[i]) < threshold) {
      if (start < 0) start = i;
    } else if (start >= 0) {
      if (i - start >= minRun) points.push((start + i) / 2.0 / rate);
      start = -1;
    }
  }
  return points;
}

/** Границы кусков: не длиннее предела, разрез — по последней паузе перед ним. */
export function chunkBounds(total, silencePoints, maxChunk) {
  const bounds = [];
  let pos = 0.0;
  while (total - pos > maxChunk) {
    const candidates = silencePoints.filter((s) => s > pos + 3 && s <= pos + maxChunk);
    const cut = candidates.length ? candidates[candidates.length - 1] : pos + maxChunk;
    bounds.push([pos, cut]);
    pos = cut;
  }
  bounds.push([pos, total]);
  return bounds;
}

/** Читает 16-битный wav (первый канал). Нужен сверке и тестам; в браузере
 *  звук приходит с микрофона уже числами. */
export function readWav(bytes) {
  const d = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
  const v = new DataView(d.buffer, d.byteOffset, d.byteLength);
  if (d.length <= 44 || v.getUint32(0, true) !== 0x46464952) throw new Error("это не wav");
  let rate = 16000, channels = 1, bits = 16;
  let i = 12;
  while (i + 8 <= d.length) {
    const id = v.getUint32(i, true), size = v.getUint32(i + 4, true), body = i + 8;
    if (id === 0x20746d66) {                         // "fmt "
      channels = v.getUint16(body + 2, true);
      rate = v.getUint32(body + 4, true);
      bits = v.getUint16(body + 14, true);
    } else if (id === 0x61746164) {                  // "data"
      if (bits !== 16) throw new Error(`нужен 16-битный wav, а тут ${bits}`);
      const end = Math.min(body + size, d.length);
      const n = Math.floor((end - body) / 2 / Math.max(1, channels));
      const out = new Float32Array(n);
      for (let k = 0; k < n; k++) out[k] = v.getInt16(body + k * 2 * channels, true) / 32768.0;
      return { samples: out, rate };
    }
    i = body + size + (size % 2);
  }
  throw new Error("в wav нет данных");
}
