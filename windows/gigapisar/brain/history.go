package brain

import (
	"encoding/json"
	"os"
	"time"
)

// История своих промптов: до 100, закреплённые не вытесняются (101-й выталкивает
// самый старый незакреплённый). Хранится в JSON-файле.
const HistoryMax = 100

type HistoryItem struct {
	Text   string `json:"text"`
	Pinned bool   `json:"pinned"`
	Ts     int64  `json:"ts"`
}

type History struct {
	Path  string
	Items []HistoryItem
}

func LoadHistory(path string) *History {
	h := &History{Path: path}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &h.Items)
	}
	return h
}

func (h *History) save() {
	b, _ := json.MarshalIndent(h.Items, "", " ")
	os.WriteFile(h.Path, b, 0o600)
}

func (h *History) Add(text string) {
	if text == "" {
		return
	}
	pinned := false
	kept := h.Items[:0:0]
	for _, it := range h.Items {
		if it.Text == text {
			pinned = it.Pinned
			continue
		}
		kept = append(kept, it)
	}
	h.Items = append([]HistoryItem{{Text: text, Pinned: pinned, Ts: time.Now().Unix()}}, kept...)
	for len(h.Items) > HistoryMax {
		victim := -1
		for i := len(h.Items) - 1; i >= 0; i-- {
			if !h.Items[i].Pinned {
				victim = i
				break
			}
		}
		if victim < 0 {
			break
		}
		h.Items = append(h.Items[:victim], h.Items[victim+1:]...)
	}
	h.save()
}

func (h *History) TogglePin(text string) {
	for i := range h.Items {
		if h.Items[i].Text == text {
			h.Items[i].Pinned = !h.Items[i].Pinned
		}
	}
	h.save()
}

func (h *History) Remove(text string) {
	kept := h.Items[:0]
	for _, it := range h.Items {
		if it.Text != text {
			kept = append(kept, it)
		}
	}
	h.Items = kept
	h.save()
}
