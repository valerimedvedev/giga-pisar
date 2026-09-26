package brain

import "testing"

func TestParseCommand(t *testing.T) {
	b, c, ok := ParseCommand("сегодня хорошая погода, Писарь, переведи на английский")
	if !ok || b != "сегодня хорошая погода" || c != "переведи на английский" {
		t.Fatalf("%q %q %v", b, c, ok)
	}
	b, c, ok = ParseCommand("я писарь и это описарь моего дела, гига писарь сократи")
	if !ok || b != "я писарь и это описарь моего дела" || c != "сократи" {
		t.Fatalf("last: %q %q %v", b, c, ok)
	}
	for _, s := range []string{"просто текст без обращения", "Писарь, исправь", "текст, Писарь"} {
		if _, _, ok := ParseCommand(s); ok {
			t.Errorf("не должно быть команды: %q", s)
		}
	}
	if StripAddress("Писарь, сократи") != "сократи" {
		t.Error("strip")
	}
	if ActionLabel("переведи на английский") != "Перевожу…" {
		t.Error("label")
	}
}

func TestKeys(t *testing.T) {
	keys, def, err := ParseKeys(`{"format":"giga-pisar-keys/1","default":"groq","services":{"groq":{"key":" gsk "},"cloudflare":{"key":"cf","account":"acc"},"x":{"key":"1"}}}`)
	if err != nil || def != "groq" || keys["groq"].Key != "gsk" || len(keys) != 2 {
		t.Fatalf("%v %q %v", keys, def, err)
	}
	e := keys["cloudflare"]
	if BaseFor(CloudByID("cloudflare"), &e) != "https://api.cloudflare.com/client/v4/accounts/acc/ai/v1" {
		t.Error("base")
	}
	if _, _, err := ParseKeys(`{"services":{}}`); err == nil {
		t.Error("empty")
	}
}

func TestHistory(t *testing.T) {
	h := &History{Path: t.TempDir() + "/h.json"}
	for i := 1; i <= 100; i++ {
		h.Add("промпт " + string(rune('0'+i%10)) + string(rune('a'+i%26)) + string(rune('A'+i/26)))
	}
	h.TogglePin(h.Items[99].Text)
	oldest := h.Items[99].Text
	h.Add("новый")
	if len(h.Items) != 100 || h.Items[0].Text != "новый" || h.Items[len(h.Items)-1].Text != oldest {
		t.Fatalf("eviction: %d %s %s", len(h.Items), h.Items[0].Text, h.Items[len(h.Items)-1].Text)
	}
}
