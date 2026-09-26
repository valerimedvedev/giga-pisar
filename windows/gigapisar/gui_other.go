//go:build !windows

package main

import (
	"fmt"
	"gigapisar/core"
)

func runGUI(app *core.App, tray bool) {
	fmt.Println("Окно Гиги Писаря есть только на Windows. Ядро проверяется тестами: go test ./...")
}
