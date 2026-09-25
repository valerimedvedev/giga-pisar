// GigaBrain — мозг Писаря на своём компьютере, одним файлом.
//
// Первый запуск: ставит llama-server (движок llama.cpp) и предлагает
// скачать нейронки из каталога. Дальше — слушает http://127.0.0.1:8091
// и отвечает страницам, знающим ключ доступа:
//
//	GET  /health                 → {"status":"ok"}
//	GET  /v1/models              → скачанные модели
//	POST /v1/chat/completions    → нужная модель поднимается сама
//
// Модели переключаются по полю "model" в запросе: страница показывает
// список, человек выбирает, GigaBrain перезапускает llama-server с ней.
//
// На Windows — окно (gui_windows.go), на macOS/Linux — консоль (console_other.go).
// Этот файл — общее ядро: настройки, каталог, скачивание, движок, маршрутизатор.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const version = "1.1.0"

//go:embed catalog.json
var embeddedCatalog []byte

// Свежий каталог — с GitHub; без сети берём встроенный.
var catalogURLs = []string{
	"https://raw.githubusercontent.com/valerimedvedev/giga-pisar/main/brain-local/catalog.json",
	"https://raw.githubusercontent.com/valerimedvedev/giga-pisar/claude/new-repo-fork-package-i5w0kf/brain-local/catalog.json",
}

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
	Tier    string  `json:"tier"`
	Default bool    `json:"default"`
}

type Catalog struct {
	Models []Model `json:"models"`
}

type Config struct {
	Port    int      `json:"port"`
	Key     string   `json:"key"`
	Backend string   `json:"backend"`
	Model   string   `json:"model"`  // основная (грузится при старте)
	Models  []Model  `json:"models"` // скачанные
	Threads int      `json:"threads"`
	Ctx     int      `json:"ctx"`
	Llama   string   `json:"llama"` // версия llama.cpp
	Extra   []string `json:"extra"` // свои ключи llama-server
	Tray    bool     `json:"tray"`  // Windows: закрытие окна прячет в область уведомлений
	LAN     bool     `json:"lan"`   // слушать и домашнюю сеть (телефон, другой компьютер); ключ обязателен
}

var (
	home    string // папка данных: bin/, models/, config.json, llama-server.log
	cfg     Config
	catalog Catalog
	cfgMu   sync.Mutex

	llamaURLFlag string
	catalogFlag  string
)

// ─────────────────────────── вывод ───────────────────────────
//
// Ядро ничего не знает про окно: пишет в протокол через logf, а прогресс
// скачивания отдаёт в progressFn. Консоль и окно подписываются на них.

var (
	logFn      = func(line string) { fmt.Println(line) }
	progressFn = func(done, total int64, speed float64) {}
)

func logf(format string, a ...any) { logFn(fmt.Sprintf(format, a...)) }

// ─────────────────────────── папка данных ───────────────────────────

// Папка по умолчанию: %LOCALAPPDATA%\GigaBrain или ~/.giga/brain.
func defaultHome() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "GigaBrain")
		}
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".giga", "brain")
}

// Файл-указатель: если человек перенёс данные на другой диск, здесь путь.
func pointerPath() string { return filepath.Join(defaultHome(), "data-dir.txt") }

func resolveHome(flagDir string) string {
	if flagDir != "" {
		return flagDir
	}
	if b, err := os.ReadFile(pointerPath()); err == nil {
		if p := strings.TrimSpace(string(b)); p != "" {
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				return p
			}
		}
	}
	return defaultHome()
}

func openHome(dir string) error {
	home = dir
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(home, "models"), 0o755); err != nil {
		return err
	}
	loadConfig()
	return nil
}

