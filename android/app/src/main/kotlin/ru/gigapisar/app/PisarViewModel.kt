package ru.gigapisar.app

import android.app.Application
import android.net.Uri
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.asCoroutineDispatcher
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.joinAll
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import ru.gigapisar.engine.Brain
import ru.gigapisar.engine.Catalog
import ru.gigapisar.engine.Chip
import ru.gigapisar.engine.LiveChunker
import ru.gigapisar.engine.LlmModel
import ru.gigapisar.engine.OpenAiChat
import ru.gigapisar.engine.Rnnt
import java.io.File
import java.io.IOException
import java.net.HttpURLConnection
import java.net.NetworkInterface
import java.net.URL
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.math.pow

/** Всё живое состояние приложения: текст, запись, мозг, скачивания, настройки. */
class PisarViewModel(app: Application) : AndroidViewModel(app) {
    val store = Store(app)
    val models = Models(app)

    enum class Kind { INFO, OK, WARN, ERROR }
    data class Progress(val label: String, val done: Long, val total: Long)
    data class Ui(
        val status: String = "Нажмите «Запись» и говорите",
        val kind: Kind = Kind.INFO,
        val recording: Boolean = false,
        val busy: Boolean = false,          // мозг думает или идёт скачивание
        val progress: Progress? = null,
        val canUndo: Boolean = false,
        val askDownload: Boolean = false,   // первая диктовка: спросить согласие на 213 МБ
        val modelReady: Boolean = false,
        val words: Int = 0,
        // диктофон
        val dictaphone: Boolean = false,    // режим диктофона включён
        val recSeconds: Double = 0.0,       // длина текущей записи
        val paused: Boolean = false,
        val parts: Int = 0,                 // сколько частей уже распознано
        val records: List<File> = emptyList(),
        val transcribing: File? = null,     // расшифровка старой записи идёт
    )
    data class Settings(
        val brainMode: String, val phoneModel: String,
        val pcBase: String, val pcKey: String, val pcModel: String,
        val serverBase: String, val serverKey: String, val serverModel: String,
        val asrThreads: Int, val llmThreads: Int, val liveInsert: Boolean, val autoTidy: Boolean,
        val chips: List<Chip>, val promptDictation: String, val promptSelection: String,
    )

    private val recordsDir = File(app.filesDir, "records").apply { mkdirs() }
    private val _ui = MutableStateFlow(Ui(modelReady = models.hasGigaAm(), dictaphone = store.dictaphone, records = listRecords()))
    val ui = _ui.asStateFlow()

    /** Текст поля — единственный источник правды, поле его только показывает. */
    var text by mutableStateOf(TextFieldValue())
        private set
    var settings by mutableStateOf(readSettings())
        private set
    var history by mutableStateOf(store.history)
        private set
    var pcModels by mutableStateOf<List<String>>(emptyList())
        private set

    private val asr = Executors.newSingleThreadExecutor { Thread(it, "asr").apply { priority = Thread.MAX_PRIORITY } }.asCoroutineDispatcher()
    private val llm = Executors.newSingleThreadExecutor { Thread(it, "llm") }.asCoroutineDispatcher()
    private var rnnt: Rnnt? = null
    private var mic: Mic? = null
    private var chunker: LiveChunker? = null
    private var recorder: Recorder? = null          // диктофон: файл записи
    @Volatile private var paused = false
    private var recTimer: Job? = null
    private val jobs = ArrayList<Job>()
    private var snapshot: TextFieldValue? = null
    private val cancel = AtomicBoolean(false)

    // ── сессия диктовки: куда вставляем и что уже надиктовано
    private var sessionStart = 0
    private var sessionLen = 0
    private var sessionText = ""
    private var sessionTail = ""   // текст после точки вставки на момент старта

    init {
        if (models.hasGigaAm()) viewModelScope.launch { runCatching { recognizer() } }   // прогрев, чтобы первая фраза не ждала
    }

    fun onTextChange(v: TextFieldValue) { text = v }

    private fun status(s: String, kind: Kind = Kind.INFO) = _ui.update { it.copy(status = s, kind = kind) }

    // ─────────────────────────── распознавание ───────────────────────────

