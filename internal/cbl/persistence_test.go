package cbl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAccountCredentialsRequiresIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := saveAccountCredentials(Credentials{AccessToken: "x", RefreshToken: "y"}); err == nil {
		t.Fatal("expected missing account id error")
	}
}

func TestSettingsValidationAndCorruptConfig(t *testing.T) {
	for _, proxy := range []string{"", "socks5h://host:0", "socks5h://user:pass@host:1080", "http://host:80/path", "http://host:80?foo=bar"} {
		t.Run(proxy, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := saveUserConfig(Options{ConfigFile: path}, true, proxy); err == nil {
				t.Fatalf("accepted invalid proxy %q", proxy)
			}
		})
	}
	for _, body := range []string{"null", "{broken"} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if err := saveUserConfig(Options{ConfigFile: path}, false, ""); err == nil {
			t.Fatal("overwrote invalid config")
		}
		got, _ := os.ReadFile(path)
		if string(got) != body {
			t.Fatal("corrupted original changed")
		}
	}
}

func TestAtomicWritePermissionsAndConfigPreservation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"unknown":42,"proxy":"socks5://localhost:1080"}`), 0600); err != nil {
		t.Fatal(err)
	}
	enabled := false
	if err := saveUserConfig(Options{ConfigFile: path}, enabled, ""); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err = json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["unknown"] != float64(42) || m["proxy_enabled"] != false {
		t.Fatalf("unexpected config: %s", b)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
}
