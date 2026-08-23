package cbl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type UserConfig struct {
	ProfileName string `json:"profile_name"`
	AuthFile    string `json:"auth_file"`
	BaseURL     string `json:"base_url"`
}

func loadUserConfig(opts Options) UserConfig {
	path := opts.ConfigFile
	if path == "" {
		if env := strings.TrimSpace(os.Getenv("CBL_CONFIG_FILE")); env != "" {
			path = env
		}
	}
	if path == "" {
		if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
			path = filepath.Join(codexHome, "config.json")
		} else {
			path = filepath.Join(mustHome(), ".config", "cbl", "config.json")
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return UserConfig{}
	}
	var cfg UserConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return UserConfig{}
	}
	cfg.ProfileName = strings.TrimSpace(cfg.ProfileName)
	cfg.AuthFile = strings.TrimSpace(cfg.AuthFile)
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	return cfg
}
