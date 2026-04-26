package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/xiongnemo/saki/internal/audio"
	"github.com/xiongnemo/saki/internal/config"
	"github.com/xiongnemo/saki/internal/mediaintegration"
	"github.com/xiongnemo/saki/internal/models"
	"github.com/xiongnemo/saki/internal/player"
	"github.com/xiongnemo/saki/internal/streamproxy"
	"github.com/xiongnemo/saki/internal/subsonic"
	"github.com/xiongnemo/saki/internal/ui"
	"github.com/xiongnemo/saki/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	shortVersion := flag.Bool("v", false, "print version and exit")
	flag.Parse()
	if *showVersion || *shortVersion {
		fmt.Fprintln(os.Stdout, version.String())
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfgStore := config.NewStore("Saki", "config.json")
	cfg, err := cfgStore.Load()
	if err != nil {
		log.Fatal(err)
	}

	client := subsonic.NewClient(nil)
	cfg = client.Configure(cfg)
	client.StartHealthChecks(ctx)

	proxy := streamproxy.New(client, cfg.Settings)
	if err := proxy.Start(ctx); err != nil {
		log.Fatal(err)
	}
	defer proxy.Close(context.Background())

	audioBackend := audio.NewBackend(cfg.Settings)
	media := mediaintegration.New()

	musicPlayer := player.New(client, audioBackend, media)
	musicPlayer.SetStreamSourceResolver(func(_ context.Context, id string) (string, error) {
		if path, ok := proxy.CachedPath(id); ok {
			return path, nil
		}
		return proxy.TrackURL(id), nil
	})
	musicPlayer.SetCacheRoot(cfg.Settings.CacheDir)
	defer musicPlayer.Close()

	if err := media.Init(); err != nil {
		log.Printf("media integration disabled: %v", err)
	}

	applyConfig := func(next models.Config) models.Config {
		next = config.WithDefaults(next)
		next = client.Configure(next)
		audioBackend.SetSettings(next.Settings)
		_ = proxy.UpdateSettings(next.Settings)
		musicPlayer.SetCacheRoot(next.Settings.CacheDir)
		return next
	}

	app := ui.New(ctx, cancel, cfgStore, cfg, client, musicPlayer, media, applyConfig)
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
