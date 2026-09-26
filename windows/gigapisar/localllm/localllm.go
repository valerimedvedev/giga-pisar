// Нейронка на этом компьютере: llama-server из llama.cpp, как в GigaBrain, но
// внутри Писаря. Движок и модели живут в папке данных; модель поднимается
// по первому запросу и держится, пока программа работает.
package localllm

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"gigapisar/setup"
)

//go:embed catalog.json
var embeddedCatalog []byte

type Model struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Vendor  string  `json:"vendor"`
	About   string  `json:"about"`
	File    string  `json:"file"`
	URL     string  `json:"url"`
	SizeGB  float64 `json:"size_gb"`
	RAMGB   float64 `json:"ram_gb"`
	Speed   string  `json:"speed"`
	Default bool    `json:"default"`
}

type Manager struct {
	Dir     string // папка данных: bin/, models/
	Backend string // cpu | vulkan | cuda
	Threads int
	Ctx     int
	Log     func(string)
	Catalog []Model

	mu      sync.Mutex
	child   *exec.Cmd
	current string
	port    int
}

func New(dir string, log func(string)) *Manager {
	m := &Manager{Dir: dir, Backend: "cpu", Threads: runtime.NumCPU() - 2, Ctx: 8192, Log: log, port: 8093}
	if m.Threads < 1 {
		m.Threads = 1
	}
	var c struct{ Models []Model }
	json.Unmarshal(embeddedCatalog, &c)
	m.Catalog = c.Models
	os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	os.MkdirAll(filepath.Join(dir, "models"), 0o755)
	return m
}

func (m *Manager) logf(f string, a ...interface{}) {
	if m.Log != nil {
		m.Log(fmt.Sprintf(f, a...))
	}
}

func exe(n string) string {
	if runtime.GOOS == "windows" {
		return n + ".exe"
	}
	return n
}

func (m *Manager) BinPath() string  { return filepath.Join(m.Dir, "bin", exe("llama-server")) }
func (m *Manager) EngineInstalled() bool { _, err := os.Stat(m.BinPath()); return err == nil }
func (m *Manager) ModelPath(mod Model) string { return filepath.Join(m.Dir, "models", mod.File) }
func (m *Manager) HasModel(mod Model) bool {
	st, err := os.Stat(m.ModelPath(mod))
	return err == nil && st.Size() > 0
}

// Installed — модели из каталога, что уже на диске, плюс свои .gguf в папке.
func (m *Manager) Installed() []Model {
	var out []Model
	seen := map[string]bool{}
	for _, c := range m.Catalog {
		if m.HasModel(c) {
			out = append(out, c)
			seen[c.File] = true
		}
	}
	entries, _ := os.ReadDir(filepath.Join(m.Dir, "models"))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".gguf") && !seen[e.Name()] {
			out = append(out, Model{ID: strings.TrimSuffix(e.Name(), ".gguf"), Name: e.Name(), File: e.Name()})
		}
	}
	return out
}

func (m *Manager) ByID(id string) *Model {
	for _, x := range m.Installed() {
		if x.ID == id {
			y := x
			return &y
		}
	}
	return nil
}

func (m *Manager) CatalogByID(id string) *Model {
	for i := range m.Catalog {
		if m.Catalog[i].ID == id {
			return &m.Catalog[i]
		}
	}
	return nil
}

// ─────────────── движок llama.cpp: подбор сборки по ключевым словам ───────────────

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}
type release struct {
	Tag    string  `json:"tag_name"`
	Assets []asset `json:"assets"`
}

var gpuWords = []string{"cuda", "cu11", "cu12", "cu13", "vulkan", "hip", "rocm", "sycl", "opencl", "openvino", "musa", "cann"}

