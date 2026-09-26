package brain

import (
	"encoding/json"
	"errors"
	"strings"
)

// Облачные сервисы с бесплатным тарифом — тот же список, что в engine/Cloud.kt и web/giga/cloud.js.
type CloudService struct {
	ID, Name, Base, Model, KeyURL, Note string
	ListsModels                         bool
	Extras                              map[string]interface{}
}

var CloudServices = []CloudService{
	{"gemini", "Google Gemini", "https://generativelanguage.googleapis.com/v1beta/openai", "gemini-flash-latest", "https://aistudio.google.com/apikey",
		"бесплатный тариф с лимитами по модели; Google может использовать данные бесплатного тарифа для улучшения продуктов — не отправляйте конфиденциальное", true, map[string]interface{}{"reasoning_effort": "low"}},
	{"groq", "GroqCloud", "https://api.groq.com/openai/v1", "llama-3.3-70b-versatile", "https://console.groq.com/keys", "очень быстрые ответы; бесплатный план с квотами по моделям", true, nil},
	{"openrouter", "OpenRouter", "https://openrouter.ai/api/v1", "google/gemma-3-27b-it:free", "https://openrouter.ai/keys", "бесплатные модели :free; без кредитов — 50 запросов в день", true, nil},
	{"mistral", "Mistral", "https://api.mistral.ai/v1", "mistral-small-latest", "https://console.mistral.ai/api-keys", "режим Free без карты, месячный объём в панели", true, nil},
	{"huggingface", "Hugging Face", "https://router.huggingface.co/v1", "Qwen/Qwen2.5-72B-Instruct", "https://huggingface.co/settings/tokens", "около $0,10 в месяц бесплатно — только проверить", true, nil},
	{"cloudflare", "Cloudflare Workers AI", "https://api.cloudflare.com/client/v4/accounts/ACCOUNT_ID/ai/v1", "@cf/meta/llama-3.3-70b-instruct-fp8-fast", "https://dash.cloudflare.com/profile/api-tokens", "10 000 нейронов в день; в адресе замените ACCOUNT_ID", false, nil},
}

func CloudByID(id string) *CloudService {
	for i := range CloudServices {
		if CloudServices[i].ID == id {
			return &CloudServices[i]
		}
	}
	return nil
}

type KeyEntry struct {
	Key     string `json:"key"`
	Account string `json:"account,omitempty"`
}

// ParseKeys разбирает giga-keys.json (или упрощённый {"groq":"ключ"}).
func ParseKeys(text string) (map[string]KeyEntry, string, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &root); err != nil {
		return nil, "", errors.New("это не JSON")
	}
	src := root
	if s, ok := root["services"]; ok {
		var m map[string]json.RawMessage
		if json.Unmarshal(s, &m) == nil {
			src = m
		}
	}
	out := map[string]KeyEntry{}
	for _, svc := range CloudServices {
		raw, ok := src[svc.ID]
		if !ok {
			continue
		}
		var s string
		var e KeyEntry
		if json.Unmarshal(raw, &s) == nil {
			if strings.TrimSpace(s) != "" {
				out[svc.ID] = KeyEntry{Key: strings.TrimSpace(s)}
			}
		} else if json.Unmarshal(raw, &e) == nil && strings.TrimSpace(e.Key) != "" {
			out[svc.ID] = KeyEntry{Key: strings.TrimSpace(e.Key), Account: strings.TrimSpace(e.Account)}
		}
	}
	if len(out) == 0 {
		return nil, "", errors.New("в файле нет ни одного известного сервиса")
	}
	var def string
	if d, ok := root["default"]; ok {
		json.Unmarshal(d, &def)
		if _, ok := out[def]; !ok {
			def = ""
		}
	}
	return out, def, nil
}

func BaseFor(svc *CloudService, e *KeyEntry) string {
	acc := "ACCOUNT_ID"
	if e != nil && e.Account != "" {
		acc = e.Account
	}
	return strings.Replace(svc.Base, "ACCOUNT_ID", acc, 1)
}