    private suspend fun recognizer(): Rnnt = withContext(asr) {
        rnnt ?: Rnnt.load(models.gigaamDir, settings.asrThreads).also { r ->
            r.transcribeWave(FloatArray(16000))      // прогрев: первый прогон всегда медленный
            rnnt = r
            _ui.update { it.copy(modelReady = true) }
        }
    }

    fun toggleRecording() { if (_ui.value.recording) stopRecording() else startRecording() }

    fun startRecording() {
        if (_ui.value.busy || _ui.value.recording) return
        if (!models.hasGigaAm()) { _ui.update { it.copy(askDownload = true) }; return }
        val t = text
        val a = t.selection.min; val b = t.selection.max
        sessionStart = a
        sessionTail = t.text.substring(b)
        sessionText = ""; sessionLen = 0
        if (b > a) text = TextFieldValue(t.text.substring(0, a) + sessionTail, TextRange(a))   // диктовка заменяет выделенное
        jobs.clear()
        val dictaphone = _ui.value.dictaphone
        // диктофон: части длиннее, паузы режут абзацами, звук пишется в файл
        val ch = if (dictaphone) LiveChunker(16000, pauseSeconds = 1.0, maxSeconds = 20.0) else LiveChunker(16000)
        chunker = ch
        val live = settings.liveInsert || dictaphone
        var rec: Recorder? = null
        if (dictaphone) {
            val name = java.text.SimpleDateFormat("yyyy-MM-dd_HH-mm-ss", java.util.Locale.US).format(java.util.Date())
            rec = Recorder(File(recordsDir, "$name.wav"))
            recorder = rec
            paused = false
            runCatching { RecorderService.start(getApplication()) }
        }
        val m = Mic { buf, n ->
            if (paused) return@Mic
            rec?.write(buf, n)
            val chunk = ch.push(buf, n)
            if (chunk != null && live) enqueue(chunk)
            else if (chunk != null) synchronized(pendingChunks) { pendingChunks.add(chunk) }
        }
        try { m.start() } catch (e: Exception) { rec?.close(); recorder = null; status(e.message ?: "микрофон не открылся", Kind.ERROR); return }
        mic = m
        _ui.update { it.copy(recording = true, canUndo = false, paused = false, recSeconds = 0.0, parts = 0) }
        if (dictaphone) {
            status("● Диктофон пишет — говорите; пауза длиннее 2,5 с начнёт новый абзац")
            recTimer = viewModelScope.launch {
                while (true) { kotlinx.coroutines.delay(500); rec?.flushHeader(); _ui.update { it.copy(recSeconds = rec?.seconds ?: 0.0) } }
            }
        } else status("Слушаю… говорите")
        viewModelScope.launch { runCatching { recognizer() }.onFailure { status("модель не загрузилась: ${it.message}", Kind.ERROR) } }
    }

    private val pendingChunks = ArrayList<FloatArray>()

    private fun enqueue(chunk: FloatArray) {
        val job = viewModelScope.launch(asr) {
            val r = recognizer()
            val t0 = System.nanoTime()
            val piece = r.transcribe(chunk)
            val ms = (System.nanoTime() - t0) / 1_000_000
            if (piece.isNotEmpty()) withContext(Dispatchers.Main) {
                val newParagraph = _ui.value.dictaphone && trailingQuiet(chunk) >= 2.5 * 16000
                appendSession(piece, newParagraph)
                _ui.update { it.copy(parts = it.parts + 1) }
                if (_ui.value.recording && !_ui.value.dictaphone) status("Слушаю… (${chunk.size / 16000} с → $ms мс)")
            }
        }
        synchronized(jobs) { jobs.add(job) }
    }

    /** Дописывает распознанную фразу в поле, туда, где идёт диктовка. */
    private fun appendSession(piece: String, newParagraph: Boolean = false) {
        sessionText = if (sessionText.isEmpty()) piece else sessionText + (if (newParagraph) "\n\n" else " ") + piece
        placeSession(sessionText)
    }

