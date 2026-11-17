package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xiongnemo/saki/internal/models"
	"github.com/xiongnemo/saki/internal/testenv"
)

func writeConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMigratesLegacyAccountURL(t *testing.T) {
	home := testenv.Home(t)
	writeConfig(t, filepath.Join(home, ".saki", "config.json"), `{
		"username": "nemo",
		"password": "secret",
		"url": "https://music.example.com/",
		"usePlaintext": true
	}`)

	cfg, err := NewStore("Saki", "config.json").Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Account.URL != "" {
		t.Fatalf("legacy URL should be cleared after migration, got %q", cfg.Account.URL)
	}
	if len(cfg.Account.Endpoints) != 1 || cfg.Account.Endpoints[0].URL != "https://music.example.com" || !cfg.Account.Endpoints[0].Enabled {
		t.Fatalf("unexpected migrated endpoints: %#v", cfg.Account.Endpoints)
	}
	if cfg.Settings.AudioCacheMaxBytes <= 0 || cfg.Settings.HealthCheckIntervalSeconds <= 0 {
		t.Fatalf("settings defaults were not applied: %#v", cfg.Settings)
	}
}

func TestStorePathPrefersNewLocationAndFallsBackToLegacy(t *testing.T) {
	home := testenv.Home(t)
	store := NewStore("Saki", "config.json")
	legacy := filepath.Join(home, ".saki", "config.json")
	current := filepath.Join(home, ".config", "saki", "config.json")

	path, err := store.Path()
	if err != nil || path != current {
		t.Fatalf("fresh path = %q, %v; want %q", path, err, current)
	}

	writeConfig(t, legacy, `{"account":{"username":"old"}}`)
	if path, _ := store.Path(); path != legacy {
		t.Fatalf("legacy-only path = %q, want %q", path, legacy)
	}
	// Saving keeps using the file that exists rather than forking a copy.
	if err := store.Save(models.Config{Account: models.Account{Username: "old"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(current); !os.IsNotExist(err) {
		t.Fatalf("saving a legacy config created %s: %v", current, err)
	}

	writeConfig(t, current, `{"account":{"username":"new"}}`)
	cfg, err := store.Load()
	if err != nil || cfg.Account.Username != "new" {
		t.Fatalf("with both present loaded %q, %v; want the new file", cfg.Account.Username, err)
	}
}

func TestStorePathHonorsAbsoluteXDGConfigHomeOnly(t *testing.T) {
	home := testenv.Home(t)
	custom := filepath.Join(home, "elsewhere")
	t.Setenv("XDG_CONFIG_HOME", custom)
	if path, _ := NewStore("Saki", "config.json").Path(); path != filepath.Join(custom, "saki", "config.json") {
		t.Fatalf("absolute XDG_CONFIG_HOME path = %q", path)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join("relative", "config"))
	if path, _ := NewStore("Saki", "config.json").Path(); path != filepath.Join(home, ".config", "saki", "config.json") {
		t.Fatalf("relative XDG_CONFIG_HOME path = %q", path)
	}
}

func TestSaveKeepsConfigPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions")
	}
	home := testenv.Home(t)
	if err := NewStore("Saki", "config.json").Save(models.Config{}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".config", "saki")
	for path, want := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, "config.json"): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s mode = %o, want %o", path, got, want)
		}
	}
}

func TestNormalizeSettingsPreservesChineseLanguageValue(t *testing.T) {
	settings := normalizeSettings(models.Settings{Language: "zh-CN"})
	if settings.Language != models.LanguageChinese {
		t.Fatalf("normalized language = %q, want zh-CN", settings.Language)
	}
}
