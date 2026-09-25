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
// Всё лежит в %LOCALAPPDATA%\GigaBrain (Windows) или ~/.giga/brain.
//
// Ключи: --add <id|url> скачать модель, --model <id> сделать основной,
// --backend cpu|vulkan|cuda, --port, --dir, --list, --no-menu.
package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const version = "1.0.0"

//go:embed catalog.json
var embeddedCatalog []byte

// Свежий каталог — с GitHub; без сети берём встроенный.
var catalogURLs = []string{
	"https://raw.githubusercontent.com/valerimedvedev/giga-pisar/main/brain-local/catalog.json",
	"https://raw.githubusercontent.com/valerimedvedev/giga-pisar/claude/new-repo-fork-package-i5w0kf/brain-local/catalog.json",
}

const llamaReleases = "https://api.github.com/repos/ggml-org/llama.cpp/releases/latest"

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
}

var (
	home    string
	cfg     Config
	catalog Catalog
	stdin   = bufio.NewReader(os.Stdin)
)

func main() {
	setupConsole()
	var (
		dir     = flag.String("dir", "", "папка GigaBrain (по умолчанию %LOCALAPPDATA%\\GigaBrain или ~/.giga/brain)")
		port    = flag.Int("port", 0, "порт (по умолчанию 8091)")
		backend = flag.String("backend", "", "cpu | vulkan | cuda (Windows/Linux)")
		add     = flag.String("add", "", "скачать модель: id из каталога или адрес .gguf")
		model   = flag.String("model", "", "основная модель (id)")
		list    = flag.Bool("list", false, "показать каталог и выйти")
		noMenu  = flag.Bool("no-menu", false, "не показывать меню в консоли")
		serverB = flag.String("server-bin", "", "свой llama-server (для отладки)")
		showVer = flag.Bool("version", false, "версия")
		catFlag = flag.String("catalog", "", "свой каталог моделей: файл или адрес (тогда каталог с GitHub не берётся)")
	)
	flag.Parse()
	if *showVer {
		fmt.Println("GigaBrain", version)
		return
	}

	home = *dir
	if home == "" {
		home = defaultHome()
	}
	must(os.MkdirAll(filepath.Join(home, "bin"), 0o755))
	must(os.MkdirAll(filepath.Join(home, "models"), 0o755))
	loadConfig()
	if *port > 0 {
		cfg.Port = *port
	}
	if *backend != "" {
		cfg.Backend = *backend
	}
	loadCatalog(*catFlag)

	fmt.Printf("== Мозг Писаря (GigaBrain %s) — %s\n", version, home)
	fmt.Printf("   память компьютера: %d ГБ, ядер: %d\n", totalRAMGB(), runtime.NumCPU())

	if *list {
		printCatalog()
		return
	}

	// 1. движок
	serverBin := *serverB
	if serverBin == "" {
		serverBin = filepath.Join(home, "bin", exe("llama-server"))
		if _, err := os.Stat(serverBin); err != nil {
			if cfg.Backend == "" {
				cfg.Backend = askBackend()
			}
			if err := installLlama(); err != nil {
				fatal("llama.cpp не установился: " + err.Error())
			}
		}
	}

	// 2. модели
	if *add != "" {
		if err := addModel(*add); err != nil {
			fatal(err.Error())
		}
	}
	if len(cfg.Models) == 0 {
		fmt.Println()
		fmt.Println("== Нейронок ещё нет. Какую скачать?")
		if err := addModel(chooseModel()); err != nil {
			fatal(err.Error())
		}
	}
	if *model != "" {
		if findInstalled(*model) == nil {
			fatal("модель «" + *model + "» не скачана; --add " + *model)
		}
		cfg.Model = *model
	}
	if findInstalled(cfg.Model) == nil {
		cfg.Model = cfg.Models[0].ID
	}
	saveConfig()
	makeShortcut()

	// 3. маршрутизатор
	r := &router{bin: serverBin}
	go r.ensure(cfg.Model) // основная модель поднимается сразу, пока человек читает
	srv := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", cfg.Port), Handler: r}
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		fatal(fmt.Sprintf("порт %d занят (уже запущен GigaBrain?): %v", cfg.Port, err))
	}
	go srv.Serve(ln)

	fmt.Println()
	fmt.Printf("✓ Мозг слушает  http://127.0.0.1:%d\n", cfg.Port)
	fmt.Printf("  Ключ доступа:  %s\n", cfg.Key)
	fmt.Printf("  Модели: %s (основная — %s)\n", installedIDs(), cfg.Model)
	fmt.Println("  На странице: ⚙ Мозг → «Нейронка на моём компьютере» → адрес и ключ → «Найти / проверить».")
	fmt.Println("  Закройте это окно (или Ctrl+C), чтобы выключить мозг.")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	if !*noMenu {
		go menu(r)
	}
	<-stop
	fmt.Println("\nВыключаю…")
	r.stopChild()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

