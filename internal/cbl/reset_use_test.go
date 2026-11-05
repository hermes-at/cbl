package cbl

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestResetUseGateAndValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	mux := http.NewServeMux()
	registerUIHandlers(mux, &cache{}, Options{})
	h := localOnly(mux)
	cases := []struct {
		method, origin, ct, body string
		want                     int
	}{{"GET", "", "", "", 405}, {"POST", "https://evil.test", "application/json", `{"account_key":"` + strings.Repeat("a", 64) + `","offer_key":"` + strings.Repeat("b", 64) + `"}`, 403}, {"POST", "", "text/plain", `{}`, 415}, {"POST", "", "application/json", `{"account_key":"x","offer_key":"y"}`, 400}, {"POST", "", "application/json", `{"account_key":"` + strings.Repeat("a", 64) + `","offer_key":"` + strings.Repeat("b", 64) + `","other":1}`, 400}, {"POST", "", "application/json", `{"account_key":"` + strings.Repeat("a", 64) + `","offer_key":"` + strings.Repeat("b", 64) + `"} {}`, 400}, {"POST", "", "application/json", `{"account_key":"` + strings.Repeat("a", 64) + `","offer_key":"` + strings.Repeat("b", 64) + `"}`, 404}}
	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, "http://127.0.0.1/api/reset-credits/use", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", tc.ct)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s got %d want %d: %s", tc.method, w.Code, tc.want, w.Body.String())
		}
	}
}
func TestResetUseLocalContractSuccessAndGuard(t *testing.T) {
	for _, code := range []string{"reset"} {
		t.Run(code, func(t *testing.T) {
			var posts atomic.Int32
			var bad atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/backend-api/wham/rate-limit-reset-credits" {
					if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer synthetic-token" || r.Header.Get("ChatGPT-Account-Id") != "synthetic-account" {
						bad.Store(true)
					}
					w.Header().Set("Content-Type", "application/json")
					if posts.Load() > 0 {
						_, _ = w.Write([]byte(`{"available_count":0,"credits":[]}`))
					} else {
						_, _ = w.Write([]byte(`{"available_count":1,"credits":[{"id":"credit_123","reset_type":"codex_rate_limits","is_supported_by_plan":true,"status":"available","expires_at":"2099-01-01T00:00:00Z"}]}`))
					}
					return
				}
				if r.URL.Path == "/backend-api/wham/rate-limit-reset-credits/consume" {
					posts.Add(1)
					var b map[string]string
					if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" || json.NewDecoder(r.Body).Decode(&b) != nil || b["credit_id"] != "credit_123" || b["redeem_request_id"] != "123e4567-e89b-42d3-a456-426614174000" {
						bad.Store(true)
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"code":"` + code + `"}`))
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))

			if err := os.MkdirAll(filepath.Dir(defaultCBLAuthFile()), 0700); err != nil {
				t.Fatal(err)
			}
			creds := Credentials{AccessToken: "synthetic-token", RefreshToken: "synthetic-refresh", AccountID: "synthetic-account"}
			if err := saveCredentials(defaultCBLAuthFile(), creds); err != nil {
				t.Fatal(err)
			}
			opts := Options{AuthFile: filepath.Join(home, ".config", "cbl", "auth.json"), BaseURL: server.URL + "/backend-api", ConfigFile: filepath.Join(home, "config.json"), ResetUseHTTPClient: server.Client(), ResetUseIDGenerator: func() (string, error) { return "123e4567-e89b-42d3-a456-426614174000", nil }}
			state := &cache{}
			mux := http.NewServeMux()
			registerUIHandlers(mux, state, opts)
			key := accountKey(creds, defaultCBLAuthFile())
			offer := hashProviderCreditID("credit_123")
			body := `{"account_key":"` + key + `","offer_key":"` + offer + `"}`
			call := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "http://127.0.0.1/api/reset-credits/use", strings.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				localOnly(mux).ServeHTTP(w, r)
				return w
			}
			w := call()
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"code":"`+code+`"`) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			again := call()
			if again.Code != 409 || posts.Load() != 1 {
				t.Fatalf("duplicate allowed: %d posts %d", again.Code, posts.Load())
			}
			if bad.Load() {
				t.Fatal("upstream request contract mismatch")
			}
		})
	}
}
func TestResetUseRejectsUnsafeOfferBeforePost(t *testing.T) {
	for _, item := range []string{`{"id":"credit_123","reset_type":"codex_rate_limits","is_supported_by_plan":false,"status":"available","expires_at":"2099-01-01T00:00:00Z"}`, `{"id":"credit_123","reset_type":"codex_rate_limits","is_supported_by_plan":true,"status":"available","expires_at":"2000-01-01T00:00:00Z"}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				t.Error("unexpected consume")
				return
			}
			_, _ = w.Write([]byte(`{"credits":[` + item + `]}`))
		}))
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))

		_ = os.MkdirAll(filepath.Dir(defaultCBLAuthFile()), 0700)
		creds := Credentials{AccessToken: "t", RefreshToken: "synthetic-refresh", AccountID: "a"}
		_ = saveCredentials(defaultCBLAuthFile(), creds)
		o := Options{AuthFile: filepath.Join(home, ".config", "cbl", "auth.json"), BaseURL: server.URL + "/backend-api", ResetUseHTTPClient: server.Client(), ResetUseIDGenerator: newResetRedeemRequestID}
		mux := http.NewServeMux()
		registerUIHandlers(mux, &cache{}, o)
		body := `{"account_key":"` + accountKey(creds, defaultCBLAuthFile()) + `","offer_key":"` + hashProviderCreditID("credit_123") + `"}`
		r := httptest.NewRequest("POST", "http://127.0.0.1/api/reset-credits/use", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		localOnly(mux).ServeHTTP(w, r)
		server.Close()
		if w.Code != 409 {
			t.Fatalf("unsafe offer status %d", w.Code)
		}
	}
}
func TestResetUseNoAutoRedirectOrRetry(t *testing.T) {
	var posts atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		http.Error(w, "redirect", http.StatusTemporaryRedirect)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`{"credits":[{"id":"credit_123","reset_type":"codex_rate_limits","is_supported_by_plan":true,"status":"available","expires_at":"2099-01-01T00:00:00Z"}]}`))
			return
		}
		w.Header().Set("Location", target.URL+"/steal")
		w.WriteHeader(307)
	}))
	defer source.Close()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))

	_ = os.MkdirAll(filepath.Dir(defaultCBLAuthFile()), 0700)
	c := Credentials{AccessToken: "t", RefreshToken: "synthetic-refresh", AccountID: "a"}
	_ = saveCredentials(defaultCBLAuthFile(), c)
	o := Options{AuthFile: filepath.Join(home, ".config", "cbl", "auth.json"), BaseURL: source.URL + "/backend-api", ResetUseHTTPClient: source.Client(), ResetUseIDGenerator: newResetRedeemRequestID}
	mux := http.NewServeMux()
	registerUIHandlers(mux, &cache{}, o)
	b := `{"account_key":"` + accountKey(c, defaultCBLAuthFile()) + `","offer_key":"` + hashProviderCreditID("credit_123") + `"}`
	r := httptest.NewRequest("POST", "http://127.0.0.1/api/reset-credits/use", strings.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	localOnly(mux).ServeHTTP(w, r)
	if posts.Load() != 0 || w.Code != 502 {
		t.Fatalf("redirect followed or wrong response: posts %d status %d", posts.Load(), w.Code)
	}
}
