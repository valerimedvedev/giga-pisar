//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"gigapisar/brain"
	"gigapisar/core"
	gwin "gigapisar/win"
)

var brainModes = []string{"off", "local", "pc", "server", "cloud"}
var brainModeTitles = []string{
	"Выключен — текст вставляется как распознан",
	"На этом компьютере — llama.cpp внутри Писаря (модель качается в папку данных)",
	"GigaBrain / Ollama / LM Studio — по адресу (на этом или другом компьютере)",
	"На сервере — GigaChat или любой OpenAI-совместимый адрес",
	"Облачный сервис с бесплатным тарифом — Gemini, Groq, OpenRouter, Mistral…",
}

func idx(list []string, v string) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return 0
}

// Мозг: где считает, модели, облако, набор ключей.
func (g *gui) settingsBrain() {
	a := g.app
	c := a.Cfg
	var dlg *walk.Dialog
	var mode *walk.ComboBox
	var localCB, cloudCB *walk.ComboBox
	var backendCB *walk.ComboBox
	var pcBase, pcKey, pcModel, srvBase, srvKey, srvModel, cloudBase, cloudKey, cloudModel *walk.LineEdit
	var keysBox *walk.TextEdit
	var note, localNote, cloudNote *walk.Label
	var modelURL *walk.LineEdit

	localNames := func() []string {
		var out []string
		for _, m := range a.Local.Installed() {
			out = append(out, m.ID)
		}
		return out
	}
	catalogNames := func() []string {
		var out []string
		for _, m := range a.Local.Catalog {
			mark := ""
			if a.Local.HasModel(m) {
				mark = "✓ "
			}
			out = append(out, fmt.Sprintf("%s%s — %.1f ГБ, памяти %.0f ГБ, %s", mark, m.Name, m.SizeGB, m.RAMGB, m.About))
		}
		return out
	}
	var catalogCB *walk.ComboBox
	cloudNames := []string{}
	for _, s := range brain.CloudServices {
		cloudNames = append(cloudNames, s.Name)
	}
	keysStored, _, _ := brain.ParseKeys(c.CloudKeys)

	fillCloud := func() {
		i := cloudCB.CurrentIndex()
		if i < 0 || i >= len(brain.CloudServices) {
			return
		}
		svc := brain.CloudServices[i]
		var e *brain.KeyEntry
		if k, ok := keysStored[svc.ID]; ok {
			e = &k
		}
		if svc.ID == c.CloudService && c.CloudBase != "" {
			cloudBase.SetText(c.CloudBase)
			cloudModel.SetText(c.CloudModel)
			cloudKey.SetText(c.CloudKey)
		} else {
			cloudBase.SetText(brain.BaseFor(&svc, e))
			cloudModel.SetText(svc.Model)
			if e != nil {
				cloudKey.SetText(e.Key)
			} else {
				cloudKey.SetText("")
			}
		}
		mark := ""
		if e != nil {
			mark = " 🔑 ключ из набора"
		}
		cloudNote.SetText(svc.Note + mark + "\nПолучить ключ: " + svc.KeyURL)
	}

	check := func(base, key string, model *walk.LineEdit, target *walk.Label) {
		target.SetText("Проверяю…")
		go func() {
			list, err := brain.ListModels(base, key, 8*time.Second)
			g.sync(func() {
				if err != nil {
					target.SetText("Не отвечает: " + brain.Friendly(err).Error())
					return
				}
				m := model.Text()
				found := false
				for _, x := range list {
					if x == m || strings.HasSuffix(x, "/"+m) {
						found = true
					}
				}
				if (m == "" || !found) && len(list) > 0 {
					model.SetText(list[0])
				}
				if len(list) > 12 {
					list = append(list[:12], "…")
				}
				target.SetText("Отвечает. Модели: " + strings.Join(list, ", "))
			})
		}()
	}

	Dialog{
		AssignTo: &dlg, Title: "Мозг Писаря", MinSize: Size{Width: 640, Height: 560},
		Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}, Spacing: 6},
		Font:   Font{Family: "Segoe UI", PointSize: 9},
		Children: []Widget{
			Label{Text: "Где считает нейронка:", Font: Font{Family: "Segoe UI", PointSize: 9, Bold: true}},
			ComboBox{AssignTo: &mode, Model: brainModeTitles, CurrentIndex: idx(brainModes, c.BrainMode)},

			GroupBox{Title: "На этом компьютере (llama.cpp)", Layout: Grid{Columns: 3, Spacing: 6}, Children: []Widget{
				Label{Text: "Считать на:"},
				ComboBox{AssignTo: &backendCB, Model: []string{"процессор", "видеокарта (Vulkan)", "видеокарта NVIDIA (CUDA)"}, CurrentIndex: idx([]string{"cpu", "vulkan", "cuda"}, c.Backend)},
				PushButton{Text: "Установить / обновить движок", OnClicked: func() {
					a.Cfg.Backend = []string{"cpu", "vulkan", "cuda"}[backendCB.CurrentIndex()]
					a.Save()
					localNote.SetText("Скачиваю движок… (ход внизу главного окна)")
					go func() {
						err := a.Local.InstallEngine(nil, func(d, t int64, _ float64) { a.OnProgress("Движок llama.cpp", d, t) })
						a.OnProgress("", 0, 0)
						g.sync(func() {
							if err != nil {
								localNote.SetText("Не установился: " + err.Error())
							} else {
								localNote.SetText("Движок установлен")
							}
						})
					}()
				}},
				Label{Text: "Скачать модель:"},
				ComboBox{AssignTo: &catalogCB, Model: catalogNames(), CurrentIndex: 0},
				PushButton{Text: "Скачать", OnClicked: func() {
					i := catalogCB.CurrentIndex()
					if i < 0 {
						return
					}
					id := a.Local.Catalog[i].ID
					go func() {
						m, err := a.Local.AddModel(id, nil, func(d, t int64, _ float64) { a.OnProgress(id, d, t) })
						a.OnProgress("", 0, 0)
						g.sync(func() {
							if err != nil {
								localNote.SetText("Не скачалась: " + err.Error())
								return
							}
							localNote.SetText(m.Name + " скачана")
							localCB.SetModel(localNames())
							catalogCB.SetModel(catalogNames())
							localCB.SetCurrentIndex(idx(localNames(), m.ID))
						})
					}()
				}},
				Label{Text: "Или свой .gguf:"},
				LineEdit{AssignTo: &modelURL, CueBanner: "https://huggingface.co/…/model.gguf"},
				PushButton{Text: "Скачать по адресу", OnClicked: func() {
					u := strings.TrimSpace(modelURL.Text())
					if u == "" {
						return
					}
					go func() {
						m, err := a.Local.AddModel(u, nil, func(d, t int64, _ float64) { a.OnProgress("Модель", d, t) })
						a.OnProgress("", 0, 0)
						g.sync(func() {
							if err != nil {
								localNote.SetText("Не скачалась: " + err.Error())
								return
							}
							localCB.SetModel(localNames())
							localCB.SetCurrentIndex(idx(localNames(), m.ID))
						})
					}()
				}},
				Label{Text: "Использовать:"},
				ComboBox{AssignTo: &localCB, Model: localNames(), CurrentIndex: idx(localNames(), c.LocalModel), ColumnSpan: 2},
				Label{AssignTo: &localNote, Text: "Движок: " + map[bool]string{true: "установлен", false: "не установлен"}[a.Local.EngineInstalled()], ColumnSpan: 3},
			}},

			GroupBox{Title: "GigaBrain / Ollama / LM Studio", Layout: Grid{Columns: 4, Spacing: 6}, Children: []Widget{
				Label{Text: "Адрес:"}, LineEdit{AssignTo: &pcBase, Text: c.PcBase, CueBanner: "http://127.0.0.1:8091"},
				Label{Text: "Ключ:"}, LineEdit{AssignTo: &pcKey, Text: c.PcKey, PasswordMode: true},
				Label{Text: "Модель:"}, LineEdit{AssignTo: &pcModel, Text: c.PcModel},
				PushButton{Text: "Проверить", ColumnSpan: 2, OnClicked: func() {
					if strings.TrimSpace(pcBase.Text()) == "" {
						pcBase.SetText("http://127.0.0.1:8091")
					}
					check(pcBase.Text(), pcKey.Text(), pcModel, note)
				}},
			}},

			GroupBox{Title: "Сервер", Layout: Grid{Columns: 4, Spacing: 6}, Children: []Widget{
				Label{Text: "Адрес:"}, LineEdit{AssignTo: &srvBase, Text: c.ServerBase, CueBanner: "https://vmindlab.ru/pisar/brain"},
				Label{Text: "Ключ:"}, LineEdit{AssignTo: &srvKey, Text: c.ServerKey, PasswordMode: true},
				Label{Text: "Модель:"}, LineEdit{AssignTo: &srvModel, Text: c.ServerModel},
				PushButton{Text: "Проверить", ColumnSpan: 2, OnClicked: func() { check(srvBase.Text(), srvKey.Text(), srvModel, note) }},
			}},

			GroupBox{Title: "Облачный сервис (свой бесплатный ключ; текст уходит в сервис)", Layout: Grid{Columns: 4, Spacing: 6}, Children: []Widget{
				Label{Text: "Сервис:"}, ComboBox{AssignTo: &cloudCB, Model: cloudNames, CurrentIndex: idx(cloudIDs(), c.CloudService), ColumnSpan: 3, OnCurrentIndexChanged: func() { fillCloud() }},
				Label{Text: "Адрес:"}, LineEdit{AssignTo: &cloudBase, ColumnSpan: 3},
				Label{Text: "Ключ API:"}, LineEdit{AssignTo: &cloudKey, PasswordMode: true},
				Label{Text: "Модель:"}, LineEdit{AssignTo: &cloudModel},
				Label{AssignTo: &cloudNote, Text: "", ColumnSpan: 3, MinSize: Size{Height: 30}},
				PushButton{Text: "Проверить", OnClicked: func() {
					if s := brain.CloudByID(cloudIDs()[cloudCB.CurrentIndex()]); s != nil && !s.ListsModels {
						note.SetText("Этот сервис не отдаёт список моделей — имя модели впишите вручную")
						return
					}
					check(cloudBase.Text(), cloudKey.Text(), cloudModel, note)
				}},
				Label{Text: "Набор ключей giga-keys.json (со страницы keys.html): вставьте текст или выберите файл", ColumnSpan: 4},
				TextEdit{AssignTo: &keysBox, ColumnSpan: 3, MinSize: Size{Height: 40}, MaxSize: Size{Height: 60}},
				Composite{Layout: VBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
					PushButton{Text: "Импорт", OnClicked: func() {
						msg, err := a.ImportKeys(keysBox.Text())
						if err != nil {
							note.SetText("Не разобрал: " + err.Error())
							return
						}
						keysStored, _, _ = brain.ParseKeys(a.Cfg.CloudKeys)
						c = a.Cfg
						mode.SetCurrentIndex(idx(brainModes, "cloud"))
						cloudCB.SetCurrentIndex(idx(cloudIDs(), c.CloudService))
						fillCloud()
						keysBox.SetText("")
						note.SetText(msg)
					}},
					PushButton{Text: "Файл…", OnClicked: func() {
						fd := new(walk.FileDialog)
						fd.Filter = "JSON (*.json)|*.json|Все файлы|*.*"
						if ok, _ := fd.ShowOpen(dlg); ok {
							if b, err := os.ReadFile(fd.FilePath); err == nil {
								keysBox.SetText(string(b))
							}
						}
					}},
				}},
			}},
			Label{AssignTo: &note, Text: "", MinSize: Size{Height: 34}},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				HSpacer{},
				PushButton{Text: "Сохранить", OnClicked: func() { dlg.Accept() }},
				PushButton{Text: "Отмена", OnClicked: func() { dlg.Cancel() }},
			}},
		},
	}.Create(g.mw)
	fillCloud()
	if dlg.Run() != walk.DlgCmdOK {
		return
	}
	a.Cfg.BrainMode = brainModes[mode.CurrentIndex()]
	a.Cfg.Backend = []string{"cpu", "vulkan", "cuda"}[backendCB.CurrentIndex()]
	if i := localCB.CurrentIndex(); i >= 0 && i < len(localNames()) {
		a.Cfg.LocalModel = localNames()[i]
	}
	a.Cfg.PcBase, a.Cfg.PcKey, a.Cfg.PcModel = strings.TrimSpace(pcBase.Text()), strings.TrimSpace(pcKey.Text()), strings.TrimSpace(pcModel.Text())
	a.Cfg.ServerBase, a.Cfg.ServerKey, a.Cfg.ServerModel = strings.TrimSpace(srvBase.Text()), strings.TrimSpace(srvKey.Text()), strings.TrimSpace(srvModel.Text())
	a.Cfg.CloudService = cloudIDs()[cloudCB.CurrentIndex()]
	a.Cfg.CloudBase, a.Cfg.CloudKey, a.Cfg.CloudModel = strings.TrimSpace(cloudBase.Text()), strings.TrimSpace(cloudKey.Text()), strings.TrimSpace(cloudModel.Text())
	if a.Cfg.BrainMode != "local" {
		a.Local.Stop()
	}
	a.Save()
	g.refresh()
	g.setStatus("Настройки мозга сохранены", core.OK)
}

