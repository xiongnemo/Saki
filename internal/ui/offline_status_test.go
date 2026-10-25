package ui

import (
	"context"
	"testing"

	"github.com/xiongnemo/saki/internal/i18n"
	"github.com/xiongnemo/saki/internal/models"
)

func TestSongStatusMarkerDistinguishesCacheAndDownload(t *testing.T) {
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
	if got := app.songStatusMarker(models.Song{ID: "cached"}); got != " [cyan][Cached][-]" {
		t.Fatalf("cached marker = %q", got)
	}
	if got := app.songStatusMarker(models.Song{ID: "downloaded"}); got != " [green][Downloaded][-]" {
		t.Fatalf("download marker = %q", got)
	}
	if got := app.songStatusMarker(models.Song{ID: "both"}); got != " [cyan][Cached][-] [green][Downloaded][-]" {
		t.Fatalf("combined marker = %q", got)
	}
	if got := app.songStatusMarker(models.Song{ID: "none"}); got != "" {
		t.Fatalf("empty marker = %q", got)
	}
}
