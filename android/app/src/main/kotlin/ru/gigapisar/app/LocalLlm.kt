package ru.gigapisar.app

import ru.gigapisar.engine.Brain
import ru.gigapisar.engine.ChatMessage
import java.io.File
import java.io.IOException

/**
 * Нейронка прямо на телефоне — llama.cpp через JNI (src/main/cpp/pisar_llm.cpp).
 * Модель держится загруженной между запросами: загрузка занимает секунды,
 * а правка одной фразы — доли секунды.
 */
object LocalLlm {
    init { System.loadLibrary("pisar_llm") }

    @JvmStatic private external fun nativeInit()
    @JvmStatic private external fun nativeLoad(path: String, threads: Int, ctx: Int): Int
    @JvmStatic private external fun nativeLoaded(): Boolean
    @JvmStatic private external fun nativeComplete(system: String, user: String, maxTokens: Int, temperature: Float): String
    @JvmStatic private external fun nativeCompleteChat(roles: Array<String>, contents: Array<String>, maxTokens: Int, temperature: Float): String
    @JvmStatic private external fun nativeCancel()
    @JvmStatic private external fun nativeUnload()

    @Volatile var loadedPath: String? = null
        private set
    private var inited = false

    @Synchronized
    fun ensureLoaded(model: File, threads: Int, ctx: Int = 4096) {
        if (!inited) { nativeInit(); inited = true }
        if (loadedPath == model.path && nativeLoaded()) return
        when (nativeLoad(model.path, threads, ctx)) {
            0 -> loadedPath = model.path
            1 -> throw IOException("модель не открылась: ${model.name}")
            else -> throw IOException("не хватило памяти под нейронку ${model.name}")
        }
    }

    /** Ответ на беседу: системная инструкция и все реплики по порядку. */
    @Synchronized
    fun chat(messages: List<ChatMessage>, maxTokens: Int = 1024, temperature: Float = 0.3f): String {
        check(nativeLoaded()) { "нейронка не загружена" }
        val out = nativeCompleteChat(messages.map { it.role }.toTypedArray(), messages.map { it.content }.toTypedArray(), maxTokens, temperature)
        if (out.isEmpty()) throw IOException("нейронка ничего не ответила")
        return Brain.stripThinking(out).trim()
    }

    fun cancel() { if (inited) nativeCancel() }

    @Synchronized
    fun unload() { if (inited && nativeLoaded()) nativeUnload(); loadedPath = null }
}