    /** Сколько тишины в хвосте куска (отсчётов) — длинная пауза значит новый абзац. */
    private fun trailingQuiet(chunk: FloatArray): Int {
        val th = 10.0.pow(-35.0 / 20.0).toFloat()
        var n = 0
        for (i in chunk.indices.reversed()) { if (kotlin.math.abs(chunk[i]) < th) n++ else break }
        return n
    }

    private fun placeSession(s: String) {
        val cur = text.text
        val head = if (sessionStart <= cur.length) cur.substring(0, sessionStart) else cur
        val gapL = if (head.isNotEmpty() && !head.last().isWhitespace() && s.isNotEmpty()) " " else ""
        val gapR = if (sessionTail.isNotEmpty() && !sessionTail.first().isWhitespace() && s.isNotEmpty()) " " else ""
        val body = gapL + s + gapR
        sessionLen = body.length
        val pos = head.length + gapL.length + s.length
        text = TextFieldValue(head + body + sessionTail, TextRange(pos))
        _ui.update { it.copy(words = wordCount(text.text)) }
    }

    fun stopRecording() {
        val m = mic ?: return
        m.stop(); mic = null
        recTimer?.cancel(); recTimer = null
        recorder?.let { r -> r.close(); recorder = null; runCatching { RecorderService.stop(getApplication()) } }
        paused = false
        _ui.update { it.copy(recording = false, paused = false, records = listRecords()) }
        status("Распознаю…")
        synchronized(pendingChunks) { pendingChunks.forEach { enqueue(it) }; pendingChunks.clear() }
        chunker?.flush()?.let { enqueue(it) }
        viewModelScope.launch {
            synchronized(jobs) { jobs.toList() }.joinAll()
            finishSession()
        }
    }

    private suspend fun finishSession() {
        val full = sessionText
        if (full.isEmpty()) { status(if (_ui.value.dictaphone) "Запись сохранена, речи в ней не услышал" else "Ничего не услышал", Kind.WARN); return }
        val cmd = Brain.parseCommand(full)
        val brainOn = settings.brainMode != "off"
        when {
            cmd != null && brainOn -> replaceSession(cmd.body, cmd.command)
            cmd != null -> status("Команда «${cmd.command}» — мозг выключен, включите его в настройках", Kind.WARN)
            settings.autoTidy && brainOn -> replaceSession(full, settings.chips.firstOrNull()?.command ?: Brain.DEFAULT_CHIPS[0].command)
            _ui.value.dictaphone -> status("Запись сохранена (${"%.0f".format(_ui.value.recSeconds)} с, ${_ui.value.parts} ч.), текст: ${wordCount(full)} сл. Мозг 🧠 причешет его целиком", Kind.OK)
            else -> status("Готово: ${wordCount(full)} сл.", Kind.OK)
        }
    }

    // ─────────────────────────── диктофон ───────────────────────────

    fun setDictaphone(on: Boolean) {
        if (_ui.value.recording) return
        store.dictaphone = on
        _ui.update { it.copy(dictaphone = on) }
        status(if (on) "Диктофон: длинная запись по частям — пауза, продолжение, файл остаётся" else "Нажмите «Запись» и говорите")
    }

    /** Пауза / продолжение записи диктофона: файл и текст продолжаются с того же места. */
    fun togglePause() {
        if (!_ui.value.recording || recorder == null) return
        paused = !paused        // буфер нарезки не сбрасываем: недоговорённая фраза доживёт до следующей паузы
        _ui.update { it.copy(paused = paused) }
        status(if (paused) "⏸ Пауза — нажмите «Продолжить», чтобы дописать" else "● Диктофон пишет дальше")
    }

    fun listRecords(): List<File> = recordsDir.listFiles { f -> f.name.endsWith(".wav") }?.sortedByDescending { it.name } ?: emptyList()

    fun deleteRecord(f: File) { f.delete(); _ui.update { it.copy(records = listRecords()) } }

    fun renameRecord(f: File, name: String) {
        val clean = name.trim().replace(Regex("[\\\\/:*?\"<>|]"), "_").ifBlank { return }
        val dest = File(recordsDir, if (clean.endsWith(".wav")) clean else "$clean.wav")
        if (f.renameTo(dest)) _ui.update { it.copy(records = listRecords()) }
    }

