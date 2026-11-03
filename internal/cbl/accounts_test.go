package cbl

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func accountTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, key := range []string{"CODEX_HOME", "CBL_AUTH_FILE", "CBL_FIXTURE", "CBL_CONFIG_FILE", "CBL_PROXY"} {
		t.Setenv(key, "")
	}
}

func TestCodexHomeOverlapPreservesCLIAuth(t *testing.T) {
	accountTestHome(t)
	t.Setenv("CODEX_HOME", filepath.Dir(defaultCBLAuthFile()))
	creds := Credentials{AccountID: "shared", AccessToken: "a", RefreshToken: "r"}
	if err := saveCredentials(defaultCBLAuthFile(), creds); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(defaultCBLAuthFile())
	if err := removeAccount(accountKey(creds, defaultCBLAuthFile()), Options{}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(defaultCBLAuthFile())
	if err != nil || string(before) != string(after) {
		t.Fatal("CODEX_HOME session modified", err)
	}
	removed, err := readRemovedAccounts()
	if err != nil || !removed[accountKey(creds, defaultCBLAuthFile())] {
		t.Fatal("shared account not suppressed in CBL", err)
	}
}

func TestMalformedAccountKeysAreIndependent(t *testing.T) {
	accountTestHome(t)
	if err := os.MkdirAll(cblAccountsDir(), 0700); err != nil {
		t.Fatal(err)
	}
	first, second := filepath.Join(cblAccountsDir(), "bad1.json"), filepath.Join(cblAccountsDir(), "bad2.json")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("{broken"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	key := accountKey(Credentials{}, first)
	if key == accountKey(Credentials{}, second) {
		t.Fatal("distinct bad files share a key")
	}
	if err := removeAccount(key, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatal("selected bad file survived")
	}
	if data, err := os.ReadFile(second); err != nil || string(data) != "{broken" {
		t.Fatal("other bad file changed", err)
	}
}

func TestAccountRemovalPreservesExternalAndAllowsExplicitReadd(t *testing.T) {
	accountTestHome(t)
	creds := Credentials{AccountID: "first", AccessToken: "a", RefreshToken: "r"}
	path, err := saveAccountCredentials(creds)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveCredentials(defaultCBLAuthFile(), creds); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(mustHome(), ".codex", "auth.json")
	if err := saveCredentials(external, creds); err != nil {
		t.Fatal(err)
	}
	other, err := saveAccountCredentials(Credentials{AccountID: "other", AccessToken: "b", RefreshToken: "s"})
	if err != nil {
		t.Fatal(err)
	}
	externalBefore, _ := os.ReadFile(external)
	otherBefore, _ := os.ReadFile(other)
	if err := removeAccount(accountKey(creds, path), Options{}); err != nil {
		t.Fatal(err)
	}
	for _, deleted := range []string{path, defaultCBLAuthFile()} {
		if _, err := os.Stat(deleted); !os.IsNotExist(err) {
			t.Fatalf("not removed: %s: %v", deleted, err)
		}
	}
	for file, want := range map[string]string{external: string(externalBefore), other: string(otherBefore)} {
		got, _ := os.ReadFile(file)
		if string(got) != want {
			t.Fatal("other/external data modified")
		}
	}
	removed, err := readRemovedAccounts()
	if err != nil || !removed[accountKey(creds, path)] {
		t.Fatalf("tombstone missing: %v", err)
	}
	info, err := os.Stat(removedAccountsFile())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("tombstone permissions: %v", err)
	}
	if _, err := saveAccountCredentials(creds); err != nil {
		t.Fatal(err)
	}
	removed, err = readRemovedAccounts()
	if err != nil || removed[accountKey(creds, path)] {
		t.Fatalf("explicit readd still hidden: %v", err)
	}
}

func TestAccountRemovalBadFileAndSymlinkSafety(t *testing.T) {
	accountTestHome(t)
	if err := os.MkdirAll(cblAccountsDir(), 0700); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(cblAccountsDir(), "broken.json")
	os.WriteFile(bad, []byte("{broken"), 0600)
	if err := removeAccount(accountKey(Credentials{}, bad), Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatal("bad file survived")
	}
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.json")
	creds := Credentials{AccountID: "outside", AccessToken: "a", RefreshToken: "r"}
	if err := saveCredentials(outsideFile, creds); err != nil {
		t.Fatal(err)
	}
	os.Remove(cblAccountsDir())
	if err := os.Symlink(outside, cblAccountsDir()); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(outsideFile)
	if err := removeAccount(accountKey(creds, outsideFile), Options{}); err == nil {
		t.Fatal("symlink directory accepted")
	}
	after, _ := os.ReadFile(outsideFile)
	if string(before) != string(after) {
		t.Fatal("external target touched")
	}
}

func TestRemovedAccountStateCorruptionFailsClosed(t *testing.T) {
	accountTestHome(t)
	os.MkdirAll(filepath.Dir(removedAccountsFile()), 0700)
	for _, content := range []string{"{bad", `{"version":1,"keys":["../auth.json"]}`, `null`} {
		os.WriteFile(removedAccountsFile(), []byte(content), 0600)
		if _, err := LoadAll(context.Background(), Options{}); err == nil {
			t.Fatalf("corrupt removal state accepted: %s", content)
		}
		if _, err := saveAccountCredentials(Credentials{AccountID: "first", AccessToken: "a", RefreshToken: "r"}); err == nil {
			t.Fatal("readd overwrote corrupt tombstones")
		}
	}
}

func TestAccountAPIRejectsBadKeysAndUpdatesCache(t *testing.T) {
	accountTestHome(t)
	creds := Credentials{AccountID: "first", AccessToken: "a", RefreshToken: "r"}
	path, err := saveAccountCredentials(creds)
	if err != nil {
		t.Fatal(err)
	}
	key := accountKey(creds, path)
	state := &cache{}
	state.setAll([]UsageSnapshot{{AccountID: "first", AccountKey: key, Status: "ready"}, {AccountID: "other", AccountKey: strings.Repeat("b", 64)}}, nil)
	mux := http.NewServeMux()
	registerUIHandlers(mux, state, Options{})
	for _, tc := range []struct {
		method, key string
		want        int
	}{{"GET", key, 405}, {"DELETE", "../../auth.json", 400}, {"DELETE", strings.Repeat("f", 64), 404}} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(tc.method, "/api/accounts?key="+tc.key, nil))
		if response.Code != tc.want {
			t.Fatalf("%s %s: %d %s", tc.method, tc.key, response.Code, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("DELETE", "/api/accounts?key="+key, nil))
	if response.Code != 200 {
		t.Fatalf("delete: %d %s", response.Code, response.Body.String())
	}
	snapshots, _ := state.getAll()
	if len(snapshots) != 1 || snapshots[0].AccountID != "other" {
		t.Fatalf("unexpected cache: %+v", snapshots)
	}
}

func TestAccountStatusClassification(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "ready"},
		{&UsageHTTPError{StatusCode: 401}, "auth_required"},
		{&UsageHTTPError{StatusCode: 429}, "rate_limited"},
		{&UsageHTTPError{StatusCode: 503}, "error"},
		{errors.New("invalid_refresh_token"), "auth_required"},
		{errors.New("auth.json exists but tokens.access_token / tokens.refresh_token are missing"), "auth_required"},
		{&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("refused")}, "connection_error"},
		{context.DeadlineExceeded, "connection_error"},
		{errors.New("bad json"), "error"},
	} {
		if got := accountStatus(tc.err); got != tc.want {
			t.Fatalf("%v: %s wanted %s", tc.err, got, tc.want)
		}
	}
}

