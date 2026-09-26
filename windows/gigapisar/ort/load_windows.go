//go:build windows

package ort

import "syscall"

func loadLibrary(path string) (uintptr, error) {
	dll, err := syscall.LoadDLL(path)
	if err != nil {
		return 0, err
	}
	p, err := dll.FindProc("OrtGetApiBase")
	if err != nil {
		return 0, err
	}
	return p.Addr(), nil
}
