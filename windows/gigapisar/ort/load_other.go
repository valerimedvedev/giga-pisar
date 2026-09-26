//go:build !windows

package ort

import "github.com/ebitengine/purego"

// Для проверки на Linux/macOS (без cgo: CGO_ENABLED=0).
func loadLibrary(path string) (uintptr, error) {
	lib, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return 0, err
	}
	return purego.Dlsym(lib, "OrtGetApiBase")
}
