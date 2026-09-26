//go:build windows

package win

import (
	"errors"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	sendInput           = user32.NewProc("SendInput")
	getForegroundWindow = user32.NewProc("GetForegroundWindow")
	setForegroundWindow = user32.NewProc("SetForegroundWindow")
	getWindowTextW      = user32.NewProc("GetWindowTextW")
	getAsyncKeyState    = user32.NewProc("GetAsyncKeyState")
	openClipboard       = user32.NewProc("OpenClipboard")
	closeClipboard      = user32.NewProc("CloseClipboard")
	emptyClipboard      = user32.NewProc("EmptyClipboard")
	getClipboardData    = user32.NewProc("GetClipboardData")
	setClipboardData    = user32.NewProc("SetClipboardData")
	isClipboardFormat   = user32.NewProc("IsClipboardFormatAvailable")
	globalAlloc         = kernel32.NewProc("GlobalAlloc")
	globalLock          = kernel32.NewProc("GlobalLock")
	globalUnlock        = kernel32.NewProc("GlobalUnlock")
	registerHotKey      = user32.NewProc("RegisterHotKey")
	unregisterHotKey    = user32.NewProc("UnregisterHotKey")
	getMessageW         = user32.NewProc("GetMessageW")
	postThreadMessageW  = user32.NewProc("PostThreadMessageW")
	getCurrentThreadId  = kernel32.NewProc("GetCurrentThreadId")
	getVersionEx        = kernel32.NewProc("GetVersionExW")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
	inputKeyboard = 1
	keyeventfKeyup = 0x0002
	vkControl     = 0x11
	vkV           = 0x56
	vkC           = 0x43
)

type keybdInput struct {
	Type      uint32
	Vk        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
	_         [8]byte // до размера INPUT (40 байт на x64)
}

func key(vk uint16, up bool) keybdInput {
	k := keybdInput{Type: inputKeyboard, Vk: vk}
	if up {
		k.Flags = keyeventfKeyup
	}
	return k
}

func sendKeys(keys []keybdInput) {
	sendInput.Call(uintptr(len(keys)), uintptr(unsafe.Pointer(&keys[0])), unsafe.Sizeof(keys[0]))
}

// Ctrl+буква; перед этим отпускаем Ctrl/Alt, которые человек мог держать после горячей клавиши.
func ctrlKey(vk uint16) {
	for i := 0; i < 20; i++ { // ждём, пока человек отпустит клавиши сочетания
		c, _, _ := getAsyncKeyState.Call(vkControl)
		a, _, _ := getAsyncKeyState.Call(0x12)
		if c&0x8000 == 0 && a&0x8000 == 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	sendKeys([]keybdInput{key(vkControl, false), key(vk, false), key(vk, true), key(vkControl, true)})
}

// Foreground — окно, где сейчас курсор, и его заголовок.
func Foreground() (uintptr, string) {
	h, _, _ := getForegroundWindow.Call()
	buf := make([]uint16, 256)
	getWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), 256)
	return h, syscall.UTF16ToString(buf)
}

func Activate(h uintptr) { setForegroundWindow.Call(h) }

// ClipboardText читает текст из буфера обмена ("" если там не текст).
func ClipboardText() string {
	for i := 0; i < 10; i++ {
		if r, _, _ := openClipboard.Call(0); r != 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer closeClipboard.Call()
	if r, _, _ := isClipboardFormat.Call(cfUnicodeText); r == 0 {
		return ""
	}
	h, _, _ := getClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return ""
	}
	p, _, _ := globalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer globalUnlock.Call(h)
	var out []uint16
	for i := 0; ; i++ {
		c := *(*uint16)(unsafe.Pointer(p + uintptr(i*2)))
		if c == 0 {
			break
		}
		out = append(out, c)
	}
	return syscall.UTF16ToString(out)
}

func SetClipboardText(s string) error {
	u := syscall.StringToUTF16(s)
	for i := 0; i < 10; i++ {
		if r, _, _ := openClipboard.Call(0); r != 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer closeClipboard.Call()
	emptyClipboard.Call()
	h, _, _ := globalAlloc.Call(gmemMoveable, uintptr(len(u)*2))
	if h == 0 {
		return errors.New("буфер обмена: нет памяти")
	}
	p, _, _ := globalLock.Call(h)
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(u)), u)
	globalUnlock.Call(h)
	if r, _, _ := setClipboardData.Call(cfUnicodeText, h); r == 0 {
		return errors.New("буфер обмена не принял текст")
	}
	return nil
}

