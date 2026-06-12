package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is the persistent app configuration.
type Config struct {
	InstancesDir             string `json:"instancesDir"`
	Launcher                 string `json:"launcher"`
	AutoCheckIntervalMinutes int    `json:"autoCheckIntervalMinutes"`
}

// ConfigDir returns the directory where config is stored.
func ConfigDir() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "minimin-sync")
}

// configPath returns the full path to the config file.
func configPath() string {
	return filepath.Join(ConfigDir(), "config.json")
}

// Load reads config from disk or returns an empty one.
func Load() (*Config, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Save writes config to disk atomically.
func (c *Config) Save() error {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0o644)
}
