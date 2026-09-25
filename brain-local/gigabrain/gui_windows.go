//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

func fatal(msg string) {
	walk.MsgBox(nil, "GigaBrain", msg, walk.MsgBoxIconError)
	os.Exit(1)
}

// ─────────────────────────── окно ───────────────────────────

type gui struct {
	mw       *walk.MainWindow
	ni       *walk.NotifyIcon
	r        *router
	o        *options
	icon     *walk.Icon
	status   *walk.Label
	addrEdit *walk.LineEdit
	keyEdit  *walk.LineEdit
	keyShown bool
	table    *walk.TableView
	models   *modelsModel
	mainBtn  *walk.PushButton
	delBtn   *walk.PushButton
	backend  *walk.ComboBox
	portEdit *walk.NumberEdit
	thrEdit  *walk.NumberEdit
	ctxEdit  *walk.NumberEdit
	autoCB   *walk.CheckBox
	trayCB   *walk.CheckBox
	dirLabel *walk.Label
	logEdit  *walk.TextEdit
	bar      *walk.ProgressBar
	barLabel *walk.Label
	busy     bool // идёт скачивание или установка
	busyMu   sync.Mutex
	quitting bool
}

var backends = []string{"cpu", "vulkan", "cuda"}
var backendTitles = []string{"Процессор", "Видеокарта (Vulkan)", "Видеокарта NVIDIA (CUDA)"}

func backendIndex(b string) int {
	for i, x := range backends {
		if x == b {
			return i
		}
	}
	return 0
}

// Строки таблицы скачанных моделей.
type modelsModel struct {
	walk.TableModelBase
	g *gui
}

func (m *modelsModel) RowCount() int { return len(cfg.Models) }
func (m *modelsModel) Value(row, col int) interface{} {
	if row >= len(cfg.Models) {
		return ""
	}
	x := cfg.Models[row]
	switch col {
	case 0:
		return x.Name
	case 1:
		if x.SizeGB > 0 {
			return fmtGB(x.SizeGB)
		}
		if st, err := os.Stat(modelPath(x)); err == nil {
			return fmtGB(float64(st.Size()) / 1e9)
		}
		return "—"
	case 2:
		cur, loading := m.g.r.status()
		s := ""
		switch {
		case loading == x.ID:
			s = "запускается…"
		case cur == x.ID:
			s = "работает"
		}
		if x.ID == cfg.Model {
			if s == "" {
				return "основная"
			}
			return "основная · " + s
		}
		return s
	}
	return ""
}

