package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const defaultConfigTmpl = `# Shelf configuration file.

# Modal endpoint URL for HTML-to-Markdown conversion.
endpoint = ""

# Directory where article data is stored.
data_dir = %q
`

type Config struct {
	Endpoint string `toml:"endpoint"`
	DataDir  string `toml:"data_dir"`
}

// Dir returns the shelf configuration directory (~/.shelf).
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, ".shelf"), nil
}

// Path returns the path to the shelf config file.
func Path() string {
	dir, _ := Dir()
	return filepath.Join(dir, "shelf.toml")
}

// Load reads the config from ~/.shelf/shelf.toml, creating a default
// config file if one doesn't exist.
func Load() (Config, error) {
	dir, err := Dir()
	if err != nil {
		return Config{}, err
	}

	path := filepath.Join(dir, "shelf.toml")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return Config{}, fmt.Errorf("could not create config directory: %w", err)
		}
		defaultDataDir := filepath.Join(dir, "data")
		contents := fmt.Sprintf(defaultConfigTmpl, defaultDataDir)
		if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
			return Config{}, fmt.Errorf("could not write default config: %w", err)
		}
	}

	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, fmt.Errorf("could not parse %s: %w", path, err)
	}

	// Expand ~ in data_dir.
	if len(cfg.DataDir) >= 2 && cfg.DataDir[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, fmt.Errorf("could not determine home directory: %w", err)
		}
		cfg.DataDir = filepath.Join(home, cfg.DataDir[2:])
	}

	return cfg, nil
}
