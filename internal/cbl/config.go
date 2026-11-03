package cbl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type UserConfig struct {
	ProfileName string `json:"profile_name"`
	AuthFile    string `json:"auth_file"`
	BaseURL     string `json:"base_url"`
	Proxy       string `json:"proxy"`
	// An absent setting preserves old configs: a configured proxy stays enabled.
	// False explicitly means a direct connection.
	ProxyEnabled *bool `json:"proxy_enabled,omitempty"`
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
	cfg.Proxy = strings.TrimSpace(cfg.Proxy)
	return cfg
}

func defaultConfigFile() string {
	if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		return filepath.Join(codexHome, "config.json")
	}
	return filepath.Join(mustHome(), ".config", "cbl", "config.json")
}

func saveUserProxy(proxy string) error {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return nil
	}
	path := defaultConfigFile()
	cfg := loadUserConfig(Options{ConfigFile: path})
	cfg.Proxy = proxy
	enabled := proxy != ""
	cfg.ProxyEnabled = &enabled
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWriteFile(path, data, 0o600)
}

func saveUserConfig(opts Options, proxyEnabled bool, proxy string) error {
	proxy = strings.TrimSpace(proxy)
	if proxyEnabled {
		if proxy == "" {
			return fmt.Errorf("proxy URL is required when enabled")
		}
		if err := validateProxy(proxy); err != nil {
			return err
		}
	} else if proxy != "" {
		if err := validateProxy(proxy); err != nil {
			return err
		}
	}
	path := opts.ConfigFile
	if path == "" {
		if e := strings.TrimSpace(os.Getenv("CBL_CONFIG_FILE")); e != "" {
			path = e
		} else {
			path = defaultConfigFile()
		}
	}
	raw := map[string]json.RawMessage{}
	if b, e := os.ReadFile(path); e == nil {
		if err := json.Unmarshal(b, &raw); err != nil {
			return fmt.Errorf("read config: %w", err)
		}
		if raw == nil {
			return fmt.Errorf("config must be a JSON object")
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	set := func(k string, v any) error {
		b, e := json.Marshal(v)
		if e == nil {
			raw[k] = b
		}
		return e
	}
	if err := set("proxy_enabled", proxyEnabled); err != nil {
		return err
	}
	if err := set("proxy", proxy); err != nil {
		return err
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return atomicWriteFile(path, data, 0600)
}
