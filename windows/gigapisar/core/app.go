// Логика Писаря без окна: распознавание, сессия диктовки (в поле или в чужое
// окно), диктофон, мозг, общение. Окно только показывает и дёргает методы.
package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gigapisar/asr"
	"gigapisar/brain"
	"gigapisar/localllm"
	"gigapisar/ort"
	"gigapisar/setup"
	"gigapisar/win"
)

type Kind int

const (
	Info Kind = iota
	OK
	Warn
	Error
)

// Target — куда идёт диктовка: поле в окне Писаря или чужое окно Windows.
type Target interface {
	// Selection — текст выделения (пусто, если ничего не выделено).
	Selection() string
	// Insert вставляет распознанное на место курсора/выделения.
	Insert(text string)
	// Replace заменяет то, что было вставлено этой сессией (undo/правка мозгом); ok=false, если нельзя.
	ReplaceSession(old, new string) bool
	Name() string
}

type App struct {
	Dir string
	Cfg Config
	Log func(string)
	// события для окна (зовутся из горутин)
	OnStatus   func(text string, kind Kind)
	OnProgress func(label string, done, total int64) // label=="" — скрыть
	OnChange   func()                                // перерисовать состояние
	OnChat     func()

	Local   *localllm.Manager
	History *brain.History

	mu        sync.Mutex
	rec       *asr.Recognizer
	recLoad   sync.Mutex
	Recognize func([]float32) (string, error) // подменяется в тестах

	mic       *win.Mic
	chunker   *asr.LiveChunker
	wav       *asr.WavWriter
	paused    int32
	recording int32
	busy      int32
	cancel    int32
	jobs      sync.WaitGroup
	sessMu    sync.Mutex
	sessText  string
	target    Target
	pending   func() [][]float32
	lastUndo  *undo
	Chat      []brain.Message
	RecSeconds float64
	Parts     int
}

type undo struct {
	target   Target
	old, new string
}

func New(dir string, log func(string)) *App {
	os.MkdirAll(filepath.Join(dir, "records"), 0o755)
	a := &App{Dir: dir, Cfg: LoadConfig(dir), Log: log}
	a.Local = localllm.New(filepath.Join(dir, "brain"), log)
	a.Local.Backend = a.Cfg.Backend
	a.Local.Threads = a.Cfg.LlmThreads
	a.History = brain.LoadHistory(filepath.Join(dir, "prompts.json"))
	a.Recognize = a.recognize
	return a
}

func (a *App) Save() { a.Cfg.Save(a.Dir); a.Local.Backend = a.Cfg.Backend; a.Local.Threads = a.Cfg.LlmThreads }

func (a *App) status(t string, k Kind) {
	if a.OnStatus != nil {
		a.OnStatus(t, k)
	}
}
func (a *App) progress(label string, d, t int64) {
	if a.OnProgress != nil {
		a.OnProgress(label, d, t)
	}
}
func (a *App) changed() {
	if a.OnChange != nil {
		a.OnChange()
	}
}
func (a *App) logf(f string, v ...interface{}) {
	if a.Log != nil {
		a.Log(fmt.Sprintf(f, v...))
	}
}

func (a *App) Recording() bool { return atomic.LoadInt32(&a.recording) != 0 }
func (a *App) Busy() bool      { return atomic.LoadInt32(&a.busy) != 0 }
func (a *App) Paused() bool    { return atomic.LoadInt32(&a.paused) != 0 }

// ─────────────────────────── модель распознавания ───────────────────────────

func (a *App) ModelDir() string { return filepath.Join(a.Dir, "gigaam") }
func (a *App) ModelReady() bool { return setup.HasOrt(a.Dir) && asr.HasModel(a.ModelDir()) }

