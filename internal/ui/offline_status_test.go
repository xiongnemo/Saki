package ui

import (
	"context"
	"testing"

	"github.com/xiongnemo/saki/internal/models"
)

func TestSongStatusMarkerDistinguishesCacheAndDownload(t *testing.T) {
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
	if got := app.songStatusMarker(models.Song{ID: "cached"}); got != " [cyan][已缓存][-]" {
		t.Fatalf("cached marker = %q", got)
	}
	if got := app.songStatusMarker(models.Song{ID: "downloaded"}); got != " [green][已下载][-]" {
		t.Fatalf("download marker = %q", got)
	}
	if got := app.songStatusMarker(models.Song{ID: "both"}); got != " [cyan][已缓存][-] [green][已下载][-]" {
		t.Fatalf("combined marker = %q", got)
	}
	if got := app.songStatusMarker(models.Song{ID: "none"}); got != "" {
		t.Fatalf("empty marker = %q", got)
	}
}
