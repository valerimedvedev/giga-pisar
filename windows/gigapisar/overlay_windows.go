//go:build windows

package main

import (
	"unsafe"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"

	"gigapisar/core"
)

// overlay — плавающая полоска статуса поверх всех окон (для диктовки в чужое
// окно): не забирает фокус, стоит у нижнего края экрана.
const spiGetWorkArea = 0x0030

type overlay struct {
	g     *gui
	w     *walk.MainWindow
	label *walk.Label
}

func newOverlay(g *gui) *overlay {
	o := &overlay{g: g}
	err := MainWindow{
		AssignTo: &o.w,
		Title:    "Гига Писарь — статус",
		Layout:   HBox{Margins: Margins{Left: 12, Top: 6, Right: 12, Bottom: 6}},
		Size:     Size{Width: 420, Height: 40},
		Font:     Font{Family: "Segoe UI", PointSize: 10, Bold: true},
		Children: []Widget{Label{AssignTo: &o.label, Text: ""}},
	}.Create()
	if err != nil {
		return o
	}
	h := o.w.Handle()
	// без рамки, поверх всех, не активируется, не в панели задач
	style := win.GetWindowLong(h, win.GWL_STYLE)
	style &^= win.WS_CAPTION | win.WS_THICKFRAME | win.WS_MINIMIZEBOX | win.WS_MAXIMIZEBOX | win.WS_SYSMENU
	style = int32(uint32(style) | uint32(win.WS_POPUP) | uint32(win.WS_BORDER))
	win.SetWindowLong(h, win.GWL_STYLE, style)
	ex := win.GetWindowLong(h, win.GWL_EXSTYLE)
	ex |= win.WS_EX_TOOLWINDOW | win.WS_EX_TOPMOST | win.WS_EX_NOACTIVATE
	ex &^= win.WS_EX_APPWINDOW
	win.SetWindowLong(h, win.GWL_EXSTYLE, ex)
	o.w.SetVisible(false)
	return o
}

func (o *overlay) show(text string, k core.Kind) {
	if o.w == nil {
		return
	}
	o.label.SetText(text)
	switch k {
	case core.OK:
		o.label.SetTextColor(walk.RGB(30, 142, 62))
	case core.Warn:
		o.label.SetTextColor(walk.RGB(199, 116, 0))
	case core.Error:
		o.label.SetTextColor(walk.RGB(200, 40, 40))
	default:
		o.label.SetTextColor(walk.RGB(91, 79, 207))
	}
	// внизу по центру рабочей области
	var rc win.RECT
	win.SystemParametersInfo(spiGetWorkArea, 0, unsafe.Pointer(&rc), 0)
	w, h := int32(460), int32(44)
	x := rc.Left + (rc.Right-rc.Left-w)/2
	y := rc.Bottom - h - 24
	win.SetWindowPos(o.w.Handle(), win.HWND_TOPMOST, x, y, w, h, win.SWP_NOACTIVATE|win.SWP_SHOWWINDOW)
}

func (o *overlay) hide() {
	if o.w != nil {
		o.w.SetVisible(false)
	}
}
