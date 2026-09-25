package ru.gigapisar.engine

import java.nio.ByteBuffer
import java.nio.ByteOrder

/** 16-битный wav (первый канал) → волна. Нужен сверке и тестам. */
object Wav {
    class Audio(val samples: FloatArray, val rate: Int)

    fun read(bytes: ByteArray): Audio {
        val v = ByteBuffer.wrap(bytes).order(ByteOrder.LITTLE_ENDIAN)
        require(bytes.size > 44 && v.getInt(0) == 0x46464952) { "это не wav" }
        var rate = 16000; var channels = 1; var bits = 16
        var i = 12
        while (i + 8 <= bytes.size) {
            val id = v.getInt(i); val size = v.getInt(i + 4); val body = i + 8
            if (id == 0x20746d66) {
                channels = v.getShort(body + 2).toInt(); rate = v.getInt(body + 4); bits = v.getShort(body + 14).toInt()
            } else if (id == 0x61746164) {
                require(bits == 16) { "нужен 16-битный wav, а тут $bits" }
                val end = minOf(body + size, bytes.size)
                val n = (end - body) / 2 / maxOf(1, channels)
                val out = FloatArray(n) { k -> v.getShort(body + k * 2 * channels) / 32768f }
                return Audio(out, rate)
            }
            i = body + size + (size % 2)
        }
        error("в wav нет данных")
    }

    /** Простейшая передискретизация к 16 кГц (линейная) — на случай микрофона без 16 кГц. */
    fun resample(x: FloatArray, from: Int, to: Int): FloatArray {
        if (from == to) return x
        val n = (x.size.toLong() * to / from).toInt()
        val out = FloatArray(n)
        val step = from.toDouble() / to
        for (i in 0 until n) {
            val p = i * step; val j = p.toInt(); val f = (p - j).toFloat()
            val a = x[minOf(j, x.size - 1)]; val b = x[minOf(j + 1, x.size - 1)]
            out[i] = a + (b - a) * f
        }
        return out
    }
}