// EnsureRecognizer грузит onnxruntime и модель (один раз), прогревает.
func (a *App) EnsureRecognizer() (*asr.Recognizer, error) {
	a.recLoad.Lock()
	defer a.recLoad.Unlock()
	if a.rec != nil {
		return a.rec, nil
	}
	if !a.ModelReady() {
		return nil, errors.New("пакет распознавания не скачан")
	}
	if err := ort.Load(setup.OrtPath(a.Dir)); err != nil {
		return nil, fmt.Errorf("onnxruntime: %w", err)
	}
	a.status("Загружаю модель в память…", Info)
	r, err := asr.Load(a.ModelDir(), a.Cfg.AsrThreads)
	if err != nil {
		return nil, err
	}
	r.TranscribeWave(make([]float32, 16000)) // прогрев
	a.rec = r
	a.changed()
	return r, nil
}

func (a *App) UnloadRecognizer() {
	a.recLoad.Lock()
	defer a.recLoad.Unlock()
	if a.rec != nil {
		a.rec.Close()
		a.rec = nil
	}
}

func (a *App) recognize(s []float32) (string, error) {
	r, err := a.EnsureRecognizer()
	if err != nil {
		return "", err
	}
	return r.Transcribe(s)
}

// InstallModel качает onnxruntime (под версию Windows) и пакет GigaAM.
func (a *App) InstallModel() error {
	if !atomic.CompareAndSwapInt32(&a.busy, 0, 1) {
		return errors.New("подождите: идёт другое скачивание")
	}
	defer atomic.StoreInt32(&a.busy, 0)
	atomic.StoreInt32(&a.cancel, 0)
	defer a.progress("", 0, 0)
	if !setup.HasOrt(a.Dir) {
		old := win.IsOldWindows()
		label := "Среда onnxruntime"
		if old {
			label += " (для Windows 7/8)"
		}
		if err := setup.InstallOrt(a.Dir, old, &a.cancel, func(d, t int64, _ float64) { a.progress(label, d, t) }); err != nil {
			return fmt.Errorf("onnxruntime: %w", brain.Friendly(err))
		}
	}
	if !asr.HasModel(a.ModelDir()) {
		if err := setup.InstallGigaAm(a.Dir, asr.Files, &a.cancel, func(d, t int64, _ float64) { a.progress("Пакет распознавания GigaAM", d, t) }); err != nil {
			return brain.Friendly(err)
		}
	}
	a.status("Пакет распознавания готов", OK)
	a.changed()
	go a.EnsureRecognizer()
	return nil
}

func (a *App) CancelDownload() { atomic.StoreInt32(&a.cancel, 1) }

// ─────────────────────────── диктовка ───────────────────────────

// StartRecording начинает запись в цель. dictaphone — писать wav и резать абзацами.
func (a *App) StartRecording(t Target, dictaphone bool) error {
	if a.Recording() {
		return nil
	}
	if !a.ModelReady() {
		return errors.New("пакет распознавания не скачан")
	}
	a.target = t
	a.sessText = ""
	a.Parts, a.RecSeconds = 0, 0
	a.lastUndo = nil
	var ch *asr.LiveChunker
	if dictaphone {
		ch = asr.NewLiveChunker(win.Rate, 1.0, 0.4, 20.0)
		name := time.Now().Format("2006-01-02_15-04-05") + ".wav"
		w, err := asr.NewWavWriter(filepath.Join(a.Dir, "records", name), win.Rate)
		if err != nil {
			return err
		}
		a.wav = w
	} else {
		ch = asr.NewLiveChunker(win.Rate, 0.7, 0.4, 20.0)
	}
	a.chunker = ch
	atomic.StoreInt32(&a.paused, 0)
	live := a.Cfg.LiveInsert || dictaphone
	var pending [][]float32
	var pmu sync.Mutex
	a.mic = win.NewMic(func(s []float32) {
		if a.Paused() {
			return
		}
		if a.wav != nil {
			a.wav.Write(s)
			a.RecSeconds = a.wav.Seconds()
		}
		if chunk := ch.Push(s); chunk != nil {
			if live {
				a.enqueue(chunk, dictaphone)
			} else {
				pmu.Lock()
				pending = append(pending, chunk)
				pmu.Unlock()
			}
		}
	})
	if err := a.mic.Start(); err != nil {
		if a.wav != nil {
			a.wav.Close()
			a.wav = nil
		}
		return err
	}
	a.pending = func() [][]float32 { pmu.Lock(); defer pmu.Unlock(); p := pending; pending = nil; return p }
	atomic.StoreInt32(&a.recording, 1)
	if dictaphone {
		a.status("● Диктофон пишет — говорите; пауза длиннее 2,5 с начнёт новый абзац", Info)
	} else if t.Selection() != "" && a.BrainOn() {
		a.status("● Запись — скажите команду над выделенным", Info)
	} else {
		a.status("● Запись — говорите; «Стоп», когда закончите", Info)
	}
	a.changed()
	go a.EnsureRecognizer()
	return nil
}

