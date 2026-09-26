//go:build windows

package main

import (
	"strings"

	"github.com/lxn/walk"

	"gigapisar/win"
)

// fieldTarget — многострочное поле в окне Писаря: вставка на место курсора.
type fieldTarget struct {
	te   *walk.TextEdit
	sync func(func())
	name string
}

func (f *fieldTarget) Name() string { return f.name }

func (f *fieldTarget) Selection() string {
	var s string
	f.sync(func() {
		a, b := f.te.TextSelection()
		if b > a {
			s = substrUTF16(f.te.Text(), a, b)
		}
	})
	return s
}

func (f *fieldTarget) Insert(text string) {
	f.sync(func() {
		a, b := f.te.TextSelection()
		t := f.te.Text()
		head, tail := cutUTF16(t, a, b)
		// пробел, если слова слипнутся
		if head != "" && !strings.HasSuffix(head, " ") && !strings.HasSuffix(head, "\n") && !strings.HasPrefix(text, " ") && !strings.HasPrefix(text, "\r") {
			text = " " + text
		}
		f.te.SetText(head + text + tail)
		pos := utf16Len(head + text)
		f.te.SetTextSelection(pos, pos)
	})
}

func (f *fieldTarget) ReplaceSession(old, new string) bool {
	ok := false
	f.sync(func() {
		t := f.te.Text()
		i := strings.LastIndex(t, old)
		if i < 0 {
			return
		}
		f.te.SetText(t[:i] + new + t[i+len(old):])
		pos := utf16Len(t[:i] + new)
		f.te.SetTextSelection(utf16Len(t[:i]), pos)
		ok = true
	})
	return ok
}

// foregroundTarget — чужое окно Windows: вставка через буфер обмена и Ctrl+V.
type foregroundTarget struct {
	hwnd     uintptr
	title    string
	selected string
	inserted string
}

func newForegroundTarget() *foregroundTarget {
	h, title := win.Foreground()
	t := &foregroundTarget{hwnd: h, title: title}
	t.selected = win.Selection() // что выделено в момент нажатия горячей клавиши
	return t
}

func (t *foregroundTarget) Name() string      { return "window:" + t.title }
func (t *foregroundTarget) Selection() string { return t.selected }
func (t *foregroundTarget) Insert(text string) {
	win.Activate(t.hwnd)
	win.TypeText(text)
	t.inserted += text
}

// ReplaceSession: убираем вставленное (Backspace на каждую букву слишком медленно
// и ненадёжно), поэтому выделяем его Shift+Left… тоже ненадёжно. Честный путь:
// выделенное заменяется вставкой (оно уже выделено при команде над выделением);
// надиктованное в чужом окне не откатываем — ставим новый текст следом.
func (t *foregroundTarget) ReplaceSession(old, new string) bool {
	win.Activate(t.hwnd)
	if t.selected != "" && old == t.selected && t.inserted == "" {
		win.TypeText(new) // выделение ещё стоит — вставка заменяет его
		t.inserted = new
		return true
	}
	if t.inserted == old {
		// выделить только что вставленное: Shift+Ctrl+Left по словам ненадёжно, поэтому
		// стираем по символам — для коротких фраз приемлемо
		if n := utf16Len(old); n <= 400 {
			win.Backspace(n)
			win.TypeText(new)
			t.inserted = new
			return true
		}
	}
	return false
}

// ─────────────── строки в единицах UTF-16 (так считает Windows) ───────────────

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func byteAtUTF16(s string, idx int) int {
	n := 0
	for i, r := range s {
		if n >= idx {
			return i
		}
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return len(s)
}

func substrUTF16(s string, a, b int) string { return s[byteAtUTF16(s, a):byteAtUTF16(s, b)] }
func cutUTF16(s string, a, b int) (string, string) {
	return s[:byteAtUTF16(s, a)], s[byteAtUTF16(s, b):]
}
