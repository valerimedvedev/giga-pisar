package ru.gigapisar.engine

/**
 * Нейронки, которые разумно крутить прямо на телефоне (Galaxy S23 Ultra — 12 ГБ памяти,
 * Snapdragon 8 Gen 2). Порядок — по скорости; первая по умолчанию.
 */
data class LlmModel(
    val id: String, val name: String, val file: String, val url: String,
    val sizeGb: Double, val ramGb: Double, val about: String, val default: Boolean = false,
)

object Catalog {
    val PHONE = listOf(
        LlmModel("qwen3-4b", "Qwen3-4B Instruct 2507 (Q4_K_M)", "Qwen3-4B-Instruct-2507-Q4_K_M.gguf",
            "https://huggingface.co/unsloth/Qwen3-4B-Instruct-2507-GGUF/resolve/main/Qwen3-4B-Instruct-2507-Q4_K_M.gguf",
            2.5, 4.0, "лучший баланс: хороший русский, ~10 слов/с на S23 Ultra", default = true),
        LlmModel("qwen3-4b-q3", "Qwen3-4B Instruct 2507 (Q3_K_M, легче)", "Qwen3-4B-Instruct-2507-Q3_K_M.gguf",
            "https://huggingface.co/unsloth/Qwen3-4B-Instruct-2507-GGUF/resolve/main/Qwen3-4B-Instruct-2507-Q3_K_M.gguf",
            1.9, 3.0, "та же модель, что в браузерной версии; чуть быстрее и чуть грубее"),
        LlmModel("qwen3-1.7b", "Qwen3-1.7B (Q4_K_M, самая быстрая)", "Qwen3-1.7B-Q4_K_M.gguf",
            "https://huggingface.co/unsloth/Qwen3-1.7B-GGUF/resolve/main/Qwen3-1.7B-Q4_K_M.gguf",
            1.1, 2.0, "мгновенная правка коротких фраз; русский попроще"),
        LlmModel("gemma3-4b", "Gemma 3 4B it (Q4_K_M)", "gemma-3-4b-it-Q4_K_M.gguf",
            "https://huggingface.co/unsloth/gemma-3-4b-it-GGUF/resolve/main/gemma-3-4b-it-Q4_K_M.gguf",
            2.5, 4.0, "аккуратный стиль, хорошо переводит"),
        LlmModel("gigachat3.1-10b", "GigaChat 3.1 10B-A1.8B (Q4_K_M)", "GigaChat3.1-10B-A1.8B-Q4_K_M.gguf",
            "https://huggingface.co/ai-sage/GigaChat3.1-10B-A1.8B-GGUF/resolve/main/GigaChat3.1-10B-A1.8B-Q4_K_M.gguf",
            6.5, 9.0, "родной русский от Сбера; активных параметров мало — быстро, но занимает 6,5 ГБ"),
        LlmModel("yandexgpt-5-lite-8b", "YandexGPT 5 Lite 8B (Q4_K_M)", "YandexGPT-5-Lite-8B-instruct-Q4_K_M.gguf",
            "https://huggingface.co/yandex/YandexGPT-5-Lite-8B-instruct-GGUF/resolve/main/YandexGPT-5-Lite-8B-instruct-Q4_K_M.gguf",
            5.0, 7.0, "родной русский, деловой стиль; медленнее (8B плотных)"),
    )

    /** Пакет распознавания речи — тот же архив, что качают плагин и приложение для macOS. */
    const val GIGAAM_URL = "https://github.com/moznoazachem/giga-pisar-cli/releases/download/v1.0/gigaam-v3-onnx-int8.tar.gz"
    const val GIGAAM_BYTES = 213_000_000L

    fun byId(id: String) = PHONE.firstOrNull { it.id == id }
}
