package cbl

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultAuthIssuer = "https://auth.openai.com"
	oauthClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
)

type LoginOptions struct {
	AuthFile string
	Issuer   string
	ClientID string
	Proxy    string
	Out      io.Writer
}

type DeviceCode struct {
	VerificationURL string
	UserCode        string
	deviceAuthID    string
	interval        time.Duration
}

type userCodeResp struct {
	DeviceAuthID string `json:"device_auth_id"`
	UserCode     string `json:"user_code"`
	UserCodeAlt  string `json:"usercode"`
	IntervalRaw  any    `json:"interval"`
}

type tokenPollResp struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeChallenge     string `json:"code_challenge"`
	CodeVerifier      string `json:"code_verifier"`
}

type exchangedTokens struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func RunDeviceLogin(ctx context.Context, opts LoginOptions) (string, error) {
	client, err := newHTTPClient(opts.Proxy)
	if err != nil {
		return "", err
	}
	device, err := RequestDeviceCode(ctx, client, opts)
	if err != nil {
		return "", err
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	fmt.Fprintf(out, "Open this URL and sign in:\n  %s\n\nEnter this code:\n  %s\n\nWaiting for confirmation...\n", device.VerificationURL, device.UserCode)
	creds, err := CompleteDeviceLogin(ctx, client, opts, device)
	if err != nil {
		return "", err
	}
	path := opts.AuthFile
	if path == "" {
		path = defaultCBLAuthFile()
	}
	if err := saveCredentials(path, creds); err != nil {
		return "", err
	}
	_, _ = saveAccountCredentials(creds)
	if err := saveUserProxy(resolvedProxy(opts.Proxy)); err != nil {
		return "", err
	}
	return path, nil
}

func RequestDeviceCode(ctx context.Context, client *http.Client, opts LoginOptions) (DeviceCode, error) {
	issuer := loginIssuer(opts)
	clientID := loginClientID(opts)
	body, _ := json.Marshal(map[string]string{"client_id": clientID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, issuer+"/api/accounts/deviceauth/usercode", bytes.NewReader(body))
	if err != nil {
		return DeviceCode{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "cbl")
	resp, err := client.Do(req)
	if err != nil {
		return DeviceCode{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return DeviceCode{}, fmt.Errorf("device code request failed: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var parsed userCodeResp
	if err := json.Unmarshal(data, &parsed); err != nil {
		return DeviceCode{}, fmt.Errorf("decode device code response: %w", err)
	}
	code := firstNonEmpty(parsed.UserCode, parsed.UserCodeAlt)
	if parsed.DeviceAuthID == "" || code == "" {
		return DeviceCode{}, fmt.Errorf("device code response missing device_auth_id/user_code")
	}
	interval := parseInterval(parsed.IntervalRaw)
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return DeviceCode{
		VerificationURL: issuer + "/codex/device",
		UserCode:        code,
		deviceAuthID:    parsed.DeviceAuthID,
		interval:        interval,
	}, nil
}

func CompleteDeviceLogin(ctx context.Context, client *http.Client, opts LoginOptions, device DeviceCode) (Credentials, error) {
	issuer := loginIssuer(opts)
	poll, err := pollDeviceToken(ctx, client, issuer, device)
	if err != nil {
		return Credentials{}, err
	}
	if poll.CodeVerifier == "" {
		return Credentials{}, fmt.Errorf("device token response missing code_verifier")
	}
	redirectURI := issuer + "/deviceauth/callback"
	tokens, err := exchangeAuthorizationCode(ctx, client, issuer, loginClientID(opts), redirectURI, poll.AuthorizationCode, poll.CodeVerifier)
	if err != nil {
		return Credentials{}, err
	}
	return credentialsFromTokens(tokens)
}

func pollDeviceToken(ctx context.Context, client *http.Client, issuer string, device DeviceCode) (tokenPollResp, error) {
	deadline := time.Now().Add(15 * time.Minute)
	for {
		body, _ := json.Marshal(map[string]string{
			"device_auth_id": device.deviceAuthID,
			"user_code":      device.UserCode,
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, issuer+"/api/accounts/deviceauth/token", bytes.NewReader(body))
		if err != nil {
			return tokenPollResp{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "cbl")
		resp, err := client.Do(req)
		if err != nil {
			return tokenPollResp{}, err
		}
		data, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
			var parsed tokenPollResp
			if err := json.Unmarshal(data, &parsed); err != nil {
				return tokenPollResp{}, fmt.Errorf("decode device token response: %w", err)
			}
			if parsed.AuthorizationCode == "" {
				return tokenPollResp{}, fmt.Errorf("device token response missing authorization_code")
			}
			return parsed, nil
		}
		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
			return tokenPollResp{}, fmt.Errorf("device auth failed: %s: %s", resp.Status, strings.TrimSpace(string(data)))
		}
		if time.Now().Add(device.interval).After(deadline) {
			return tokenPollResp{}, fmt.Errorf("device auth timed out after 15 minutes")
		}
		select {
		case <-time.After(device.interval):
		case <-ctx.Done():
			return tokenPollResp{}, ctx.Err()
		}
	}
}

func exchangeAuthorizationCode(ctx context.Context, client *http.Client, issuer, clientID, redirectURI, code, verifier string) (exchangedTokens, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, issuer+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return exchangedTokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "cbl")
	resp, err := client.Do(req)
	if err != nil {
		return exchangedTokens{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return exchangedTokens{}, fmt.Errorf("oauth token exchange failed: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var tokens exchangedTokens
	if err := json.Unmarshal(data, &tokens); err != nil {
		return exchangedTokens{}, fmt.Errorf("decode oauth token response: %w", err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		return exchangedTokens{}, fmt.Errorf("oauth token response missing access_token/refresh_token")
	}
	return tokens, nil
}

func RefreshCredentials(ctx context.Context, client *http.Client, authPath string, creds Credentials) (Credentials, error) {
	if creds.RefreshToken == "" {
		return Credentials{}, fmt.Errorf("auth.json has no refresh_token; run cbl login again")
	}
	body, _ := json.Marshal(map[string]string{
		"client_id":     oauthClientID,
		"grant_type":    "refresh_token",
		"refresh_token": creds.RefreshToken,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, defaultAuthIssuer+"/oauth/token", bytes.NewReader(body))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "cbl")
	resp, err := client.Do(req)
	if err != nil {
		return Credentials{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Credentials{}, fmt.Errorf("refresh token failed: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var tokens exchangedTokens
	if err := json.Unmarshal(data, &tokens); err != nil {
		return Credentials{}, fmt.Errorf("decode refresh response: %w", err)
	}
	if tokens.AccessToken == "" {
		tokens.AccessToken = creds.AccessToken
	}
	if tokens.RefreshToken == "" {
		tokens.RefreshToken = creds.RefreshToken
	}
	if tokens.IDToken == "" {
		tokens.IDToken = creds.IDToken
	}
	refreshed, err := credentialsFromTokens(tokens)
	if err != nil {
		return Credentials{}, err
	}
	if authPath != "" {
		if err := saveCredentials(authPath, refreshed); err != nil {
			return Credentials{}, err
		}
	}
	return refreshed, nil
}

func saveCredentials(path string, creds Credentials) error {
	if path == "" {
		path = defaultCBLAuthFile()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	doc := map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]string{
			"id_token":      creds.IDToken,
			"access_token":  creds.AccessToken,
			"refresh_token": creds.RefreshToken,
			"account_id":    creds.AccountID,
		},
		"last_refresh": time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func credentialsFromTokens(tokens exchangedTokens) (Credentials, error) {
	accountID := accountIDFromJWT(tokens.IDToken)
	if accountID == "" {
		accountID = accountIDFromJWT(tokens.AccessToken)
	}
	return Credentials{
		AccessToken:  strings.TrimSpace(tokens.AccessToken),
		RefreshToken: strings.TrimSpace(tokens.RefreshToken),
		IDToken:      strings.TrimSpace(tokens.IDToken),
		AccountID:    accountID,
		Source:       "tokens",
	}, nil
}

func accountIDFromJWT(jwt string) string {
	claims := jwtClaims(jwt)
	if auth, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		for _, key := range []string{"chatgpt_account_id", "organization_id"} {
			if v, ok := auth[key].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func jwtClaims(jwt string) map[string]any {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 || parts[1] == "" {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	return claims
}

func generatePKCE() (verifier, challenge string, err error) {
	buf := make([]byte, 64)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func loginIssuer(opts LoginOptions) string {
	issuer := strings.TrimRight(strings.TrimSpace(opts.Issuer), "/")
	if issuer == "" {
		issuer = defaultAuthIssuer
	}
	return issuer
}

func loginClientID(opts LoginOptions) string {
	if opts.ClientID != "" {
		return opts.ClientID
	}
	if env := strings.TrimSpace(os.Getenv("CODEX_APP_SERVER_LOGIN_CLIENT_ID")); env != "" {
		return env
	}
	return oauthClientID
}

func parseInterval(raw any) time.Duration {
	switch v := raw.(type) {
	case float64:
		return time.Duration(v) * time.Second
	case string:
		var seconds int
		if _, err := fmt.Sscan(v, &seconds); err == nil {
			return time.Duration(seconds) * time.Second
		}
	}
	return 0
}
