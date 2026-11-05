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

// All requests terminate at localhost. No production token or endpoint is used.
func TestResetUseCrossProcessLock(t *testing.T) {
	o := Options{ConfigFile: filepath.Join(t.TempDir(), "config.json")}
	release, err := lockResetUse(o)
	if err != nil {
		t.Fatal(err)
	}
	if unexpected, err := lockResetUse(o); err == nil {
		unexpected()
		release()
		t.Fatal("independent descriptor acquired held lock")
	}
	release()
	next, err := lockResetUse(o)
	if err != nil {
		t.Fatal(err)
	}
	next()
}

func TestResetUseAmbiguityAndDefaultGenerator(t *testing.T) {
	for _, mode := range []string{"success", "unchanged", "http-error", "invalid-response", "already-redeemed"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
			creds := Credentials{AccessToken: "fixture-token", RefreshToken: "fixture-refresh", AccountID: "fixture-account"}
			if err := saveCredentials(defaultCBLAuthFile(), creds); err != nil {
				t.Fatal(err)
			}
			var posts, usage atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/backend-api/wham/usage":
					usage.Add(1)
					w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":0}},"rate_limit_reset_credits":{"available_count":0}}`))
				case "/backend-api/wham/rate-limit-reset-credits":
					if posts.Load() > 0 && mode == "success" {
						w.Write([]byte(`{"available_count":0,"credits":[]}`))
						return
					}
					w.Write([]byte(`{"available_count":1,"credits":[{"id":"fixture-grant","reset_type":"codex_rate_limits","status":"available","is_supported_by_plan":true,"expires_at":"2099-01-01T00:00:00Z"}]}`))
				case "/backend-api/wham/rate-limit-reset-credits/consume":
					posts.Add(1)
					var body map[string]string
					if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil || !requestIDPattern.MatchString(body["redeem_request_id"]) {
						t.Error("invalid consume payload")
					}
					if body["credit_id"] != "fixture-grant" {
						t.Error("client must not supply provider ID")
					}
					if mode == "http-error" {
						w.WriteHeader(500)
					}
					if mode == "already-redeemed" {
						w.Write([]byte(`{"code":"already_redeemed"}`))
					} else if mode == "invalid-response" {
						w.Write([]byte(`not-json`))
					} else {
						w.Write([]byte(`{"code":"reset"}`))
					}
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			o := Options{AuthFile: defaultCBLAuthFile(), BaseURL: srv.URL + "/backend-api", ResetUseHTTPClient: srv.Client()}
			call := func(state *cache) *httptest.ResponseRecorder {
				mux := http.NewServeMux()
				registerUIHandlers(mux, state, o)
				body := `{"account_key":"` + accountKey(creds, defaultCBLAuthFile()) + `","offer_key":"` + hashProviderCreditID("fixture-grant") + `"}`
				r := httptest.NewRequest("POST", "http://127.0.0.1/api/reset-credits/use", strings.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				localOnly(mux).ServeHTTP(w, r)
				return w
			}
			w := call(&cache{})
			want := 502
			if mode == "success" {
				want = 200
			}
			if w.Code != want {
				t.Fatalf("got%d want%d %s", w.Code, want, w.Body.String())
			}
			if posts.Load() != 1 || usage.Load() == 0 {
				t.Fatalf("posts=%d usage refreshes=%d", posts.Load(), usage.Load())
			}
			// A new service cache cannot resend an ambiguous request after restart.
			again := call(&cache{})
			if again.Code != 409 || posts.Load() != 1 {
				t.Fatalf("repeat allowed: status%d posts%d", again.Code, posts.Load())
			}
			if mode == "success" {
				if err := writeRemovedAccounts(map[string]bool{accountKey(creds, defaultCBLAuthFile()): true}); err != nil {
					t.Fatal(err)
				}
				if removed := call(&cache{}); removed.Code != 404 {
					t.Fatalf("removed account accepted: %d", removed.Code)
				}
			}
			stat, err := os.Stat(resetUseGuardFile(o))
			if err != nil || stat.Mode().Perm() != 0600 {
				t.Fatal("attempt guard must be persisted privately")
			}
		})
	}
}