func runApp(o *options, r *router) {
	g := &gui{r: r, o: o, models: nil}
	g.models = &modelsModel{g: g}
	r.onState = func() { g.sync(func() { g.refresh() }) }
	if icon, err := walk.NewIconFromResourceId(2); err == nil { // 1 — манифест, 2 — иконка (порядок rsrc)
		g.icon = icon
	}
	logFn = func(line string) { g.sync(func() { g.appendLog(line) }) }
	progressFn = func(done, total int64, speed float64) { g.sync(func() { g.progress(done, total, speed) }) }

	err := MainWindow{
		AssignTo: &g.mw,
		Title:    "GigaBrain — мозг Писаря",
		Size:     Size{Width: 640, Height: 620},
		MinSize:  Size{Width: 560, Height: 520},
		Layout:   VBox{Margins: Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}, Spacing: 6},
		Font:     Font{Family: "Segoe UI", PointSize: 9},
		Children: []Widget{
			Label{AssignTo: &g.status, Text: "Запускаюсь…", Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}},
			Composite{
				Layout: Grid{Columns: 5, MarginsZero: true, Spacing: 6},
				Children: []Widget{
					Label{Text: "Адрес:"},
					LineEdit{AssignTo: &g.addrEdit, ReadOnly: true, MinSize: Size{Width: 170}},
					PushButton{Text: "Копировать", OnClicked: func() { walk.Clipboard().SetText(g.addrEdit.Text()) }},
					HSpacer{},
					HSpacer{},
					Label{Text: "Ключ доступа:"},
					LineEdit{AssignTo: &g.keyEdit, ReadOnly: true, PasswordMode: true, MinSize: Size{Width: 170}},
					PushButton{Text: "Копировать", OnClicked: func() { walk.Clipboard().SetText(cfg.Key) }},
					PushButton{Text: "Показать", OnClicked: func() {
						g.keyShown = !g.keyShown
						g.keyEdit.SetPasswordMode(!g.keyShown)
					}},
					HSpacer{},
				},
			},
			GroupBox{
				Title:  "Нейронки на этом компьютере",
				Layout: VBox{Margins: Margins{Left: 6, Top: 4, Right: 6, Bottom: 6}, Spacing: 4},
				Children: []Widget{
					TableView{
						AssignTo:            &g.table,
						AlternatingRowBG:    true,
						LastColumnStretched: true,
						MinSize:             Size{Height: 90},
						Model:               g.models,
						Columns: []TableViewColumn{
							{Title: "Модель", Width: 260},
							{Title: "Размер", Width: 80},
							{Title: "Состояние"},
						},
						OnCurrentIndexChanged: func() { g.refreshButtons() },
						OnItemActivated:       func() { g.makeMain() },
					},
					Composite{
						Layout: HBox{MarginsZero: true, Spacing: 6},
						Children: []Widget{
							PushButton{AssignTo: &g.mainBtn, Text: "Сделать основной", OnClicked: g.makeMain},
							PushButton{Text: "Скачать ещё…", OnClicked: g.showCatalog},
							PushButton{AssignTo: &g.delBtn, Text: "Удалить", OnClicked: g.deleteModel},
							HSpacer{},
							PushButton{Text: "Папка моделей", OnClicked: func() { openFolder(home + `\models`) }},
						},
					},
				},
			},
			GroupBox{
				Title:  "Настройки",
				Layout: Grid{Columns: 7, Margins: Margins{Left: 6, Top: 4, Right: 6, Bottom: 6}, Spacing: 6},
				Children: []Widget{
					Label{Text: "Считать на:"},
					ComboBox{AssignTo: &g.backend, Model: backendTitles, ColumnSpan: 2, OnCurrentIndexChanged: g.backendChanged},
					PushButton{Text: "Переустановить движок", ColumnSpan: 4, OnClicked: func() { g.installEngine(true) }},

					Label{Text: "Порт:"},
					NumberEdit{AssignTo: &g.portEdit, MinValue: 1024, MaxValue: 65535, Decimals: 0},
					Label{Text: "Потоки:"},
					NumberEdit{AssignTo: &g.thrEdit, MinValue: 1, MaxValue: 256, Decimals: 0},
					Label{Text: "Контекст:"},
					NumberEdit{AssignTo: &g.ctxEdit, MinValue: 1024, MaxValue: 262144, Decimals: 0},
					PushButton{Text: "Применить", OnClicked: func() { g.portChanged(); g.perfChanged() }},

					CheckBox{AssignTo: &g.autoCB, Text: "Запускать при входе в Windows", ColumnSpan: 3, OnCheckedChanged: g.autostartChanged},
					CheckBox{AssignTo: &g.trayCB, Text: "Закрытие окна прячет мозг в область уведомлений", ColumnSpan: 4, OnCheckedChanged: func() {
						cfg.Tray = g.trayCB.Checked()
						saveConfig()
					}},

					Label{Text: "Папка данных:"},
					Label{AssignTo: &g.dirLabel, Text: home, ColumnSpan: 3, EllipsisMode: EllipsisPath},
					PushButton{Text: "Открыть", OnClicked: func() { openFolder(home) }},
					PushButton{Text: "Перенести…", ColumnSpan: 2, OnClicked: g.moveData},
				},
			},
			GroupBox{
				Title:  "Протокол",
				Layout: VBox{Margins: Margins{Left: 6, Top: 4, Right: 6, Bottom: 6}, Spacing: 4},
				Children: []Widget{
					TextEdit{AssignTo: &g.logEdit, ReadOnly: true, VScroll: true, MinSize: Size{Height: 90},
						Font: Font{Family: "Consolas", PointSize: 9}},
					Composite{
						Layout: HBox{MarginsZero: true, Spacing: 6},
						Children: []Widget{
							ProgressBar{AssignTo: &g.bar, MinValue: 0, MaxValue: 1000, Visible: false, MaxSize: Size{Height: 16}},
							Label{AssignTo: &g.barLabel, Text: "", MinSize: Size{Width: 200}},
							HSpacer{},
							PushButton{Text: "Выключить мозг", OnClicked: g.quit},
						},
					},
				},
			},
		},
	}.Create()
	if err != nil {
		fatal("окно не открылось: " + err.Error())
	}
	if g.icon != nil {
		g.mw.SetIcon(g.icon)
	}
	g.setupTray()
	g.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		if g.quitting {
			return
		}
		if cfg.Tray && g.ni != nil {
			*canceled = true
			g.mw.Hide()
			if !g.busyNow() {
				g.ni.ShowInfo("GigaBrain", "Мозг работает. Значок в области уведомлений: открыть или выключить.")
			}
			return
		}
		g.quitting = true
		g.r.shutdown()
	})

	g.fill()
	g.appendLog(fmt.Sprintf("== Мозг Писаря (GigaBrain %s) — %s", version, home))
	g.appendLog(fmt.Sprintf("   память компьютера: %d ГБ, ядер: %d, считает: %s", totalRAMGB(), runtime.NumCPU(), backendName(cfg.Backend)))

	if o.tray && cfg.Tray && g.ni != nil {
		g.mw.SetVisible(false)
	} else {
		g.mw.SetVisible(true)
	}
	go g.bootstrap()
	g.mw.Run()
}

