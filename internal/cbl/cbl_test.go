package cbl

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestLoadCredentialsFromApiKey(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "auth.json")
	mustWrite(t, path, []byte(`{"OPENAI_API_KEY":"sk-test"}`))
	creds, err := loadCredentials(Options{AuthFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if !creds.IsAPIKey || creds.AccessToken != "sk-test" {
		t.Fatalf("unexpected creds: %#v", creds)
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

func TestRenderWaybarJSON(t *testing.T) {
	snap := UsageSnapshot{
		FetchedAt: time.Unix(0, 0).UTC(),
		PrimaryWindow: &UsageWindow{UsedPercent: 20, RemainingPercent: 80},
		SecondaryWindow: &UsageWindow{UsedPercent: 30, RemainingPercent: 70},
		CreditsBalance: floatPtr(0),
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
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}
