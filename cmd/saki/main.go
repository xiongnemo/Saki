package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/xiongnemo/saki/internal/audio"
	"github.com/xiongnemo/saki/internal/config"
	"github.com/xiongnemo/saki/internal/mediaintegration"
	"github.com/xiongnemo/saki/internal/models"
	"github.com/xiongnemo/saki/internal/player"
	"github.com/xiongnemo/saki/internal/selfinstall"
	"github.com/xiongnemo/saki/internal/streamproxy"
	"github.com/xiongnemo/saki/internal/subsonic"
	"github.com/xiongnemo/saki/internal/ui"
	"github.com/xiongnemo/saki/internal/version"
)

func main() {
	os.Exit(runCLIProcess(os.Args[1:]))
}

func runCLIProcess(args []string) int {
	runtime := commandRuntime{
		stdin:   os.Stdin,
		stdout:  os.Stdout,
		stderr:  os.Stderr,
		version: version.String(),
		runTUI:  runTUI,
		install: runInstall,
	}
	return runCLI(args, runtime)
}

func runInstall(options installOptions) error {
	installer, err := selfinstall.New(selfinstall.Options{
		Directory: options.directory,
		AssumeYes: options.yes,
	}, selfinstall.SystemRuntime())
	if err != nil {
		return err
	}
	return installer.Install()
}

func runTUI() error {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfgStore := config.NewStore("Saki", "config.json")
	cfg, err := cfgStore.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	client := subsonic.NewClient(nil)
	cfg = client.Configure(cfg)
	client.StartHealthChecks(ctx)

	proxy := streamproxy.New(client, cfg.Settings)
	if err := proxy.Start(ctx); err != nil {
		return fmt.Errorf("start stream proxy: %w", err)
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
	musicPlayer.SetCacheReadyResolver(func(id string) bool {
		_, ok := proxy.CachedPath(id)
		return ok
	})
	musicPlayer.SetCacheRoot(cfg.Settings.CacheDir)
	defer musicPlayer.Close()

	if err := media.Init(); err != nil {
		log.Printf("media integration disabled: %v", err)
	}

	applyConfig := func(next models.Config) models.Config {
		previousBackend := normalizeAudioBackend(cfg.Settings.AudioBackend)
		next = config.WithDefaults(next)
		next = client.Configure(next)
		if previousBackend != normalizeAudioBackend(next.Settings.AudioBackend) {
			musicPlayer.Stop()
		}
		audioBackend.SetSettings(next.Settings)
		_ = proxy.UpdateSettings(next.Settings)
		musicPlayer.SetCacheRoot(next.Settings.CacheDir)
		cfg = next
		return next
	}

	app := ui.New(ctx, cancel, cfgStore, cfg, client, musicPlayer, media, applyConfig)
	if err := app.Run(); err != nil {
		return fmt.Errorf("run TUI: %w", err)
	}
	return nil
}

func normalizeAudioBackend(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "miniaudio", "mpv":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "auto"
	}
}
