package ru.gigapisar.engine

/**
 * Облачные сервисы с бесплатным тарифом. Все говорят по одному протоколу
 * (OpenAI-совместимый chat/completions с ключом Bearer), различаются адресом,
 * именем модели и условиями. Список тот же в web/giga/cloud.js и плагине.
 */
data class CloudService(
    val id: String, val name: String, val base: String, val model: String,
    val keyUrl: String, val note: String,
    val listsModels: Boolean = true,      // умеет GET /v1/models
    val browser: Boolean = true,          // пускает запросы прямо из браузера (CORS)
)

object Cloud {
    val SERVICES = listOf(
        CloudService("gemini", "Google Gemini", "https://generativelanguage.googleapis.com/v1beta/openai", "gemini-2.5-flash",
            "https://aistudio.google.com/apikey",
            "бесплатный тариф с лимитами по модели; Google может использовать данные бесплатного тарифа для улучшения продуктов — не отправляйте конфиденциальное"),
        CloudService("groq", "GroqCloud", "https://api.groq.com/openai/v1", "llama-3.3-70b-versatile",
            "https://console.groq.com/keys",
            "очень быстрые ответы; бесплатный план с квотами по моделям (Llama, GPT-OSS, Qwen)"),
        CloudService("openrouter", "OpenRouter", "https://openrouter.ai/api/v1", "google/gemma-3-27b-it:free",
            "https://openrouter.ai/keys",
            "бесплатные модели с пометкой :free; без кредитов — 50 запросов в день и 20 в минуту"),
        CloudService("mistral", "Mistral", "https://api.mistral.ai/v1", "mistral-small-latest",
            "https://console.mistral.ai/api-keys",
            "режим Free без карты, месячный объём в панели аккаунта", browser = false),
        CloudService("huggingface", "Hugging Face", "https://router.huggingface.co/v1", "Qwen/Qwen2.5-72B-Instruct",
            "https://huggingface.co/settings/tokens",
            "бесплатному аккаунту дают около $0,10 в месяц — хватит проверить, не хватит работать", browser = false),
        CloudService("cloudflare", "Cloudflare Workers AI", "https://api.cloudflare.com/client/v4/accounts/ACCOUNT_ID/ai/v1", "@cf/meta/llama-3.3-70b-instruct-fp8-fast",
            "https://dash.cloudflare.com/profile/api-tokens",
            "10 000 нейронов в день; в адресе замените ACCOUNT_ID на идентификатор аккаунта (Workers & Pages → Overview)", listsModels = false, browser = false),
    )

    fun byId(id: String) = SERVICES.firstOrNull { it.id == id }
}