// ─────────────────────────── настройки ───────────────────────────

func defaultHome() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "GigaBrain")
		}
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".giga", "brain")
}

func configPath() string { return filepath.Join(home, "config.json") }

func loadConfig() {
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

// ─────────────────────────── каталог ───────────────────────────

func loadCatalog(own string) {
	_ = json.Unmarshal(embeddedCatalog, &catalog)
	client := &http.Client{Timeout: 8 * time.Second}
	urls := catalogURLs
	if own != "" {
		if !strings.HasPrefix(own, "http") {
			b, err := os.ReadFile(own)
			if err != nil {
				fatal("каталог не прочитался: " + err.Error())
			}
			var c Catalog
			if json.Unmarshal(b, &c) != nil || len(c.Models) == 0 {
				fatal("каталог " + own + " не разобрался")
			}
			catalog = c
			return
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
}

func catalogModel(id string) *Model {
	for i := range catalog.Models {
		if catalog.Models[i].ID == id {
			return &catalog.Models[i]
		}
	}
	return nil
}

func printCatalog() {
	ram := float64(totalRAMGB())
	fmt.Println()
	fmt.Printf("   %-3s %-28s %-8s %-8s %-10s %s\n", "№", "Модель", "Файл", "Памяти", "Скорость", "Про что")
	for i, m := range catalog.Models {
		mark := " "
		if findInstalled(m.ID) != nil {
			mark = "✓"
		} else if m.RAMGB > ram {
			mark = "!"
		}
		fmt.Printf(" %s %2d. %-28s %5.1f ГБ %5.0f ГБ  %-10s %s\n", mark, i+1, m.Name, m.SizeGB, m.RAMGB, m.Speed, m.About)
	}
	fmt.Println("   ✓ — уже скачана, ! — памяти впритык. Любой другой .gguf: --add <адрес>")
}

func chooseModel() string {
	printCatalog()
	def := 1
	for i, m := range catalog.Models {
		if m.Default {
			def = i + 1
		}
	}
	for {
		fmt.Printf("\nНомер модели [%d]: ", def)
		line, _ := stdin.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return catalog.Models[def-1].ID
		}
		if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= len(catalog.Models) {
			return catalog.Models[n-1].ID
		}
		if strings.HasPrefix(line, "http") {
			return line
		}
		fmt.Println("Введите номер из списка или адрес .gguf")
	}
}

func addModel(idOrURL string) error {
	var m Model
	if strings.HasPrefix(idOrURL, "http://") || strings.HasPrefix(idOrURL, "https://") {
		u, err := url.Parse(idOrURL)
		if err != nil {
			return err
		}
		file := filepath.Base(u.Path)
		m = Model{ID: strings.TrimSuffix(strings.ToLower(file), ".gguf"), Name: file, File: file, URL: idOrURL}
	} else if c := catalogModel(idOrURL); c != nil {
		m = *c
	} else {
		return fmt.Errorf("в каталоге нет «%s» (--list покажет, что есть)", idOrURL)
	}
	if findInstalled(m.ID) != nil {
		fmt.Printf("== %s уже скачана\n", m.Name)
		return nil
	}
	if m.RAMGB > 0 && float64(totalRAMGB()) < m.RAMGB {
		fmt.Printf("!  %s нужно около %.0f ГБ памяти, у компьютера %d ГБ: запустится, но будет выгружать другие программы на диск.\n", m.Name, m.RAMGB, totalRAMGB())
	}
	fmt.Printf("== Скачиваю %s (%s) — это надолго\n", m.Name, m.File)
	if err := download(m.URL, modelPath(m)); err != nil {
		os.Remove(modelPath(m))
		if errors.Is(err, errNotFound) {
			return fmt.Errorf("файла нет по адресу %s\n   Ссылка в каталоге устарела: найдите модель на huggingface.co и укажите адрес .gguf: --add <адрес>", m.URL)
		}
		return fmt.Errorf("модель не скачалась: %w", err)
	}
	cfg.Models = append(cfg.Models, m)
	if cfg.Model == "" {
		cfg.Model = m.ID
	}
	saveConfig()
	fmt.Printf("   ✓ %s готова\n", m.Name)
	return nil
}

// ─────────────────────────── llama.cpp ───────────────────────────

func askBackend() string {
	if runtime.GOOS == "darwin" {
		return "cpu" // на маке сборка одна, видеокарта включается сама
	}
	fmt.Println()
	fmt.Println("== Где считать?")
	fmt.Println("   1. Процессор — работает везде (по умолчанию)")
	fmt.Println("   2. Видеокарта через Vulkan — NVIDIA, AMD, Intel; заметно быстрее")
	if runtime.GOOS == "windows" {
		fmt.Println("   3. Видеокарта NVIDIA через CUDA — быстрее всего, нужны драйверы NVIDIA")
	}
	fmt.Print("Вариант [1]: ")
	line, _ := stdin.ReadString('\n')
	switch strings.TrimSpace(line) {
	case "2":
		return "vulkan"
	case "3":
		return "cuda"
	}
	return "cpu"
}

var errNotFound = errors.New("404")

// Сборка llama.cpp под платформу: имена файлов в выпусках.
func llamaAssetPattern() (*regexp.Regexp, error) {
	be := cfg.Backend
	if be == "" {
		be = "cpu"
	}
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "windows/amd64":
		switch be {
		case "vulkan":
			return regexp.MustCompile(`bin-win-vulkan-x64\.zip$`), nil
		case "cuda":
			return regexp.MustCompile(`bin-win-cuda-1[23][\.\d]*-x64\.zip$`), nil
		default:
			return regexp.MustCompile(`bin-win-cpu-x64\.zip$`), nil
		}
	case "windows/arm64":
		return regexp.MustCompile(`bin-win-cpu-arm64\.zip$`), nil
	case "darwin/arm64":
		return regexp.MustCompile(`bin-macos-arm64\.(zip|tar\.gz)$`), nil
	case "darwin/amd64":
		return regexp.MustCompile(`bin-macos-x64\.(zip|tar\.gz)$`), nil
	case "linux/amd64":
		if be == "vulkan" {
			return regexp.MustCompile(`bin-ubuntu-vulkan-x64\.(zip|tar\.gz)$`), nil
		}
		return regexp.MustCompile(`bin-ubuntu-x64\.(zip|tar\.gz)$`), nil
	case "linux/arm64":
		return regexp.MustCompile(`bin-ubuntu-arm64\.(zip|tar\.gz)$`), nil
	}
	return nil, fmt.Errorf("нет сборки llama.cpp для %s/%s", runtime.GOOS, runtime.GOARCH)
}

func installLlama() error {
	pat, err := llamaAssetPattern()
	if err != nil {
		return err
	}
	fmt.Printf("== Скачиваю llama.cpp (%s)\n", cfg.Backend)
	req, _ := http.NewRequest("GET", llamaReleases, nil)
	req.Header.Set("User-Agent", "giga-pisar-brain")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var rel struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return fmt.Errorf("GitHub не отдал список выпусков: %w", err)
	}
	var mainURL, cudartURL string
	for _, a := range rel.Assets {
		if mainURL == "" && pat.MatchString(a.Name) {
			mainURL = a.URL
		}
		if cfg.Backend == "cuda" && strings.HasPrefix(a.Name, "cudart") && strings.Contains(a.Name, "win") {
			cudartURL = a.URL
		}
	}
	if mainURL == "" {
		return fmt.Errorf("в выпуске %s нет сборки под %s — посмотрите https://github.com/ggml-org/llama.cpp/releases и запустите с --backend cpu", rel.Tag, cfg.Backend)
	}
	bin := filepath.Join(home, "bin")
	if err := fetchArchive(mainURL, bin, "llama-server"); err != nil {
		return err
	}
	if cudartURL != "" {
		if err := fetchArchive(cudartURL, bin, ""); err != nil {
			return fmt.Errorf("библиотеки CUDA: %w", err)
		}
	}
	if runtime.GOOS != "windows" {
		os.Chmod(filepath.Join(bin, "llama-server"), 0o755)
	}
	cfg.Llama = rel.Tag
	saveConfig()
	fmt.Printf("   ✓ llama.cpp %s\n", rel.Tag)
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
	// первый проход — найти папку с anchor; второй — распаковать
	base := ""
	if anchor != "" {
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
	fmt.Println()
	if err != nil {
		return fmt.Errorf("оборвалось (%w) — запустите ещё раз, докачается", err)
	}
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
	if time.Since(p.last) > 500*time.Millisecond || err != nil {
		p.last = time.Now()
		mb := float64(p.done) / 1e6
		speed := mb / time.Since(p.start).Seconds()
		if p.total > 0 {
			fmt.Printf("\r   %5.1f%%  %6.1f / %.1f МБ  %5.1f МБ/с   ", 100*float64(p.done)/float64(p.total), mb, float64(p.total)/1e6, speed)
		} else {
			fmt.Printf("\r   %6.1f МБ  %5.1f МБ/с   ", mb, speed)
		}
	}
	return n, err
}

// ─────────────────────────── маршрутизатор ───────────────────────────

type router struct {
	bin     string
	mu      sync.Mutex
	child   *exec.Cmd
	current string
	port    int
	proxy   *httputil.ReverseProxy
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
	logf, _ := os.Create(filepath.Join(home, "llama-server.log"))
	cmd.Stdout, cmd.Stderr = logf, logf
	fmt.Printf("\n== Запускаю %s…", m.Name)
	if err := cmd.Start(); err != nil {
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
				fmt.Println(" готово")
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
		fmt.Print(".")
	}
	fmt.Println()
	r.stopChildLocked()
	return fmt.Errorf("не поднялась за 3 минуты, подробности в %s", filepath.Join(home, "llama-server.log"))
}

func (r *router) stopChild() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopChildLocked()
}

