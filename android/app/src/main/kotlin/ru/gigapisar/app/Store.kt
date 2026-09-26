package ru.gigapisar.app

import android.content.Context
import android.content.SharedPreferences
import org.json.JSONArray
import org.json.JSONObject
import ru.gigapisar.engine.Brain
import ru.gigapisar.engine.Chip

/** Настройки и история промптов — в SharedPreferences, всё на телефоне. */
class Store(context: Context) {
    private val p: SharedPreferences = context.getSharedPreferences("pisar", Context.MODE_PRIVATE)

    // ── мозг
    var brainMode: String get() = p.getString("brain.mode", "off")!!; set(v) = p.edit().putString("brain.mode", v).apply()
    var phoneModel: String get() = p.getString("phone.model", "")!!; set(v) = p.edit().putString("phone.model", v).apply()
    var pcBase: String get() = p.getString("pc.base", "")!!; set(v) = p.edit().putString("pc.base", v).apply()
    var pcKey: String get() = p.getString("pc.key", "")!!; set(v) = p.edit().putString("pc.key", v).apply()
    var pcModel: String get() = p.getString("pc.model", "")!!; set(v) = p.edit().putString("pc.model", v).apply()
    var serverBase: String get() = p.getString("srv.base", "")!!; set(v) = p.edit().putString("srv.base", v).apply()
    var serverKey: String get() = p.getString("srv.key", "")!!; set(v) = p.edit().putString("srv.key", v).apply()
    var cloudService: String get() = p.getString("cloud.service", "gemini")!!; set(v) = p.edit().putString("cloud.service", v).apply()
    var cloudBase: String get() = p.getString("cloud.base", "")!!; set(v) = p.edit().putString("cloud.base", v).apply()
    var cloudKey: String get() = p.getString("cloud.key", "")!!; set(v) = p.edit().putString("cloud.key", v).apply()
    var cloudModel: String get() = p.getString("cloud.model", "")!!; set(v) = p.edit().putString("cloud.model", v).apply()
    var serverModel: String get() = p.getString("srv.model", "")!!; set(v) = p.edit().putString("srv.model", v).apply()

    // ── скорость
    var asrThreads: Int get() = p.getInt("asr.threads", 4); set(v) = p.edit().putInt("asr.threads", v).apply()
    var llmThreads: Int get() = p.getInt("llm.threads", 4); set(v) = p.edit().putInt("llm.threads", v).apply()
    var liveInsert: Boolean get() = p.getBoolean("live", true); set(v) = p.edit().putBoolean("live", v).apply()
    var mode: String get() = p.getString("mode", "dictation")!!; set(v) = p.edit().putString("mode", v).apply()
    var promptChat: String get() = p.getString("prompt.chat", "")!!.ifBlank { CHAT_PROMPT }; set(v) = p.edit().putString("prompt.chat", v).apply()
    var autoTidy: Boolean get() = p.getBoolean("autoTidy", false); set(v) = p.edit().putBoolean("autoTidy", v).apply()

    // ── команды и промпты (как на вкладке «Мозг» плагина)
    var chips: List<Chip>
        get() = p.getString("chips", null)?.let { s ->
            runCatching { val a = JSONArray(s); (0 until a.length()).map { val o = a.getJSONObject(it); Chip(o.getString("title"), o.getString("command")) } }.getOrNull()
        } ?: Brain.DEFAULT_CHIPS
        set(v) = p.edit().putString("chips", JSONArray().apply { v.forEach { put(JSONObject().put("title", it.title).put("command", it.command)) } }.toString()).apply()
    var promptDictation: String get() = p.getString("prompt.dictation", "")!!.ifBlank { Brain.DICTATION_PROMPT }; set(v) = p.edit().putString("prompt.dictation", v).apply()
    var promptSelection: String get() = p.getString("prompt.selection", "")!!.ifBlank { Brain.SELECTION_PROMPT }; set(v) = p.edit().putString("prompt.selection", v).apply()

    // ── история промптов: до 100, закреплённые не вытесняются
    data class HistoryItem(val text: String, val pinned: Boolean, val ts: Long)

    var history: List<HistoryItem>
        get() = p.getString("history", null)?.let { s ->
            runCatching { val a = JSONArray(s); (0 until a.length()).map { val o = a.getJSONObject(it); HistoryItem(o.getString("text"), o.optBoolean("pinned"), o.optLong("ts")) } }.getOrNull()
        } ?: emptyList()
        private set(v) = p.edit().putString("history", JSONArray().apply { v.forEach { put(JSONObject().put("text", it.text).put("pinned", it.pinned).put("ts", it.ts)) } }.toString()).apply()

    fun historyAdd(text: String) {
        val t = text.trim(); if (t.isEmpty()) return
        val list = history.toMutableList()
        val old = list.indexOfFirst { it.text == t }
        val pinned = old >= 0 && list[old].pinned
        if (old >= 0) list.removeAt(old)
        list.add(0, HistoryItem(t, pinned, System.currentTimeMillis()))
        while (list.size > HISTORY_MAX) {
            val victim = list.indexOfLast { !it.pinned }
            if (victim < 0) break
            list.removeAt(victim)
        }
        history = list
    }
    fun historyPin(text: String) { history = history.map { if (it.text == text) it.copy(pinned = !it.pinned) else it } }
    fun historyRemove(text: String) { history = history.filter { it.text != text } }

    companion object {
        const val HISTORY_MAX = 100
        const val CHAT_PROMPT = "Ты — помощник в приложении «Гига Писарь». Отвечай по-русски, кратко и по делу, без лишних вступлений. Если просят написать текст — пиши сразу готовый текст."
    }
}