// Переносит данные (движок, модели, настройки) в другую папку и запоминает её.
func moveHome(dest string) error {
	dest = filepath.Clean(dest)
	if dest == filepath.Clean(home) {
		return nil
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"bin", "models", "config.json", "GigaBrain.exe"} {
		src := filepath.Join(home, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := filepath.Join(dest, name)
		if err := os.Rename(src, dst); err != nil { // другой диск — копируем
			logf("   %s: копирую (другой диск)…", name)
			if err := copyTree(src, dst); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			os.RemoveAll(src)
		}
	}
	os.MkdirAll(defaultHome(), 0o755)
	if err := os.WriteFile(pointerPath(), []byte(dest), 0o644); err != nil {
		return err
	}
	home = dest
	return nil
}

func copyTree(src, dst string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return copyFile(src, dst)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, _ := in.Stat()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ─────────────────────────── настройки ───────────────────────────

func configPath() string { return filepath.Join(home, "config.json") }
func serverBinPath() string {
	return filepath.Join(home, "bin", exe("llama-server"))
}
func engineInstalled() bool {
	_, err := os.Stat(serverBinPath())
	return err == nil
}

func loadConfig() {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	cfg = Config{}
	b, err := os.ReadFile(configPath())
	if err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	if cfg.Port == 0 {
		cfg.Port = 8091
	}
	if cfg.Key == "" {
		buf := make([]byte, 18)
		_, _ = rand.Read(buf)
		cfg.Key = strings.NewReplacer("+", "a", "/", "b", "=", "").Replace(base64.StdEncoding.EncodeToString(buf))
	}
	if cfg.Threads == 0 {
		cfg.Threads = runtime.NumCPU() - 2
		if cfg.Threads < 1 {
			cfg.Threads = 1
		}
	}
	if cfg.Ctx == 0 {
		cfg.Ctx = 8192
	}
	if err != nil { // первый запуск
		cfg.Tray = true
	}
	// файлы, которые пропали с диска, из списка убираем
	var kept []Model
	for _, m := range cfg.Models {
		if _, err := os.Stat(modelPath(m)); err == nil {
			kept = append(kept, m)
		}
	}
	cfg.Models = kept
}

func saveConfig() {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	b, _ := json.MarshalIndent(cfg, "", "  ")
	_ = os.WriteFile(configPath(), b, 0o600)
}

func modelPath(m Model) string { return filepath.Join(home, "models", m.File) }

func findInstalled(id string) *Model {
	for i := range cfg.Models {
		if cfg.Models[i].ID == id {
			return &cfg.Models[i]
		}
	}
	return nil
}

func installedIDs() string {
	var ids []string
	for _, m := range cfg.Models {
		ids = append(ids, m.ID)
	}
	return strings.Join(ids, ", ")
}

// Убирает модель с диска и из списка.
func removeModel(id string) {
	var kept []Model
	for _, m := range cfg.Models {
		if m.ID == id {
			os.Remove(modelPath(m))
			os.Remove(modelPath(m) + ".part")
			continue
		}
		kept = append(kept, m)
	}
	cfg.Models = kept
	if cfg.Model == id {
		cfg.Model = ""
		if len(kept) > 0 {
			cfg.Model = kept[0].ID
		}
	}
	saveConfig()
}

// ─────────────────────────── каталог ───────────────────────────

func loadCatalog(own string) error {
	_ = json.Unmarshal(embeddedCatalog, &catalog)
	client := &http.Client{Timeout: 8 * time.Second}
	urls := catalogURLs
	if own != "" {
		if !strings.HasPrefix(own, "http") {
			b, err := os.ReadFile(own)
			if err != nil {
				return errors.New("каталог не прочитался: " + err.Error())
			}
			var c Catalog
			if json.Unmarshal(b, &c) != nil || len(c.Models) == 0 {
				return errors.New("каталог " + own + " не разобрался")
			}
			catalog = c
			return nil
		}
		urls = []string{own}
	}
	for _, u := range urls {
		resp, err := client.Get(u)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		var fresh Catalog
		if json.NewDecoder(resp.Body).Decode(&fresh) == nil && len(fresh.Models) > 0 {
			catalog = fresh
		}
		resp.Body.Close()
		break
	}
	return nil
}

func catalogModel(id string) *Model {
	for i := range catalog.Models {
		if catalog.Models[i].ID == id {
			return &catalog.Models[i]
		}
	}
	return nil
}

func defaultCatalogIndex() int {
	for i, m := range catalog.Models {
		if m.Default {
			return i
		}
	}
	return 0
}

// Модель по id из каталога или по адресу .gguf.
func modelFor(idOrURL string) (Model, error) {
	if strings.HasPrefix(idOrURL, "http://") || strings.HasPrefix(idOrURL, "https://") {
		u, err := url.Parse(idOrURL)
		if err != nil {
			return Model{}, err
		}
		file := filepath.Base(u.Path)
		if !strings.HasSuffix(strings.ToLower(file), ".gguf") {
			return Model{}, fmt.Errorf("по адресу должен лежать файл .gguf, а не «%s»", file)
		}
		return Model{ID: strings.TrimSuffix(strings.ToLower(file), ".gguf"), Name: file, File: file, URL: idOrURL}, nil
	}
	if c := catalogModel(idOrURL); c != nil {
		return *c, nil
	}
	return Model{}, fmt.Errorf("в каталоге нет «%s»", idOrURL)
}

func addModel(idOrURL string) error {
	m, err := modelFor(idOrURL)
	if err != nil {
		return err
	}
	if findInstalled(m.ID) != nil {
		logf("== %s уже скачана", m.Name)
		return nil
	}
	if m.RAMGB > 0 && float64(totalRAMGB()) < m.RAMGB {
		logf("!  %s нужно около %.0f ГБ памяти, у компьютера %d ГБ: запустится, но будет выгружать другие программы на диск.", m.Name, m.RAMGB, totalRAMGB())
	}
	logf("== Скачиваю %s (%s) — это надолго", m.Name, m.File)
	if err := download(m.URL, modelPath(m)); err != nil {
		if errors.Is(err, errNotFound) {
			os.Remove(modelPath(m) + ".part")
			return fmt.Errorf("файла нет по адресу %s\n   Ссылка в каталоге устарела: найдите модель на huggingface.co и укажите адрес .gguf в поле «Свой адрес»", m.URL)
		}
		return fmt.Errorf("модель не скачалась: %w", err)
	}
	cfg.Models = append(cfg.Models, m)
	if cfg.Model == "" {
		cfg.Model = m.ID
	}
	saveConfig()
	logf("   ✓ %s готова", m.Name)
	return nil
}

// ─────────────────────────── llama.cpp ───────────────────────────

var errNotFound = errors.New("404")

// Сборка llama.cpp под платформу — по ключевым словам в имени файла:
// схема имён в выпусках меняется (b1234 → v0.5.0), жёсткий шаблон ломается.
type asset struct{ Name, URL string }

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

// Подходит ли файл под платформу и вариант счёта. Возвращает вес: чем больше, тем лучше.
func assetScoreFor(name, backend, goos, goarch string) int {
	n := strings.ToLower(name)
	if !strings.HasSuffix(n, ".zip") && !strings.HasSuffix(n, ".tar.gz") && !strings.HasSuffix(n, ".tgz") {
		return 0
	}
	if has(n, "cudart", "src", "source", "sha256", ".txt") {
		return 0
	}
	switch goos {
	case "windows":
		if !has(n, "win") {
			return 0
		}
	case "darwin":
		if !has(n, "macos", "darwin", "osx") {
			return 0
		}
	default:
		if !has(n, "ubuntu", "linux") {
			return 0
		}
	}
	switch goarch {
	case "amd64":
		if has(n, "arm64", "aarch64") {
			return 0
		}
	case "arm64":
		if !has(n, "arm64", "aarch64") {
			return 0
		}
	}
	score := 10
	switch backend {
	case "cuda":
		if !has(n, "cuda", "cu11", "cu12", "cu13") {
			return 0
		}
		if has(n, "cu12", "cuda-12", "cuda12") {
			score += 5 // самая ходовая версия CUDA
		}
	case "vulkan":
		if !has(n, "vulkan") {
			return 0
		}
	default: // cpu: файл с «cpu», а если таких нет — без слов про видеокарты
		if has(n, gpuWords...) {
			return 0
		}
		if has(n, "cpu") {
			score += 5
		}
		if has(n, "avx512") {
			score -= 3 // не на всех процессорах
		}
		if has(n, "openblas", "noavx") {
			score -= 5
		}
	}
	return score
}

// Лучший файл из выпуска (и cudart для CUDA на Windows), либо пусто.
func pickAsset(assets []asset, backend string) (best, cudart asset) {
	return pickAssetFor(assets, backend, runtime.GOOS, runtime.GOARCH)
}

func pickAssetFor(assets []asset, backend, goos, goarch string) (best, cudart asset) {
	bestScore := 0
	for _, a := range assets {
		if sc := assetScoreFor(a.Name, backend, goos, goarch); sc > bestScore {
			best, bestScore = a, sc
		}
		if backend == "cuda" && goos == "windows" && has(a.Name, "cudart") && has(a.Name, "win") {
			if cudart.URL == "" || has(a.Name, "cu12", "cuda-12", "cuda12") {
				cudart = a
			}
		}
	}
	return
}

type release struct {
	Tag    string  `json:"tag_name"`
	Assets []asset `json:"assets"`
}

func (a *asset) UnmarshalJSON(b []byte) error {
	var raw struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	a.Name, a.URL = raw.Name, raw.URL
	return nil
}

func githubJSON(u string, v any) error {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "giga-pisar-brain")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GitHub ответил %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// Выбор сборки не удался: ядро отдаёт файлы свежего выпуска, чтобы
// интерфейс (окно или консоль) дал человеку выбрать самому.
type errNoAsset struct {
	Tag    string
	Assets []asset
}

func (e *errNoAsset) Error() string {
	return fmt.Sprintf("в выпуске %s нет сборки под %s/%s (%s)", e.Tag, runtime.GOOS, runtime.GOARCH, cfg.Backend)
}

// Находит подходящую сборку llama.cpp в последних выпусках.
func findLlama() (best, cudart asset, tag string, err error) {
	var rels []release
	if err = githubJSON("https://api.github.com/repos/ggml-org/llama.cpp/releases?per_page=6", &rels); err != nil {
		return best, cudart, "", fmt.Errorf("GitHub не отдал список выпусков: %w", err)
	}
	if len(rels) == 0 {
		return best, cudart, "", errors.New("GitHub вернул пустой список выпусков")
	}
	for _, r := range rels { // свежий выпуск может быть ещё без сборок — берём следующий
		best, cudart = pickAsset(r.Assets, cfg.Backend)
		if best.URL != "" {
			return best, cudart, r.Tag, nil
		}
	}
	return best, cudart, "", &errNoAsset{Tag: rels[0].Tag, Assets: rels[0].Assets}
}

// Ставит движок: по адресу (--llama-url), по выбранному файлу выпуска или сам.
func installLlama(chosen *asset, chosenTag string) error {
	bin := filepath.Join(home, "bin")
	if llamaURLFlag != "" {
		logf("== Скачиваю llama.cpp по адресу %s", llamaURLFlag)
		if err := fetchArchive(llamaURLFlag, bin, "llama-server"); err != nil {
			return err
		}
		cfg.Llama = "custom"
		saveConfig()
		return nil
	}
	var best, cudart asset
	var tag string
	if chosen != nil {
		best, tag = *chosen, chosenTag
	} else {
		logf("== Скачиваю llama.cpp (%s)", cfg.Backend)
		var err error
		best, cudart, tag, err = findLlama()
		if err != nil {
			return err
		}
	}
	logf("   %s → %s", tag, best.Name)
	if err := fetchArchive(best.URL, bin, "llama-server"); err != nil {
		return err
	}
	if cfg.Backend == "cuda" && cudart.URL != "" {
		logf("   библиотеки CUDA: %s", cudart.Name)
		if err := fetchArchive(cudart.URL, bin, ""); err != nil {
			return fmt.Errorf("библиотеки CUDA: %w", err)
		}
	}
	if runtime.GOOS != "windows" {
		os.Chmod(filepath.Join(bin, "llama-server"), 0o755)
	}
	if !engineInstalled() {
		return fmt.Errorf("в архиве %s не оказалось llama-server", best.Name)
	}
	cfg.Llama = tag
	saveConfig()
	logf("   ✓ llama.cpp %s", tag)
	return nil
}

// Скачивает архив и раскладывает его файлы в dest. Если задан anchor,
// берётся папка, где лежит этот файл (в архивах бывает вложенная папка).
func fetchArchive(u, dest, anchor string) error {
	tmp := filepath.Join(home, "download.tmp")
	if err := download(u, tmp); err != nil {
		return err
	}
	defer os.Remove(tmp)
	if strings.HasSuffix(u, ".zip") {
		return unzip(tmp, dest, anchor)
	}
	return untar(tmp, dest, anchor)
}

func unzip(src, dest, anchor string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	base := ""
	if anchor != "" {
		for _, f := range r.File {
			if filepath.Base(f.Name) == exe(anchor) {
				base = strings.TrimSuffix(f.Name, filepath.Base(f.Name))
			}
		}
	}
	for _, f := range r.File {
		if f.FileInfo().IsDir() || (base != "" && !strings.HasPrefix(f.Name, base)) {
			continue
		}
		rel := strings.TrimPrefix(f.Name, base)
		if base == "" {
			rel = filepath.Base(f.Name)
		}
		if err := extractOne(f.Open, filepath.Join(dest, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	return nil
}

func untar(src, dest, anchor string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	base := ""
	if anchor != "" { // первый проход — найти папку с anchor; второй — распаковать
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err != nil {
				break
			}
			if filepath.Base(h.Name) == anchor {
				base = strings.TrimSuffix(h.Name, filepath.Base(h.Name))
			}
		}
		f.Seek(0, io.SeekStart)
		gz, _ = gzip.NewReader(f)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg || (base != "" && !strings.HasPrefix(h.Name, base)) {
			continue
		}
		rel := strings.TrimPrefix(h.Name, base)
		if base == "" {
			rel = filepath.Base(h.Name)
		}
		out := filepath.Join(dest, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(out), 0o755)
		w, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)|0o644)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, tr)
		w.Close()
		if err != nil {
			return err
		}
	}
}

func extractOne(open func() (io.ReadCloser, error), out string) error {
	rc, err := open()
	if err != nil {
		return err
	}
	defer rc.Close()
	os.MkdirAll(filepath.Dir(out), 0o755)
	w, err := os.Create(out)
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = io.Copy(w, rc)
	return err
}

// ─────────────────────────── скачивание с докачкой ───────────────────────────

func download(u, dest string) error {
	part := dest + ".part"
	var have int64
	if st, err := os.Stat(part); err == nil {
		have = st.Size()
	}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "giga-pisar-brain")
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := (&http.Client{Timeout: 0}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200:
		have = 0
	case 206:
	case 404:
		return errNotFound
	default:
		return fmt.Errorf("сервер ответил %s", resp.Status)
	}
	flags := os.O_CREATE | os.O_WRONLY
	if have > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return err
	}
	total := have + resp.ContentLength
	if resp.ContentLength < 0 {
		total = 0
	}
	pw := &progress{w: f, done: have, total: total, start: time.Now()}
	_, err = io.Copy(pw, resp.Body)
	f.Close()
	progressFn(pw.done, total, 0)
	if err != nil {
		return fmt.Errorf("оборвалось (%w) — запустите скачивание ещё раз, докачается", err)
	}
	progressFn(0, 0, 0)
	return os.Rename(part, dest)
}