// Всё, что трогает виджеты, — только в потоке окна.
func (g *gui) sync(f func()) {
	if g.mw == nil {
		f()
		return
	}
	g.mw.Synchronize(f)
}

func (g *gui) busyNow() bool {
	g.busyMu.Lock()
	defer g.busyMu.Unlock()
	return g.busy
}

// Занимает программу долгим делом (скачивание, установка); второе не пускает.
func (g *gui) run(what string, f func() error) {
	g.busyMu.Lock()
	if g.busy {
		g.busyMu.Unlock()
		g.sync(func() { walk.MsgBox(g.mw, "GigaBrain", "Подождите: сейчас идёт другое скачивание.", walk.MsgBoxIconInformation) })
		return
	}
	g.busy = true
	g.busyMu.Unlock()
	go func() {
		err := f()
		g.busyMu.Lock()
		g.busy = false
		g.busyMu.Unlock()
		g.sync(func() {
			g.progress(0, 0, 0)
			if err != nil {
				g.appendLog("! " + what + ": " + err.Error())
				walk.MsgBox(g.mw, what, err.Error(), walk.MsgBoxIconWarning)
			}
			g.refresh()
		})
	}()
}

func (g *gui) appendLog(line string) {
	if g.logEdit == nil {
		return
	}
	line = strings.ReplaceAll(line, "\n", "\r\n")
	g.logEdit.AppendText(time.Now().Format("15:04:05 ") + line + "\r\n")
}

func (g *gui) progress(done, total int64, speed float64) {
	if g.bar == nil {
		return
	}
	if done == 0 && total == 0 {
		g.bar.SetVisible(false)
		g.barLabel.SetText("")
		return
	}
	g.bar.SetVisible(true)
	mb := float64(done) / 1e6
	if total > 0 {
		g.bar.SetMarqueeMode(false)
		g.bar.SetValue(int(1000 * done / total))
		g.barLabel.SetText(fmt.Sprintf("%.0f%%  %.0f / %.0f МБ  %.1f МБ/с", 100*float64(done)/float64(total), mb, float64(total)/1e6, speed))
	} else {
		g.bar.SetMarqueeMode(true)
		g.barLabel.SetText(fmt.Sprintf("%.0f МБ  %.1f МБ/с", mb, speed))
	}
}

// Заполняет окно из настроек (один раз при старте и после переноса папки).
func (g *gui) fill() {
	g.addrEdit.SetText(fmt.Sprintf("http://127.0.0.1:%d", cfg.Port))
	g.keyEdit.SetText(cfg.Key)
	g.backend.SetCurrentIndex(backendIndex(cfg.Backend))
	g.portEdit.SetValue(float64(cfg.Port))
	g.thrEdit.SetValue(float64(cfg.Threads))
	g.ctxEdit.SetValue(float64(cfg.Ctx))
	g.autoCB.SetChecked(autostartEnabled())
	g.trayCB.SetChecked(cfg.Tray)
	g.dirLabel.SetText(home)
	g.refresh()
}

