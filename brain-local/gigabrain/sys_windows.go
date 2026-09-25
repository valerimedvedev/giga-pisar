//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

// Дочерний llama-server — без чёрного окна консоли.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// Автозапуск при входе в Windows: запись в реестре текущего пользователя,
// запускает копию из папки данных свёрнутой в область уведомлений.
func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue("GigaBrain")
	return err == nil && v != ""
}

func setAutostart(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		err := k.DeleteValue("GigaBrain")
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	exePath := installedExe()
	return k.SetStringValue("GigaBrain", fmt.Sprintf(`"%s" --tray`, exePath))
}

// Копия программы в папке данных: с флешки или из «Загрузок» она может исчезнуть.
func installedExe() string {
	self, err := os.Executable()
	if err != nil {
		return "GigaBrain.exe"
	}
	dst := filepath.Join(home, "GigaBrain.exe")
	if strings.EqualFold(self, dst) {
		return dst
	}
	if b, err := os.ReadFile(self); err == nil {
		if old, err2 := os.ReadFile(dst); err2 != nil || len(old) != len(b) {
			if os.WriteFile(dst, b, 0o755) != nil {
				return self
			}
		}
	}
	return dst
}

// Ярлык «Мозг Писаря» на рабочем столе — один раз.
func makeShortcut() {
	mark := filepath.Join(home, ".shortcut")
	if _, err := os.Stat(mark); err == nil {
		return
	}
	dst := installedExe()
	ps := fmt.Sprintf(`$s=(New-Object -ComObject WScript.Shell).CreateShortcut([Environment]::GetFolderPath('Desktop')+'\Мозг Писаря.lnk');$s.TargetPath='%s';$s.WorkingDirectory='%s';$s.Description='GigaBrain — мозг Писаря';$s.Save()`, dst, home)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	hideWindow(cmd)
	if cmd.Run() == nil {
		logf("   ✓ ярлык «Мозг Писаря» на рабочем столе")
	}
	_ = os.WriteFile(mark, []byte("1"), 0o644)
}

func openFolder(dir string) {
	_ = exec.Command("explorer.exe", dir).Start()
}
