// Облачные сервисы с бесплатным тарифом — тот же список, что в engine/Cloud.kt
// и плагине. Все говорят по одному протоколу (OpenAI-совместимый
// chat/completions, ключ Bearer); отличаются адресом, моделью и условиями.
// browser: false — сервис не пускает запросы прямо из браузера (CORS), такой
// подключают через сервер сайта (плагин) или через GigaBrain на компьютере.

export const CLOUD_SERVICES = [
  { id: "gemini", name: "Google Gemini", base: "https://generativelanguage.googleapis.com/v1beta/openai", model: "gemini-2.5-flash",
    keyUrl: "https://aistudio.google.com/apikey",
    note: "бесплатный тариф с лимитами по модели; Google может использовать данные бесплатного тарифа для улучшения продуктов — не отправляйте конфиденциальное", browser: true, listsModels: true },
  { id: "groq", name: "GroqCloud", base: "https://api.groq.com/openai/v1", model: "llama-3.3-70b-versatile",
    keyUrl: "https://console.groq.com/keys",
    note: "очень быстрые ответы; бесплатный план с квотами по моделям (Llama, GPT-OSS, Qwen)", browser: true, listsModels: true },
  { id: "openrouter", name: "OpenRouter", base: "https://openrouter.ai/api/v1", model: "google/gemma-3-27b-it:free",
    keyUrl: "https://openrouter.ai/keys",
    note: "бесплатные модели с пометкой :free; без кредитов — 50 запросов в день и 20 в минуту", browser: true, listsModels: true },
  { id: "mistral", name: "Mistral", base: "https://api.mistral.ai/v1", model: "mistral-small-latest",
    keyUrl: "https://console.mistral.ai/api-keys",
    note: "режим Free без карты, месячный объём в панели аккаунта; из браузера не пускает — только через сервер сайта", browser: false, listsModels: true },
  { id: "huggingface", name: "Hugging Face", base: "https://router.huggingface.co/v1", model: "Qwen/Qwen2.5-72B-Instruct",
    keyUrl: "https://huggingface.co/settings/tokens",
    note: "около $0,10 в месяц бесплатно — хватит проверить; из браузера может не пускать", browser: false, listsModels: true },
  { id: "cloudflare", name: "Cloudflare Workers AI", base: "https://api.cloudflare.com/client/v4/accounts/ACCOUNT_ID/ai/v1", model: "@cf/meta/llama-3.3-70b-instruct-fp8-fast",
    keyUrl: "https://dash.cloudflare.com/profile/api-tokens",
    note: "10 000 нейронов в день; в адресе замените ACCOUNT_ID; из браузера не пускает — только через сервер сайта", browser: false, listsModels: false },
];

export const cloudService = (id) => CLOUD_SERVICES.find((s) => s.id === id) ?? null;
