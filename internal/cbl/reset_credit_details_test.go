package cbl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResetCreditReadOnlyEnrichment(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "details", true: "optional failure"}[fail], func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != "GET" {
					t.Errorf("unexpected mutation: %s", r.Method)
				}
				if r.Header.Get("Authorization") != "Bearer fixture-token" || r.Header.Get("ChatGPT-Account-Id") != "fixture-account" {
					t.Error("missing scoped auth")
				}
				switch r.URL.Path {
				case "/backend-api/wham/usage":
					w.Write([]byte(`{"rate_limit_reset_credits":{"available_count":2,"applicable_available_count":0}}`))
				case "/backend-api/wham/rate-limit-reset-credits":
					if fail {
						w.WriteHeader(503)
						return
					}
					w.Write([]byte(`{"available_count":2,"credits":[{"id":"private-grant","reset_type":"codex_rate_limits","status":"available","is_supported_by_plan":true,"expires_at":"2099-11-30T19:20:21.954244Z","profile_user_id":"private-profile"}]}`))
				default:
					t.Errorf("unexpected endpoint: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			snap, err := fetchUsage(context.Background(), srv.Client(), Credentials{AccessToken: "fixture-token", AccountID: "fixture-account"}, srv.URL+"/backend-api")
			if err != nil {
				t.Fatal(err)
			}
			if requests != 2 {
				t.Fatalf("requests=%d", requests)
			}
			if snap.ResetCredits == nil || *snap.ResetCredits.ApplicableAvailableCount != 0 {
				t.Fatal("inventory lost")
			}
			if fail {
				if snap.ResetCredits.State != "unavailable" {
					t.Fatal("must retain unknown details")
				}
				return
			}
			if len(snap.ResetCredits.Offers) != 1 || snap.ResetCredits.Offers[0].Scope != "full" {
				t.Fatal("missing offer")
			}
			body, _ := json.Marshal(snap.ResetCredits)
			if strings.Contains(string(body), "private") {
				t.Fatal("provider data leaked")
			}
		})
	}
}

func TestResetCreditDetailsFiltering(t *testing.T) {
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	body := []byte(`{"available_count":2,"credits":[
 {"id":"valid","reset_type":"codex_rate_limits","status":"available","is_supported_by_plan":true,"expires_at":"2026-11-30T19:20:00Z"},
 {"id":"valid","reset_type":"codex_rate_limits","status":"available","is_supported_by_plan":true},
 {"id":"expired","status":"available","is_supported_by_plan":true,"expires_at":"2000-01-01T00:00:00Z"},
 {"id":"redeemed","status":"redeemed","is_supported_by_plan":true},
 {"id":"unsupported","status":"available","is_supported_by_plan":false}]}`)
	got := parseResetCreditDetails(body, now)
	if got == nil || len(got.Offers) != 1 {
		t.Fatalf("unexpected filtered details: %+v", got)
	}
	for _, raw := range []string{`null`, `{}`, `{"available_count":-1}`, `{"available_count":2,"credits":[{"expires_at":"bad"}]}`} {
		if parseResetCreditDetails([]byte(raw), now) != nil {
			t.Fatalf("invalid details accepted: %s", raw)
		}
	}
}
