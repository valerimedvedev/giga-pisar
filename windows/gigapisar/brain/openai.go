package brain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrNeedsKey = errors.New("нужен ключ доступа")

func NormBase(u string) string { return strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(u), "/"), "/v1") }

// ListModels — GET /v1/models у OpenAI-совместимого сервера.
func ListModels(base, key string, timeout time.Duration) ([]string, error) {
	req, _ := http.NewRequest("GET", NormBase(base)+"/v1/models", nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, ErrNeedsKey
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ответил %d", resp.StatusCode)
	}
	var j struct {
		Data   []map[string]interface{} `json:"data"`
		Models []map[string]interface{} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&j); err != nil {
		return nil, err
	}
	list := j.Data
	if len(list) == 0 {
		list = j.Models
	}
	var out []string
	for _, m := range list {
		for _, k := range []string{"id", "name", "model"} {
			if s, ok := m[k].(string); ok && s != "" {
				out = append(out, s)
				break
			}
		}
	}
	return out, nil
}

// ChatOptions — что добавить к запросу.
type ChatOptions struct {
	Key, Model  string
	MaxTokens   int
	LlamaExtras bool                   // chat_template_kwargs — только своему llama-server
	Extras      map[string]interface{} // особые поля сервиса (reasoning_effort у Gemini)
	Timeout     time.Duration
}

// Chat — POST /v1/chat/completions, ответ без <think>.
func Chat(base string, messages []Message, o ChatOptions) (string, error) {
	body := map[string]interface{}{"messages": messages, "temperature": 0.3, "max_tokens": 2048, "stream": false}
	if o.MaxTokens > 0 {
		body["max_tokens"] = o.MaxTokens
	}
	if o.Model != "" {
		body["model"] = o.Model
	}
	if o.LlamaExtras {
		body["chat_template_kwargs"] = map[string]bool{"enable_thinking": false}
	}
	for k, v := range o.Extras {
		body[k] = v
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", NormBase(base)+"/v1/chat/completions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if o.Key != "" {
		req.Header.Set("Authorization", "Bearer "+o.Key)
	}
	timeout := o.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return "", Friendly(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return "", ErrNeedsKey
	case resp.StatusCode == 429:
		return "", errors.New("слишком много запросов подряд, подождите минуту")
	case resp.StatusCode != 200:
		msg := strings.TrimSpace(string(raw))
		msg = strings.TrimPrefix(msg, "[") // Gemini заворачивает ошибку в массив
		var e struct {
			Error struct{ Message string } `json:"error"`
		}
		if json.Unmarshal([]byte(strings.TrimSuffix(msg, "]")), &e) == nil && e.Error.Message != "" {
			return "", fmt.Errorf("сервер ответил %d: %s", resp.StatusCode, trunc(e.Error.Message, 200))
		}
		return "", fmt.Errorf("сервер ответил %d", resp.StatusCode)
	}
	var j struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &j); err != nil || len(j.Choices) == 0 {
		return "", errors.New("нейронка ничего не ответила")
	}
	text := strings.TrimSpace(StripThinking(j.Choices[0].Message.Content))
	if text == "" {
		return "", errors.New("нейронка ничего не ответила")
	}
	return text, nil
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// Friendly — понятная причина вместо английского сообщения из недр сети.
func Friendly(err error) error {
	if err == nil {
		return nil
	}
	m := strings.ToLower(err.Error())
	switch {
	case strings.Contains(m, "connection reset") || strings.Contains(m, "broken pipe") || strings.Contains(m, "eof"):
		return errors.New("связь оборвалась")
	case strings.Contains(m, "timeout") || strings.Contains(m, "deadline"):
		return errors.New("сервер не отвечает")
	case strings.Contains(m, "no such host") || strings.Contains(m, "lookup"):
		return errors.New("нет интернета или не найден адрес")
	case strings.Contains(m, "connection refused") || strings.Contains(m, "actively refused"):
		return errors.New("не отвечает — запущена ли программа?")
	case strings.Contains(m, "certificate") || strings.Contains(m, "tls"):
		return errors.New("ошибка защищённого соединения")
	}
	return err
}
