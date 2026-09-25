package ru.gigapisar.engine

import ai.onnxruntime.OnnxTensor
import ai.onnxruntime.OrtEnvironment
import ai.onnxruntime.OrtSession
import java.io.File
import java.nio.FloatBuffer
import java.nio.LongBuffer

/**
 * Распознавание речи: звук → признаки → энкодер → жадное декодирование RNN-T → текст.
 * Повторяет server/giga_core.py и web/giga/recognizer.js шаг в шаг; тот же
 * пакет ai.onnxruntime работает и на JVM (сверка), и на Android.
 */
class Rnnt private constructor(
    private val env: OrtEnvironment,
    val config: ModelConfig,
    private val encoder: OrtSession,
    private val decoder: OrtSession,
    private val joint: OrtSession,
    val tokenizer: Tokenizer,
) : AutoCloseable {
    class ModelConfig(val features: FeatureConfig, val predHidden: Int, val predLayers: Int)

    private val features = Features(config.features)
    val sampleRate get() = config.features.sampleRate
    private val encIn = encoder.inputInfo.keys.toList()
    private val encOut = encoder.outputInfo.keys.toList()
    private val decIn = decoder.inputInfo.keys.toList()
    private val decOut = decoder.outputInfo.keys.toList()
    private val jIn = joint.inputInfo.keys.toList()

    /** Распознаёт запись любой длины: длинная режется по паузам и склеивается. */
    fun transcribe(samples: FloatArray): String {
        val rate = sampleRate
        val total = samples.size.toDouble() / rate
        if (total <= MAX_CHUNK + 1) return transcribeWave(samples)
        val parts = ArrayList<String>()
        for ((a, b) in Chunker.chunkBounds(total, Chunker.silences(samples, rate), MAX_CHUNK)) {
            val from = minOf(samples.size, (a * rate).toInt()); val to = minOf(samples.size, (b * rate).toInt())
            if (to > from) transcribeWave(samples.copyOfRange(from, to)).takeIf { it.isNotEmpty() }?.let(parts::add)
        }
        return parts.joinToString(" ")
    }

    /** Одна волна не длиннее предела модели. */
    @Synchronized
    fun transcribeWave(wave: FloatArray): String {
        val f = features.compute(wave)
        if (f.frames <= 0) return ""
        val nMels = config.features.nMels.toLong()
        OnnxTensor.createTensor(env, FloatBuffer.wrap(f.values), longArrayOf(1, nMels, f.frames.toLong())).use { x ->
            OnnxTensor.createTensor(env, LongBuffer.wrap(longArrayOf(features.outLen(wave.size).toLong())), longArrayOf(1)).use { len ->
                encoder.run(mapOf(encIn[0] to x, encIn[1] to len)).use { out ->
                    val enc = out[0] as OnnxTensor
                    val shape = enc.info.shape
                    val encD = shape[1].toInt(); val encT = shape[2].toInt()
                    val encLen = minOf(lengthOf(out[1] as OnnxTensor) ?: encT, encT)
                    val ids = greedy(enc.floatBuffer, encD, encT, encLen)
                    return tokenizer.decode(ids)
                }
            }
        }
    }

    /**
     * Жадное декодирование. Выход декодера зависит только от последней буквы
     * и состояния, поэтому считается заново лишь когда буква выдана.
     */
    private fun greedy(encoded: FloatBuffer, encD: Int, encT: Int, encLen: Int): IntArray {
        val blank = tokenizer.blankId
        val layers = config.predLayers; val hidden = config.predHidden
        val stateShape = longArrayOf(layers.toLong(), 1, hidden.toLong())
        val zeros = FloatArray(layers * hidden)
        val hyp = ArrayList<Int>()
        var label = blank
        var h = zeros; var c = zeros
        var started = false
        var g: FloatArray? = null; var hNext = zeros; var cNext = zeros
        val frame = FloatArray(encD)

        fun runDecoder() {
            val lab = OnnxTensor.createTensor(env, LongBuffer.wrap(longArrayOf((if (started) label else blank).toLong())), longArrayOf(1, 1))
            val hT = OnnxTensor.createTensor(env, FloatBuffer.wrap(if (started) h else zeros), stateShape)
            val cT = OnnxTensor.createTensor(env, FloatBuffer.wrap(if (started) c else zeros), stateShape)
            try {
                decoder.run(mapOf(decIn[0] to lab, decIn[1] to hT, decIn[2] to cT)).use { r ->
                    g = floats(r[0] as OnnxTensor); hNext = floats(r[1] as OnnxTensor); cNext = floats(r[2] as OnnxTensor)
                }
            } finally { lab.close(); hT.close(); cT.close() }
        }

        for (t in 0 until encLen) {
            for (d in 0 until encD) frame[d] = encoded.get(d * encT + t)
            OnnxTensor.createTensor(env, FloatBuffer.wrap(frame), longArrayOf(1, encD.toLong(), 1)).use { frameT ->
                for (s in 0 until MAX_SYMBOLS_PER_FRAME) {
                    if (g == null) runDecoder()
                    val gT = OnnxTensor.createTensor(env, FloatBuffer.wrap(g!!), longArrayOf(1, hidden.toLong(), 1))
                    val best: Int
                    try {
                        joint.run(mapOf(jIn[0] to frameT, jIn[1] to gT)).use { r ->
                            val logits = (r[0] as OnnxTensor).floatBuffer
                            var bi = 0; var bv = Float.NEGATIVE_INFINITY
                            val n = logits.remaining()
                            for (i in 0 until n) { val v = logits.get(i); if (v > bv) { bv = v; bi = i } }
                            best = bi
                        }
                    } finally { gT.close() }
                    if (best == blank) break
                    hyp.add(best)
                    label = best; h = hNext; c = cNext; started = true
                    g = null
                }
            }
        }
        return hyp.toIntArray()
    }

    override fun close() { encoder.close(); decoder.close(); joint.close() }

    companion object {
        const val MODEL_NAME = "v3_e2e_rnnt"
        val FILES = listOf("$MODEL_NAME.yaml", "${MODEL_NAME}_encoder.onnx", "${MODEL_NAME}_decoder.onnx",
            "${MODEL_NAME}_joint.onnx", "${MODEL_NAME}_tokenizer.model")
        const val MAX_CHUNK = 24.0            // предел одного прохода модели — 25 секунд
        const val MAX_SYMBOLS_PER_FRAME = 3

        /** Длина выхода энкодера: int64 или int32 — смотря как экспортирована модель. */
        private fun lengthOf(t: OnnxTensor): Int? = when (t.info.type) {
            ai.onnxruntime.OnnxJavaType.INT64 -> t.longBuffer?.takeIf { it.remaining() > 0 }?.get(0)?.toInt()
            ai.onnxruntime.OnnxJavaType.INT32 -> t.intBuffer?.takeIf { it.remaining() > 0 }?.get(0)
            else -> null
        }

        private fun floats(t: OnnxTensor): FloatArray { val b = t.floatBuffer; val a = FloatArray(b.remaining()); b.get(a); return a }

        fun hasModel(dir: File) = FILES.all { File(dir, it).length() > 0 }

        /** Разбор нужных строк yaml — все ключи уникальны по смыслу. */
        fun parseConfig(text: String): ModelConfig {
            fun value(key: String) = text.lineSequence().map { it.trim() }.firstOrNull { it.startsWith("$key:") }?.substring(key.length + 1)?.trim()
            fun int(key: String, def: Int) = value(key)?.toIntOrNull() ?: def
            val d = FeatureConfig()
            return ModelConfig(
                FeatureConfig(int("sample_rate", d.sampleRate), int("features", d.nMels), int("n_fft", d.nFFT),
                    int("win_length", d.winLength), int("hop_length", d.hopLength), value("center")?.let { it == "true" } ?: d.center),
                int("pred_hidden", 320), int("pred_rnn_layers", 1),
            )
        }

        /** Загружает модель из папки с пятью файлами. [threads] — потоков на энкодер. */
        fun load(dir: File, threads: Int = 4): Rnnt {
            val env = OrtEnvironment.getEnvironment()
            fun opts() = OrtSession.SessionOptions().apply {
                setOptimizationLevel(OrtSession.SessionOptions.OptLevel.ALL_OPT)
                setIntraOpNumThreads(threads)
            }
            val cfg = parseConfig(File(dir, FILES[0]).readText())
            val encoder = env.createSession(File(dir, FILES[1]).path, opts())
            // декодер и джойнт крошечные, их гоняют тысячи раз: один поток быстрее, чем раздача работы
            val small = OrtSession.SessionOptions().apply { setOptimizationLevel(OrtSession.SessionOptions.OptLevel.ALL_OPT); setIntraOpNumThreads(1) }
            val decoder = env.createSession(File(dir, FILES[2]).path, small)
            val joint = env.createSession(File(dir, FILES[3]).path, small)
            return Rnnt(env, cfg, encoder, decoder, joint, Tokenizer(File(dir, FILES[4]).readBytes()))
        }
    }
}
