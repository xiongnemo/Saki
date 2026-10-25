package i18n

import "testing"

func TestResolveAndTranslate(t *testing.T) {
	if got := Resolve("zh"); got != Chinese {
		t.Fatalf("Resolve zh = %q", got)
	}
	if got := Resolve("en"); got != English {
		t.Fatalf("Resolve en = %q", got)
	}
	if got := Translate(Chinese, "Cached"); got != "已缓存" {
		t.Fatalf("Chinese Cached = %q", got)
	}
	if got := Translate(English, "Cached"); got != "Cached" {
		t.Fatalf("English Cached = %q", got)
	}
}
