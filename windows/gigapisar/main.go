// Гига Писарь для Windows: диктовка в поле и в любое окно, диктофон, общение
// с нейронкой, мозг (на этом компьютере, GigaBrain/Ollama, сервер, облако).
// Окно — gui_windows.go; здесь общий запуск.
package main

import (
	"flag"
	"fmt"
	"os"

	"gigapisar/core"
)

const version = "1.0.0"

func main() {
	dir := flag.String("dir", "", "папка данных (по умолчанию %APPDATA%\\GigaPisar)")
	tray := flag.Bool("tray", false, "запуститься свёрнутым в область уведомлений")
	ver := flag.Bool("version", false, "версия")
	flag.Parse()
	if *ver {
		fmt.Println("Гига Писарь", version)
		return
	}
	d := *dir
	if d == "" {
		d = core.DefaultDir()
	}
	os.MkdirAll(d, 0o755)
	app := core.New(d, nil)
	runGUI(app, *tray)
}
