package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xiongnemo/saki/internal/models"
)

type Store struct {
	appName  string
	fileName string
}

func NewStore(appName, fileName string) Store {
	return Store{appName: appName, fileName: fileName}
}

func (s Store) Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".saki", s.fileName), nil
}

func (s Store) Load() (models.Config, error) {
	path, err := s.Path()
	if err != nil {
		return models.Config{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return WithDefaults(models.Config{}), nil
		}
		return models.Config{}, err
	}

	var cfg models.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return models.Config{}, err
	}
	if cfg.Account.Username == "" && cfg.Account.Password == "" && cfg.Account.URL == "" && len(cfg.Account.Endpoints) == 0 {
		var account models.Account
		if err := json.Unmarshal(data, &account); err != nil {
			return models.Config{}, err
		}
		cfg.Account = account
	}
	return WithDefaults(cfg), nil
}

func (s Store) Save(cfg models.Config) error {
	path, err := s.Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(WithDefaults(cfg), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func WithDefaults(cfg models.Config) models.Config {
	cfg.Account = normalizeAccount(cfg.Account)
	cfg.Settings = normalizeSettings(cfg.Settings)
	return cfg
}

func normalizeAccount(account models.Account) models.Account {
	if len(account.Endpoints) == 0 && strings.TrimSpace(account.URL) != "" {
		account.Endpoints = []models.Endpoint{{
			Name:    "Default",
			URL:     strings.TrimRight(strings.TrimSpace(account.URL), "/"),
			Enabled: true,
		}}
	}
	for i := range account.Endpoints {
		account.Endpoints[i].URL = strings.TrimRight(strings.TrimSpace(account.Endpoints[i].URL), "/")
		if account.Endpoints[i].Name == "" {
			account.Endpoints[i].Name = "Endpoint " + strconv.Itoa(i+1)
		}
	}
	hasEnabled := false
	for _, endpoint := range account.Endpoints {
		if endpoint.Enabled {
			hasEnabled = true
			break
		}
	}
	if !hasEnabled {
		for i := range account.Endpoints {
			if account.Endpoints[i].URL != "" {
				account.Endpoints[i].Enabled = true
			}
		}
	}
	account.URL = ""
	return account
}

func normalizeSettings(settings models.Settings) models.Settings {
	switch strings.ToLower(strings.TrimSpace(settings.AudioBackend)) {
	case "", "auto":
		settings.AudioBackend = "auto"
	case "miniaudio", "mpv":
		settings.AudioBackend = strings.ToLower(strings.TrimSpace(settings.AudioBackend))
	default:
		settings.AudioBackend = "auto"
	}
	if settings.AudioCacheMaxBytes <= 0 {
		settings.AudioCacheMaxBytes = 2 * 1024 * 1024 * 1024
	}
	if settings.HealthCheckIntervalSeconds <= 0 {
		settings.HealthCheckIntervalSeconds = 5
	}
	if settings.EndpointTimeoutSeconds <= 0 {
		settings.EndpointTimeoutSeconds = 2
	}
	if settings.EndpointSwitchThreshold <= 0 {
		settings.EndpointSwitchThreshold = 0.30
	}
	return settings
}
