package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
)

// Ключи командной строки общие для окна и консоли.
type options struct {
	dir, backend, add, model, serverBin string
	port                                int
	list, noMenu, tray, lan             bool
}

func main() {
	var o options
	flag.StringVar(&o.dir, "dir", "", "папка данных (по умолчанию %LOCALAPPDATA%\\GigaBrain или ~/.giga/brain)")
	flag.IntVar(&o.port, "port", 0, "порт (по умолчанию 8091)")
	flag.StringVar(&o.backend, "backend", "", "cpu | vulkan | cuda (Windows/Linux)")
	flag.StringVar(&o.add, "add", "", "скачать модель: id из каталога или адрес .gguf")
	flag.StringVar(&o.model, "model", "", "основная модель (id)")
	flag.BoolVar(&o.list, "list", false, "показать каталог и выйти")
	flag.BoolVar(&o.noMenu, "no-menu", false, "не показывать меню в консоли")
	flag.BoolVar(&o.lan, "lan", false, "слушать и домашнюю сеть — для приложения на телефоне (ключ обязателен)")
	flag.BoolVar(&o.tray, "tray", false, "Windows: запуститься свёрнутым в область уведомлений (так запускает автозапуск)")
	flag.StringVar(&o.serverBin, "server-bin", "", "свой llama-server (для отладки)")
	flag.StringVar(&catalogFlag, "catalog", "", "свой каталог моделей: файл или адрес (тогда каталог с GitHub не берётся)")
	flag.StringVar(&llamaURLFlag, "llama-url", "", "адрес архива llama.cpp, если автоматический подбор не справился")
	showVer := flag.Bool("version", false, "версия")
	flag.Parse()
	if *showVer {
		fmt.Println("GigaBrain", version)
		return
	}

	must(openHome(resolveHome(o.dir)))
	if o.port > 0 {
		cfg.Port = o.port
	}
	if o.backend != "" {
		cfg.Backend = o.backend
	}
	if o.lan {
		cfg.LAN = true
	}
	if cfg.Backend == "" && runtime.GOOS == "darwin" {
		cfg.Backend = "cpu" // на маке сборка одна, видеокарта включается сама
	}
	must(loadCatalog(catalogFlag))

	r := &router{bin: o.serverBin}
	if r.bin == "" {
		r.bin = serverBinPath()
	}
	runApp(&o, r) // окно на Windows, консоль на остальных
	os.Exit(0)
}

// Общий для окна и консоли порядок запуска после того, как движок и хотя бы
// одна модель на месте: сохранить, поднять слушателя и основную модель.
func startServing(r *router) error {
	if findInstalled(cfg.Model) == nil && len(cfg.Models) > 0 {
		cfg.Model = cfg.Models[0].ID
	}
	saveConfig()
	if err := r.listen(); err != nil {
		return err
	}
	logf("✓ Мозг слушает  http://127.0.0.1:%d", cfg.Port)
	if cfg.LAN {
		for _, ip := range lanAddresses() {
			logf("  и в домашней сети: http://%s:%d (для телефона)", ip, cfg.Port)
		}
	}
	if cfg.Model != "" {
		go r.ensure(cfg.Model) // основная модель поднимается сразу, пока человек читает
	}
	return nil
}
