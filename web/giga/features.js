// Превращение звуковой волны в лог-мел-спектрограмму — ровно то, что ждёт энкодер.
//
// Самое хрупкое место всего распознавания: разойтись здесь с оригиналом
// значит получить на выходе не «чуть хуже», а бессмыслицу. Поэтому всё
// повторяет питоновский server/giga_core.py шаг в шаг, включая места,
// где числа округляются до float32 (Math.fround): numpy считает окно,
// кадры, спектр и мел-полосы именно в такой точности.
//
// Преобразование Фурье — умножение на заранее посчитанную матрицу синусов
// и косинусов, как в swift/Features.swift: длина окна 320 не степень двойки,
// а на таких размерах матрица честна, проста и достаточно быстра.

export const DEFAULT_FEATURES = {
  sampleRate: 16000,
  nMels: 64,
  nFFT: 320,
  winLength: 320,
  hopLength: 160,
  center: false,
};

/** Шкала мелов, вариант htk — по умолчанию именно он. */
const hzToMel = (f) => 2595.0 * Math.log10(1.0 + f / 700.0);
const melToHz = (m) => 700.0 * (Math.pow(10.0, m / 2595.0) - 1.0);

/** Треугольные фильтры, как в torchaudio.functional.melscale_fbanks (norm=None).
 *  Считаются в двойной точности и хранятся в float32 — как у numpy. */
function melFilterbank(nFreqs, fMin, fMax, nMels, sampleRate) {
  const top = Math.floor(sampleRate / 2);
  const allFreqs = new Float64Array(nFreqs);
  for (let i = 0; i < nFreqs; i++) allFreqs[i] = (top * i) / (nFreqs - 1);

  const mMin = hzToMel(fMin), mMax = hzToMel(fMax);
  const fPts = new Float64Array(nMels + 2);
  for (let i = 0; i < nMels + 2; i++) fPts[i] = melToHz(mMin + ((mMax - mMin) * i) / (nMels + 1));
  const fDiff = new Float64Array(nMels + 1);
  for (let i = 0; i < nMels + 1; i++) fDiff[i] = fPts[i + 1] - fPts[i];

  const out = new Float32Array(nFreqs * nMels);        // [nFreqs × nMels]
  for (let i = 0; i < nFreqs; i++) {
    for (let m = 0; m < nMels; m++) {
      const down = -(fPts[m] - allFreqs[i]) / fDiff[m];
      const up = (fPts[m + 2] - allFreqs[i]) / fDiff[m + 1];
      out[i * nMels + m] = Math.max(0.0, Math.min(down, up));
    }
  }
  return out;
}

export class Features {
  constructor(cfg = DEFAULT_FEATURES) {
    this.cfg = { ...DEFAULT_FEATURES, ...cfg };
    const { nFFT, winLength, sampleRate, nMels } = this.cfg;
    this.nFreqs = Math.floor(nFFT / 2) + 1;

    // Окно Ханна, периодическое — как torch.hann_window
    this.window = new Float32Array(winLength);
    for (let n = 0; n < winLength; n++) {
      this.window[n] = 0.5 - 0.5 * Math.cos((2.0 * Math.PI * n) / winLength);
    }

    // Матрицы преобразования Фурье, [nFreqs × nFFT] — строка на частоту,
    // чтобы внутренний цикл шёл по памяти подряд. Двойная точность:
    // numpy.fft.rfft над float32 тоже считает в double.
    this.cosT = new Float64Array(this.nFreqs * nFFT);
    this.sinT = new Float64Array(this.nFreqs * nFFT);
    for (let k = 0; k < this.nFreqs; k++) {
      for (let n = 0; n < nFFT; n++) {
        // (k·n) mod N — угол остаётся маленьким и точным
        const a = (2.0 * Math.PI * ((k * n) % nFFT)) / nFFT;
        this.cosT[k * nFFT + n] = Math.cos(a);
        this.sinT[k * nFFT + n] = -Math.sin(a);
      }
    }

    this.fb = melFilterbank(this.nFreqs, 0, sampleRate / 2, nMels, sampleRate);
  }

  /** Сколько кадров получится — та же формула, что в оригинале. */
  outLen(samples) {
    const { center, hopLength, winLength } = this.cfg;
    return center
      ? Math.floor(samples / hopLength) + 1
      : Math.floor((samples - winLength) / hopLength) + 1;
  }

  /** Волна (Float32Array, 16 кГц) → { values: [nMels × кадры] подряд по строкам, frames }. */
  compute(wave) {
    const { nFFT, hopLength, nMels, center } = this.cfg;
    const nFreqs = this.nFreqs;

    let x = wave;
    if (center) {
      // отражение краёв — np.pad(mode="reflect")
      const pad = Math.floor(nFFT / 2);
      const padded = new Float32Array(x.length + 2 * pad);
      for (let i = 0; i < pad; i++) padded[i] = x[Math.min(pad - i, x.length - 1)];
      padded.set(x, pad);
      for (let i = 0; i < pad; i++) padded[pad + x.length + i] = x[Math.max(0, x.length - 2 - i)];
      x = padded;
    }

    const n = Math.max(0, Math.floor((x.length - nFFT) / hopLength) + 1);
    if (n === 0) return { values: new Float32Array(0), frames: 0 };

    const frame = new Float32Array(nFFT);
    const power = new Float32Array(nFreqs);
    const out = new Float32Array(nMels * n);             // энкодер ждёт [1, nMels, кадры]
    const { cosT, sinT, fb, window } = this;

    for (let f = 0; f < n; f++) {
      // кадр с наложенным окном (float32, как frames у numpy)
      const off = f * hopLength;
      for (let j = 0; j < nFFT; j++) frame[j] = x[off + j] * window[j];

      // мощность = |rfft|², посчитанная в double и сохранённая во float32
      for (let k = 0; k < nFreqs; k++) {
        const row = k * nFFT;
        let re = 0, im = 0;
        for (let j = 0; j < nFFT; j++) {
          const v = frame[j];
          re += v * cosT[row + j];
          im += v * sinT[row + j];
        }
        power[k] = re * re + im * im;
      }

      // мел-полосы и логарифм с обрезкой, как в оригинале
      for (let m = 0; m < nMels; m++) {
        let s = 0;
        for (let k = 0; k < nFreqs; k++) s += power[k] * fb[k * nMels + m];
        const mel = Math.min(Math.max(Math.fround(s), 1e-9), 1e9);
        out[m * n + f] = Math.log(mel);
      }
    }
    return { values: out, frames: n };
  }
}
