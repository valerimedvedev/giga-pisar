//go:build !windows

// Заглушки для сборки и проверок ядра на Linux/macOS.
package win

import "errors"

const Rate = 16000
const (
	ModAlt     = 1
	ModControl = 2
	ModShift   = 4
	ModWin     = 8
)

type Mic struct{ onSamples func([]float32) }

func NewMic(onSamples func([]float32)) *Mic { return &Mic{onSamples: onSamples} }
func (m *Mic) Start() error                { return errors.New("микрофон есть только на Windows") }
func (m *Mic) Stop()                       {}

func Foreground() (uintptr, string)         { return 0, "" }
func Activate(uintptr)                      {}
func ClipboardText() string                 { return "" }
func SetClipboardText(string) error         { return nil }
func TypeText(string) error                 { return errors.New("вставка есть только на Windows") }
func Selection() string                     { return "" }
func Backspace(int)                         {}

type Hotkey struct{}

func Register(id, mods, vk int, fn func()) (*Hotkey, error) { return &Hotkey{}, nil }
func (h *Hotkey) Unregister()                                 {}
func AutostartEnabled(string) bool                            { return false }
func SetAutostart(string, string, bool) error                 { return errors.New("только на Windows") }
func WindowsVersion() (uint32, uint32)                        { return 0, 0 }
func IsOldWindows() bool                                      { return false }