// Перерисовывает статус и таблицу моделей.
func (g *gui) refresh() {
	if g.mw == nil {
		return
	}
	g.models.PublishRowsReset()
	g.refreshButtons()
	cur, loading := g.r.status()
	var s string
	switch {
	case !engineInstalled():
		s = "○ Движок llama.cpp не установлен"
	case len(cfg.Models) == 0:
		s = "○ Нейронок ещё нет — «Скачать ещё…»"
	case loading != "":
		s = "◐ Запускается " + nameOf(loading) + "…"
	case cur != "":
		s = fmt.Sprintf("● Мозг слушает http://127.0.0.1:%d · %s готова", cfg.Port, nameOf(cur))
	default:
		s = fmt.Sprintf("● Мозг слушает http://127.0.0.1:%d · нейронка поднимется по первому запросу", cfg.Port)
	}
	g.status.SetText(s)
	if g.ni != nil {
		g.ni.SetToolTip("GigaBrain — " + strings.TrimLeft(s, "○◐● "))
	}
}

func (g *gui) refreshButtons() {
	i := g.table.CurrentIndex()
	ok := i >= 0 && i < len(cfg.Models)
	g.mainBtn.SetEnabled(ok && cfg.Models[i].ID != cfg.Model)
	g.delBtn.SetEnabled(ok)
}

func nameOf(id string) string {
	if m := findInstalled(id); m != nil {
		return m.Name
	}
	return id
}

// ─────────────────────────── запуск ───────────────────────────

// Первый запуск: движок и первая нейронка. Дальше — просто слушать.
func (g *gui) bootstrap() {
	if !engineInstalled() && g.o.serverBin == "" {
		g.sync(func() { g.firstRun() })
		return
	}
	if g.o.add != "" {
		g.run("Скачивание", func() error {
			if err := addModel(g.o.add); err != nil {
				return err
			}
			return startServing(g.r)
		})
		return
	}
	if g.o.model != "" && findInstalled(g.o.model) != nil {
		cfg.Model = g.o.model
	}
	makeShortcut()
	if err := startServing(g.r); err != nil {
		g.sync(func() {
			g.appendLog("! " + err.Error())
			walk.MsgBox(g.mw, "GigaBrain", err.Error(), walk.MsgBoxIconError)
		})
	}
	g.sync(g.refresh)
}

