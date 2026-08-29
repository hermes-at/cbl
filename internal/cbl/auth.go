package cbl

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type authFile struct {
	Tokens struct {
		AccessToken   string `json:"access_token"`
		RefreshToken  string `json:"refresh_token"`
		IDToken       string `json:"id_token"`
		AccountID     string `json:"account_id"`
		AccessToken2  string `json:"accessToken"`
		RefreshToken2 string `json:"refreshToken"`
		IDToken2      string `json:"idToken"`
		AccountID2    string `json:"accountId"`
	} `json:"tokens"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	AccountID    string `json:"account_id"`
	APIKey       string `json:"OPENAI_API_KEY"`
	LastRefresh  string `json:"last_refresh"`
}

func loadCredentials(opts Options) (Credentials, error) {
	cfg := loadUserConfig(opts)
	path := opts.AuthFile
	if path == "" {
		if env := strings.TrimSpace(os.Getenv("CBL_AUTH_FILE")); env != "" {
			path = env
		}
	}
	if path == "" && cfg.AuthFile != "" {
		path = cfg.AuthFile
	}
	if path == "" {
		for _, candidate := range defaultAuthFileCandidates() {
			if _, err := os.Stat(candidate); err == nil {
				path = candidate
				break
			}
		}
		if path == "" {
			path = defaultAuthFileCandidates()[0]
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, fmt.Errorf("read auth.json %s: %w", path, err)
	}
	var doc authFile
	if err := json.Unmarshal(data, &doc); err != nil {
		return Credentials{}, fmt.Errorf("decode auth.json: %w", err)
	}
	if doc.APIKey != "" {
		return Credentials{AccessToken: strings.TrimSpace(doc.APIKey), IsAPIKey: true, Source: "api-key"}, nil
	}
	access := firstNonEmpty(doc.Tokens.AccessToken, doc.Tokens.AccessToken2, doc.AccessToken)
	refresh := firstNonEmpty(doc.Tokens.RefreshToken, doc.Tokens.RefreshToken2, doc.RefreshToken)
	idToken := firstNonEmpty(doc.Tokens.IDToken, doc.Tokens.IDToken2, doc.IDToken)
	accountID := firstNonEmpty(doc.Tokens.AccountID, doc.Tokens.AccountID2, doc.AccountID)
	if access == "" || refresh == "" {
		return Credentials{}, errors.New("auth.json exists but tokens.access_token / tokens.refresh_token are missing")
	}
	return Credentials{
		AccessToken:  access,
		RefreshToken: refresh,
		IDToken:      idToken,
		AccountID:    accountID,
		Source:       "tokens",
	}, nil
}

func defaultAuthFileCandidates() []string {
	if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		return []string{filepath.Join(codexHome, "auth.json")}
	}
	home := mustHome()
	return []string{
		filepath.Join(home, ".codex", "auth.json"),
		filepath.Join(home, ".config", "codex", "auth.json"),
	}
}

func loadBaseURL(opts Options) string {
	if opts.BaseURL != "" {
		return strings.TrimSpace(opts.BaseURL)
	}
	if env := strings.TrimSpace(os.Getenv("CBL_BASE_URL")); env != "" {
		return env
	}
	cfg := loadUserConfig(opts)
	if cfg.BaseURL != "" {
		return cfg.BaseURL
	}
	if opts.ConfigFile != "" {
		if url := parseBaseURLFromFile(opts.ConfigFile); url != "" {
			return url
		}
	}
	if env := strings.TrimSpace(os.Getenv("CBL_CONFIG_FILE")); env != "" {
		if url := parseBaseURLFromFile(env); url != "" {
			return url
		}
	}
	if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		if url := parseBaseURLFromFile(filepath.Join(codexHome, "config.toml")); url != "" {
			return url
		}
	}
	if url := parseBaseURLFromFile(filepath.Join(mustHome(), ".codex", "config.toml")); url != "" {
		return url
	}
	return "https://chatgpt.com/backend-api"
}

func parseBaseURLFromFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" || !strings.HasPrefix(line, "chatgpt_base_url") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, "\"'")
		if val != "" {
			return val
		}
	}
	return ""
}

func mustHome() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "/root"
	}
	return home
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
