//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"

	"gigapisar/brain"
	"gigapisar/core"
	"gigapisar/export"
	gwin "gigapisar/win"
)

type gui struct {
	app  *core.App
	mw   *walk.MainWindow
	ni   *walk.NotifyIcon
	icon *walk.Icon
	tabs *walk.TabWidget

	// диктовка / диктофон
	text     *walk.TextEdit
	status   *walk.Label
	recBtn   *walk.PushButton
	pauseBtn *walk.PushButton
	brainBtn *walk.PushButton
	modeCB   *walk.ComboBox
	recList  *walk.TableView
	recModel *recordsModel
	bar      *walk.ProgressBar
	barLabel *walk.Label
	cancelBtn *walk.PushButton

	// общение
	chatView  *walk.TextEdit
	chatInput *walk.TextEdit
	sendBtn   *walk.PushButton
	chatMicBtn *walk.PushButton

	// мозг
	panel *walk.Composite
	chips *walk.Composite
	own   *walk.TextEdit
	hist  *walk.ListBox
	undoBtn *walk.PushButton
	scopeLbl *walk.Label
	whereLbl *walk.Label

	overlay  *overlay
	hotkey   *gwin.Hotkey
	quitting bool
	sysDict  *foregroundTarget // текущая системная диктовка
	logBuf   []string
}

