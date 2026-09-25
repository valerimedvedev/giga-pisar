package ru.gigapisar.app

import android.annotation.SuppressLint
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
import java.io.IOException
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Микрофон → волна 16 кГц кусками по 20 мс. Источник VOICE_RECOGNITION —
 * без «улучшайзеров», которые портят звук для распознавания.
 */
class Mic(private val onSamples: (FloatArray, Int) -> Unit) {
    private val running = AtomicBoolean(false)
    private var thread: Thread? = null

    @SuppressLint("MissingPermission")
    fun start() {
        if (running.getAndSet(true)) return
        val rate = 16000
        val min = AudioRecord.getMinBufferSize(rate, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT)
        if (min <= 0) { running.set(false); throw IOException("микрофон не даёт 16 кГц") }
        val rec = AudioRecord(MediaRecorder.AudioSource.VOICE_RECOGNITION, rate, AudioFormat.CHANNEL_IN_MONO,
            AudioFormat.ENCODING_PCM_16BIT, maxOf(min, rate / 2 * 2))
        if (rec.state != AudioRecord.STATE_INITIALIZED) { rec.release(); running.set(false); throw IOException("микрофон занят") }
        thread = Thread({
            val pcm = ShortArray(rate / 50)          // 20 мс
            val out = FloatArray(pcm.size)
            try {
                rec.startRecording()
                while (running.get()) {
                    val n = rec.read(pcm, 0, pcm.size)
                    if (n <= 0) continue
                    for (i in 0 until n) out[i] = pcm[i] / 32768f
                    onSamples(out, n)
                }
            } finally {
                try { rec.stop() } catch (_: Exception) {}
                rec.release()
            }
        }, "mic").apply { priority = Thread.MAX_PRIORITY; start() }
    }

    fun stop() {
        running.set(false)
        thread?.join(1000)
        thread = null
    }
}