// Окно первого запуска: где считать и какую нейронку скачать.
func (g *gui) firstRun() {
	var dlg *walk.Dialog
	var rb [3]*walk.RadioButton
	var modelCB *walk.ComboBox
	var autoCB *walk.CheckBox
	names := make([]string, len(catalog.Models))
	ram := float64(totalRAMGB())
	for i, m := range catalog.Models {
		mark := ""
		if m.RAMGB > ram {
			mark = "  (памяти впритык)"
		}
		names[i] = fmt.Sprintf("%s — %s, нужно %.0f ГБ памяти%s", m.Name, fmtGB(m.SizeGB), m.RAMGB, mark)
	}
	def := backendIndex(cfg.Backend)
	if cfg.Backend == "" {
		def = 0
	}
	Dialog{
		AssignTo: &dlg,
		Title:    "GigaBrain — первый запуск",
		MinSize:  Size{Width: 520, Height: 300},
		Layout:   VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}, Spacing: 8},
		Font:     Font{Family: "Segoe UI", PointSize: 9},
		Children: []Widget{
			Label{Text: fmt.Sprintf("Память компьютера: %d ГБ, ядер: %d. Всё будет лежать в %s", totalRAMGB(), runtime.NumCPU(), home)},
			GroupBox{
				Title:  "Где считать нейронку",
				Layout: VBox{Spacing: 2},
				Children: []Widget{
					RadioButton{AssignTo: &rb[0], Text: "Процессор — работает везде"},
					RadioButton{AssignTo: &rb[1], Text: "Видеокарта через Vulkan — NVIDIA, AMD, Intel; заметно быстрее"},
					RadioButton{AssignTo: &rb[2], Text: "Видеокарта NVIDIA через CUDA — быстрее всего, нужны драйверы NVIDIA"},
				},
			},
			Label{Text: "Первая нейронка (можно добавить другие потом):"},
			ComboBox{AssignTo: &modelCB, Model: names, CurrentIndex: defaultCatalogIndex()},
			CheckBox{AssignTo: &autoCB, Text: "Запускать мозг при входе в Windows", Checked: true},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{Text: "Установить", OnClicked: func() { dlg.Accept() }},
					PushButton{Text: "Выход", OnClicked: func() { dlg.Cancel() }},
				},
			},
		},
	}.Create(g.mw)
	rb[def].SetChecked(true)
	g.mw.SetVisible(true)
	if dlg.Run() != walk.DlgCmdOK {
		g.quit()
		return
	}
	for i, b := range rb {
		if b.Checked() {
			cfg.Backend = backends[i]
		}
	}
	g.backend.SetCurrentIndex(backendIndex(cfg.Backend))
	modelID := ""
	if i := modelCB.CurrentIndex(); i >= 0 && i < len(catalog.Models) {
		modelID = catalog.Models[i].ID
	}
	wantAuto := autoCB.Checked()
	saveConfig()
	g.run("Установка", func() error {
		if err := g.installEngineSteps(); err != nil {
			return err
		}
		if wantAuto {
			if err := setAutostart(true); err == nil {
				g.sync(func() { g.autoCB.SetChecked(true) })
			}
		}
		makeShortcut()
		if modelID != "" {
			if err := addModel(modelID); err != nil {
				return err
			}
		}
		return startServing(g.r)
	})
}

// Ставит движок; если сборка не подобралась — даёт выбрать файл выпуска.
func (g *gui) installEngineSteps() error {
	err := installLlama(nil, "")
	var na *errNoAsset
	if errors.As(err, &na) {
		var pick *asset
		done := make(chan struct{})
		g.sync(func() {
			pick = g.pickAssetDialog(na)
			close(done)
		})
		<-done
		if pick == nil {
			return errors.New("сборка не выбрана: можно переключиться на процессор («Считать на») и повторить")
		}
		err = installLlama(pick, na.Tag)
	}
	return err
}

func (g *gui) pickAssetDialog(na *errNoAsset) *asset {
	var dlg *walk.Dialog
	var lb *walk.ListBox
	names := make([]string, len(na.Assets))
	for i, a := range na.Assets {
		names[i] = a.Name
	}
	Dialog{
		AssignTo: &dlg,
		Title:    "Сборка llama.cpp не подобралась",
		MinSize:  Size{Width: 520, Height: 400},
		Layout:   VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}, Spacing: 8},
		Children: []Widget{
			Label{Text: na.Error() + ". Выберите файл выпуска вручную:"},
			ListBox{AssignTo: &lb, Model: names, MinSize: Size{Height: 250}},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{Text: "Скачать этот", OnClicked: func() { dlg.Accept() }},
					PushButton{Text: "Отмена", OnClicked: func() { dlg.Cancel() }},
				},
			},
		},
	}.Create(g.mw)
	if dlg.Run() != walk.DlgCmdOK {
		return nil
	}
	if i := lb.CurrentIndex(); i >= 0 && i < len(na.Assets) {
		return &na.Assets[i]
	}
	return nil
}

func (g *gui) installEngine(reinstall bool) {
	g.run("Установка движка", func() error {
		g.r.stopChild()
		if reinstall {
			os.Remove(serverBinPath())
		}
		if err := g.installEngineSteps(); err != nil {
			return err
		}
		if g.r.ln == nil {
			return startServing(g.r)
		}
		if cfg.Model != "" {
			go g.r.ensure(cfg.Model)
		}
		return nil
	})
}

// ─────────────────────────── действия ───────────────────────────

func (g *gui) makeMain() {
	i := g.table.CurrentIndex()
	if i < 0 || i >= len(cfg.Models) {
		return
	}
	cfg.Model = cfg.Models[i].ID
	saveConfig()
	g.appendLog("== Основная модель: " + cfg.Models[i].Name)
	g.refresh()
	go g.r.ensure(cfg.Model)
}