// TypeText вставляет текст в активное окно: через буфер обмена и Ctrl+V,
// прежнее содержимое буфера возвращается.
func TypeText(s string) error {
	old := ClipboardText()
	if err := SetClipboardText(s); err != nil {
		return err
	}
	ctrlKey(vkV)
	time.Sleep(150 * time.Millisecond)
	if old != "" {
		SetClipboardText(old)
	}
	return nil
}

// Backspace стирает n символов в активном окне.
func Backspace(n int) {
	if n <= 0 {
		return
	}
	keys := make([]keybdInput, 0, n*2)
	for i := 0; i < n; i++ {
		keys = append(keys, key(0x08, false), key(0x08, true))
	}
	for i := 0; i < len(keys); i += 200 {
		j := i + 200
		if j > len(keys) {
			j = len(keys)
		}
		sendKeys(keys[i:j])
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
}

// Selection — выделенный текст в активном окне: Ctrl+C и чтение буфера (буфер потом возвращаем).
func Selection() string {
	old := ClipboardText()
	SetClipboardText("")
	ctrlKey(vkC)
	time.Sleep(120 * time.Millisecond)
	s := ClipboardText()
	if old != "" {
		SetClipboardText(old)
	}
	return s
}

// ─────────────── горячая клавиша ───────────────

const (
	ModAlt     = 1
	ModControl = 2
	ModShift   = 4
	ModWin     = 8
)

// Hotkey держит свой поток с очередью сообщений: RegisterHotKey работает только в нём.
type Hotkey struct {
	thread uintptr
	stop   chan struct{}
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      [2]int32
}

// Register регистрирует сочетание (mods — ModControl|ModAlt…, vk — код клавиши) и зовёт fn.
func Register(id int, mods, vk int, fn func()) (*Hotkey, error) {
	h := &Hotkey{stop: make(chan struct{})}
	errc := make(chan error, 1)
	go func() {
		lockThread()
		tid, _, _ := getCurrentThreadId.Call()
		h.thread = tid
		if r, _, e := registerHotKey.Call(0, uintptr(id), uintptr(mods|0x4000 /* MOD_NOREPEAT */), uintptr(vk)); r == 0 {
			if r2, _, _ := registerHotKey.Call(0, uintptr(id), uintptr(mods), uintptr(vk)); r2 == 0 { // Windows 7 не знает MOD_NOREPEAT
				errc <- errors.New("сочетание занято другой программой: " + e.Error())
				return
			}
		}
		errc <- nil
		var m msg
		for {
			r, _, _ := getMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if r == 0 || int32(r) == -1 {
				break
			}
			if m.Message == 0x0312 /* WM_HOTKEY */ && int(m.WParam) == id {
				fn()
			}
			if m.Message == 0x0012 /* WM_QUIT */ {
				break
			}
		}
		unregisterHotKey.Call(0, uintptr(id))
	}()
	return h, <-errc
}

func (h *Hotkey) Unregister() {
	if h != nil && h.thread != 0 {
		postThreadMessageW.Call(h.thread, 0x0012, 0, 0)
	}
}

// ─────────────── автозапуск и версия Windows ───────────────

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func AutostartEnabled(name string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	return err == nil && v != ""
}

func SetAutostart(name, cmdline string, on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(name); err == registry.ErrNotExist {
			return nil
		} else {
			return err
		}
	}
	return k.SetStringValue(name, cmdline)
}

type osVersionInfo struct {
	Size                            uint32
	Major, Minor, Build, PlatformID uint32
	CSD                             [128]uint16
}

// WindowsVersion — старшая и младшая версии ядра (Windows 7 = 6.1).
func WindowsVersion() (major, minor uint32) {
	// GetVersionEx врёт без манифеста, поэтому смотрим реестр
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err == nil {
		defer k.Close()
		if maj, _, err := k.GetIntegerValue("CurrentMajorVersionNumber"); err == nil {
			min, _, _ := k.GetIntegerValue("CurrentMinorVersionNumber")
			return uint32(maj), uint32(min)
		}
		if v, _, err := k.GetStringValue("CurrentVersion"); err == nil && len(v) >= 3 { // "6.1"
			return uint32(v[0] - '0'), uint32(v[2] - '0')
		}
	}
	var vi osVersionInfo
	vi.Size = uint32(unsafe.Sizeof(vi))
	getVersionEx.Call(uintptr(unsafe.Pointer(&vi)))
	return vi.Major, vi.Minor
}

// IsWindows7 — Windows 7 или 8 (ядро 6.x): нужны старые сборки библиотек.
func IsOldWindows() bool { maj, _ := WindowsVersion(); return maj > 0 && maj < 10 }
