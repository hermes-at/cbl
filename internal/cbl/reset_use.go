package cbl

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const resetUseBodyLimit = 4096

var providerCreditID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)
var requestIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func newResetRedeemRequestID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	x := make([]byte, 36)
	hex.Encode(x[0:8], b[0:4])
	x[8] = '-'
	hex.Encode(x[9:13], b[4:6])
	x[13] = '-'
	hex.Encode(x[14:18], b[6:8])
	x[18] = '-'
	hex.Encode(x[19:23], b[8:10])
	x[23] = '-'
	hex.Encode(x[24:36], b[10:16])
	return string(x), nil
}
func resetUseGuardFile(o Options) string {
	if strings.TrimSpace(o.ConfigFile) != "" {
		return o.ConfigFile + ".reset-use-guard.json"
	}
	if p := strings.TrimSpace(os.Getenv("CBL_CONFIG_FILE")); p != "" {
		return p + ".reset-use-guard.json"
	}
	return filepath.Join(mustHome(), ".config", "cbl", "reset-use-guard.json")
}
func readResetUseGuard(o Options) (map[string]bool, error) {
	p := resetUseGuardFile(o)
	i, e := os.Lstat(p)
	if os.IsNotExist(e) {
		return map[string]bool{}, nil
	}
	if e != nil {
		return nil, e
	}
	if !i.Mode().IsRegular() || i.Mode().Perm()&0077 != 0 {
		return nil, errors.New("invalid guard permissions")
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return nil, e
	}
	var d struct {
		Version int      `json:"version"`
		Keys    []string `json:"keys"`
	}
	if json.Unmarshal(b, &d) != nil || d.Version != 1 || d.Keys == nil {
		return nil, errors.New("invalid guard")
	}
	m := map[string]bool{}
	for _, k := range d.Keys {
		if !validAccountKey(k) {
			return nil, errors.New("invalid guard")
		}
		m[k] = true
	}
	return m, nil
}
func writeResetUseGuard(o Options, m map[string]bool) error {
	keys := []string{}
	for k, v := range m {
		if v {
			keys = append(keys, k)
		}
	}
	b, e := json.Marshal(struct {
		Version int      `json:"version"`
		Keys    []string `json:"keys"`
	}{1, keys})
	if e != nil {
		return e
	}
	return atomicWriteFile(resetUseGuardFile(o), b, 0600)
}
func resetUseHandler(state *cache, o Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			writeJSON(w, 405, map[string]any{"ok": false, "error_code": "method_not_allowed"})
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, e := url.Parse(origin)
			if e != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
				writeJSON(w, 403, map[string]any{"ok": false, "error_code": "forbidden"})
				return
			}
		}
		if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
			writeJSON(w, 415, map[string]any{"ok": false, "error_code": "invalid_request"})
			return
		}
		var in struct {
			AccountKey string `json:"account_key"`
			OfferKey   string `json:"offer_key"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, resetUseBodyLimit))
		d.DisallowUnknownFields()
		if d.Decode(&in) != nil || !validAccountKey(in.AccountKey) || !validAccountKey(in.OfferKey) {
			writeJSON(w, 400, map[string]any{"ok": false, "error_code": "invalid_request"})
			return
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			writeJSON(w, 400, map[string]any{"ok": false, "error_code": "invalid_request"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		state.opMu.Lock()
		defer state.opMu.Unlock()
		unlock, e := lockResetUse(o)
		if e != nil {
			writeJSON(w, 409, map[string]any{"ok": false, "error_code": "reset_in_progress"})
			return
		}
		defer unlock()
		removed, e := readRemovedAccounts()
		if e != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error_code": "account_unavailable"})
			return
		}
		files := accountAuthFiles()
		explicit := firstNonEmpty(o.AuthFile, os.Getenv("CBL_AUTH_FILE"))
		if explicit != "" {
			files = []string{explicit}
		} else {
			files = append(files, defaultCBLAuthFile())
			files = append(files, nativeAccountFiles()...)
		}
		seen := map[string]bool{}
		var creds Credentials
		found := false
		for _, p := range files {
			c, err := loadCredentials(Options{AuthFile: p})
			if err != nil {
				continue
			}
			key := accountKey(c, p)
			if removed[key] || seen[key] {
				continue
			}
			seen[key] = true
			if key == in.AccountKey {
				creds = c
				found = true
				break
			}
		}
		if !found {
			writeJSON(w, 404, map[string]any{"ok": false, "error_code": "account_not_found"})
			return
		}
		if creds.IsAPIKey {
			writeJSON(w, 409, map[string]any{"ok": false, "error_code": "offer_unavailable"})
			return
		}
		client := o.ResetUseHTTPClient
		if client == nil {
			client, e = newConfiguredHTTPClient(o)
			if e != nil {
				writeJSON(w, 502, map[string]any{"ok": false, "error_code": "provider_unavailable"})
				return
			}
		}
		// Never redirect credentials or replay the mutation through redirects.
		bounded := *client
		bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client = &bounded
		base, e := url.Parse(strings.TrimSpace(loadBaseURL(o)))
		if e != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error_code": "provider_unavailable"})
			return
		}
		base.Path = strings.TrimRight(base.Path, "/")
		if strings.Contains(base.String(), "/backend-api") {
			base.Path += "/wham/rate-limit-reset-credits"
		} else {
			base.Path += "/api/codex/rate-limit-reset-credits"
		}
		get, e := http.NewRequestWithContext(ctx, "GET", base.String(), nil)
		if e != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error_code": "provider_unavailable"})
			return
		}
		setProviderHeaders(get, creds)
		resp, e := client.Do(get)
		if e != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error_code": "provider_unavailable"})
			return
		}
		body, e := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
		resp.Body.Close()
		if e != nil || len(body) > 1024*1024 || resp.StatusCode != 200 {
			writeJSON(w, 502, map[string]any{"ok": false, "error_code": "provider_unavailable"})
			return
		}
		var inventory struct {
			Credits []struct {
				ID        string     `json:"id"`
				Type      string     `json:"reset_type"`
				Supported bool       `json:"is_supported_by_plan"`
				Status    string     `json:"status"`
				Expires   *time.Time `json:"expires_at"`
			} `json:"credits"`
		}
		if json.Unmarshal(body, &inventory) != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error_code": "provider_unavailable"})
			return
		}
		providerID := ""
		for _, c := range inventory.Credits {
			if !providerCreditID.MatchString(c.ID) || c.Type != "codex_rate_limits" || !c.Supported || c.Status != "available" || (c.Expires == nil || !c.Expires.After(time.Now())) {
				continue
			}
			if hashProviderCreditID(c.ID) == in.OfferKey {
				providerID = c.ID
				break
			}
		}
		if providerID == "" {
			writeJSON(w, 409, map[string]any{"ok": false, "error_code": "offer_unavailable"})
			return
		}
		guard, e := readResetUseGuard(o)
		if e != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error_code": "reset_guard_unavailable"})
			return
		}
		guardKey := hashProviderCreditID(in.AccountKey + ":" + in.OfferKey)
		if guard[guardKey] {
			writeJSON(w, 409, map[string]any{"ok": false, "error_code": "offer_already_attempted"})
			return
		}
		generateID := o.ResetUseIDGenerator
		if generateID == nil {
			generateID = newResetRedeemRequestID
		}
		id, e := generateID()
		if e != nil || !requestIDPattern.MatchString(id) {
			writeJSON(w, 503, map[string]any{"ok": false, "error_code": "reset_use_disabled"})
			return
		}
		guard[guardKey] = true
		if writeResetUseGuard(o, guard) != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error_code": "reset_guard_unavailable"})
			return
		}
		// Persisted attempt marker blocks repeats after ambiguity, even after
		// restart. Refresh usage after every attempted POST, never auto-retry.
		defer func() {
			rc, cc := context.WithTimeout(context.Background(), 30*time.Second)
			defer cc()
			state.lastRefresh = time.Time{}
			state.refreshLocked(rc, o)
		}()
		postURL := *base
		postURL.Path = strings.TrimSuffix(base.Path, "/rate-limit-reset-credits") + "/rate-limit-reset-credits/consume"
		payload, _ := json.Marshal(map[string]string{"credit_id": providerID, "redeem_request_id": id})
		post, e := http.NewRequestWithContext(ctx, "POST", postURL.String(), strings.NewReader(string(payload)))
		if e != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error_code": "provider_unavailable"})
			return
		}
		setProviderHeaders(post, creds)
		post.Header.Set("Content-Type", "application/json")
		noRedirect := *client
		noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		resp, e = noRedirect.Do(post)
		if e != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error_code": "outcome_uncertain"})
			return
		}
		defer resp.Body.Close()
		result, e := io.ReadAll(io.LimitReader(resp.Body, 65537))
		if e != nil || len(result) > 65536 {
			writeJSON(w, 502, map[string]any{"ok": false, "error_code": "outcome_uncertain"})
			return
		}
		var out struct {
			Code string `json:"code"`
		}
		// We never replay uncertain attempts, so already_redeemed cannot be
		// attributed to this request. Do not report it as a fresh success.
		if resp.StatusCode != http.StatusOK || json.Unmarshal(result, &out) != nil || out.Code != "reset" {
			writeJSON(w, 502, map[string]any{"ok": false, "error_code": "provider_unavailable"})
			return
		}
		// HTTP success alone is not enough: re-read the exact provider inventory.
		rc, cc := context.WithTimeout(context.Background(), 10*time.Second)
		defer cc()
		verify, err := client.Do(get.Clone(rc))
		if err != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error_code": "outcome_uncertain"})
			return
		}
		data, err := io.ReadAll(io.LimitReader(verify.Body, 1024*1024+1))
		verify.Body.Close()
		details := parseResetCreditDetails(data, time.Now())
		if err != nil || len(data) > 1024*1024 || verify.StatusCode != 200 || details == nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error_code": "outcome_uncertain"})
			return
		}
		for _, offer := range details.Offers {
			if offer.OfferKey == in.OfferKey {
				writeJSON(w, 502, map[string]any{"ok": false, "error_code": "outcome_uncertain"})
				return
			}
		}
		writeJSON(w, 200, map[string]any{"ok": true, "code": out.Code})
	}
}

// Advisory file lock serializes independent CBL processes as well as caches.
func lockResetUse(o Options) (func(), error) {
	path := resetUseGuardFile(o) + ".lock"
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

func hashProviderCreditID(s string) string {
	x := sha256.Sum256([]byte(s))
	return hex.EncodeToString(x[:])
}
func setProviderHeaders(r *http.Request, c Credentials) {
	r.Header.Set("Authorization", "Bearer "+c.AccessToken)
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", "cbl")
	if c.AccountID != "" {
		r.Header.Set("ChatGPT-Account-Id", c.AccountID)
		r.Header.Set("ChatGPT-Account-ID", c.AccountID)
	}
}