var _ = sort.Strings

func (a *App) enqueue(chunk []float32, dictaphone bool) {
	a.jobs.Add(1)
	go func() {
		defer a.jobs.Done()
		t0 := time.Now()
		piece, err := a.Recognize(chunk)
		if err != nil {
			a.status("Не распознал: "+err.Error(), Error)
			return
		}
		if piece == "" {
			return
		}
		newPara := dictaphone && asr.TrailingQuiet(chunk) >= int(2.5*win.Rate)
		a.sessMu.Lock()
		a.appendSession(piece, newPara)
		a.Parts++
		a.sessMu.Unlock()
		if a.Recording() && !dictaphone {
			a.status(fmt.Sprintf("● Запись — %d с → %d мс", len(chunk)/win.Rate, time.Since(t0).Milliseconds()), Info)
		}
	}()
}

// appendSession: фразы вставляются подряд туда, где идёт диктовка.
func (a *App) appendSession(piece string, newPara bool) {
	sep := " "
	if a.sessText == "" {
		sep = ""
	} else if newPara {
		sep = "\r\n\r\n"
	}
	a.sessText += sep + piece
	a.target.Insert(sep + piece)
}

func (a *App) TogglePause() {
	if !a.Recording() || a.wav == nil {
		return
	}
	if atomic.LoadInt32(&a.paused) == 0 {
		atomic.StoreInt32(&a.paused, 1)
		a.status("⏸ Пауза — «Продолжить», чтобы дописать", Info)
	} else {
		atomic.StoreInt32(&a.paused, 0)
		a.status("● Диктофон пишет дальше", Info)
	}
	a.changed()
}

// StopRecording останавливает микрофон, дожидается фраз и разбирает команду.
func (a *App) StopRecording() {
	if !atomic.CompareAndSwapInt32(&a.recording, 1, 0) {
		return
	}
	a.mic.Stop()
	dictaphone := a.wav != nil
	if a.wav != nil {
		a.wav.Close()
		a.wav = nil
	}
	a.status("Распознаю…", Info)
	a.changed()
	if a.pending != nil {
		for _, c := range a.pending() {
			a.enqueue(c, dictaphone)
		}
	}
	if rest := a.chunker.Flush(); rest != nil {
		a.enqueue(rest, dictaphone)
	}
	go func() {
		a.jobs.Wait()
		a.finishSession(dictaphone)
	}()
}

func (a *App) CancelRecording() {
	if !atomic.CompareAndSwapInt32(&a.recording, 1, 0) {
		return
	}
	a.mic.Stop()
	if a.wav != nil {
		a.wav.Close()
		a.wav = nil
	}
	a.status("Запись отменена", Info)
	a.changed()
}

