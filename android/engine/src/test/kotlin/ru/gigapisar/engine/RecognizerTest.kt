package ru.gigapisar.engine

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assume.assumeTrue
import org.junit.Test
import java.io.File

/**
 * Сверка с питоновским ядром: GIGA_MODEL — папка модели, GIGA_WAV — папка с
 * записями и ref_transcripts.json (имя → расшифровка питоном). Без них тест
 * пропускается.
 */
class RecognizerTest {
    @Test fun matchesPython() {
        val model = System.getProperty("giga.model").orEmpty()
        val wav = System.getProperty("giga.wav").orEmpty()
        assumeTrue("нет GIGA_MODEL/GIGA_WAV", model.isNotEmpty() && wav.isNotEmpty() && Rnnt.hasModel(File(model)))
        val ref = JSONObject(File(wav, "ref_transcripts.json").readText())
        Rnnt.load(File(model), threads = 4).use { r ->
            var ok = 0; var n = 0
            for (name in ref.keys()) {
                val f = File(wav, name)
                if (!f.exists()) continue
                val a = Wav.read(f.readBytes())
                val samples = Wav.resample(a.samples, a.rate, r.sampleRate)
                val t0 = System.nanoTime()
                val got = r.transcribe(samples)
                val ms = (System.nanoTime() - t0) / 1_000_000
                val want = ref.getString(name)
                n++
                if (got == want) ok++ else println("✗ $name\n   питон:  $want\n   kotlin: $got")
                println("${if (got == want) "✓" else "✗"} $name (${a.samples.size / a.rate} с звука → $ms мс): $got")
            }
            assertEquals("совпало $ok из $n", n, ok)
        }
    }

    @Test fun liveChunkerCutsOnPauses() {
        val rate = 16000
        val c = LiveChunker(rate, pauseSeconds = 0.5, minSpeechSeconds = 0.3)
        val speech = FloatArray(rate) { if (it % 2 == 0) 0.3f else -0.3f }
        val silence = FloatArray(rate)
        assertEquals(null, c.push(silence))          // тишина без речи — ничего
        assertEquals(null, c.push(speech))
        val chunk = c.push(silence)!!               // речь + пауза → кусок
        assert(chunk.size in (rate * 1.4).toInt()..(rate * 2.8).toInt()) { "размер ${chunk.size}" }
        assertEquals(null, c.flush())               // остаток — одна тишина
        c.push(speech.copyOf(rate / 2))
        assert(c.flush()!!.isNotEmpty())            // недоговорённая фраза отдаётся при остановке
    }
}