func cloudIDs() []string {
	var out []string
	for _, s := range brain.CloudServices {
		out = append(out, s.ID)
	}
	return out
}

// Диктовка: пакет, потоки, горячая клавиша, автозапуск, трей.
func (g *gui) settingsGeneral() {
	a := g.app
	c := a.Cfg
	var dlg *walk.Dialog
	var thr *walk.NumberEdit
	var live, tidy, tray, auto *walk.CheckBox
	var modCtrl, modAlt, modShift, modWin *walk.CheckBox
	var keyCB *walk.ComboBox
	var modelLbl *walk.Label
	keys := []string{"Space", "F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12", "Insert", "Home", "ScrollLock", "Pause"}
	vks := []int{0x20, 0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7A, 0x7B, 0x2D, 0x24, 0x91, 0x13}
	kIdx := 0
	for i, v := range vks {
		if v == c.HotkeyVk {
			kIdx = i
		}
	}
	modelState := func() string {
		if a.ModelReady() {
			return "Пакет распознавания GigaAM v3 на месте (" + a.Dir + ")"
		}
		return "Пакет распознавания ещё не скачан"
	}
	exe, _ := os.Executable()
	Dialog{
		AssignTo: &dlg, Title: "Диктовка", MinSize: Size{Width: 560, Height: 420},
		Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}, Spacing: 8},
		Font:   Font{Family: "Segoe UI", PointSize: 9},
		Children: []Widget{
			GroupBox{Title: "Распознавание", Layout: VBox{Spacing: 4}, Children: []Widget{
				Label{AssignTo: &modelLbl, Text: modelState()},
				Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
					PushButton{Text: "Скачать / докачать пакет", OnClicked: func() { go g.install() }},
					PushButton{Text: "Удалить пакет", OnClicked: func() {
						a.UnloadRecognizer()
						os.RemoveAll(a.ModelDir())
						modelLbl.SetText(modelState())
					}},
					Label{Text: "Потоков:"}, NumberEdit{AssignTo: &thr, Value: float64(c.AsrThreads), MinValue: 1, MaxValue: 32, Decimals: 0},
				}},
				CheckBox{AssignTo: &live, Text: "Вставлять фразы по ходу речи (после «Стоп» ждать почти не нужно)", Checked: c.LiveInsert},
				CheckBox{AssignTo: &tidy, Text: "Причёсывать каждую диктовку мозгом (первая команда из списка)", Checked: c.AutoTidy},
			}},
			GroupBox{Title: "Диктовка в любое окно Windows", Layout: VBox{Spacing: 4}, Children: []Widget{
				Label{Text: "Нажали сочетание в любой программе — запись в неё; ещё раз — стоп, текст встаёт по курсору. Выделили текст и нажали — команда голосом над выделенным."},
				Composite{Layout: HBox{MarginsZero: true, Spacing: 8}, Children: []Widget{
					CheckBox{AssignTo: &modCtrl, Text: "Ctrl", Checked: c.HotkeyMods&gwin.ModControl != 0},
					CheckBox{AssignTo: &modAlt, Text: "Alt", Checked: c.HotkeyMods&gwin.ModAlt != 0},
					CheckBox{AssignTo: &modShift, Text: "Shift", Checked: c.HotkeyMods&gwin.ModShift != 0},
					CheckBox{AssignTo: &modWin, Text: "Win", Checked: c.HotkeyMods&gwin.ModWin != 0},
					Label{Text: "+"}, ComboBox{AssignTo: &keyCB, Model: keys, CurrentIndex: kIdx},
				}},
			}},
			GroupBox{Title: "Запуск", Layout: VBox{Spacing: 4}, Children: []Widget{
				CheckBox{AssignTo: &auto, Text: "Запускать при входе в Windows (свёрнутым в область уведомлений)", Checked: gwin.AutostartEnabled("GigaPisar")},
				CheckBox{AssignTo: &tray, Text: "Закрытие окна прячет Писаря в область уведомлений (горячая клавиша работает)", Checked: c.Tray},
			}},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				HSpacer{},
				PushButton{Text: "Сохранить", OnClicked: func() { dlg.Accept() }},
				PushButton{Text: "Отмена", OnClicked: func() { dlg.Cancel() }},
			}},
		},
	}.Create(g.mw)
	if dlg.Run() != walk.DlgCmdOK {
		return
	}
	if n := int(thr.Value()); n != a.Cfg.AsrThreads {
		a.Cfg.AsrThreads = n
		a.UnloadRecognizer()
		go a.EnsureRecognizer()
	}
	a.Cfg.LiveInsert, a.Cfg.AutoTidy, a.Cfg.Tray = live.Checked(), tidy.Checked(), tray.Checked()
	mods := 0
	if modCtrl.Checked() {
		mods |= gwin.ModControl
	}
	if modAlt.Checked() {
		mods |= gwin.ModAlt
	}
	if modShift.Checked() {
		mods |= gwin.ModShift
	}
	if modWin.Checked() {
		mods |= gwin.ModWin
	}
	if mods == 0 {
		mods = gwin.ModControl | gwin.ModAlt
	}
	a.Cfg.HotkeyMods, a.Cfg.HotkeyVk = mods, vks[keyCB.CurrentIndex()]
	if err := gwin.SetAutostart("GigaPisar", fmt.Sprintf(`"%s" --tray`, exe), auto.Checked()); err != nil {
		g.setStatus("Автозапуск: "+err.Error(), core.Warn)
	}
	a.Save()
	g.registerHotkey()
	g.setStatus("Настройки сохранены. Горячая клавиша: "+hotkeyText(a.Cfg.HotkeyMods, a.Cfg.HotkeyVk), core.OK)
}