func (a *App) finishSession(dictaphone bool) {
	a.sessMu.Lock()
	full := a.sessText
	t := a.target
	a.sessMu.Unlock()
	defer a.changed()
	if strings.TrimSpace(full) == "" {
		if dictaphone {
			a.status("Запись сохранена, речи в ней не услышал", Warn)
		} else {
			a.status("Ничего не услышал", Warn)
		}
		return
	}
	words := len(strings.Fields(full))
	if a.Cfg.Mode == "chat" && t.Name() == "chat" {
		a.status(fmt.Sprintf("Надиктовано: %d сл. — «Отправить»", words), OK)
		return
	}
	body, command, ok := brain.ParseCommand(full)
	switch {
	case ok && a.BrainOn():
		a.replaceSession(t, full, body, command)
	case ok:
		a.status("Команда «"+command+"» — мозг выключен, включите его в настройках", Warn)
	case a.Cfg.AutoTidy && a.BrainOn() && !dictaphone:
		a.replaceSession(t, full, full, a.Cfg.Chips[0].Command)
	case dictaphone:
		a.status(fmt.Sprintf("Запись сохранена (%.0f с, %d ч.), текст: %d сл.", a.RecSeconds, a.Parts, words), OK)
	default:
		a.status(fmt.Sprintf("Готово: %d сл.", words), OK)
	}
}

// Надиктованное целиком → нейронка → на то же место.
func (a *App) replaceSession(t Target, full, body, command string) {
	atomic.StoreInt32(&a.busy, 1)
	defer atomic.StoreInt32(&a.busy, 0)
	a.status(brain.ActionLabel(command), Info)
	out, err := a.Transform(body, command, false)
	if err != nil {
		a.status("Мозг: "+err.Error()+". Текст вставлен как есть", Warn)
		return
	}
	if t.ReplaceSession(full, out) {
		a.lastUndo = &undo{t, full, out}
		a.status("Готово: "+strings.TrimSuffix(brain.ActionLabel(command), "…")+fmt.Sprintf(" — %d сл.", len(strings.Fields(out))), OK)
	} else {
		a.status("Мозг ответил, но заменить текст в этом окне нельзя — ответ скопирован в буфер обмена", Warn)
		win.SetClipboardText(out)
	}
}

// ─────────────────────────── мозг ───────────────────────────

func (a *App) BrainOn() bool { return a.Cfg.BrainMode != "off" }

// RunCommand — команда над выделенным, а без выделения — над всем текстом цели.
func (a *App) RunCommand(t Target, whole string, command string, own bool) {
	cmd := brain.StripAddress(command)
	if cmd == "" || a.Busy() || a.Recording() {
		return
	}
	if own {
		a.History.Add(command)
	}
	sel := t.Selection()
	body, selection := sel, true
	if strings.TrimSpace(sel) == "" {
		body, selection = whole, false
	}
	if strings.TrimSpace(body) == "" {
		a.status("Нет текста для обработки", Warn)
		return
	}
	atomic.StoreInt32(&a.busy, 1)
	a.changed()
	go func() {
		defer func() { atomic.StoreInt32(&a.busy, 0); a.changed() }()
		a.status(brain.ActionLabel(cmd), Info)
		out, err := a.Transform(body, cmd, selection)
		if err != nil {
			a.status("Мозг: "+err.Error(), Error)
			return
		}
		if t.ReplaceSession(body, out) {
			a.lastUndo = &undo{t, body, out}
			if selection {
				a.status("Готово (над выделенным). Не понравилось — «Вернуть как было»", OK)
			} else {
				a.status("Готово. Не понравилось — «Вернуть как было»", OK)
			}
		} else {
			a.status("Ответ скопирован в буфер обмена", Warn)
		}
	}()
}

func (a *App) CanUndo() bool { return a.lastUndo != nil }

func (a *App) Undo() {
	u := a.lastUndo
	if u == nil {
		return
	}
	if u.target.ReplaceSession(u.new, u.old) {
		a.status("Вернул как было", OK)
	}
	a.lastUndo = nil
	a.changed()
}

// Transform прогоняет текст через выбранный мозг.
func (a *App) Transform(body, command string, selection bool) (string, error) {
	msgs := brain.MessagesFor(body, command, selection, a.Cfg.Prompt(false), a.Cfg.Prompt(true))
	return a.Ask(msgs, brain.ActionLabel(command), 1024)
}

