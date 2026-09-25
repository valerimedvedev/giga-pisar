package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Перенос папки данных: файлы переезжают, указатель запоминает новое место.
func TestMoveHome(t *testing.T) {
	base := t.TempDir()
	if err := openHome(filepath.Join(base, "one")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(home, "models", "x.gguf"), []byte("model"), 0o644)
	os.WriteFile(filepath.Join(home, "bin", "llama-server"), []byte("bin"), 0o755)
	cfg.Model = "x"
	saveConfig()
	t.Setenv("HOME", base)
	t.Setenv("LOCALAPPDATA", base)
	dest := filepath.Join(base, "two")
	if err := moveHome(dest); err != nil {
		t.Fatal(err)
	}
	if home != dest {
		t.Fatalf("home = %s", home)
	}
	for _, f := range []string{"models/x.gguf", "bin/llama-server", "config.json"} {
		if _, err := os.Stat(filepath.Join(dest, f)); err != nil {
			t.Errorf("%s не переехал: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "one", "models", "x.gguf")); err == nil {
		t.Error("старая копия осталась")
	}
	if got := resolveHome(""); got != dest {
		t.Errorf("resolveHome = %s, want %s", got, dest)
	}
	if got := resolveHome("/явно"); got != "/явно" {
		t.Errorf("--dir должен побеждать: %s", got)
	}
}
