package ru.gigapisar.engine

import kotlin.math.abs
import kotlin.math.pow

/** Нарезка длинных записей по паузам — как swift/Audio.swift и web/giga/audio.js. */
object Chunker {
    /** Середины пауз (в секундах): тише порога дольше заданного времени (ffmpeg silencedetect). */
    fun silences(x: FloatArray, rate: Int, noiseDb: Double = -35.0, minSeconds: Double = 0.3): List<Double> {
        val threshold = 10.0.pow(noiseDb / 20.0).toFloat()
        val minRun = (minSeconds * rate).toInt()
        val points = ArrayList<Double>()
        var start = -1
        for (i in x.indices) {
            if (abs(x[i]) < threshold) {
                if (start < 0) start = i
            } else if (start >= 0) {
                if (i - start >= minRun) points.add((start + i) / 2.0 / rate)
                start = -1
            }
        }
        return points
    }

    /** Границы кусков: не длиннее предела, разрез — по последней паузе перед ним. */
    fun chunkBounds(total: Double, silencePoints: List<Double>, maxChunk: Double): List<Pair<Double, Double>> {
        val bounds = ArrayList<Pair<Double, Double>>()
        var pos = 0.0
        while (total - pos > maxChunk) {
            val cut = silencePoints.filter { it > pos + 3 && it <= pos + maxChunk }.lastOrNull() ?: (pos + maxChunk)
            bounds.add(pos to cut)
            pos = cut
        }
        bounds.add(pos to total)
        return bounds
    }
}

/**
 * Живая нарезка во время записи — ради скорости: фраза распознаётся, пока
 * человек говорит следующую, и к моменту «Стоп» почти всё уже в поле.
 *
 * Кусок отдаётся, когда после речи наступила пауза [pauseSeconds], либо
 * когда накопилось [maxSeconds] (тогда режем по последней тишине).
 * Тишина без речи не отдаётся вовсе.
 */
class LiveChunker(
    private val rate: Int,
    private val pauseSeconds: Double = 0.7,
    private val minSpeechSeconds: Double = 0.4,
    private val maxSeconds: Double = 20.0,
    noiseDb: Double = -35.0,
) {
    private val threshold = 10.0.pow(noiseDb / 20.0).toFloat()
    private var buf = FloatArray(rate * 30)
    private var len = 0
    private var speech = 0           // отсчётов речи в текущем куске
    private var quiet = 0            // отсчётов тишины подряд в конце
    private var lastQuietMid = -1    // середина последней паузы (для принудительного разреза)

    /** Добавляет звук; возвращает готовый кусок или null. */
    fun push(samples: FloatArray, n: Int = samples.size): FloatArray? {
        if (len + n > buf.size) buf = buf.copyOf((len + n) * 2)
        for (i in 0 until n) {
            val v = samples[i]
            buf[len++] = v
            if (abs(v) < threshold) {
                quiet++
                if (quiet == (0.3 * rate).toInt()) lastQuietMid = len - quiet / 2
            } else {
                speech++
                quiet = 0
            }
        }
        val pauseRun = (pauseSeconds * rate).toInt()
        if (speech >= (minSpeechSeconds * rate).toInt() && quiet >= pauseRun) return cut(len - quiet / 2)
        if (len >= maxSeconds * rate) {
            if (speech < (minSpeechSeconds * rate).toInt()) { reset(); return null }   // сплошная тишина
            return cut(if (lastQuietMid > len / 4) lastQuietMid else len)
        }
        return null
    }

    /** Остаток при остановке записи (null, если речи не было). */
    fun flush(): FloatArray? {
        if (speech < (0.15 * rate).toInt()) { reset(); return null }
        return cut(len)
    }

    private fun cut(at: Int): FloatArray {
        val out = buf.copyOfRange(0, at)
        val rest = len - at
        System.arraycopy(buf, at, buf, 0, rest)
        len = rest
        speech = 0; quiet = 0; lastQuietMid = -1
        for (i in 0 until rest) if (abs(buf[i]) >= threshold) speech++ else quiet++
        return out
    }

    private fun reset() { len = 0; speech = 0; quiet = 0; lastQuietMid = -1 }
}