// Команды на кнопках: название + команда, до 10.
func (g *gui) settingsChips() {
	a := g.app
	var dlg *walk.Dialog
	type row struct{ t, c *walk.LineEdit }
	var rows []row
	var box *walk.Composite
	addRow := func(ch brain.Chip) {
		var r row
		Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
			LineEdit{AssignTo: &r.t, Text: ch.Title, MaxSize: Size{Width: 170}, CueBanner: "Название кнопки"},
			LineEdit{AssignTo: &r.c, Text: ch.Command, CueBanner: "Команда нейронке"},
		}}.Create(NewBuilder(box))
		rows = append(rows, r)
	}
	Dialog{
		AssignTo: &dlg, Title: "Команды на кнопках", MinSize: Size{Width: 700, Height: 460},
		Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}, Spacing: 6},
		Font:   Font{Family: "Segoe UI", PointSize: 9},
		Children: []Widget{
			Label{Text: "Кнопка выполняется над выделенным или над всем текстом. Пустые строки не сохраняются. Не больше 10."},
			ScrollView{Layout: VBox{MarginsZero: true, Spacing: 4}, Children: []Widget{Composite{AssignTo: &box, Layout: VBox{MarginsZero: true, Spacing: 4}}}},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
				PushButton{Text: "Добавить", OnClicked: func() {
					if len(rows) < 10 {
						addRow(brain.Chip{})
					}
				}},
				PushButton{Text: "По умолчанию", OnClicked: func() {
					for _, r := range rows {
						r.t.SetText("")
						r.c.SetText("")
					}
					for i, ch := range brain.DefaultChips {
						if i < len(rows) {
							rows[i].t.SetText(ch.Title)
							rows[i].c.SetText(ch.Command)
						} else {
							addRow(ch)
						}
					}
				}},
				HSpacer{},
				PushButton{Text: "Сохранить", OnClicked: func() { dlg.Accept() }},
				PushButton{Text: "Отмена", OnClicked: func() { dlg.Cancel() }},
			}},
		},
	}.Create(g.mw)
	for _, ch := range a.Cfg.Chips {
		addRow(ch)
	}
	if dlg.Run() != walk.DlgCmdOK {
		return
	}
	var chips []brain.Chip
	for _, r := range rows {
		t, c := strings.TrimSpace(r.t.Text()), strings.TrimSpace(r.c.Text())
		if t != "" && c != "" && len(chips) < 10 {
			chips = append(chips, brain.Chip{Title: t, Command: c})
		}
	}
	if len(chips) == 0 {
		chips = brain.DefaultChips
	}
	a.Cfg.Chips = chips
	a.Save()
	g.renderChips()
}

