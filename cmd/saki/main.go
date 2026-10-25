package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/xiongnemo/saki/internal/audio"
	"github.com/xiongnemo/saki/internal/config"
	"github.com/xiongnemo/saki/internal/mediaintegration"
	"github.com/xiongnemo/saki/internal/models"
	"github.com/xiongnemo/saki/internal/offline"
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
	var cfgMu sync.RWMutex
	var healthMu sync.Mutex
	healthStarted := false
	var onlineStateMu sync.Mutex
	onlineEstablished := false
	startHealthChecks := func() {
		healthMu.Lock()
		defer healthMu.Unlock()
		if healthStarted {
			return
		}
		client.StartHealthChecks(ctx)
		healthStarted = true
	}
	activeEndpoint := client.ActiveEndpoint()
	catalogStore, err := offline.Open(cfg.Settings.CacheDir, cfg.Account.Username, activeEndpoint.URL, cfg.Account.LibraryFingerprint)
	if err != nil {
		return fmt.Errorf("open offline catalog: %w", err)
	}
	defer catalogStore.Close()
	router := offline.NewRouter(client, catalogStore, cfg.Settings.ConnectionMode)

	proxy := streamproxy.New(client, cfg.Settings)
	if err := proxy.Start(ctx); err != nil {
		return fmt.Errorf("start stream proxy: %w", err)
	}
	defer proxy.Close(context.Background())

	audioBackend := audio.NewBackend(cfg.Settings)
	media := mediaintegration.New()

	musicPlayer := player.New(client, audioBackend, media)
	musicPlayer.SetCatalog(router)
	downloads := offline.NewDownloadManager(catalogStore, catalogStore.DownloadRoot(), cfg.Settings.OfflineStorageMaxBytes, cfg.Settings.DownloadConcurrency, func(ctx context.Context, id, variant, rangeHeader string) (*http.Response, string, error) {
		if variant == models.DownloadVariantRaw {
			resp, endpoint, err := client.OpenDownloadWithEndpoint(ctx, id, rangeHeader)
			return resp, endpoint.URL, err
		}
		resp, endpoint, err := client.OpenStreamWithEndpoint(ctx, id, rangeHeader)
		return resp, endpoint.URL, err
	})
	musicPlayer.SetStreamSourceResolver(func(_ context.Context, id string) (string, error) {
		cfgMu.RLock()
		defaultVariant := cfg.Settings.DefaultDownloadVariant
		cfgMu.RUnlock()
		for _, variant := range []string{defaultVariant, models.DownloadVariantStream, models.DownloadVariantRaw} {
			if path, ok := downloads.Path(context.Background(), id, variant); ok {
				return path, nil
			}
		}
		if router.IsOffline() {
			return "", fmt.Errorf("track %q is not downloaded", id)
		}
		if path, ok := proxy.CachedPath(id); ok {
			return path, nil
		}
		return proxy.TrackURL(id), nil
	})
	musicPlayer.SetCacheReadyResolver(func(id string) bool {
		if _, ok := proxy.CachedPath(id); ok {
			return true
		}
		return downloads.HasAny(context.Background(), id)
	})
	musicPlayer.SetCacheRoot(cfg.Settings.CacheDir)
	defer musicPlayer.Close()

	if err := media.Init(); err != nil {
		log.Printf("media integration disabled: %v", err)
	}
	var syncMu sync.Mutex

	applyConfig := func(next models.Config) models.Config {
		cfgMu.RLock()
		previousBackend := normalizeAudioBackend(cfg.Settings.AudioBackend)
		cfgMu.RUnlock()
		next = config.WithDefaults(next)
		next = client.Configure(next)
		endpoint := client.ActiveEndpoint()
		syncMu.Lock()
		err := catalogStore.Rebind(next.Settings.CacheDir, next.Account.Username, endpoint.URL, next.Account.LibraryFingerprint)
		syncMu.Unlock()
		if err != nil {
			log.Printf("offline catalog scope update failed: %v", err)
		}
		router.SetStore(catalogStore)
		downloads.SetStore(catalogStore, catalogStore.DownloadRoot())
		if previousBackend != normalizeAudioBackend(next.Settings.AudioBackend) {
			musicPlayer.Stop()
		}
		audioBackend.SetSettings(next.Settings)
		_ = proxy.UpdateSettings(next.Settings)
		downloads.SetMaxBytes(next.Settings.OfflineStorageMaxBytes)
		router.SetConfiguredMode(next.Settings.ConnectionMode)
		onlineStateMu.Lock()
		ready := onlineEstablished
		onlineStateMu.Unlock()
		if ready && next.Settings.ConnectionMode != models.ConnectionModeOffline {
			startHealthChecks()
		}
		musicPlayer.SetCacheRoot(next.Settings.CacheDir)
		cfgMu.Lock()
		cfg = next
		cfgMu.Unlock()
		return next
	}

	app := ui.New(ctx, cancel, cfgStore, cfg, client, musicPlayer, media, applyConfig)
	app.SetOfflineRouter(router)
	app.SetLocalStatus(func(statusCtx context.Context, id string) (bool, bool) {
		_, cached := proxy.CachedPath(id)
		downloaded := downloads.HasAny(statusCtx, id)
		return cached, downloaded
	})
	downloads.SetOnComplete(func(string, string) { app.NotifyDownloadComplete() })
	proxy.SetOnCacheComplete(func(string) { app.NotifyCacheComplete() })
	syncCatalog := func(syncCtx context.Context) error {
		syncMu.Lock()
		defer syncMu.Unlock()
		if router.IsOffline() {
			return fmt.Errorf("switch to online before syncing")
		}
		return offline.Sync(syncCtx, client, catalogStore, nil)
	}
	var syncLoopMu sync.Mutex
	syncLoopStarted := false
	startSyncLoop := func() {
		syncLoopMu.Lock()
		defer syncLoopMu.Unlock()
		if syncLoopStarted {
			return
		}
		syncLoopStarted = true
		go syncLoop(ctx, func() models.Settings {
			cfgMu.RLock()
			defer cfgMu.RUnlock()
			return cfg.Settings
		}, router, syncCatalog, catalogStore)
	}
	app.SetOnlineReady(func() {
		onlineStateMu.Lock()
		onlineEstablished = true
		onlineStateMu.Unlock()
		startHealthChecks()
		startSyncLoop()
	})
	app.SetOfflineActions(
		syncCatalog,
		func(retryCtx context.Context) error {
			if err := router.RetryOnline(retryCtx); err != nil {
				return err
			}
			onlineStateMu.Lock()
			onlineEstablished = true
			onlineStateMu.Unlock()
			startHealthChecks()
			startSyncLoop()
			return nil
		},
		func(downloadCtx context.Context, id, variant string) error {
			return downloads.Enqueue(downloadCtx, id, variant, true)
		},
		catalogStore.RemoveDownload,
	)
	if err := app.Run(); err != nil {
		return fmt.Errorf("run TUI: %w", err)
	}
	return nil
}

func syncLoop(ctx context.Context, settings func() models.Settings, router *offline.Router, syncCatalog func(context.Context) error, store *offline.Store) {
	initialDelay := time.NewTimer(1500 * time.Millisecond)
	select {
	case <-initialDelay.C:
	case <-ctx.Done():
		initialDelay.Stop()
		return
	}
	if !router.IsOffline() {
		if err := syncCatalog(ctx); err != nil && store != nil && router.HasOfflineData(ctx) {
			router.SetOffline()
		}
	}
	for {
		current := settings()
		interval := current.CatalogSyncIntervalSeconds
		if interval <= 0 {
			interval = 300
		}
		timer := time.NewTimer(time.Duration(interval) * time.Second)
		select {
		case <-timer.C:
			if router.IsOffline() {
				continue
			}
			if err := syncCatalog(ctx); err != nil && store != nil && router.HasOfflineData(ctx) {
				router.SetOffline()
			}
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}
	}
}

func normalizeAudioBackend(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "miniaudio", "mpv":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "auto"
	}
}
