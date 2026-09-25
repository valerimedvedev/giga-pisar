package ru.gigapisar.engine

import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.ln
import kotlin.math.log10
import kotlin.math.max
import kotlin.math.min
import kotlin.math.pow

/**
 * Звуковая волна → лог-мел-спектрограмма, ровно как ждёт энкодер GigaAM.
 *
 * Самое хрупкое место: разойтись здесь с оригиналом (server/giga_core.py,
 * web/giga/features.js) значит получить бессмыслицу. Поэтому всё шаг в шаг,
 * включая места, где числа округляются до float32 — numpy считает окно,
 * кадры, спектр и мел-полосы именно в такой точности.
 *
 * Преобразование Фурье — умножение на матрицу синусов и косинусов: длина
 * окна 320 не степень двойки, а на таких размерах матрица честна и быстра.
 */
data class FeatureConfig(
    val sampleRate: Int = 16000,
    val nMels: Int = 64,
    val nFFT: Int = 320,
    val winLength: Int = 320,
    val hopLength: Int = 160,
    val center: Boolean = false,
)

class Features(val cfg: FeatureConfig = FeatureConfig()) {
    val nFreqs = cfg.nFFT / 2 + 1
    private val window = FloatArray(cfg.winLength) { n -> (0.5 - 0.5 * cos(2.0 * PI * n / cfg.winLength)).toFloat() }
    private val cosT = DoubleArray(nFreqs * cfg.nFFT)
    private val sinT = DoubleArray(nFreqs * cfg.nFFT)
    private val fb: FloatArray

    init {
        val n = cfg.nFFT
        for (k in 0 until nFreqs) for (j in 0 until n) {
            val a = 2.0 * PI * ((k * j) % n) / n     // (k·n) mod N — угол маленький и точный
            cosT[k * n + j] = cos(a)
            sinT[k * n + j] = -kotlin.math.sin(a)
        }
        fb = melFilterbank(nFreqs, 0.0, cfg.sampleRate / 2.0, cfg.nMels, cfg.sampleRate)
    }

    /** Сколько кадров получится — та же формула, что в оригинале. */
    fun outLen(samples: Int): Int =
        if (cfg.center) samples / cfg.hopLength + 1 else (samples - cfg.winLength) / cfg.hopLength + 1

    class Result(val values: FloatArray, val frames: Int)

    /** Волна (16 кГц, float) → [nMels × кадры] подряд по строкам. */
    fun compute(wave: FloatArray): Result {
        val nFFT = cfg.nFFT; val hop = cfg.hopLength; val nMels = cfg.nMels
        var x = wave
        if (cfg.center) {
            val pad = nFFT / 2
            val padded = FloatArray(x.size + 2 * pad)
            for (i in 0 until pad) padded[i] = x[min(pad - i, x.size - 1)]
            System.arraycopy(x, 0, padded, pad, x.size)
            for (i in 0 until pad) padded[pad + x.size + i] = x[max(0, x.size - 2 - i)]
            x = padded
        }
        val n = max(0, (x.size - nFFT) / hop + 1)
        if (n == 0) return Result(FloatArray(0), 0)
        val frame = FloatArray(nFFT)
        val power = FloatArray(nFreqs)
        val out = FloatArray(nMels * n)
        for (f in 0 until n) {
            val off = f * hop
            for (j in 0 until nFFT) frame[j] = x[off + j] * window[j]
            for (k in 0 until nFreqs) {
                val row = k * nFFT
                var re = 0.0; var im = 0.0
                for (j in 0 until nFFT) {
                    val v = frame[j].toDouble()
                    re += v * cosT[row + j]
                    im += v * sinT[row + j]
                }
                power[k] = (re * re + im * im).toFloat()
            }
            for (m in 0 until nMels) {
                var s = 0.0
                for (k in 0 until nFreqs) s += power[k].toDouble() * fb[k * nMels + m].toDouble()
                val mel = min(max(s.toFloat(), 1e-9f), 1e9f)
                out[m * n + f] = ln(mel.toDouble()).toFloat()
            }
        }
        return Result(out, n)
    }

    companion object {
        private fun hzToMel(f: Double) = 2595.0 * log10(1.0 + f / 700.0)
        private fun melToHz(m: Double) = 700.0 * (10.0.pow(m / 2595.0) - 1.0)

        /** Треугольные фильтры, как torchaudio.functional.melscale_fbanks (norm=None). */
        fun melFilterbank(nFreqs: Int, fMin: Double, fMax: Double, nMels: Int, sampleRate: Int): FloatArray {
            val top = (sampleRate / 2).toDouble()
            val allFreqs = DoubleArray(nFreqs) { i -> top * i / (nFreqs - 1) }
            val mMin = hzToMel(fMin); val mMax = hzToMel(fMax)
            val fPts = DoubleArray(nMels + 2) { i -> melToHz(mMin + (mMax - mMin) * i / (nMels + 1)) }
            val fDiff = DoubleArray(nMels + 1) { i -> fPts[i + 1] - fPts[i] }
            val out = FloatArray(nFreqs * nMels)
            for (i in 0 until nFreqs) for (m in 0 until nMels) {
                val down = -(fPts[m] - allFreqs[i]) / fDiff[m]
                val up = (fPts[m + 2] - allFreqs[i]) / fDiff[m + 1]
                out[i * nMels + m] = max(0.0, min(down, up)).toFloat()
            }
            return out
        }
    }
}