func (g *gui) deleteModel() {
	i := g.table.CurrentIndex()
	if i < 0 || i >= len(cfg.Models) {
		return
	}
	m := cfg.Models[i]
	if walk.MsgBox(g.mw, "Удалить нейронку", fmt.Sprintf("Удалить %s с диска (%s)?", m.Name, fmtGB(m.SizeGB)), walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		return
	}
	if cur, _ := g.r.status(); cur == m.ID {
		g.r.stopChild()
	}
	removeModel(m.ID)
	g.appendLog("== Удалена " + m.Name)
	g.refresh()
	if cfg.Model != "" {
		go g.r.ensure(cfg.Model)
	}
}

// Каталог: таблица с размером и памятью, поле для своего адреса .gguf.
func (g *gui) showCatalog() {
	var dlg *walk.Dialog
	var tv *walk.TableView
	var urlEdit *walk.LineEdit
	cm := &catalogModel_{}
	Dialog{
		AssignTo: &dlg,
		Title:    "Скачать нейронку",
		MinSize:  Size{Width: 720, Height: 460},
		Layout:   VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}, Spacing: 8},
		Font:     Font{Family: "Segoe UI", PointSize: 9},
		Children: []Widget{
			Label{Text: fmt.Sprintf("Память компьютера: %d ГБ. ✓ — уже скачана, ! — памяти впритык (запустится, но медленно).", totalRAMGB())},
			TableView{
				AssignTo:            &tv,
				AlternatingRowBG:    true,
				LastColumnStretched: true,
				Model:               cm,
				Columns: []TableViewColumn{
					{Title: "", Width: 24},
					{Title: "Модель", Width: 200},
					{Title: "Файл", Width: 60},
					{Title: "Памяти", Width: 60},
					{Title: "Скорость", Width: 70},
					{Title: "Про что"},
				},
				OnItemActivated: func() { dlg.Accept() },
			},
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 6},
				Children: []Widget{
					Label{Text: "Или свой адрес .gguf:"},
					LineEdit{AssignTo: &urlEdit, CueBanner: "https://huggingface.co/…/model.gguf"},
				},
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{Text: "Скачать", OnClicked: func() { dlg.Accept() }},
					PushButton{Text: "Закрыть", OnClicked: func() { dlg.Cancel() }},
				},
			},
		},
	}.Create(g.mw)
	tv.SetCurrentIndex(defaultCatalogIndex())
	if dlg.Run() != walk.DlgCmdOK {
		return
	}
	what := strings.TrimSpace(urlEdit.Text())
	if what == "" {
		if i := tv.CurrentIndex(); i >= 0 && i < len(catalog.Models) {
			what = catalog.Models[i].ID
		}
	}
	if what == "" {
		return
	}
	g.run("Скачивание", func() error {
		if err := addModel(what); err != nil {
			return err
		}
		if g.r.ln == nil {
			return startServing(g.r)
		}
		return nil
	})
}

type catalogModel_ struct{ walk.TableModelBase }

func (m *catalogModel_) RowCount() int { return len(catalog.Models) }
func (m *catalogModel_) Value(row, col int) interface{} {
	x := catalog.Models[row]
	switch col {
	case 0:
		if findInstalled(x.ID) != nil {
			return "✓"
		}
		if x.RAMGB > float64(totalRAMGB()) {
			return "!"
		}
		return ""
	case 1:
		return x.Name
	case 2:
		return fmtGB(x.SizeGB)
	case 3:
		return fmtGB(x.RAMGB)
	case 4:
		return x.Speed
	case 5:
		return x.About
	}
	return ""
}