    /**
     * Расшифровать запись заново: файл читается по частям (по паузам, не длиннее
     * 20 с), каждая часть встаёт в поле сразу — длинная запись не заставляет ждать.
     */
    fun transcribeRecord(f: File) {
        if (_ui.value.busy || _ui.value.recording) return
        if (!models.hasGigaAm()) { _ui.update { it.copy(askDownload = true) }; return }
        _ui.update { it.copy(busy = true, transcribing = f) }
        val t = text
        sessionStart = t.selection.min; sessionTail = t.text.substring(t.selection.max); sessionText = ""; sessionLen = 0
        if (t.selection.max > sessionStart) text = TextFieldValue(t.text.substring(0, sessionStart) + sessionTail, TextRange(sessionStart))
        viewModelScope.launch(asr) {
            try {
                val r = recognizer()
                val audio = ru.gigapisar.engine.Wav.read(f.readBytes())
                val samples = ru.gigapisar.engine.Wav.resample(audio.samples, audio.rate, r.sampleRate)
                val total = samples.size.toDouble() / r.sampleRate
                val bounds = ru.gigapisar.engine.Chunker.chunkBounds(total, ru.gigapisar.engine.Chunker.silences(samples, r.sampleRate), 20.0)
                var n = 0
                for ((a, b) in bounds) {
                    val from = (a * r.sampleRate).toInt(); val to = minOf(samples.size, (b * r.sampleRate).toInt())
                    if (to <= from) continue
                    val chunk = samples.copyOfRange(from, to)
                    val piece = r.transcribe(chunk)
                    n++
                    withContext(Dispatchers.Main) {
                        if (piece.isNotEmpty()) appendSession(piece, trailingQuiet(chunk) >= 2.5 * r.sampleRate)
                        status("Расшифровываю ${f.name}: часть $n из ${bounds.size}")
                    }
                }
                withContext(Dispatchers.Main) { status("Готово: ${f.name}, ${wordCount(sessionText)} сл.", Kind.OK) }
            } catch (e: Exception) {
                withContext(Dispatchers.Main) { status("Не расшифровал: ${e.message}", Kind.ERROR) }
            } finally { _ui.update { it.copy(busy = false, transcribing = null) } }
        }
    }

    /** Надиктованное целиком → нейронка → на то же место. */
    private suspend fun replaceSession(body: String, command: String) {
        snapshot = text
        _ui.update { it.copy(busy = true) }
        try {
            val out = transform(body, command, selection = false)
            placeSession(out)
            _ui.update { it.copy(canUndo = true) }
            status("Готово: ${Brain.actionLabel(command).trimEnd('…')} — ${wordCount(out)} сл.", Kind.OK)
        } catch (e: Exception) {
            status("Мозг: ${e.message}. Текст вставлен как есть", Kind.WARN)
        } finally { _ui.update { it.copy(busy = false) } }
    }

    // ─────────────────────────── мозг ───────────────────────────

    val brainEnabled get() = settings.brainMode != "off"

    /** Команда над выделенным или над всем текстом. [own] — свой промпт, идёт в историю. */
    fun runCommand(command: String, own: Boolean = false) {
        val cmd = Brain.stripAddress(command)
        if (cmd.isEmpty() || _ui.value.busy || _ui.value.recording) return
        val t = text
        val whole = t.selection.collapsed
        val body = if (whole) t.text else t.text.substring(t.selection.min, t.selection.max)
        if (body.isBlank()) { status("Нет текста для обработки", Kind.WARN); return }
        if (own) { store.historyAdd(command); history = store.history }
        snapshot = t
        _ui.update { it.copy(busy = true) }
        status(Brain.actionLabel(cmd))
        viewModelScope.launch {
            try {
                val out = transform(body, cmd, selection = !whole)
                text = if (whole) TextFieldValue(out, TextRange(out.length)) else {
                    val a = t.selection.min; val b = t.selection.max
                    TextFieldValue(t.text.substring(0, a) + out + t.text.substring(b), TextRange(a, a + out.length))
                }
                _ui.update { it.copy(canUndo = true, words = wordCount(text.text)) }
                status("Готово" + if (!whole) " (над выделенным)" else "", Kind.OK)
            } catch (e: CancellationException) { throw e
            } catch (e: Exception) { status("Мозг: ${e.message}", Kind.ERROR)
            } finally { _ui.update { it.copy(busy = false) } }
        }
    }