// Ask — беседа → выбранная нейронка → ответ.
func (a *App) Ask(msgs []brain.Message, label string, maxTokens int) (string, error) {
	c := a.Cfg
	switch c.BrainMode {
	case "local":
		if c.LocalModel == "" {
			return "", errors.New("нейронка не выбрана: Настройки → Мозг → На этом компьютере")
		}
		if a.Local.Current() != c.LocalModel {
			a.status("Запускаю нейронку…", Info)
		}
		base, err := a.Local.Ensure(c.LocalModel)
		if err != nil {
			return "", err
		}
		a.status(label, Info)
		return brain.Chat(base, msgs, brain.ChatOptions{MaxTokens: maxTokens, LlamaExtras: true})
	case "pc":
		if c.PcBase == "" {
			return "", errors.New("не задан адрес GigaBrain/Ollama: Настройки → Мозг")
		}
		return brain.Chat(c.PcBase, msgs, brain.ChatOptions{Key: c.PcKey, Model: c.PcModel, MaxTokens: maxTokens, LlamaExtras: true})
	case "server":
		if c.ServerBase == "" {
			return "", errors.New("не задан адрес сервера: Настройки → Мозг")
		}
		return brain.Chat(c.ServerBase, msgs, brain.ChatOptions{Key: c.ServerKey, Model: c.ServerModel, MaxTokens: maxTokens, LlamaExtras: true})
	case "cloud":
		svc := brain.CloudByID(c.CloudService)
		base := c.CloudBase
		if base == "" && svc != nil {
			base = svc.Base
		}
		if base == "" || strings.Contains(base, "ACCOUNT_ID") {
			return "", errors.New("не задан адрес сервиса: Настройки → Мозг → Облачный сервис")
		}
		if c.CloudKey == "" {
			return "", errors.New("нет ключа API: Настройки → Мозг → Облачный сервис")
		}
		model := c.CloudModel
		var extras map[string]interface{}
		if svc != nil {
			if model == "" {
				model = svc.Model
			}
			extras = svc.Extras
		}
		return brain.Chat(base, msgs, brain.ChatOptions{Key: c.CloudKey, Model: model, MaxTokens: maxTokens, Extras: extras})
	}
	return "", errors.New("мозг выключен: Настройки → Мозг")
}

// ApplyCloudService: адрес и модель из пресета, ключ и аккаунт — из набора.
func (a *App) ApplyCloudService(id string) {
	svc := brain.CloudByID(id)
	if svc == nil {
		return
	}
	keys, _, _ := brain.ParseKeys(a.Cfg.CloudKeys)
	var e *brain.KeyEntry
	if k, ok := keys[id]; ok {
		e = &k
	}
	a.Cfg.BrainMode, a.Cfg.CloudService, a.Cfg.CloudBase, a.Cfg.CloudModel = "cloud", id, brain.BaseFor(svc, e), svc.Model
	a.Cfg.CloudKey = ""
	if e != nil {
		a.Cfg.CloudKey = e.Key
	}
	a.Save()
}

// ImportKeys — набор giga-keys.json.
func (a *App) ImportKeys(text string) (string, error) {
	keys, def, err := brain.ParseKeys(text)
	if err != nil {
		return "", err
	}
	a.Cfg.CloudKeys = text
	id := def
	if id == "" {
		if _, ok := keys[a.Cfg.CloudService]; ok {
			id = a.Cfg.CloudService
		} else {
			for k := range keys {
				if id == "" || k < id {
					id = k
				}
			}
		}
	}
	a.ApplyCloudService(id)
	var names []string
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	return "Ключи: " + strings.Join(names, ", ") + " — включён " + brain.CloudByID(id).Name, nil
}

// ─────────────────────────── общение ───────────────────────────

