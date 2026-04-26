package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMigratesLegacyAccountURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".saki")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{
		"username": "nemo",
		"password": "secret",
		"url": "https://music.example.com/",
		"usePlaintext": true
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

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