// Промпты: для надиктованного, для выделенного, для общения.
func (g *gui) settingsPrompts() {
	a := g.app
	var dlg *walk.Dialog
	var d, s, ch *walk.TextEdit
	Dialog{
		AssignTo: &dlg, Title: "Промпты нейронке", MinSize: Size{Width: 640, Height: 520},
		Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}, Spacing: 6},
		Font:   Font{Family: "Segoe UI", PointSize: 9},
		Children: []Widget{
			Label{Text: "Для надиктованного:"}, TextEdit{AssignTo: &d, Text: a.Cfg.Prompt(false), VScroll: true},
			Label{Text: "Для выделенного текста:"}, TextEdit{AssignTo: &s, Text: a.Cfg.Prompt(true), VScroll: true},
			Label{Text: "Для режима «Общение»:"}, TextEdit{AssignTo: &ch, Text: a.Cfg.ChatPromptText(), VScroll: true, MaxSize: Size{Height: 70}},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
				PushButton{Text: "Вернуть образец", OnClicked: func() {
					d.SetText(brain.DictationPrompt)
					s.SetText(brain.SelectionPrompt)
					ch.SetText(brain.ChatPrompt)
				}},
				HSpacer{},
				PushButton{Text: "Сохранить", OnClicked: func() { dlg.Accept() }},
				PushButton{Text: "Отмена", OnClicked: func() { dlg.Cancel() }},
			}},
		},
	}.Create(g.mw)
	if dlg.Run() != walk.DlgCmdOK {
		return
	}
	a.Cfg.PromptDictation, a.Cfg.PromptSelection, a.Cfg.PromptChat = strings.TrimSpace(d.Text()), strings.TrimSpace(s.Text()), strings.TrimSpace(ch.Text())
	if a.Cfg.PromptDictation == brain.DictationPrompt {
		a.Cfg.PromptDictation = ""
	}
	if a.Cfg.PromptSelection == brain.SelectionPrompt {
		a.Cfg.PromptSelection = ""
	}
	if a.Cfg.PromptChat == brain.ChatPrompt {
		a.Cfg.PromptChat = ""
	}
	a.Save()
}