type progress struct {
	w           io.Writer
	done, total int64
	start, last time.Time
}

func (p *progress) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.done += int64(n)
	if time.Since(p.last) > 300*time.Millisecond || err != nil {
		p.last = time.Now()
		speed := float64(p.done) / 1e6 / time.Since(p.start).Seconds()
		progressFn(p.done, p.total, speed)
	}
	return n, err
}

// ─────────────────────────── маршрутизатор ───────────────────────────

type router struct {
	bin     string
	mu      sync.Mutex
	child   *exec.Cmd
	current string
	loading string
	port    int
	proxy   *httputil.ReverseProxy
	onState func() // окно перерисовывает состояние
	srv     *http.Server
	ln      net.Listener
	lnMu    sync.Mutex
}

func (r *router) changed() {
	if r.onState != nil {
		r.onState()
	}
}

// Что сейчас с нейронкой: id запущенной и id загружаемой.
func (r *router) status() (current, loading string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.child != nil && r.child.ProcessState == nil {
		current = r.current
	}
	return current, r.loading
}

func (r *router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")
	if origin == "" {
		origin = "*"
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", origin)
	h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	h.Set("Access-Control-Allow-Private-Network", "true")
	h.Set("Vary", "Origin")
	if req.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	if req.Header.Get("Authorization") != "Bearer "+cfg.Key {
		writeJSON(w, 401, map[string]any{"error": map[string]string{"message": "нужен ключ доступа GigaBrain (он в окне программы)", "type": "unauthorized"}})
		return
	}
	switch {
	case req.URL.Path == "/health":
		writeJSON(w, 200, map[string]string{"status": "ok"})
	case req.URL.Path == "/v1/models" && req.Method == http.MethodGet:
		var data []map[string]any
		for _, m := range cfg.Models {
			data = append(data, map[string]any{"id": m.ID, "object": "model", "owned_by": m.Vendor, "name": m.Name})
		}
		writeJSON(w, 200, map[string]any{"object": "list", "data": data})
	case req.URL.Path == "/v1/chat/completions" || req.URL.Path == "/v1/completions":
		body, err := io.ReadAll(io.LimitReader(req.Body, 4<<20))
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		var probe struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &probe)
		id := probe.Model
		if findInstalled(id) == nil {
			id = cfg.Model
		}
		if err := r.ensure(id); err != nil {
			writeJSON(w, 503, map[string]any{"error": map[string]string{"message": "нейронка не запустилась: " + err.Error()}})
			return
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		r.proxy.ServeHTTP(w, req)
	default:
		if r.proxy == nil {
			writeJSON(w, 503, map[string]string{"error": "нейронка ещё не запущена"})
			return
		}
		r.proxy.ServeHTTP(w, req)
	}
}

// Поднимает llama-server с нужной моделью (если ещё не она) и ждёт готовности.
func (r *router) ensure(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current == id && r.child != nil && r.child.ProcessState == nil {
		return nil
	}
	m := findInstalled(id)
	if m == nil {
		return fmt.Errorf("модель «%s» не скачана", id)
	}
	if !engineInstalled() {
		return errors.New("движок llama.cpp не установлен")
	}
	r.stopChildLocked()
	if r.port == 0 {
		r.port = cfg.Port + 1
	}
	args := []string{"-m", modelPath(*m), "--host", "127.0.0.1", "--port", strconv.Itoa(r.port),
		"-c", strconv.Itoa(cfg.Ctx), "-t", strconv.Itoa(cfg.Threads), "-ngl", "99", "--jinja", "--no-webui"}
	args = append(args, cfg.Extra...)
	cmd := exec.Command(r.bin, args...)
	cmd.Dir = filepath.Dir(r.bin)
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+filepath.Dir(r.bin), "DYLD_LIBRARY_PATH="+filepath.Dir(r.bin))
	hideWindow(cmd)
	logf_, _ := os.Create(filepath.Join(home, "llama-server.log"))
	cmd.Stdout, cmd.Stderr = logf_, logf_
	logf("== Запускаю %s…", m.Name)
	r.loading = id
	r.changed()
	if err := cmd.Start(); err != nil {
		r.loading = ""
		r.changed()
		return err
	}
	r.child, r.current = cmd, id
	target, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(r.port))
	r.proxy = httputil.NewSingleHostReverseProxy(target)
	r.proxy.FlushInterval = -1 // потоковые ответы — сразу
	// ждём «ok»; большая модель с холодного диска грузится и минуту
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if cmd.ProcessState != nil {
			break
		}
		resp, err := http.Get(target.String() + "/health")
		if err == nil {
			ok := resp.StatusCode == 200
			resp.Body.Close()
			if ok {
				logf("   ✓ %s готова", m.Name)
				r.loading = ""
				r.changed()
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	r.stopChildLocked()
	r.loading = ""
	r.changed()
	return fmt.Errorf("не поднялась за 3 минуты, подробности в %s", filepath.Join(home, "llama-server.log"))
}

func (r *router) stopChild() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopChildLocked()
	r.changed()
}

