package ru.gigapisar.engine

/**
 * Мозг Писаря — промпты, команды и обращение «Писарь, …».
 * Один в один с web/giga/brain.js и плагином WordPress: текст обрабатывается
 * одинаково, где бы ни считала нейронка — на телефоне, на компьютере или на сервере.
 */
data class ChatMessage(val role: String, val content: String)

data class Chip(val title: String, val command: String)

object Brain {
    const val SELECTION_PROMPT =
        "Ты редактируешь текст, который пользователь выделил в своём документе, " +
        "и выполняешь над ним команду пользователя. Сохраняй смысл и разбиение " +
        "на абзацы, ничего не добавляй от себя и не комментируй. Тон и стиль " +
        "сохраняй, если только команда не велит их изменить: команда важнее. " +
        "Команда дана в конце этой инструкции, в сам текст не входит, и " +
        "упоминать её в ответе нельзя. Верни ТОЛЬКО готовый текст, без кавычек " +
        "вокруг него."

    const val DICTATION_PROMPT =
        "Ты обрабатываешь надиктованный голосом текст перед вставкой. Правила: " +
        "убери слова-паразиты и оговорки (э, ну, типа, вот, как бы), убери повторы " +
        "и самоисправления, расставь знаки препинания, исправь очевидные ошибки " +
        "распознавания. Сохраняй смысл и лексику, ничего не добавляй от себя и " +
        "не комментируй. Живой тон автора сохраняй, если только команда не велит " +
        "его изменить: команда важнее тона. Выполни команду пользователя: она " +
        "дана в конце этой инструкции, в сам текст не входит, и упоминать её " +
        "в ответе нельзя. Верни ТОЛЬКО готовый текст, без кавычек вокруг него."

    /** Семь функций на кнопках — те же, что в плагине. */
    val DEFAULT_CHIPS = listOf(
        Chip("Причесать", "причеши текст: убери слова-паразиты и повторы, поправь пунктуацию и очевидные ошибки; смысл, порядок мыслей и лексику не меняй"),
        Chip("Исправить ошибки", "исправь только орфографию, пунктуацию и ошибки распознавания; слова, порядок и стиль не меняй"),
        Chip("Сократить", "сократи, сохранив суть"),
        Chip("Собрать мысль", "собери мысль: из сбивчивой речи сделай связный текст в том же порядке мыслей, ничего не добавляя от себя"),
        Chip("Сгладить", "сгладь тон: сделай мягче и вежливее, убери резкость; смысл не меняй"),
        Chip("Деловой стиль", "перепиши в деловом стиле: нейтрально, чётко, без разговорных слов; смысл не меняй"),
        Chip("Перевести на английский", "переведи на английский"),
    )

    private const val ADDRESS = "(?:гига[\\s,—-]+)?п[еиэ]сар[ьяюе]?(?![а-яё])[\\s,.:!—-]*"
    private val addressRe = Regex(ADDRESS, setOf(RegexOption.IGNORE_CASE))
    private val leadingAddressRe = Regex("^\\s*$ADDRESS", setOf(RegexOption.IGNORE_CASE))
    private val letter = Regex("[а-яёa-z]", RegexOption.IGNORE_CASE)

    class Command(val body: String, val command: String)

    /**
     * Ищет обращение к Писарю; всё после него — команда. Берётся ПОСЛЕДНЕЕ
     * вхождение, стоящее отдельным словом («описарь» не считается).
     */
    fun parseCommand(text: String): Command? {
        var last: MatchResult? = null
        for (m in addressRe.findAll(text)) {
            val i = m.range.first
            val prev = if (i > 0) text.substring(i - 1, i) else ""
            if (prev.isEmpty() || !letter.matches(prev)) last = m
        }
        val m = last ?: return null
        val command = text.substring(m.range.last + 1).trim()
        var body = text.substring(0, m.range.first).trim()
        while (body.isNotEmpty() && body.last() in ",—-–") body = body.dropLast(1).trimEnd()
        if (command.isEmpty() || body.isEmpty()) return null
        return Command(body, command)
    }

    /** «Писарь,» в начале команды над выделением — необязательное. */
    fun stripAddress(text: String) = leadingAddressRe.replace(text, "").trim()

    /** Что показать, пока нейронка думает. */
    fun actionLabel(command: String): String {
        val c = command.lowercase()
        return when {
            c.startsWith("причеши") -> "Причёсываю…"
            "перевед" in c || "англ" in c -> "Перевожу…"
            "сократ" in c || "короче" in c -> "Сокращаю…"
            "мысль" in c -> "Собираю мысль…"
            "сглад" in c || "мягче" in c -> "Сглаживаю…"
            "делов" in c || "официальн" in c -> "Делаю деловым…"
            "исправ" in c || "ошибк" in c -> "Исправляю…"
            else -> "Причёсываю…"
        }
    }

    /** Если модель всё же подумала вслух, оставляем только ответ. */
    fun stripThinking(s: String): String {
        val i = s.indexOf("</think>")
        return if (i >= 0) s.substring(i + "</think>".length) else s
    }

    /**
     * Команда — в системную инструкцию, текст — отдельным сообщением целиком:
     * иначе нейронка норовит обработать команду как часть текста.
     */
    fun messagesFor(body: String, command: String, selection: Boolean,
                    dictationPrompt: String = DICTATION_PROMPT, selectionPrompt: String = SELECTION_PROMPT): List<ChatMessage> {
        val base = (if (selection) selectionPrompt else dictationPrompt).ifBlank { if (selection) SELECTION_PROMPT else DICTATION_PROMPT }
        return listOf(
            ChatMessage("system", base + "\n\nКоманда пользователя к тексту: $command."),
            ChatMessage("user", body),
        )
    }
}