func TestDeleteSerializesWithRefresh(t *testing.T) {
	accountTestHome(t)
	creds := Credentials{AccountID: "first", AccessToken: "a", RefreshToken: "r"}
	path, err := saveAccountCredentials(creds)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"account_id":"first","plan_type":"plus"}`))
	}))
	defer upstream.Close()
	opts := Options{BaseURL: upstream.URL}
	state := &cache{}
	refreshed := make(chan struct{})
	go func() { defer close(refreshed); state.refresh(context.Background(), opts) }()
	<-started
	mux := http.NewServeMux()
	registerUIHandlers(mux, state, opts)
	deleted := make(chan struct{})
	response := httptest.NewRecorder()
	go func() {
		defer close(deleted)
		mux.ServeHTTP(response, httptest.NewRequest("DELETE", "/api/accounts?key="+accountKey(creds, path), nil))
	}()
	cacheSnapshot := state.payload()
	if cacheSnapshot["refreshing"] != true {
		t.Error("refresh activity not exposed")
	}
	close(release)
	select {
	case <-deleted:
	case <-time.After(3 * time.Second):
		t.Fatal("delete stuck")
	}
	<-refreshed
	if response.Code != 200 {
		t.Fatalf("delete: %d %s", response.Code, response.Body.String())
	}
	state.lastRefresh = time.Time{}
	state.refresh(context.Background(), opts)
	snaps, err := state.getAll()
	if err != nil || len(snaps) != 0 {
		t.Fatalf("resurrected: %+v %v", snaps, err)
	}
}
