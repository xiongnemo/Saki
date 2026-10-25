package ui

import (
	"context"
	"testing"

	"github.com/xiongnemo/saki/internal/i18n"
	"github.com/xiongnemo/saki/internal/models"
)

func TestSongStatusMarkerDistinguishesCacheAndDownload(t *testing.T) {
	previous := i18n.Current()
	defer i18n.SetCurrent(string(previous))
	i18n.SetCurrent(models.LanguageEnglish)
	app := &App{ctx: context.Background(), localStatus: func(_ context.Context, id string) (bool, bool) {
		switch id {
		case "cached":
			return true, false
		case "downloaded":
			return false, true
		case "both":
			return true, true
		default:
			return false, false
		}
	}}
	if got := app.songStatusMarker(models.Song{ID: "cached"}); got != " [cyan][Cached[][-]" {
		t.Fatalf("cached marker = %q", got)
	}
	if got := app.songStatusMarker(models.Song{ID: "downloaded"}); got != " [yellow][Downloaded[][-]" {
		t.Fatalf("download marker = %q", got)
	}
	if got := app.songStatusMarker(models.Song{ID: "both"}); got != " [cyan][Cached[][-] [yellow][Downloaded[][-]" {
		t.Fatalf("combined marker = %q", got)
	}
	if got := app.songStatusMarker(models.Song{ID: "none"}); got != "" {
		t.Fatalf("empty marker = %q", got)
	}
}

func TestSongStatusMarkerUsesChineseLabels(t *testing.T) {
	previous := i18n.Current()
	defer i18n.SetCurrent(string(previous))
	i18n.SetCurrent(models.LanguageChinese)
	app := &App{ctx: context.Background(), localStatus: func(context.Context, string) (bool, bool) {
		return true, true
	}}
	if got := app.songStatusMarker(models.Song{ID: "both"}); got != " [cyan][已缓存][-] [yellow][已下载][-]" {
		t.Fatalf("Chinese combined marker = %q", got)
	}
}
