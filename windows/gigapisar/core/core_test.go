package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gigapisar/asr"
)

// Поддельная цель: строка с курсором в конце.
type fakeTarget struct{ text, sel string }

func (f *fakeTarget) Selection() string { return f.sel }
func (f *fakeTarget) Insert(s string)   { f.text += s }
func (f *fakeTarget) ReplaceSession(old, new string) bool {
	if !strings.Contains(f.text, old) {
		return false
	}
	f.text = strings.Replace(f.text, old, new, 1)
	return true
}
func (f *fakeTarget) Name() string { return "field" }

// Поддельный OpenAI-совместимый сервер: [MOCK команда] ТЕКСТ.
func mockServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(401)
			return
		}
		var req struct {
			Messages []struct{ Role, Content string }
			Model    string
		}
		b := make([]byte, 1<<20)
		n, _ := r.Body.Read(b)
		_ = n
		body := string(b[:n])
		_ = req
		cmd := ""
		if i := strings.Index(body, "Команда пользователя к тексту: "); i >= 0 {
			cmd = strings.SplitN(body[i+len("Команда пользователя к тексту: "):], ".", 2)[0]
		}
		last := body[strings.LastIndex(body, `"content":"`)+len(`"content":"`):]
		last = last[:strings.Index(last, `"}`)]
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"[MOCK ` + cmd + `] ` + strings.ToUpper(last) + `"}}]}`))
	}))
}

func newApp(t *testing.T) *App {
	dir := t.TempDir()
	a := New(dir, func(string) {})
	a.OnStatus = func(s string, k Kind) { t.Log("status:", s) }
	return a
}

func TestCommandOverSelectionAndUndo(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()
	a := newApp(t)
	a.Cfg.BrainMode, a.Cfg.ServerBase, a.Cfg.ServerKey = "server", srv.URL, "secret"
	ft := &fakeTarget{text: "первый абзац. ну это типа второй", sel: "ну это типа второй"}
	a.RunCommand(ft, ft.text, "сократи", true)
	deadline := time.Now().Add(5 * time.Second)
	for a.Busy() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(ft.text, "[MOCK сократи] НУ ЭТО ТИПА ВТОРОЙ") || !strings.HasPrefix(ft.text, "первый абзац. ") {
		t.Fatalf("text: %q", ft.text)
	}
	if !a.CanUndo() {
		t.Fatal("undo")
	}
	a.Undo()
	if ft.text != "первый абзац. ну это типа второй" {
		t.Fatalf("undo text: %q", ft.text)
	}
	if len(a.History.Items) != 1 || a.History.Items[0].Text != "сократи" {
		t.Fatal("history")
	}
}

func TestTranscribeRecordAndVoiceCommand(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()
	a := newApp(t)
	a.Cfg.BrainMode, a.Cfg.ServerBase, a.Cfg.ServerKey = "server", srv.URL, "secret"
	// «распознаём» по длине куска: короткий — фраза, длинный — команда
	calls := 0
	a.Recognize = func(s []float32) (string, error) {
		calls++
		if calls == 1 {
			return "привет мир", nil
		}
		return "это тест, Писарь, сократи", nil
	}
	// wav из двух «фраз» с паузой 3 с между ними
	rate := 16000
	var samples []float32
	tone := func(sec float64) {
		for i := 0; i < int(sec*float64(rate)); i++ {
			if i%2 == 0 {
				samples = append(samples, 0.3)
			} else {
				samples = append(samples, -0.3)
			}
		}
	}
	tone(4)
	samples = append(samples, make([]float32, rate*3)...)
	tone(20)
	p := filepath.Join(a.Dir, "records", "x.wav")
	w, _ := asr.NewWavWriter(p, rate)
	w.Write(samples)
	w.Close()
	ft := &fakeTarget{}
	a.TranscribeRecord(p, ft)
	deadline := time.Now().Add(5 * time.Second)
	for a.Busy() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if calls < 2 || !strings.Contains(ft.text, "привет мир") {
		t.Fatalf("calls %d text %q", calls, ft.text)
	}
	if len(a.Records()) != 1 || a.Records()[0].Seconds < 26 {
		t.Fatalf("records: %+v", a.Records())
	}
	// голосовая команда в конце сессии
	a.sessText, a.target = "это тест, Писарь, сократи", ft
	ft.text = "это тест, Писарь, сократи"
	a.finishSession(false)
	if !strings.Contains(ft.text, "[MOCK сократи] ЭТО ТЕСТ") {
		t.Fatalf("voice command: %q", ft.text)
	}
}

func TestChatAndKeys(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()
	a := newApp(t)
	msg, err := a.ImportKeys(`{"format":"giga-pisar-keys/1","default":"groq","services":{"groq":{"key":"secret"},"cloudflare":{"key":"cf","account":"acc"}}}`)
	if err != nil || a.Cfg.BrainMode != "cloud" || a.Cfg.CloudService != "groq" || a.Cfg.CloudKey != "secret" {
		t.Fatalf("%s %v %+v", msg, err, a.Cfg)
	}
	a.ApplyCloudService("cloudflare")
	if !strings.Contains(a.Cfg.CloudBase, "/accounts/acc/") || a.Cfg.CloudKey != "cf" {
		t.Fatalf("cloudflare: %s", a.Cfg.CloudBase)
	}
	a.ApplyCloudService("groq")
	a.Cfg.CloudBase = srv.URL
	a.SendChat("привет")
	deadline := time.Now().Add(5 * time.Second)
	for a.Busy() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(a.Chat) != 2 || !strings.HasPrefix(a.Chat[1].Content, "[MOCK ] ПРИВЕТ") {
		t.Fatalf("chat: %+v", a.Chat)
	}
	if !strings.Contains(a.ChatAsText(), "Вы: привет") {
		t.Fatal("chat text")
	}
	if _, err := os.Stat(filepath.Join(a.Dir, "config.json")); err != nil {
		t.Fatal("config not saved")
	}
}