func runGUI(app *core.App, tray bool) {
	g := &gui{app: app}
	app.Log = func(s string) { g.logBuf = append(g.logBuf, time.Now().Format("15:04:05 ")+s); if len(g.logBuf) > 500 { g.logBuf = g.logBuf[100:] } }
	app.OnStatus = func(t string, k core.Kind) { g.sync(func() { g.setStatus(t, k) }) }
	app.OnProgress = func(label string, d, t int64) { g.sync(func() { g.setProgress(label, d, t) }) }
	app.OnChange = func() { g.sync(g.refresh) }
	app.OnChat = func() { g.sync(g.renderChat) }
	if ic, err := walk.NewIconFromResourceId(2); err == nil {
		g.icon = ic
	}
	g.recModel = &recordsModel{g: g}

	err := MainWindow{
		AssignTo: &g.mw,
		Title:    "Гига Писарь",
		Size:     Size{Width: 760, Height: 640},
		MinSize:  Size{Width: 600, Height: 480},
		Layout:   VBox{Margins: Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 6},
		Font:     Font{Family: "Segoe UI", PointSize: 10},
		MenuItems: []MenuItem{
			Menu{Text: "&Файл", Items: []MenuItem{
				Action{Text: "Сохранить как Word (.docx)…", OnTriggered: func() { g.export("docx") }},
				Action{Text: "Сохранить как Markdown (.md)…", OnTriggered: func() { g.export("md") }},
				Action{Text: "Сохранить как текст (.txt)…", OnTriggered: func() { g.export("txt") }},
				Separator{},
				Action{Text: "Копировать всё", OnTriggered: func() { walk.Clipboard().SetText(g.exportText()) }},
				Action{Text: "Очистить", OnTriggered: g.clear},
				Separator{},
				Action{Text: "Папка данных", OnTriggered: func() { exec.Command("explorer.exe", app.Dir).Start() }},
				Action{Text: "Выход", OnTriggered: g.quit},
			}},
			Menu{Text: "&Настройки", Items: []MenuItem{
				Action{Text: "Мозг и нейронки…", OnTriggered: g.settingsBrain},
				Action{Text: "Диктовка, горячая клавиша, автозапуск…", OnTriggered: g.settingsGeneral},
				Action{Text: "Команды на кнопках…", OnTriggered: g.settingsChips},
				Action{Text: "Промпты нейронке…", OnTriggered: g.settingsPrompts},
				Separator{},
				Action{Text: "Протокол…", OnTriggered: g.showLog},
			}},
		},
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 8},
				Children: []Widget{
					Label{Text: "Режим:"},
					ComboBox{AssignTo: &g.modeCB, Model: []string{"Диктовка", "Диктофон", "Общение"}, OnCurrentIndexChanged: g.modeChanged},
					HSpacer{},
					Label{Text: "Диктовать в любое окно: ", Font: Font{Family: "Segoe UI", PointSize: 9}},
					Label{Text: hotkeyText(app.Cfg.HotkeyMods, app.Cfg.HotkeyVk), Font: Font{Family: "Segoe UI", PointSize: 9, Bold: true}},
				},
			},
			TabWidget{
				AssignTo: &g.tabs,
				Pages: []TabPage{
					{Title: "Текст", Layout: VBox{Margins: Margins{Left: 6, Top: 6, Right: 6, Bottom: 6}, Spacing: 6}, Children: []Widget{
						Composite{
							Layout: HBox{MarginsZero: true, Spacing: 6},
							Children: []Widget{
								TextEdit{AssignTo: &g.text, VScroll: true, Font: Font{Family: "Segoe UI", PointSize: 11}},
								Composite{
									AssignTo: &g.panel, Visible: false, MinSize: Size{Width: 260}, MaxSize: Size{Width: 300},
									Layout: VBox{MarginsZero: true, Spacing: 4},
									Children: []Widget{
										Label{AssignTo: &g.whereLbl, Text: "Мозг", Font: Font{Family: "Segoe UI", PointSize: 9, Bold: true}},
										Label{AssignTo: &g.scopeLbl, Text: "", Font: Font{Family: "Segoe UI", PointSize: 8}},
										Composite{AssignTo: &g.chips, Layout: Flow{MarginsZero: true, Spacing: 4}},
										Label{Text: "Свой промпт (Ctrl+Enter — выполнить):", Font: Font{Family: "Segoe UI", PointSize: 8}},
										TextEdit{AssignTo: &g.own, MinSize: Size{Height: 48}, MaxSize: Size{Height: 70}, OnKeyDown: func(k walk.Key) {
											if k == walk.KeyReturn && walk.ControlDown() {
												g.runOwn()
											}
										}},
										PushButton{Text: "Выполнить", OnClicked: g.runOwn},
										Label{Text: "История (📌 не вытесняется; двойной клик — выполнить):", Font: Font{Family: "Segoe UI", PointSize: 8}},
										ListBox{AssignTo: &g.hist, MinSize: Size{Height: 90}, OnItemActivated: func() {
											if i := g.hist.CurrentIndex(); i >= 0 && i < len(app.History.Items) {
												g.runCommand(app.History.Items[i].Text, true)
											}
										}, OnCurrentIndexChanged: func() {
											if i := g.hist.CurrentIndex(); i >= 0 && i < len(app.History.Items) {
												g.own.SetText(app.History.Items[i].Text)
											}
										}},
										Composite{Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
											PushButton{Text: "📌", ToolTipText: "Закрепить / открепить", MaxSize: Size{Width: 36}, OnClicked: func() {
												if i := g.hist.CurrentIndex(); i >= 0 && i < len(app.History.Items) {
													app.History.TogglePin(app.History.Items[i].Text)
													g.renderHistory()
												}
											}},
											PushButton{Text: "✕", ToolTipText: "Удалить из истории", MaxSize: Size{Width: 36}, OnClicked: func() {
												if i := g.hist.CurrentIndex(); i >= 0 && i < len(app.History.Items) {
													app.History.Remove(app.History.Items[i].Text)
													g.renderHistory()
												}
											}},
											HSpacer{},
											PushButton{AssignTo: &g.undoBtn, Text: "Вернуть как было", OnClicked: func() { app.Undo() }},
										}},
										PushButton{Text: "⚙ Где считает мозг…", OnClicked: g.settingsBrain},
									},
								},
							},
						},
						Composite{
							Layout: HBox{MarginsZero: true, Spacing: 8},
							Children: []Widget{
								PushButton{AssignTo: &g.recBtn, Text: "🎙 Запись", MinSize: Size{Width: 150, Height: 40}, OnClicked: g.toggleRecord},
								PushButton{AssignTo: &g.pauseBtn, Text: "⏸ Пауза", Visible: false, MinSize: Size{Width: 120, Height: 40}, OnClicked: func() { app.TogglePause() }},
								PushButton{AssignTo: &g.brainBtn, Text: "🧠 Мозг", MinSize: Size{Width: 120, Height: 40}, OnClicked: g.togglePanel},
								HSpacer{},
								PushButton{Text: "Копировать", OnClicked: func() { walk.Clipboard().SetText(g.text.Text()) }},
							},
						},
						GroupBox{Title: "Записи диктофона", Layout: VBox{Margins: Margins{Left: 6, Top: 4, Right: 6, Bottom: 6}}, MaxSize: Size{Height: 170}, Children: []Widget{
							TableView{AssignTo: &g.recList, Model: g.recModel, LastColumnStretched: true, MinSize: Size{Height: 60},
								Columns: []TableViewColumn{{Title: "Запись", Width: 180}, {Title: "Длина", Width: 70}, {Title: "Размер"}},
								OnItemActivated: g.transcribeSelected},
							Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
								PushButton{Text: "В текст", OnClicked: g.transcribeSelected},
								PushButton{Text: "Открыть папку", OnClicked: func() { exec.Command("explorer.exe", filepath.Join(app.Dir, "records")).Start() }},
								PushButton{Text: "Удалить", OnClicked: g.deleteSelected},
								HSpacer{},
							}},
						}},
					}},
					{Title: "Общение", Layout: VBox{Margins: Margins{Left: 6, Top: 6, Right: 6, Bottom: 6}, Spacing: 6}, Children: []Widget{
						TextEdit{AssignTo: &g.chatView, ReadOnly: true, VScroll: true, Font: Font{Family: "Segoe UI", PointSize: 10}},
						Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
							TextEdit{AssignTo: &g.chatInput, MinSize: Size{Height: 60}, MaxSize: Size{Height: 90}, OnKeyDown: func(k walk.Key) {
								if k == walk.KeyReturn && walk.ControlDown() {
									g.sendChat()
								}
							}},
							Composite{Layout: VBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
								PushButton{AssignTo: &g.sendBtn, Text: "Отправить (Ctrl+Enter)", OnClicked: g.sendChat},
								PushButton{AssignTo: &g.chatMicBtn, Text: "🎙 Надиктовать", OnClicked: g.toggleRecord},
								PushButton{Text: "Очистить беседу", OnClicked: func() { app.Chat = nil; g.renderChat() }},
							}},
						}},
					}},
				},
			},
			Label{AssignTo: &g.status, Text: "Загружаюсь…", MinSize: Size{Height: 22}},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
				ProgressBar{AssignTo: &g.bar, MinValue: 0, MaxValue: 1000, Visible: false, MaxSize: Size{Height: 16}},
				Label{AssignTo: &g.barLabel, Text: ""},
				PushButton{AssignTo: &g.cancelBtn, Text: "Остановить", Visible: false, OnClicked: func() { app.CancelDownload() }},
				HSpacer{},
			}},
		},
	}.Create()
	if err != nil {
		walk.MsgBox(nil, "Гига Писарь", "окно не открылось: "+err.Error(), walk.MsgBoxIconError)
		return
	}
	if g.icon != nil {
		g.mw.SetIcon(g.icon)
	}
	g.modeCB.SetCurrentIndex(map[string]int{"dictation": 0, "dictaphone": 1, "chat": 2}[app.Cfg.Mode])
	g.setupTray()
	g.overlay = newOverlay(g)
	g.registerHotkey()
	g.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		if g.quitting {
			return
		}
		if app.Cfg.Tray && g.ni != nil {
			*canceled = true
			g.mw.Hide()
			return
		}
		g.quit()
	})
	g.refresh()
	g.renderChips()
	g.renderHistory()
	g.recModel.PublishRowsReset()
	if tray && app.Cfg.Tray && g.ni != nil {
		g.mw.SetVisible(false)
	} else {
		g.mw.SetVisible(true)
	}
	go g.bootstrap()
	g.mw.Run()
}