func (a *App) SendChat(q string) {
	q = strings.TrimSpace(q)
	if q == "" || a.Busy() || a.Recording() {
		return
	}
	if !a.BrainOn() {
		a.status("Для общения включите мозг: Настройки → Мозг", Warn)
		return
	}
	a.Chat = append(a.Chat, brain.Message{Role: "user", Content: q})
	atomic.StoreInt32(&a.busy, 1)
	a.changed()
	if a.OnChat != nil {
		a.OnChat()
	}
	go func() {
		defer func() { atomic.StoreInt32(&a.busy, 0); a.changed() }()
		hist := a.Chat
		if len(hist) > 20 {
			hist = hist[len(hist)-20:]
		}
		msgs := append([]brain.Message{{Role: "system", Content: a.Cfg.ChatPromptText()}}, hist...)
		a.status("Думает…", Info)
		ans, err := a.Ask(msgs, "Думает…", 2048)
		if err != nil {
			a.Chat = a.Chat[:len(a.Chat)-1]
			a.status("Мозг: "+err.Error(), Error)
		} else {
			a.Chat = append(a.Chat, brain.Message{Role: "assistant", Content: ans})
			a.status("Ответ получен", OK)
		}
		if a.OnChat != nil {
			a.OnChat()
		}
	}()
}

func (a *App) ChatAsText() string {
	var b strings.Builder
	for i, m := range a.Chat {
		if i > 0 {
			b.WriteString("\r\n\r\n")
		}
		if m.Role == "user" {
			b.WriteString("Вы: ")
		} else {
			b.WriteString("Писарь: ")
		}
		b.WriteString(m.Content)
	}
	return b.String()
}

// ─────────────────────────── записи диктофона ───────────────────────────

type Record struct {
	Path    string
	Name    string
	Seconds float64
	Bytes   int64
}

func (a *App) Records() []Record {
	entries, _ := os.ReadDir(filepath.Join(a.Dir, "records"))
	var out []Record
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".wav") {
			continue
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		size := st.Size() - 44
		if size < 0 {
			size = 0
		}
		out = append(out, Record{Path: filepath.Join(a.Dir, "records", e.Name()), Name: strings.TrimSuffix(e.Name(), ".wav"), Seconds: float64(size) / 2 / win.Rate, Bytes: st.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out
}

// TranscribeRecord расшифровывает запись по частям, вставляя каждую в цель.
func (a *App) TranscribeRecord(path string, t Target) {
	if !atomic.CompareAndSwapInt32(&a.busy, 0, 1) {
		return
	}
	a.changed()
	go func() {
		defer func() { atomic.StoreInt32(&a.busy, 0); a.changed() }()
		b, err := os.ReadFile(path)
		if err != nil {
			a.status(err.Error(), Error)
			return
		}
		s, rate, err := asr.ReadWav(b)
		if err != nil {
			a.status(err.Error(), Error)
			return
		}
		s = asr.Resample(s, rate, win.Rate)
		total := float64(len(s)) / win.Rate
		bounds := asr.ChunkBounds(total, asr.Silences(s, win.Rate, -35, 0.3), 20)
		a.sessMu.Lock()
		a.target, a.sessText = t, ""
		a.sessMu.Unlock()
		for i, bd := range bounds {
			from, to := int(bd[0]*win.Rate), int(bd[1]*win.Rate)
			if to > len(s) {
				to = len(s)
			}
			if to <= from {
				continue
			}
			chunk := s[from:to]
			piece, err := a.Recognize(chunk)
			if err != nil {
				a.status("Не расшифровал: "+err.Error(), Error)
				return
			}
			a.sessMu.Lock()
			if piece != "" {
				a.appendSession(piece, asr.TrailingQuiet(chunk) >= int(2.5*win.Rate))
			}
			a.sessMu.Unlock()
			a.status(fmt.Sprintf("Расшифровываю %s: часть %d из %d", filepath.Base(path), i+1, len(bounds)), Info)
		}
		a.status(fmt.Sprintf("Готово: %s, %d сл.", filepath.Base(path), len(strings.Fields(a.sessText))), OK)
	}()
}

// pending — куски, накопленные, пока вставка по ходу речи выключена.
var _ = errors.New

func (a *App) SessionText() string { a.sessMu.Lock(); defer a.sessMu.Unlock(); return a.sessText }
