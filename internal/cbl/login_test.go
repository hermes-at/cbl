package cbl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDeviceLoginSavesCBLAuth(t *testing.T) {
	jwt := testJWT(map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct-1",
		},
	})
	var sawUserCode, sawPoll, sawExchange bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			sawUserCode = true
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["client_id"] != "test-client" {
				t.Fatalf("client_id = %q", body["client_id"])
			}
			_ = json.NewEncoder(w).Encode(map[string]string{
				"device_auth_id": "device-1",
				"user_code":      "USER-CODE",
				"interval":       "1",
			})
		case "/api/accounts/deviceauth/token":
			sawPoll = true
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["device_auth_id"] != "device-1" || body["user_code"] != "USER-CODE" {
				t.Fatalf("bad poll body: %#v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{
				"authorization_code": "auth-code",
				"code_challenge":     "challenge",
				"code_verifier":      "verifier",
			})
		case "/oauth/token":
			sawExchange = true
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("client_id") != "test-client" || r.Form.Get("code") != "auth-code" || r.Form.Get("code_verifier") != "verifier" {
				t.Fatalf("bad exchange form: %#v", r.Form)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id_token":      jwt,
				"access_token":  "access-token",
				"refresh_token": "refresh-token",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tmp := t.TempDir()
	authPath := filepath.Join(tmp, "auth.json")
	var out strings.Builder
	savedPath, err := RunDeviceLogin(context.Background(), LoginOptions{AuthFile: authPath, Issuer: server.URL, ClientID: "test-client", Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if savedPath != authPath {
		t.Fatalf("savedPath = %q", savedPath)
	}
	if !sawUserCode || !sawPoll || !sawExchange {
		t.Fatalf("missing auth calls: user=%v poll=%v exchange=%v", sawUserCode, sawPoll, sawExchange)
	}
	if !strings.Contains(out.String(), "USER-CODE") || !strings.Contains(out.String(), server.URL+"/codex/device") {
		t.Fatalf("prompt did not include login details: %q", out.String())
	}
	creds, err := loadCredentials(Options{AuthFile: authPath})
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != "access-token" || creds.RefreshToken != "refresh-token" || creds.AccountID != "acct-1" {
		t.Fatalf("unexpected creds: %#v", creds)
	}
}

func TestDefaultAuthCandidatesPreferCBLAuth(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("CODEX_HOME", "")
	cblPath := filepath.Join(tmp, ".config", "cbl", "auth.json")
	codexPath := filepath.Join(tmp, ".codex", "auth.json")
	mustWrite(t, cblPath, []byte(`{"tokens":{"access_token":"cbl","refresh_token":"r","account_id":"acct-1"}}`))
	mustWrite(t, codexPath, []byte(`{"tokens":{"access_token":"codex","refresh_token":"r","account_id":"acct-1"}}`))
	creds, err := loadCredentials(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != "cbl" {
		t.Fatalf("got %q, want cbl", creds.AccessToken)
	}
}

func testJWT(claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}
