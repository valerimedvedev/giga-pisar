package ru.gigapisar.engine

import org.json.JSONArray
import org.json.JSONObject
import java.io.IOException
import java.net.HttpURLConnection
import java.net.SocketTimeoutException
import java.net.URL

/**
 * Запрос к любому OpenAI-совместимому серверу: GigaBrain на компьютере,
 * Ollama, LM Studio, llama-server с GigaChat на сервере сайта.
 */
object OpenAiChat {
    fun normBase(u: String) = u.trim().trimEnd('/').removeSuffix("/v1")

    private fun open(url: String, key: String, method: String, timeoutMs: Int): HttpURLConnection {
        val c = URL(url).openConnection() as HttpURLConnection
        c.requestMethod = method
        c.connectTimeout = minOf(timeoutMs, 10_000)
        c.readTimeout = timeoutMs
        if (key.isNotBlank()) c.setRequestProperty("Authorization", "Bearer $key")
        return c
    }

    /** Список моделей; бросает, если сервер не отвечает или нужен ключ. */
    fun listModels(base: String, key: String = "", timeoutMs: Int = 3000): List<String> {
        val c = open(normBase(base) + "/v1/models", key, "GET", timeoutMs)
        try {
            val code = c.responseCode
            if (code == 401 || code == 403) throw NeedsKey()
            if (code != 200) throw IOException("ответил $code")
            val j = JSONObject(c.inputStream.bufferedReader().readText())
            val arr: JSONArray = j.optJSONArray("data") ?: j.optJSONArray("models") ?: JSONArray()
            return (0 until arr.length()).mapNotNull { i ->
                val m = arr.optJSONObject(i) ?: return@mapNotNull null
                (m.optString("id").ifEmpty { m.optString("name") }.ifEmpty { m.optString("model") }).ifEmpty { null }
            }
        } finally { c.disconnect() }
    }

    class NeedsKey : IOException("нужен ключ доступа")

    /** Ответ нейронки на сообщения. Думать вслух запрещаем (enable_thinking=false). */
    fun chat(base: String, messages: List<ChatMessage>, key: String = "", model: String = "",
             temperature: Double = 0.3, maxTokens: Int = 2048, timeoutMs: Int = 120_000): String {
        val body = JSONObject().apply {
            if (model.isNotBlank()) put("model", model)
            put("messages", JSONArray().apply { messages.forEach { put(JSONObject().put("role", it.role).put("content", it.content)) } })
            put("temperature", temperature)
            put("max_tokens", maxTokens)
            put("stream", false)
            put("chat_template_kwargs", JSONObject().put("enable_thinking", false))
        }.toString()
        val c = open(normBase(base) + "/v1/chat/completions", key, "POST", timeoutMs)
        try {
            c.doOutput = true
            c.setRequestProperty("Content-Type", "application/json")
            c.outputStream.use { it.write(body.toByteArray()) }
            val code = c.responseCode
            if (code == 429) throw IOException("слишком много запросов подряд, подождите минуту")
            if (code == 401 || code == 403) throw NeedsKey()
            if (code != 200) throw IOException("сервер ответил $code")
            val j = JSONObject(c.inputStream.bufferedReader().readText())
            val text = j.optJSONArray("choices")?.optJSONObject(0)?.optJSONObject("message")?.optString("content") ?: ""
            return Brain.stripThinking(text).trim()
        } catch (e: SocketTimeoutException) {
            throw IOException("сервер не ответил за ${timeoutMs / 1000} с")
        } finally { c.disconnect() }
    }
}
