// Настройки Писаря — JSON в папке данных (%APPDATA%\GigaPisar).
package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"gigapisar/brain"
)

type Config struct {
	Mode string `json:"mode"` // dictation | dictaphone | chat

	BrainMode  string `json:"brain_mode"` // off | local | pc | server | cloud
	LocalModel string `json:"local_model"`
	Backend    string `json:"backend"` // cpu | vulkan | cuda
	PcBase     string `json:"pc_base"`
	PcKey      string `json:"pc_key"`
	PcModel    string `json:"pc_model"`
	ServerBase string `json:"server_base"`
	ServerKey  string `json:"server_key"`
	ServerModel string `json:"server_model"`
	CloudService string `json:"cloud_service"`
	CloudBase  string `json:"cloud_base"`
	CloudKey   string `json:"cloud_key"`
	CloudModel string `json:"cloud_model"`
	CloudKeys  string `json:"cloud_keys"` // giga-keys.json как импортирован

	AsrThreads int  `json:"asr_threads"`
	LlmThreads int  `json:"llm_threads"`
	LiveInsert bool `json:"live_insert"`
	AutoTidy   bool `json:"auto_tidy"`

	Chips           []brain.Chip `json:"chips"`
	PromptDictation string       `json:"prompt_dictation"`
	PromptSelection string       `json:"prompt_selection"`
	PromptChat      string       `json:"prompt_chat"`

	HotkeyMods int  `json:"hotkey_mods"` // системная диктовка: Ctrl+Alt+Space по умолчанию
	HotkeyVk   int  `json:"hotkey_vk"`
	Tray       bool `json:"tray"`
	StartHidden bool `json:"start_hidden"`
}

func DefaultDir() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("APPDATA"); d != "" {
			return filepath.Join(d, "GigaPisar")
		}
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".giga", "pisar")
}

func defaultConfig() Config {
	th := runtime.NumCPU() / 2
	if th < 2 {
		th = 2
	}
	if th > 8 {
		th = 8
	}
	return Config{Mode: "dictation", BrainMode: "off", Backend: "cpu", CloudService: "gemini",
		AsrThreads: th, LlmThreads: th, LiveInsert: true, Chips: brain.DefaultChips,
		HotkeyMods: 2 | 1 /* Ctrl+Alt */, HotkeyVk: 0x20 /* Space */, Tray: true}
}

func LoadConfig(dir string) Config {
	c := defaultConfig()
	if b, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		json.Unmarshal(b, &c)
	}
	if len(c.Chips) == 0 {
		c.Chips = brain.DefaultChips
	}
	if c.HotkeyVk == 0 {
		c.HotkeyMods, c.HotkeyVk = 3, 0x20
	}
	return c
}

func (c Config) Save(dir string) error {
	os.MkdirAll(dir, 0o755)
	b, _ := json.MarshalIndent(c, "", " ")
	return os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600)
}

func (c Config) Prompt(selection bool) string {
	if selection {
		if c.PromptSelection != "" {
			return c.PromptSelection
		}
		return brain.SelectionPrompt
	}
	if c.PromptDictation != "" {
		return c.PromptDictation
	}
	return brain.DictationPrompt
}

func (c Config) ChatPromptText() string {
	if c.PromptChat != "" {
		return c.PromptChat
	}
	return brain.ChatPrompt
}
