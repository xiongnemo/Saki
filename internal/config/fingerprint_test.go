package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xiongnemo/saki/internal/models"
)

func TestSaveLoadPreservesAccountLibraryFingerprint(t *testing.T) {
	// Given
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	store := NewStore("Saki", "config.json")
	cfg := models.Config{Account: models.Account{
		Username:           "nemo",
		Password:           "secret",
		LibraryFingerprint: "verified-library",
		Endpoints: []models.Endpoint{{
			Name:    "Primary",
			URL:     "https://music.example",
			Enabled: true,
		}},
	}}

	// When
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Account.LibraryFingerprint != "verified-library" {
		t.Fatalf("library fingerprint = %q", loaded.Account.LibraryFingerprint)
	}
	if _, err := os.Stat(filepath.Join(home, ".saki", "config.json")); err != nil {
		t.Fatalf("config file was not saved: %v", err)
	}
}