func (g *gui) backendChanged() {
	i := g.backend.CurrentIndex()
	if i < 0 || i >= len(backends) || backends[i] == cfg.Backend {
		return
	}
	cfg.Backend = backends[i]
	saveConfig()
	if engineInstalled() {
		if walk.MsgBox(g.mw, "Сменить движок", "Для «"+backendTitles[i]+"» нужно скачать другую сборку llama.cpp. Скачать сейчас?", walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes {
			g.installEngine(true)
		}
	}
}

func (g *gui) portChanged() {
	p := int(g.portEdit.Value())
	if p == cfg.Port || p < 1024 {
		return
	}
	old := cfg.Port
	cfg.Port = p
	if err := g.r.listen(); err != nil {
		cfg.Port = old
		g.portEdit.SetValue(float64(old))
		g.r.listen()
		walk.MsgBox(g.mw, "Порт", err.Error(), walk.MsgBoxIconWarning)
		return
	}
	saveConfig()
	g.addrEdit.SetText(fmt.Sprintf("http://127.0.0.1:%d", cfg.Port))
	g.appendLog(fmt.Sprintf("== Порт: %d (на странице укажите новый адрес)", cfg.Port))
	g.refresh()
}

func (g *gui) perfChanged() {
	t, c := int(g.thrEdit.Value()), int(g.ctxEdit.Value())
	if t == cfg.Threads && c == cfg.Ctx {
		return
	}
	cfg.Threads, cfg.Ctx = t, c
	saveConfig()
	g.appendLog(fmt.Sprintf("== Потоки: %d, контекст: %d — применится при следующем запуске нейронки", t, c))
	if cur, _ := g.r.status(); cur != "" {
		g.r.stopChild()
		go g.r.ensure(cfg.Model)
	}
}

func (g *gui) autostartChanged() {
	want := g.autoCB.Checked()
	if want == autostartEnabled() {
		return
	}
	if err := setAutostart(want); err != nil {
		g.autoCB.SetChecked(!want)
		walk.MsgBox(g.mw, "Автозапуск", err.Error(), walk.MsgBoxIconWarning)
		return
	}
	if want {
		g.appendLog("== Автозапуск включён: мозг будет стартовать при входе в Windows, свёрнутым")
	} else {
		g.appendLog("== Автозапуск выключен")
	}
}

func (g *gui) moveData() {
	if g.busyNow() {
		walk.MsgBox(g.mw, "Папка данных", "Дождитесь конца скачивания.", walk.MsgBoxIconInformation)
		return
	}
	dlg := new(walk.FileDialog)
	dlg.Title = "Куда перенести движок и нейронки"
	dlg.InitialDirPath = home
	if ok, _ := dlg.ShowBrowseFolder(g.mw); !ok || dlg.FilePath == "" {
		return
	}
	dest := dlg.FilePath
	g.run("Перенос папки", func() error {
		g.r.stopChild()
		logf("== Переношу данные в %s…", dest)
		if err := moveHome(dest); err != nil {
			return err
		}
		g.r.bin = serverBinPath()
		if autostartEnabled() {
			_ = setAutostart(true) // путь к exe изменился
		}
		logf("   ✓ теперь всё лежит в %s", home)
		g.sync(func() { g.dirLabel.SetText(home) })
		if cfg.Model != "" {
			go g.r.ensure(cfg.Model)
		}
		return nil
	})
}

func (g *gui) quit() {
	g.quitting = true
	g.r.shutdown()
	if g.ni != nil {
		g.ni.Dispose()
	}
	walk.App().Exit(0)
}

// ─────────────────────────── область уведомлений ───────────────────────────

func (g *gui) setupTray() {
	ni, err := walk.NewNotifyIcon(g.mw)
	if err != nil {
		return
	}
	g.ni = ni
	if g.icon != nil {
		ni.SetIcon(g.icon)
	}
	ni.SetToolTip("GigaBrain — мозг Писаря")
	show := func() {
		g.mw.SetVisible(true)
		win.ShowWindow(g.mw.Handle(), win.SW_RESTORE)
		win.SetForegroundWindow(g.mw.Handle())
	}
	ni.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			show()
		}
	})
	open := walk.NewAction()
	open.SetText("Открыть окно")
	open.Triggered().Attach(show)
	ni.ContextMenu().Actions().Add(open)
	quit := walk.NewAction()
	quit.SetText("Выключить мозг")
	quit.Triggered().Attach(g.quit)
	ni.ContextMenu().Actions().Add(quit)
	ni.SetVisible(true)
}

var _ = strconv.Itoa