func (g *gui) sync(f func()) {
	if g.mw == nil {
		f()
		return
	}
	g.mw.Synchronize(f)
}

func (g *gui) bootstrap() {
	if !g.app.ModelReady() {
		g.sync(func() {
			g.setStatus("Для диктовки нужен пакет распознавания (213 МБ) — скачать один раз?", core.Warn)
			if walk.MsgBox(g.mw, "Пакет распознавания речи", "Для диктовки нужно один раз скачать среду onnxruntime и пакет GigaAM v3 (213 МБ). Дальше речь распознаётся на этом компьютере, звук никуда не уходит. Скачать?", walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes {
				go g.install()
			}
		})
		return
	}
	if _, err := g.app.EnsureRecognizer(); err != nil {
		g.app.OnStatus("Модель не загрузилась: "+err.Error(), core.Error)
		return
	}
	g.app.OnStatus("Готово — поставьте курсор в текст, нажмите «Запись» и говорите", core.OK)
}

func (g *gui) install() {
	if err := g.app.InstallModel(); err != nil {
		g.app.OnStatus("Не скачалось: "+err.Error()+" — Настройки → Диктовка → «Скачать»", core.Error)
	}
}

// ─────────────────────────── состояние ───────────────────────────

func (g *gui) setStatus(t string, k core.Kind) {
	g.status.SetText(t)
	switch k {
	case core.OK:
		g.status.SetTextColor(walk.RGB(30, 142, 62))
	case core.Warn:
		g.status.SetTextColor(walk.RGB(199, 116, 0))
	case core.Error:
		g.status.SetTextColor(walk.RGB(200, 40, 40))
	default:
		g.status.SetTextColor(walk.RGB(60, 60, 60))
	}
	if g.overlay != nil && g.sysDict != nil {
		g.overlay.show(t, k)
	}
}

func (g *gui) setProgress(label string, d, t int64) {
	if label == "" {
		g.bar.SetVisible(false)
		g.cancelBtn.SetVisible(false)
		g.barLabel.SetText("")
		return
	}
	g.bar.SetVisible(true)
	g.cancelBtn.SetVisible(true)
	if t > 0 {
		g.bar.SetValue(int(1000 * d / t))
		g.barLabel.SetText(fmt.Sprintf("%s: %d из %d МБ (%d%%)", label, d/1e6, t/1e6, 100*d/t))
	} else {
		g.barLabel.SetText(fmt.Sprintf("%s: %d МБ", label, d/1e6))
	}
}

func (g *gui) refresh() {
	a := g.app
	rec, busy := a.Recording(), a.Busy()
	dictaphone := a.Cfg.Mode == "dictaphone"
	if rec {
		if dictaphone {
			g.recBtn.SetText(fmt.Sprintf("⏹ Стоп %d:%02d", int(a.RecSeconds)/60, int(a.RecSeconds)%60))
		} else {
			g.recBtn.SetText("⏹ Стоп")
		}
	} else {
		g.recBtn.SetText("🎙 Запись")
	}
	g.recBtn.SetEnabled(!busy)
	g.pauseBtn.SetVisible(dictaphone && rec)
	if a.Paused() {
		g.pauseBtn.SetText("▶ Продолжить")
	} else {
		g.pauseBtn.SetText("⏸ Пауза")
	}
	g.brainBtn.SetEnabled(!rec)
	g.undoBtn.SetEnabled(a.CanUndo() && !busy)
	g.sendBtn.SetEnabled(!busy && !rec)
	if busy && a.Cfg.Mode == "chat" {
		g.sendBtn.SetText("Думает…")
	} else {
		g.sendBtn.SetText("Отправить (Ctrl+Enter)")
	}
	g.chatMicBtn.SetText(map[bool]string{true: "⏹ Стоп", false: "🎙 Надиктовать"}[rec])
	g.whereLbl.SetText(g.whereText())
	g.recModel.PublishRowsReset()
	if g.ni != nil {
		g.ni.SetToolTip("Гига Писарь — " + g.status.Text())
	}
	if rec && dictaphone { // таймер записи
		go func() { time.Sleep(500 * time.Millisecond); g.sync(func() { if a.Recording() { g.refresh() } }) }()
	}
}

func (g *gui) whereText() string {
	c := g.app.Cfg
	switch c.BrainMode {
	case "local":
		return "Мозг: на этом компьютере · " + c.LocalModel
	case "pc":
		return "Мозг: GigaBrain/Ollama · " + c.PcModel
	case "server":
		return "Мозг: сервер · " + c.ServerModel
	case "cloud":
		if s := brain.CloudByID(c.CloudService); s != nil {
			return "Мозг: " + s.Name
		}
		return "Мозг: облако"
	}
	return "Мозг выключен — ⚙ ниже"
}

func (g *gui) modeChanged() {
	modes := []string{"dictation", "dictaphone", "chat"}
	i := g.modeCB.CurrentIndex()
	if i < 0 || i >= len(modes) || g.app.Recording() {
		return
	}
	g.app.Cfg.Mode = modes[i]
	g.app.Save()
	if modes[i] == "chat" {
		g.tabs.SetCurrentIndex(1)
		g.setStatus("Общение с нейронкой: пишите или надиктуйте вопрос", core.Info)
	} else {
		g.tabs.SetCurrentIndex(0)
		if modes[i] == "dictaphone" {
			g.setStatus("Диктофон: длинная запись по частям — пауза, продолжение, файл остаётся", core.Info)
		} else {
			g.setStatus("Нажмите «Запись» и говорите", core.Info)
		}
	}
	g.refresh()
}

// ─────────────────────────── диктовка в окне ───────────────────────────

func (g *gui) fieldTarget() *fieldTarget {
	if g.app.Cfg.Mode == "chat" {
		return &fieldTarget{te: g.chatInput, sync: g.sync, name: "chat"}
	}
	return &fieldTarget{te: g.text, sync: g.sync, name: "field"}
}

func (g *gui) toggleRecord() {
	a := g.app
	if a.Recording() {
		a.StopRecording()
		return
	}
	if !a.ModelReady() {
		go g.bootstrap()
		return
	}
	if err := a.StartRecording(g.fieldTarget(), a.Cfg.Mode == "dictaphone"); err != nil {
		g.setStatus(err.Error(), core.Error)
	}
	g.refresh()
}

func (g *gui) togglePanel() {
	if !g.app.BrainOn() {
		g.settingsBrain()
		return
	}
	g.panel.SetVisible(!g.panel.Visible())
	if g.panel.Visible() {
		g.renderChips()
		g.renderHistory()
		g.updateScope()
	}
	g.mw.SendMessage(win.WM_SIZE, 0, 0)
}

func (g *gui) updateScope() {
	a, b := g.text.TextSelection()
	if b > a {
		g.scopeLbl.SetText(fmt.Sprintf("Работает над выделенным (%d зн.)", b-a))
	} else {
		g.scopeLbl.SetText(fmt.Sprintf("Работает над всем текстом (%d сл.). Выделите часть, чтобы править только её", len(strings.Fields(g.text.Text()))))
	}
}

func (g *gui) renderChips() {
	for g.chips.Children().Len() > 0 {
		g.chips.Children().At(0).Dispose()
	}
	for _, c := range g.app.Cfg.Chips {
		cmd := c.Command
		b, _ := walk.NewPushButton(g.chips)
		b.SetText(c.Title)
		b.SetToolTipText(cmd)
		b.Clicked().Attach(func() { g.runCommand(cmd, false) })
	}
}

func (g *gui) renderHistory() {
	var items []string
	for _, h := range g.app.History.Items {
		pin := "   "
		if h.Pinned {
			pin = "📌 "
		}
		items = append(items, pin+h.Text)
	}
	g.hist.SetModel(items)
}

func (g *gui) runOwn() {
	if p := strings.TrimSpace(g.own.Text()); p != "" {
		g.runCommand(p, true)
	}
}

func (g *gui) runCommand(command string, own bool) {
	g.updateScope()
	g.app.RunCommand(g.fieldTarget(), g.text.Text(), command, own)
	if own {
		g.renderHistory()
	}
}

// ─────────────────────────── общение ───────────────────────────

func (g *gui) sendChat() {
	q := strings.TrimSpace(g.chatInput.Text())
	if q == "" {
		return
	}
	g.chatInput.SetText("")
	g.app.SendChat(q)
}

func (g *gui) renderChat() {
	var b strings.Builder
	for _, m := range g.app.Chat {
		if m.Role == "user" {
			b.WriteString("Вы: ")
		} else {
			b.WriteString("Писарь: ")
		}
		b.WriteString(strings.ReplaceAll(m.Content, "\n", "\r\n"))
		b.WriteString("\r\n\r\n")
	}
	g.chatView.SetText(b.String())
	g.chatView.SendMessage(win.WM_VSCROLL, win.SB_BOTTOM, 0)
	g.refresh()
}

// ─────────────────────────── записи ───────────────────────────

type recordsModel struct {
	walk.TableModelBase
	g *gui
}

func (m *recordsModel) RowCount() int { return len(m.g.app.Records()) }
func (m *recordsModel) Value(row, col int) interface{} {
	r := m.g.app.Records()
	if row >= len(r) {
		return ""
	}
	switch col {
	case 0:
		return r[row].Name
	case 1:
		return fmt.Sprintf("%d:%02d", int(r[row].Seconds)/60, int(r[row].Seconds)%60)
	case 2:
		return fmt.Sprintf("%.1f МБ", float64(r[row].Bytes)/1e6)
	}
	return ""
}

func (g *gui) transcribeSelected() {
	i := g.recList.CurrentIndex()
	r := g.app.Records()
	if i < 0 || i >= len(r) {
		return
	}
	g.tabs.SetCurrentIndex(0)
	g.app.TranscribeRecord(r[i].Path, &fieldTarget{te: g.text, sync: g.sync, name: "field"})
}

func (g *gui) deleteSelected() {
	i := g.recList.CurrentIndex()
	r := g.app.Records()
	if i < 0 || i >= len(r) {
		return
	}
	if walk.MsgBox(g.mw, "Удалить запись", "Удалить "+r[i].Name+".wav?", walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes {
		os.Remove(r[i].Path)
		g.recModel.PublishRowsReset()
	}
}

// ─────────────────────────── экспорт ───────────────────────────

func (g *gui) exportText() string {
	if g.app.Cfg.Mode == "chat" {
		return g.app.ChatAsText()
	}
	return g.text.Text()
}

func (g *gui) export(kind string) {
	text := g.exportText()
	if strings.TrimSpace(text) == "" {
		g.setStatus("Нечего сохранять", core.Warn)
		return
	}
	dlg := new(walk.FileDialog)
	dlg.Title = "Сохранить"
	dlg.FilePath = "Гига Писарь " + time.Now().Format("2006-01-02 15-04") + "." + kind
	dlg.Filter = map[string]string{"docx": "Word (*.docx)|*.docx", "md": "Markdown (*.md)|*.md", "txt": "Текст (*.txt)|*.txt"}[kind]
	if ok, _ := dlg.ShowSave(g.mw); !ok {
		return
	}
	path := dlg.FilePath
	if !strings.HasSuffix(strings.ToLower(path), "."+kind) {
		path += "." + kind
	}
	var data []byte
	switch kind {
	case "docx":
		data = export.Docx(text, "")
	case "md":
		data = export.Md(text, "")
	default:
		data = export.Txt(text)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		g.setStatus("Не сохранил: "+err.Error(), core.Error)
		return
	}
	g.setStatus("Сохранено: "+path, core.OK)
}

func (g *gui) clear() {
	if g.app.Cfg.Mode == "chat" {
		g.app.Chat = nil
		g.renderChat()
	} else {
		g.text.SetText("")
	}
}

// ─────────────────────────── системная диктовка ───────────────────────────

func (g *gui) registerHotkey() {
	if g.hotkey != nil {
		g.hotkey.Unregister()
		g.hotkey = nil
	}
	hk, err := gwin.Register(1, g.app.Cfg.HotkeyMods, g.app.Cfg.HotkeyVk, func() { g.sync(g.systemDictation) })
	if err != nil {
		g.app.Log("Горячая клавиша: " + err.Error())
		g.sync(func() { g.setStatus("Горячая клавиша не зарегистрировалась: "+err.Error()+" — смените её в настройках", core.Warn) })
		return
	}
	g.hotkey = hk
}

// systemDictation: нажали сочетание в любом окне — запись в него; ещё раз — стоп.
func (g *gui) systemDictation() {
	a := g.app
	if a.Recording() {
		a.StopRecording()
		return
	}
	if a.Busy() {
		return
	}
	if !a.ModelReady() {
		g.mw.SetVisible(true)
		go g.bootstrap()
		return
	}
	t := newForegroundTarget()
	if strings.Contains(t.title, "Гига Писарь") {
		// курсор в нашем же окне — диктуем в поле
		t2 := g.fieldTarget()
		if err := a.StartRecording(t2, false); err != nil {
			g.setStatus(err.Error(), core.Error)
		}
		g.refresh()
		return
	}
	g.sysDict = t
	if err := a.StartRecording(t, false); err != nil {
		g.overlay.show("Микрофон: "+err.Error(), core.Error)
		g.sysDict = nil
		return
	}
	g.overlay.show("● Запись → "+t.title, core.Info)
	go func() { // после завершения — спрятать статус
		for a.Recording() || a.Busy() {
			time.Sleep(200 * time.Millisecond)
		}
		time.Sleep(4 * time.Second)
		g.sync(func() { g.sysDict = nil; g.overlay.hide() })
	}()
}

// ─────────────────────────── трей и выход ───────────────────────────

func (g *gui) setupTray() {
	ni, err := walk.NewNotifyIcon(g.mw)
	if err != nil {
		return
	}
	g.ni = ni
	if g.icon != nil {
		ni.SetIcon(g.icon)
	}
	ni.SetToolTip("Гига Писарь")
	show := func() {
		g.mw.SetVisible(true)
		win.ShowWindow(g.mw.Handle(), win.SW_RESTORE)
		win.SetForegroundWindow(g.mw.Handle())
	}
	ni.MouseDown().Attach(func(x, y int, b walk.MouseButton) {
		if b == walk.LeftButton {
			show()
		}
	})
	add := func(text string, f func()) {
		a := walk.NewAction()
		a.SetText(text)
		a.Triggered().Attach(f)
		ni.ContextMenu().Actions().Add(a)
	}
	add("Открыть окно", show)
	add("Диктовать в активное окно ("+hotkeyText(g.app.Cfg.HotkeyMods, g.app.Cfg.HotkeyVk)+")", g.systemDictation)
	add("Выход", g.quit)
	ni.SetVisible(true)
}

func (g *gui) quit() {
	g.quitting = true
	if g.app.Recording() {
		g.app.CancelRecording()
	}
	g.app.Local.Stop()
	if g.hotkey != nil {
		g.hotkey.Unregister()
	}
	if g.ni != nil {
		g.ni.Dispose()
	}
	walk.App().Exit(0)
}

func (g *gui) showLog() {
	var dlg *walk.Dialog
	Dialog{AssignTo: &dlg, Title: "Протокол", MinSize: Size{Width: 640, Height: 400}, Layout: VBox{},
		Children: []Widget{
			TextEdit{Text: strings.Join(g.logBuf, "\r\n"), ReadOnly: true, VScroll: true, Font: Font{Family: "Consolas", PointSize: 9}},
			PushButton{Text: "Закрыть", OnClicked: func() { dlg.Cancel() }},
		}}.Create(g.mw)
	dlg.Run()
}

func hotkeyText(mods, vk int) string {
	var p []string
	if mods&gwin.ModControl != 0 {
		p = append(p, "Ctrl")
	}
	if mods&gwin.ModAlt != 0 {
		p = append(p, "Alt")
	}
	if mods&gwin.ModShift != 0 {
		p = append(p, "Shift")
	}
	if mods&gwin.ModWin != 0 {
		p = append(p, "Win")
	}
	names := map[int]string{0x20: "Space", 0x70: "F1", 0x71: "F2", 0x72: "F3", 0x73: "F4", 0x74: "F5", 0x75: "F6", 0x76: "F7", 0x77: "F8", 0x78: "F9", 0x79: "F10", 0x7A: "F11", 0x7B: "F12", 0x2D: "Insert", 0x24: "Home", 0x91: "ScrollLock", 0x13: "Pause"}
	n, ok := names[vk]
	if !ok {
		n = string(rune(vk))
	}
	return strings.Join(append(p, n), "+")
}
