// Package config manages the application-level configuration file
// (config.yml), distinct from per-profile server settings stored in
// the database.
package config

import (
	"os"

	"lmvpn/internal/paths"

	"gopkg.in/yaml.v3"
)

// AppConfig holds application-wide settings.
type AppConfig struct {
	AutoConnect      bool   `yaml:"auto_connect"`
	MinimizeToTray   bool   `yaml:"minimize_to_tray"`
	CloseToTray      bool   `yaml:"close_to_tray"`
	DefaultProfileID int64  `yaml:"default_profile_id"`
	LogLevel         string `yaml:"log_level"` // debug, info, warn, error
}

// Default returns the default configuration.
func Default() AppConfig {
	return AppConfig{
		AutoConnect:      false,
		MinimizeToTray:   true,
		CloseToTray:      true,
		DefaultProfileID: 0,
		LogLevel:         "info",
	}
}

// Load reads the config file, returning defaults if it does not exist.
func Load() (AppConfig, error) {
	cfg := Default()
	data, err := os.ReadFile(paths.ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Default(), err
	}
	return cfg, nil
}

// Save writes the config file with 0600 permissions.
func Save(cfg AppConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(paths.ConfigPath(), data, 0o600)
}
