package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	DatabaseURL   string
	Listen        string
	URL           string
	Token         string
	ProjectionDir string
	ConfigPath    string
}

func Load() (Config, error) {
	home, _ := os.UserHomeDir()
	c := Config{
		Listen:        "127.0.0.1:8741",
		URL:           "http://127.0.0.1:8741",
		ConfigPath:    filepath.Join(home, ".config", "harness-memory", "config.toml"),
		ProjectionDir: filepath.Join(home, ".local", "share", "harness-memory", "projection"),
	}
	if v := os.Getenv("MEMORY_DATABASE_URL"); v != "" {
		c.DatabaseURL = v
	}
	if v := os.Getenv("MEMORY_LISTEN"); v != "" {
		c.Listen = v
	}
	if v := os.Getenv("MEMORY_URL"); v != "" {
		c.URL = v
	}
	if v := os.Getenv("MEMORY_TOKEN"); v != "" {
		c.Token = v
	}
	if v := os.Getenv("MEMORY_PROJECTION_DIR"); v != "" {
		c.ProjectionDir = v
	}
	return c, nil
}
