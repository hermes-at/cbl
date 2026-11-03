package cbl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoadAllKeepsFailedAccountVisible(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "")
	for _, id := range []string{"good", "broken"} {
		if _, err := saveAccountCredentials(Credentials{AccountID: id, AccessToken: "test", RefreshToken: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ChatGPT-Account-Id") == "broken" {
			http.Error(w, "temporarily unavailable", 503)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"account_id": "good", "rate_limit": map[string]any{"primary_window": map[string]any{"used_percent": 25}}})
	}))
	defer upstream.Close()
	snaps, err := LoadAll(context.Background(), Options{BaseURL: upstream.URL + "/backend-api"})
	if err != nil {
		t.Fatal(err)
	}
	accounts := dashboardPayloadAll(snaps, nil)["accounts"].([]map[string]any)
	if len(accounts) != 2 {
		t.Fatalf("got %d accounts, want both including failed one", len(accounts))
	}
	for _, a := range accounts {
		if a["account_id"] == "broken" {
			if a["error"] == nil || a["error"] == "" {
				t.Fatalf("missing error: %v", a)
			}
			if a["class"] != "error" {
				t.Fatalf("bad class: %v", a)
			}
			return
		}
	}
	t.Fatal("broken account absent")
}

func TestSavedAccountTakesPriorityOverLegacyDuplicate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "")
	if err := saveCredentials(defaultCBLAuthFile(), Credentials{AccountID: "same", AccessToken: "OLD", RefreshToken: "OLD"}); err != nil {
		t.Fatal(err)
	}
	if _, err := saveAccountCredentials(Credentials{AccountID: "same", AccessToken: "NEW", RefreshToken: "NEW"}); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer NEW" {
			http.Error(w, "old duplicate selected", 503)
			return
		}
		_, _ = w.Write([]byte(`{"account_id":"same"}`))
	}))
	defer upstream.Close()
	snaps, err := LoadAll(context.Background(), Options{BaseURL: upstream.URL + "/backend-api"})
	if err != nil || len(snaps) != 1 || snaps[0].Error != "" {
		t.Fatalf("fresh login shadowed: %v %v", snaps, err)
	}
}

func TestConcurrentRefreshIsCoalesced(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "")
	_, err := saveAccountCredentials(Credentials{AccountID: "one", AccessToken: "test", RefreshToken: "test"})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(40 * time.Millisecond)
		_, _ = w.Write([]byte(`{"account_id":"one"}`))
	}))
	defer upstream.Close()
	mux := http.NewServeMux()
	registerUIHandlers(mux, &cache{}, Options{BaseURL: upstream.URL + "/backend-api"})
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := httptest.NewRecorder()
			mux.ServeHTTP(res, httptest.NewRequest("POST", "/api/refresh", nil))
			if res.Code != 200 {
				t.Errorf("status %d", res.Code)
			}
		}()
	}
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("upstream called %d times; want one coalesced refresh", got)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config.json")
	mux := http.NewServeMux()
	registerUIHandlers(mux, &cache{}, Options{ConfigFile: config})
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest("POST", "/api/settings", strings.NewReader(`{"proxy_enabled":true,"proxy":"socks5h://127.0.0.1:1080"}`)))
	if res.Code != 200 {
		t.Fatalf("POST settings: %d %s", res.Code, res.Body.String())
	}
	res = httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest("GET", "/api/settings", nil))
	var data map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data["proxy_enabled"] != true || data["proxy"] != "socks5h://127.0.0.1:1080" {
		t.Fatalf("settings: %v", data)
	}
	res = httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest("POST", "/api/settings", strings.NewReader(`{"proxy_enabled":true,"proxy":"file:///etc/passwd"}`)))
	if res.Code != 400 {
		t.Fatalf("invalid proxy accepted: %d", res.Code)
	}
}

func TestLoginSessionRetryAndExclusiveClaim(t *testing.T) {
	sessions := newLoginSessions()
	id := sessions.put(DeviceCode{UserCode: "TEST"})
	if _, ok := sessions.take(id); !ok {
		t.Fatal("first claim failed")
	}
	if _, ok := sessions.take(id); ok {
		t.Fatal("duplicate completion allowed")
	}
	sessions.finish(id, false)
	if _, ok := sessions.take(id); !ok {
		t.Fatal("transient failure consumed session")
	}
	sessions.finish(id, true)
	if _, ok := sessions.take(id); ok {
		t.Fatal("successful session reusable")
	}
}

func TestLocalAPIRejectsForeignHostAndOrigin(t *testing.T) {
	for _, test := range []struct {
		host, origin string
		want         int
	}{
		{"127.0.0.1:18088", "", 200}, {"localhost:18088", "", 200}, {"[::1]:18088", "", 200},
		{"evil.example", "", 403}, {"127.0.0.1:18088", "https://evil.example", 403},
		{"127.0.0.1:18088", "http://127.0.0.1:18088", 200},
		{"127.0.0.1:18088", "http://127.0.0.1:9999", 403},
	} {
		called := false
		handler := localOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(200) }))
		req := httptest.NewRequest("POST", "http://"+test.host+"/api/refresh", nil)
		if test.origin != "" {
			req.Header.Set("Origin", test.origin)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != test.want || called != (test.want == 200) {
			t.Fatalf("host=%s origin=%s code=%d called=%v", test.host, test.origin, res.Code, called)
		}
	}
}
