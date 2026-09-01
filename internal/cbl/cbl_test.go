package cbl

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadCredentialsFromTokens(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "auth.json")
	mustWrite(t, path, []byte(`{"tokens":{"access_token":"a","refresh_token":"r","account_id":"acct-1"}}`))
	opts := Options{AuthFile: path}
	creds, err := loadCredentials(opts)
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != "a" || creds.RefreshToken != "r" || creds.AccountID != "acct-1" {
		t.Fatalf("unexpected creds: %#v", creds)
	}
}

func TestLoadCredentialsUsesJWTEmailForDisplay(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "auth.json")
	mustWrite(t, path, []byte(`{"tokens":{"access_token":"a","refresh_token":"r","id_token":"x.eyJlbWFpbCI6ICJtYXhAZXhhbXBsZS5jb20iLCAibmFtZSI6ICJNYXhpbSJ9.y","account_id":"acct-1"}}`))
	creds, err := loadCredentials(Options{AuthFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccountEmail != "max@example.com" || creds.AccountName != "Maxim" {
		t.Fatalf("unexpected display fields: %#v", creds)
	}
}

func TestLoadCredentialsFromConfigAuthFile(t *testing.T) {
	tmp := t.TempDir()
	authPath := filepath.Join(tmp, "auth.json")
	mustWrite(t, authPath, []byte(`{"tokens":{"access_token":"a","refresh_token":"r","account_id":"acct-1"}}`))
	cfgPath := filepath.Join(tmp, "config.json")
	mustWrite(t, cfgPath, []byte(`{"profile_name":"work","auth_file":"`+authPath+`","base_url":"https://chatgpt.com/backend-api"}`))
	creds, err := loadCredentials(Options{ConfigFile: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != "a" || creds.RefreshToken != "r" || creds.AccountID != "acct-1" {
		t.Fatalf("unexpected creds: %#v", creds)
	}
}

func TestSaveAccountCredentialsAddsAccountFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("CODEX_HOME", "")
	path, err := saveAccountCredentials(Credentials{AccessToken: "a", RefreshToken: "r", AccountID: "acct/new"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, filepath.Join(".config", "cbl", "accounts", "acct_new.json")) {
		t.Fatalf("path = %q", path)
	}
	files := accountAuthFiles()
	if len(files) != 1 || files[0] != path {
		t.Fatalf("files = %#v, want %q", files, path)
	}
	creds, err := loadCredentials(Options{AuthFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccountID != "acct/new" || creds.AccessToken != "a" {
		t.Fatalf("creds = %#v", creds)
	}
}

func TestLoadCredentialsFallsBackToXDGStyleCodexAuth(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("CODEX_HOME", "")
	authPath := filepath.Join(tmp, ".config", "codex", "auth.json")
	mustWrite(t, authPath, []byte(`{"tokens":{"access_token":"a","refresh_token":"r","account_id":"acct-1"}}`))
	creds, err := loadCredentials(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != "a" || creds.RefreshToken != "r" || creds.AccountID != "acct-1" {
		t.Fatalf("unexpected creds: %#v", creds)
	}
}

func TestLoadCredentialsPrefersLegacyCodexAuth(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("CODEX_HOME", "")
	legacyPath := filepath.Join(tmp, ".codex", "auth.json")
	xdgPath := filepath.Join(tmp, ".config", "codex", "auth.json")
	mustWrite(t, legacyPath, []byte(`{"tokens":{"access_token":"legacy","refresh_token":"r","account_id":"acct-1"}}`))
	mustWrite(t, xdgPath, []byte(`{"tokens":{"access_token":"xdg","refresh_token":"r","account_id":"acct-1"}}`))
	creds, err := loadCredentials(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != "legacy" {
		t.Fatalf("got %q, want legacy", creds.AccessToken)
	}
}

func TestLoadBaseURLFromConfig(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	mustWrite(t, cfgPath, []byte(`{"base_url":"https://example.com/custom"}`))
	if got := loadBaseURL(Options{ConfigFile: cfgPath}); got != "https://example.com/custom" {
		t.Fatalf("got %s", got)
	}
}

func TestMapSnapshotFromRateLimitResponse(t *testing.T) {
	jsonText := `{
	  "account_id": "acct-99",
	  "plan_type": "plus",
	  "rate_limit": {
	    "primary_window": {"used_percent": 12, "reset_at": 1786161204, "limit_window_seconds": 18000},
	    "secondary_window": {"used_percent": 23, "reset_at": 1786164804, "limit_window_seconds": 604800},
	    "individual_limit": {"used": 7761, "limit": 100000, "remaining_percent": 92.239, "resets_at": 1782864000}
	  },
	  "credits": {"has_credits": true, "unlimited": false, "balance": "0"},
	  "additional_rate_limits": [
	    {"limit_name": "GPT-5.3-Codex-Spark", "rate_limit": {"used_percent": 30, "reset_at": 1786161204, "limit_window_seconds": 18000}}
	  ]
	}`
	snap, err := mapFromBytes([]byte(jsonText), "https://chatgpt.com/backend-api")
	if err != nil {
		t.Fatal(err)
	}
	if snap.AccountID != "acct-99" || snap.PlanType != "plus" {
		t.Fatalf("unexpected snap: %#v", snap)
	}
	if snap.PrimaryWindow == nil || snap.PrimaryWindow.Remaining() != 88 {
		t.Fatalf("unexpected primary: %#v", snap.PrimaryWindow)
	}
	if snap.SecondaryWindow == nil || snap.SecondaryWindow.Remaining() != 77 {
		t.Fatalf("unexpected secondary: %#v", snap.SecondaryWindow)
	}
	if snap.IndividualLimit == nil || snap.IndividualLimit.Remaining != 92239 {
		t.Fatalf("unexpected limit: %#v", snap.IndividualLimit)
	}
	if len(snap.AdditionalRates) != 1 || snap.AdditionalRates[0].Name != "GPT-5.3-Codex-Spark" {
		t.Fatalf("unexpected additional: %#v", snap.AdditionalRates)
	}
}

func TestLoadAllFetchesSavedAccounts(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("CODEX_HOME", "")
	_, err := saveAccountCredentials(Credentials{AccessToken: "access-1", RefreshToken: "r1", AccountID: "acct-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = saveAccountCredentials(Credentials{AccessToken: "access-2", RefreshToken: "r2", AccountID: "acct-2"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/wham/usage" {
			http.NotFound(w, r)
			return
		}
		acct := r.Header.Get("ChatGPT-Account-Id")
		used := 10
		if acct == "acct-2" {
			used = 40
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"account_id": acct,
			"plan_type":  "plus",
			"rate_limit": map[string]any{"primary_window": map[string]any{"used_percent": used}},
		})
	}))
	defer server.Close()
	snaps, err := LoadAll(context.Background(), Options{BaseURL: server.URL + "/backend-api"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 2 {
		t.Fatalf("snaps = %#v", snaps)
	}
	if snaps[0].AccountID != "acct-1" || snaps[0].PrimaryWindow.Remaining() != 90 || snaps[1].AccountID != "acct-2" || snaps[1].PrimaryWindow.Remaining() != 60 {
		t.Fatalf("unexpected snaps: %#v", snaps)
	}
}

func TestLoadAllReturnsEmptyWhenNoAccounts(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("CODEX_HOME", "")
	snaps, err := LoadAll(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 0 {
		t.Fatalf("snaps = %#v", snaps)
	}
}

func TestRenderWaybarJSON(t *testing.T) {
	snap := UsageSnapshot{
		FetchedAt:       time.Unix(0, 0).UTC(),
		PrimaryWindow:   &UsageWindow{UsedPercent: 20, RemainingPercent: 80},
		SecondaryWindow: &UsageWindow{UsedPercent: 30, RemainingPercent: 70},
		CreditsBalance:  floatPtr(0),
	}
	var buf bytes.Buffer
	if err := Render(&buf, snap, RenderOptions{Waybar: true}); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload["text"].(string), "Codex") {
		t.Fatalf("unexpected text: %#v", payload["text"])
	}
	if payload["class"] != "good" {
		t.Fatalf("unexpected class: %#v", payload["class"])
	}
}

func TestResolveUsageURL(t *testing.T) {
	got, err := resolveUsageURL("https://chatgpt.com/backend-api")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://chatgpt.com/backend-api/wham/usage"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	got, err = resolveUsageURL("https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.com/api/codex/usage" {
		t.Fatalf("got %s", got)
	}
}

func TestWatchWithFixture(t *testing.T) {
	tmp := t.TempDir()
	fixture := filepath.Join(tmp, "usage.json")
	mustWrite(t, fixture, []byte(`{"rate_limit":{"primary_window":{"used_percent":1,"reset_at":1786161204,"limit_window_seconds":18000}}}`))
	var buf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = Watch(ctx, time.Second, Options{Fixture: fixture, Waybar: true}, &buf)
		cancel()
	}()
	time.Sleep(1500 * time.Millisecond)
	cancel()
	if buf.Len() == 0 {
		t.Fatal("expected output")
	}
}

func floatPtr(v float64) *float64 { return &v }

func mustWrite(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}
