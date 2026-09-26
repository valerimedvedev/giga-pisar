package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Пакет распознавания — тот же архив, что у плагина, страницы и Android.
const GigaAmURL = "https://github.com/moznoazachem/giga-pisar-cli/releases/download/v1.0/gigaam-v3-onnx-int8.tar.gz"
const GigaAmBytes = 213_000_000

// onnxruntime: свежий для Windows 10/11, 1.12.1 — последний, что запускается на Windows 7/8
// (модели GigaAM собраны с opset 17, ниже 1.12 нельзя).
const (
	OrtVersionNew = "1.20.1"
	OrtVersionOld = "1.12.1"
)

func OrtURL(version, arch string) string {
	return fmt.Sprintf("https://github.com/microsoft/onnxruntime/releases/download/v%s/onnxruntime-win-%s-%s.zip", version, arch, version)
}

// OrtPath — где лежит библиотека onnxruntime для этого запуска.
func OrtPath(dir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(dir, "ort", "onnxruntime.dll")
	}
	return filepath.Join(dir, "ort", "libonnxruntime.so")
}

func HasOrt(dir string) bool { _, err := os.Stat(OrtPath(dir)); return err == nil }

// InstallOrt качает и раскладывает onnxruntime (old — сборка для Windows 7/8).
func InstallOrt(dir string, old bool, cancel *int32, p Progress) error {
	if runtime.GOOS != "windows" {
		return errors.New("onnxruntime ставится автоматически только на Windows")
	}
	ver := OrtVersionNew
	if old {
		ver = OrtVersionOld
	}
	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	tmp := filepath.Join(dir, "ort.zip")
	if err := Download(OrtURL(ver, arch), tmp, cancel, p); err != nil {
		return err
	}
	err := ExtractZip(tmp, filepath.Join(dir, "ort"), "onnxruntime.dll")
	os.Remove(tmp)
	if err != nil {
		return err
	}
	if !HasOrt(dir) {
		return errors.New("в архиве onnxruntime не оказалось onnxruntime.dll")
	}
	return nil
}

// InstallGigaAm качает архив модели и раскладывает пять файлов в dir/gigaam.
func InstallGigaAm(dir string, files []string, cancel *int32, p Progress) error {
	tmp := filepath.Join(dir, "gigaam.tar.gz")
	if err := Download(GigaAmURL, tmp, cancel, func(d, t int64, s float64) {
		if t <= 0 {
			t = GigaAmBytes
		}
		p(d, t, s)
	}); err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, f := range files {
		wanted[f] = true
	}
	err := ExtractTarGz(tmp, filepath.Join(dir, "gigaam"), wanted)
	os.Remove(tmp)
	return err
}