func (r *router) stopChildLocked() {
	if r.child != nil && r.child.Process != nil {
		_ = r.child.Process.Kill()
		_, _ = r.child.Process.Wait()
	}
	r.child, r.current = nil, ""
}

// Слушает порт из настроек; повторный вызов переоткрывает на новом порту.
func (r *router) listen() error {
	r.lnMu.Lock()
	defer r.lnMu.Unlock()
	if r.ln != nil {
		r.ln.Close()
		r.ln = nil
	}
	host := "127.0.0.1"
	if cfg.LAN {
		host = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%d", host, cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("порт %d занят (уже запущен GigaBrain?): %v", cfg.Port, err)
	}
	r.ln = ln
	r.srv = &http.Server{Addr: addr, Handler: r}
	go r.srv.Serve(ln)
	return nil
}

func (r *router) shutdown() {
	r.stopChild()
	r.lnMu.Lock()
	if r.ln != nil {
		r.ln.Close()
		r.ln = nil
	}
	r.lnMu.Unlock()
}

// Адреса компьютера в домашней сети — чтобы показать, что вводить на телефоне.
func lanAddresses() []string {
	var out []string
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				if ip4 := ipn.IP.To4(); ip4 != nil && ip4.IsPrivate() {
					out = append(out, ip4.String())
				}
			}
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ─────────────────────────── мелочи ───────────────────────────

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func must(err error) {
	if err != nil {
		fatal(err.Error())
	}
}

func backendName(b string) string {
	switch b {
	case "vulkan":
		return "видеокарта (Vulkan)"
	case "cuda":
		return "видеокарта NVIDIA (CUDA)"
	}
	return "процессор"
}

func fmtGB(gb float64) string {
	if gb <= 0 {
		return "—"
	}
	return strconv.FormatFloat(gb, 'f', 1, 64) + " ГБ"
}
