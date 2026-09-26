package ru.gigapisar.app

import java.io.File
import java.io.RandomAccessFile

/**
 * Диктофон: пишет звук микрофона в wav (16 кГц, моно, 16 бит) по мере записи,
 * чтобы длинная запись не держалась в памяти и не пропала при сбое.
 * Заголовок дописывается при закрытии (и при паузе — файл валиден в любой момент).
 */
class Recorder(val file: File) {
    private val raf = RandomAccessFile(file, "rw")
    private var dataBytes = 0L
    val seconds get() = dataBytes / 2.0 / RATE

    init {
        raf.setLength(0)
        raf.write(ByteArray(44))          // место под заголовок
    }

    fun write(samples: FloatArray, n: Int) {
        val b = ByteArray(n * 2)
        for (i in 0 until n) {
            val v = (samples[i].coerceIn(-1f, 1f) * 32767f).toInt()
            b[i * 2] = (v and 0xff).toByte(); b[i * 2 + 1] = (v shr 8 and 0xff).toByte()
        }
        raf.seek(44 + dataBytes)
        raf.write(b)
        dataBytes += b.size
    }

    /** Заголовок — по текущей длине; можно вызывать сколько угодно раз. */
    fun flushHeader() {
        raf.seek(0)
        raf.write(header(dataBytes))
    }

    fun close() { flushHeader(); raf.close() }

    companion object {
        const val RATE = 16000

        fun header(dataBytes: Long): ByteArray {
            val b = java.nio.ByteBuffer.allocate(44).order(java.nio.ByteOrder.LITTLE_ENDIAN)
            b.put("RIFF".toByteArray()).putInt((36 + dataBytes).toInt()).put("WAVE".toByteArray())
            b.put("fmt ".toByteArray()).putInt(16).putShort(1).putShort(1).putInt(RATE).putInt(RATE * 2).putShort(2).putShort(16)
            b.put("data".toByteArray()).putInt(dataBytes.toInt())
            return b.array()
        }

        /** Длительность wav-файла в секундах (по размеру данных). */
        fun durationOf(f: File): Double = maxOf(0L, f.length() - 44) / 2.0 / RATE
    }
}