func has(name string, words ...string) bool {
	n := strings.ToLower(name)
	for _, w := range words {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

func assetScore(name, backend string) int {
	n := strings.ToLower(name)
	if !strings.HasSuffix(n, ".zip") && !strings.HasSuffix(n, ".tar.gz") {
		return 0
	}
	if has(n, "cudart", "src", "source", "sha256", ".txt") || !has(n, "win") || has(n, "arm64", "aarch64") {
		return 0
	}
	score := 10
	switch backend {
	case "cuda":
		if !has(n, "cuda", "cu11", "cu12", "cu13") {
			return 0
		}
		if has(n, "cu12", "cuda-12", "cuda12") {
			score += 5
		}
	case "vulkan":
		if !has(n, "vulkan") {
			return 0
		}
	default:
		if has(n, gpuWords...) {
			return 0
		}
		if has(n, "cpu") {
			score += 5
		}
		if has(n, "avx512") {
			score -= 3
		}
	}
	return score
}

func pickAsset(assets []asset, backend string) (best, cudart asset) {
	bestScore := 0
	for _, a := range assets {
		if sc := assetScore(a.Name, backend); sc > bestScore {
			best, bestScore = a, sc
		}
		if backend == "cuda" && has(a.Name, "cudart") && has(a.Name, "win") && (cudart.URL == "" || has(a.Name, "cu12", "cuda-12", "cuda12")) {
			cudart = a
		}
	}
	return
}

// InstallEngine скачивает llama-server под выбранный вариант счёта.
func (m *Manager) InstallEngine(cancel *int32, p setup.Progress) error {
	m.logf("Скачиваю llama.cpp (%s)…", m.Backend)
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/ggml-org/llama.cpp/releases?per_page=6", nil)
	req.Header.Set("User-Agent", "giga-pisar-windows")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("GitHub не отдал список выпусков: %w", err)
	}
	defer resp.Body.Close()
	var rels []release
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil || len(rels) == 0 {
		return errors.New("GitHub вернул непонятный список выпусков")
	}
	var best, cudart asset
	var tag string
	for _, r := range rels {
		best, cudart = pickAsset(r.Assets, m.Backend)
		if best.URL != "" {
			tag = r.Tag
			break
		}
	}
	if best.URL == "" {
		return fmt.Errorf("в выпуске %s нет сборки под Windows (%s); попробуйте «процессор»", rels[0].Tag, m.Backend)
	}
	m.logf("   %s → %s", tag, best.Name)
	bin := filepath.Join(m.Dir, "bin")
	tmp := filepath.Join(m.Dir, "llama.zip")
	if err := setup.Download(best.URL, tmp, cancel, p); err != nil {
		return err
	}
	err = setup.ExtractZip(tmp, bin, "llama-server.exe")
	os.Remove(tmp)
	if err != nil {
		return err
	}
	if m.Backend == "cuda" && cudart.URL != "" {
		m.logf("   библиотеки CUDA: %s", cudart.Name)
		if err := setup.Download(cudart.URL, tmp, cancel, p); err != nil {
			return err
		}
		err = setup.ExtractZip(tmp, bin, "")
		os.Remove(tmp)
		if err != nil {
			return err
		}
	}
	if !m.EngineInstalled() {
		return fmt.Errorf("в архиве %s не оказалось llama-server", best.Name)
	}
	m.logf("   ✓ llama.cpp %s", tag)
	return nil
}

// AddModel качает модель каталога или свой .gguf по адресу.
func (m *Manager) AddModel(idOrURL string, cancel *int32, p setup.Progress) (Model, error) {
	var mod Model
	if strings.HasPrefix(idOrURL, "http") {
		file := filepath.Base(strings.SplitN(idOrURL, "?", 2)[0])
		if !strings.HasSuffix(strings.ToLower(file), ".gguf") {
			return mod, fmt.Errorf("по адресу должен лежать файл .gguf, а не «%s»", file)
		}
		mod = Model{ID: strings.TrimSuffix(strings.ToLower(file), ".gguf"), Name: file, File: file, URL: idOrURL}
	} else if c := m.CatalogByID(idOrURL); c != nil {
		mod = *c
	} else {
		return mod, fmt.Errorf("в каталоге нет «%s»", idOrURL)
	}
	if m.HasModel(mod) {
		return mod, nil
	}
	m.logf("Скачиваю %s (%s)…", mod.Name, mod.File)
	if err := setup.Download(mod.URL, m.ModelPath(mod), cancel, p); err != nil {
		if err == setup.ErrNotFound {
			return mod, fmt.Errorf("файла нет по адресу %s — ссылка устарела, найдите модель на huggingface.co и вставьте адрес .gguf", mod.URL)
		}
		return mod, err
	}
	m.logf("   ✓ %s готова", mod.Name)
	return mod, nil
}

func (m *Manager) Remove(mod Model) { m.Stop(); os.Remove(m.ModelPath(mod)); os.Remove(m.ModelPath(mod) + ".part") }

// Ensure поднимает llama-server с нужной моделью и возвращает адрес OpenAI-совместимого API.
func (m *Manager) Ensure(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	base := "http://127.0.0.1:" + strconv.Itoa(m.port)
	if m.current == id && m.child != nil && m.child.ProcessState == nil {
		return base, nil
	}
	mod := m.ByID(id)
	if mod == nil {
		return "", fmt.Errorf("модель «%s» не скачана", id)
	}
	if !m.EngineInstalled() {
		return "", errors.New("движок llama.cpp не установлен")
	}
	m.stopLocked()
	args := []string{"-m", m.ModelPath(*mod), "--host", "127.0.0.1", "--port", strconv.Itoa(m.port),
		"-c", strconv.Itoa(m.Ctx), "-t", strconv.Itoa(m.Threads), "-ngl", "99", "--jinja", "--no-webui"}
	cmd := exec.Command(m.BinPath(), args...)
	cmd.Dir = filepath.Dir(m.BinPath())
	hideWindow(cmd)
	logf, _ := os.Create(filepath.Join(m.Dir, "llama-server.log"))
	cmd.Stdout, cmd.Stderr = logf, logf
	m.logf("Запускаю %s…", mod.Name)
	if err := cmd.Start(); err != nil {
		return "", err
	}
	m.child, m.current = cmd, id
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if cmd.ProcessState != nil {
			break
		}
		if resp, err := http.Get(base + "/health"); err == nil {
			ok := resp.StatusCode == 200
			resp.Body.Close()
			if ok {
				m.logf("   ✓ %s готова", mod.Name)
				return base, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	m.stopLocked()
	return "", fmt.Errorf("не поднялась за 3 минуты, подробности в %s", filepath.Join(m.Dir, "llama-server.log"))
}

func (m *Manager) Current() string { m.mu.Lock(); defer m.mu.Unlock(); if m.child != nil && m.child.ProcessState == nil { return m.current }; return "" }

func (m *Manager) Stop() { m.mu.Lock(); defer m.mu.Unlock(); m.stopLocked() }

func (m *Manager) stopLocked() {
	if m.child != nil && m.child.Process != nil {
		m.child.Process.Kill()
		m.child.Process.Wait()
	}
	m.child, m.current = nil, ""
}