func (r *router) stopChildLocked() {
	if r.child != nil && r.child.Process != nil {
		_ = r.child.Process.Kill()
		_, _ = r.child.Process.Wait()
	}
	r.child, r.current = nil, ""
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ─────────────────────────── меню в консоли ───────────────────────────

func menu(r *router) {
	fmt.Println("\n  Команды: [m] сменить основную модель  [a] скачать ещё  [k] показать ключ  [l] каталог  [q] выход")
	for {
		line, err := stdin.ReadString('\n')
		if err != nil {
			return
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "m":
			for i, m := range cfg.Models {
				fmt.Printf("   %d. %s\n", i+1, m.Name)
			}
			fmt.Print("Номер: ")
			s, _ := stdin.ReadString('\n')
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n >= 1 && n <= len(cfg.Models) {
				cfg.Model = cfg.Models[n-1].ID
				saveConfig()
				go r.ensure(cfg.Model)
			}
		case "a":
			if err := addModel(chooseModel()); err != nil {
				fmt.Println("!", err)
			}
		case "k":
			fmt.Printf("   Адрес: http://127.0.0.1:%d   Ключ: %s\n", cfg.Port, cfg.Key)
		case "l":
			printCatalog()
		case "q":
			r.stopChild()
			os.Exit(0)
		}
	}
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

func fatal(msg string) {
	fmt.Println("\n! " + msg)
	if runtime.GOOS == "windows" {
		fmt.Print("Нажмите Enter, чтобы закрыть…")
		stdin.ReadString('\n')
	}
	os.Exit(1)
}

func makeShortcut() {
	if runtime.GOOS != "windows" {
		return
	}
	mark := filepath.Join(home, ".shortcut")
	if _, err := os.Stat(mark); err == nil {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	// exe копируем к себе: с флешки или из «Загрузок» он может исчезнуть
	dst := filepath.Join(home, "GigaBrain.exe")
	if !strings.EqualFold(self, dst) {
		if b, err := os.ReadFile(self); err == nil {
			_ = os.WriteFile(dst, b, 0o755)
		}
	}
	ps := fmt.Sprintf(`$s=(New-Object -ComObject WScript.Shell).CreateShortcut([Environment]::GetFolderPath('Desktop')+'\Мозг Писаря.lnk');$s.TargetPath='%s';$s.WorkingDirectory='%s';$s.Description='GigaBrain — мозг Писаря';$s.Save()`, dst, home)
	if exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Run() == nil {
		fmt.Println("   ✓ ярлык «Мозг Писаря» на рабочем столе")
	}
	_ = os.WriteFile(mark, []byte("1"), 0o644)
}
