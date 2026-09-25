//go:build windows

package main

import "syscall"

// Русский текст в консоли Windows: переключаем кодировку вывода на UTF-8.
func setupConsole() {
	k := syscall.NewLazyDLL("kernel32.dll")
	k.NewProc("SetConsoleOutputCP").Call(65001)
	k.NewProc("SetConsoleCP").Call(65001)
}
