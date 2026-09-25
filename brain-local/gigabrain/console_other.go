//go:build !windows

package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
)

var stdin = bufio.NewReader(os.Stdin)

func fatal(msg string) {
	fmt.Println("\n! " + msg)
	os.Exit(1)
}

func hideWindow(cmd *exec.Cmd) {}

func autostartEnabled() bool          { return false }
func setAutostart(on bool) error      { return errors.New("автозапуск есть только на Windows") }
func makeShortcut()                   {}

// Консольный запуск (macOS, Linux): вопросы в терминале, меню по буквам.
func runApp(o *options, r *router) {
	progressFn = func(done, total int64, speed float64) {
		if done == 0 && total == 0 {
			fmt.Println()
			return
		}
		mb := float64(done) / 1e6
		if total > 0 {
			fmt.Printf("\r   %5.1f%%  %6.1f / %.1f МБ  %5.1f МБ/с   ", 100*float64(done)/float64(total), mb, float64(total)/1e6, speed)
		} else {
			fmt.Printf("\r   %6.1f МБ  %5.1f МБ/с   ", mb, speed)
		}
	}
	fmt.Printf("== Мозг Писаря (GigaBrain %s) — %s\n", version, home)
	fmt.Printf("   память компьютера: %d ГБ, ядер: %d\n", totalRAMGB(), runtime.NumCPU())
	if o.list {
		printCatalog()
		return
	}
	if !engineInstalled() && o.serverBin == "" {
		if cfg.Backend == "" {
			cfg.Backend = askBackend()
		}
		if err := installLlama(nil, ""); err != nil {
			var na *errNoAsset
			if errors.As(err, &na) {
				fmt.Printf("\n! %s. Файлы выпуска:\n", err)
				for i, a := range na.Assets {
					fmt.Printf("   %2d. %s\n", i+1, a.Name)
				}
				fmt.Print("Номер нужного файла (Enter — отмена): ")
				line, _ := stdin.ReadString('\n')
				n, e := strconv.Atoi(strings.TrimSpace(line))
				if e != nil || n < 1 || n > len(na.Assets) {
					fatal("сборка не выбрана. Можно указать адрес архива: --llama-url <адрес>, или считать на процессоре: --backend cpu")
				}
				err = installLlama(&na.Assets[n-1], na.Tag)
			}
			if err != nil {
				fatal("llama.cpp не установился: " + err.Error())
			}
		}
	}
	if o.add != "" {
		must(addModel(o.add))
	}
	if len(cfg.Models) == 0 {
		fmt.Println("\n== Нейронок ещё нет. Какую скачать?")
		must(addModel(chooseModel()))
	}
	if o.model != "" {
		if findInstalled(o.model) == nil {
			fatal("модель «" + o.model + "» не скачана; --add " + o.model)
		}
		cfg.Model = o.model
	}
	must(startServing(r))
	fmt.Printf("  Ключ доступа:  %s\n", cfg.Key)
	fmt.Printf("  Модели: %s (основная — %s)\n", installedIDs(), cfg.Model)
	fmt.Println("  На странице: ⚙ Мозг → «Нейронка на моём компьютере» → адрес и ключ → «Найти / проверить».")
	fmt.Println("  Закройте это окно (или Ctrl+C), чтобы выключить мозг.")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	if !o.noMenu {
		go menu(r)
	}
	<-stop
	fmt.Println("\nВыключаю…")
	r.shutdown()
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
	def := defaultCatalogIndex() + 1
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

func askBackend() string {
	fmt.Println()
	fmt.Println("== Где считать?")
	fmt.Println("   1. Процессор — работает везде (по умолчанию)")
	fmt.Println("   2. Видеокарта через Vulkan — NVIDIA, AMD, Intel; заметно быстрее")
	fmt.Println("   3. Видеокарта NVIDIA через CUDA — быстрее всего, нужны драйверы NVIDIA")
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
			r.shutdown()
			os.Exit(0)
		}
	}
}
