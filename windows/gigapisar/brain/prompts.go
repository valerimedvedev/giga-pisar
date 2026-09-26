// Мозг Писаря: промпты, команды и обращение «Писарь, …» — один в один с
// плагином WordPress, страницей и приложением для Android.
package brain

import (
	"regexp"
	"strings"
)

const SelectionPrompt = "Ты редактируешь текст, который пользователь выделил в своём документе, " +
	"и выполняешь над ним команду пользователя. Сохраняй смысл и разбиение " +
	"на абзацы, ничего не добавляй от себя и не комментируй. Тон и стиль " +
	"сохраняй, если только команда не велит их изменить: команда важнее. " +
	"Команда дана в конце этой инструкции, в сам текст не входит, и " +
	"упоминать её в ответе нельзя. Верни ТОЛЬКО готовый текст, без кавычек " +
	"вокруг него."

const DictationPrompt = "Ты обрабатываешь надиктованный голосом текст перед вставкой. Правила: " +
	"убери слова-паразиты и оговорки (э, ну, типа, вот, как бы), убери повторы " +
	"и самоисправления, расставь знаки препинания, исправь очевидные ошибки " +
	"распознавания. Сохраняй смысл и лексику, ничего не добавляй от себя и " +
	"не комментируй. Живой тон автора сохраняй, если только команда не велит " +
	"его изменить: команда важнее тона. Выполни команду пользователя: она " +
	"дана в конце этой инструкции, в сам текст не входит, и упоминать её " +
	"в ответе нельзя. Верни ТОЛЬКО готовый текст, без кавычек вокруг него."

const ChatPrompt = "Ты — помощник в программе «Гига Писарь». Отвечай по-русски, кратко и по делу, без лишних вступлений. Если просят написать текст — пиши сразу готовый текст."

type Chip struct {
	Title   string `json:"title"`
	Command string `json:"command"`
}

var DefaultChips = []Chip{
	{"Причесать", "причеши текст: убери слова-паразиты и повторы, поправь пунктуацию и очевидные ошибки; смысл, порядок мыслей и лексику не меняй"},
	{"Исправить ошибки", "исправь только орфографию, пунктуацию и ошибки распознавания; слова, порядок и стиль не меняй"},
	{"Сократить", "сократи, сохранив суть"},
	{"Собрать мысль", "собери мысль: из сбивчивой речи сделай связный текст в том же порядке мыслей, ничего не добавляя от себя"},
	{"Сгладить", "сгладь тон: сделай мягче и вежливее, убери резкость; смысл не меняй"},
	{"Деловой стиль", "перепиши в деловом стиле: нейтрально, чётко, без разговорных слов; смысл не меняй"},
	{"Перевести на английский", "переведи на английский"},
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

const address = `(?:гига[\s,—-]+)?п[еиэ]сар[ьяюе]?[\s,.:!—-]*`

var (
	addressRe = regexp.MustCompile(`(?i)` + address)
	leadingRe = regexp.MustCompile(`(?i)^\s*` + address)
	letterRe  = regexp.MustCompile(`(?i)^[а-яёa-z]$`)
	cyrLetter = regexp.MustCompile(`(?i)^[а-яё]`)
)

// ParseCommand: «…текст, Писарь, команда». Берётся последнее обращение, стоящее
// отдельным словом («описарь» не считается); после обращения не должно идти
// буквы (иначе это часть другого слова).
func ParseCommand(text string) (body, command string, ok bool) {
	rs := []rune(text)
	var last []int
	for _, m := range addressRe.FindAllStringIndex(text, -1) {
		if m[0] > 0 && letterRe.MatchString(text[lastRuneStart(text, m[0]):m[0]]) {
			continue
		}
		// обращение должно кончаться границей слова: следующий символ — не буква
		if m[1] < len(text) && cyrLetter.MatchString(text[m[1]:]) && !strings.ContainsAny(text[m[0]:m[1]], " ,.:!—-") {
			continue
		}
		last = m
	}
	if last == nil {
		return "", "", false
	}
	_ = rs
	command = strings.TrimSpace(text[last[1]:])
	body = strings.TrimSpace(text[:last[0]])
	for body != "" && strings.ContainsRune(",—-–", []rune(body)[len([]rune(body))-1]) {
		r := []rune(body)
		body = strings.TrimRight(string(r[:len(r)-1]), " \t")
	}
	if command == "" || body == "" {
		return "", "", false
	}
	return body, command, true
}

func lastRuneStart(s string, i int) int {
	j := i - 1
	for j > 0 && (s[j]&0xC0) == 0x80 {
		j--
	}
	if j < 0 {
		j = 0
	}
	return j
}

// StripAddress убирает необязательное «Писарь,» в начале команды.
func StripAddress(text string) string { return strings.TrimSpace(leadingRe.ReplaceAllString(text, "")) }

func ActionLabel(command string) string {
	c := strings.ToLower(command)
	switch {
	case strings.HasPrefix(c, "причеши"):
		return "Причёсываю…"
	case strings.Contains(c, "перевед") || strings.Contains(c, "англ"):
		return "Перевожу…"
	case strings.Contains(c, "сократ") || strings.Contains(c, "короче"):
		return "Сокращаю…"
	case strings.Contains(c, "мысль"):
		return "Собираю мысль…"
	case strings.Contains(c, "сглад") || strings.Contains(c, "мягче"):
		return "Сглаживаю…"
	case strings.Contains(c, "делов") || strings.Contains(c, "официальн"):
		return "Делаю деловым…"
	case strings.Contains(c, "исправ") || strings.Contains(c, "ошибк"):
		return "Исправляю…"
	}
	return "Причёсываю…"
}

func StripThinking(s string) string {
	if i := strings.Index(s, "</think>"); i >= 0 {
		return s[i+len("</think>"):]
	}
	return s
}

// MessagesFor: команда — в системную инструкцию, текст — отдельным сообщением.
func MessagesFor(body, command string, selection bool, dictationPrompt, selectionPrompt string) []Message {
	base := dictationPrompt
	if selection {
		base = selectionPrompt
	}
	if strings.TrimSpace(base) == "" {
		if selection {
			base = SelectionPrompt
		} else {
			base = DictationPrompt
		}
	}
	return []Message{{"system", base + "\n\nКоманда пользователя к тексту: " + command + "."}, {"user", body}}
}