    private suspend fun transform(body: String, command: String, selection: Boolean): String = withContext(llm) {
        val s = settings
        val messages = Brain.messagesFor(body, command, selection, s.promptDictation, s.promptSelection)
        when (s.brainMode) {
            "phone" -> {
                val f = models.llmByChoice(s.phoneModel) ?: throw IOException("нейронка не скачана: Настройки → Мозг → На телефоне")
                if (LocalLlm.loadedPath != f.path) status("Запускаю нейронку…")
                LocalLlm.ensureLoaded(f, s.llmThreads)
                status(Brain.actionLabel(command))
                LocalLlm.chat(messages)
            }
            "pc" -> {
                if (s.pcBase.isBlank()) throw IOException("не задан адрес GigaBrain: Настройки → Мозг → На компьютере")
                OpenAiChat.chat(s.pcBase, messages, s.pcKey, s.pcModel)
            }
            "server" -> {
                if (s.serverBase.isBlank()) throw IOException("не задан адрес сервера: Настройки → Мозг → На сервере")
                OpenAiChat.chat(s.serverBase, messages, s.serverKey, s.serverModel)
            }
            else -> throw IOException("мозг выключен: Настройки → Мозг")
        }.ifBlank { throw IOException("нейронка ничего не ответила") }
    }

    fun cancelBrain() { LocalLlm.cancel() }

    fun undo() {
        val s = snapshot ?: return
        text = s; snapshot = null
        _ui.update { it.copy(canUndo = false, words = wordCount(s.text)) }
        status("Вернул как было", Kind.OK)
    }

    fun clearText() { snapshot = text; text = TextFieldValue(); _ui.update { it.copy(canUndo = true, words = 0) }; status("Поле очищено") }

    fun historyPin(t: String) { store.historyPin(t); history = store.history }
    fun historyRemove(t: String) { store.historyRemove(t); history = store.history }

    // ─────────────────────────── скачивание ───────────────────────────

    fun dismissDownload() = _ui.update { it.copy(askDownload = false) }

    fun downloadGigaAm() {
        _ui.update { it.copy(askDownload = false) }
        runLong("Пакет распознавания") { onP ->
            models.downloadGigaAm(cancel, onP)
            _ui.update { it.copy(modelReady = true) }
            recognizer()
            "Пакет распознавания готов — нажмите «Запись»"
        }
    }

    fun deleteGigaAm() { rnnt?.close(); rnnt = null; models.deleteGigaAm(); _ui.update { it.copy(modelReady = false) }; status("Пакет распознавания удалён") }

    fun downloadLlm(m: LlmModel) = runLong(m.name) { onP ->
        if (models.freeBytes() < (m.sizeGb * 1.1e9).toLong()) throw IOException("мало места: нужно ${m.sizeGb} ГБ")
        models.downloadLlm(m, cancel, onP)
        if (settings.phoneModel.isBlank()) save(settings.copy(phoneModel = m.id))
        "${m.name} скачана"
    }

    fun importLlm(uri: Uri) = runLong("Свой файл .gguf") { onP ->
        val f = models.importLlm(uri, cancel, onP)
        save(settings.copy(phoneModel = f.name))
        "${f.name} добавлена"
    }

    fun deleteLlm(file: File) {
        if (LocalLlm.loadedPath == file.path) LocalLlm.unload()
        models.deleteLlm(file)
        if (models.llmByChoice(settings.phoneModel) == null) save(settings.copy(phoneModel = ""))
        status("${file.name} удалена")
    }

    fun cancelDownload() { cancel.set(true) }

    private fun runLong(label: String, block: suspend ((Long, Long) -> Unit) -> String) {
        if (_ui.value.busy) { status("Подождите: идёт другое скачивание", Kind.WARN); return }
        cancel.set(false)
        _ui.update { it.copy(busy = true, progress = Progress(label, 0, 0)) }
        viewModelScope.launch(Dispatchers.IO) {
            try {
                val msg = block { d, t -> _ui.update { it.copy(progress = if (d == 0L && t == 0L) Progress("$label — распаковка…", 0, 0) else Progress(label, d, t)) } }
                status(msg, Kind.OK)
            } catch (e: Exception) {
                status("$label: ${e.message}", if (e.message == "отменено") Kind.WARN else Kind.ERROR)
            } finally { _ui.update { it.copy(busy = false, progress = null) } }
        }
    }

    // ─────────────────────────── настройки ───────────────────────────

    private fun readSettings() = Settings(store.brainMode, store.phoneModel, store.pcBase, store.pcKey, store.pcModel,
        store.serverBase, store.serverKey, store.serverModel, store.asrThreads, store.llmThreads, store.liveInsert, store.autoTidy,
        store.chips, store.promptDictation, store.promptSelection)

    fun save(s: Settings) {
        val old = settings
        store.brainMode = s.brainMode; store.phoneModel = s.phoneModel
        store.pcBase = s.pcBase; store.pcKey = s.pcKey; store.pcModel = s.pcModel
        store.serverBase = s.serverBase; store.serverKey = s.serverKey; store.serverModel = s.serverModel
        store.asrThreads = s.asrThreads; store.llmThreads = s.llmThreads; store.liveInsert = s.liveInsert; store.autoTidy = s.autoTidy
        store.chips = s.chips; store.promptDictation = s.promptDictation; store.promptSelection = s.promptSelection
        settings = readSettings()
        if (old.asrThreads != s.asrThreads) { rnnt?.close(); rnnt = null }
        if (old.llmThreads != s.llmThreads || (old.phoneModel != s.phoneModel)) LocalLlm.unload()
        if (s.brainMode != "phone") LocalLlm.unload()     // освободить память телефона
    }

    fun resetChips() = save(settings.copy(chips = Brain.DEFAULT_CHIPS))
    fun resetPrompts() = save(settings.copy(promptDictation = Brain.DICTATION_PROMPT, promptSelection = Brain.SELECTION_PROMPT))

    /** Проверяет мозг на компьютере или сервере: список моделей. */
    fun checkRemote(base: String, key: String, onDone: (Result<List<String>>) -> Unit) {
        viewModelScope.launch {
            val r = withContext(Dispatchers.IO) { runCatching { OpenAiChat.listModels(base, key, 4000) } }
            r.onSuccess { pcModels = it }
            onDone(r)
        }
    }

    /** Ищет GigaBrain в домашней сети: опрашивает все адреса своей подсети на порту 8091. */
    fun findInLan(key: String, onDone: (List<String>) -> Unit) {
        viewModelScope.launch {
            val found = withContext(Dispatchers.IO) {
                val prefixes = NetworkInterface.getNetworkInterfaces().toList()
                    .flatMap { it.inetAddresses.toList() }
                    .filter { it.isSiteLocalAddress && it.hostAddress?.contains('.') == true }
                    .map { it.hostAddress!!.substringBeforeLast('.') }.distinct()
                prefixes.flatMap { p ->
                    (1..254).map { n -> async(Dispatchers.IO) { val base = "http://$p.$n:8091"; if (alive(base, key)) base else null } }.awaitAll().filterNotNull()
                }
            }
            onDone(found)
        }
    }

    private fun alive(base: String, key: String): Boolean = try {
        val c = URL("$base/health").openConnection() as HttpURLConnection
        c.connectTimeout = 400; c.readTimeout = 400
        if (key.isNotBlank()) c.setRequestProperty("Authorization", "Bearer $key")
        val code = c.responseCode; c.disconnect()
        code == 200 || code == 401
    } catch (_: Exception) { false }

    override fun onCleared() {
        recorder?.close(); runCatching { RecorderService.stop(getApplication()) }
        mic?.stop(); rnnt?.close(); LocalLlm.unload(); asr.close(); llm.close()
    }

    companion object {
        fun wordCount(s: String) = s.trim().split(Regex("\\s+")).count { it.isNotEmpty() }
        val PHONE_CATALOG get() = Catalog.PHONE
    }
}
