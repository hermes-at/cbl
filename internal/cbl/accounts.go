package cbl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var accountStorageMu sync.Mutex
var errAccountNotFound = errors.New("account not found")

func accountKey(creds Credentials, path string) string {
	identity := "account:" + strings.TrimSpace(creds.AccountID)
	if strings.TrimSpace(creds.AccountID) == "" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			absolute = filepath.Clean(path)
		}
		identity = "file:" + absolute
	}
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

func validAccountKey(key string) bool {
	if len(key) != 64 || strings.ToLower(key) != key {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}

func removedAccountsFile() string {
	return filepath.Join(mustHome(), ".config", "cbl", "removed-accounts.json")
}

// Refuse symlinked CBL storage directories before destructive or credential writes.
// A credential-file symlink can be unlinked safely; its target is never removed.
func checkAccountStorage(includeAccounts bool) error {
	paths := []string{filepath.Join(mustHome(), ".config"), filepath.Join(mustHome(), ".config", "cbl")}
	if includeAccounts {
		paths = append(paths, cblAccountsDir())
	}
	for _, path := range paths {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("CBL storage must use real directories")
		}
	}
	if info, err := os.Lstat(removedAccountsFile()); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("invalid removed-account state file")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func readRemovedAccounts() (map[string]bool, error) {
	if err := checkAccountStorage(false); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(removedAccountsFile())
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		Version int      `json:"version"`
		Keys    []string `json:"keys"`
	}
	if json.Unmarshal(data, &doc) != nil || doc.Version != 1 || doc.Keys == nil {
		return nil, errors.New("invalid removed-account state; restore or repair it before continuing")
	}
	removed := map[string]bool{}
	for _, key := range doc.Keys {
		if !validAccountKey(key) {
			return nil, errors.New("invalid removed-account key")
		}
		removed[key] = true
	}
	return removed, nil
}

func writeRemovedAccounts(removed map[string]bool) error {
	if err := checkAccountStorage(false); err != nil {
		return err
	}
	keys := make([]string, 0, len(removed))
	for key, yes := range removed {
		if yes {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	data, err := json.Marshal(struct {
		Version int      `json:"version"`
		Keys    []string `json:"keys"`
	}{1, keys})
	if err != nil {
		return err
	}
	return atomicWriteFile(removedAccountsFile(), data, 0600)
}

func nativeAccountFiles() []string {
	files, _ := filepath.Glob(filepath.Join(cblAccountsDir(), "*.json"))
	if _, err := os.Lstat(defaultCBLAuthFile()); err == nil {
		files = append(files, defaultCBLAuthFile())
	}
	return files
}

// Preserve a native-looking file if Codex CLI is configured to use the same
// actual path. A CBL symlink itself may still be safely unlinked.
func sharedCLIAuthFile(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	actual, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	candidates := []string{filepath.Join(mustHome(), ".codex", "auth.json"), filepath.Join(mustHome(), ".config", "codex", "auth.json")}
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		candidates = append(candidates, filepath.Join(home, "auth.json"))
	}
	for _, candidate := range candidates {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err == nil && filepath.Clean(resolved) == filepath.Clean(actual) {
			return true
		}
	}
	return false
}

// Resolve opaque keys only against enumerated credentials; no caller-provided path.
// Serialize this with refresh/login (cache.opMu) when called by the service.
func removeAccount(key string, opts Options) error {
	accountStorageMu.Lock()
	defer accountStorageMu.Unlock()
	if !validAccountKey(key) {
		return errors.New("invalid account key")
	}
	if err := checkAccountStorage(true); err != nil {
		return err
	}
	removed, err := readRemovedAccounts()
	if err != nil {
		return err
	}
	files := accountAuthFiles()
	explicit := firstNonEmpty(opts.AuthFile, os.Getenv("CBL_AUTH_FILE"))
	if explicit != "" {
		files = append(files, explicit)
	}
	files = append(files, nativeAccountFiles()...)
	found := false
	for _, path := range files {
		creds, _ := loadCredentials(Options{AuthFile: path})
		if accountKey(creds, path) == key {
			found = true
			break
		}
	}
	// Idempotence after a lost success response, but unknown keys remain 404.
	if !found && !removed[key] {
		return errAccountNotFound
	}
	removed[key] = true
	// Persist suppression first: failed/partial deletes must never re-import CLI auth.
	if err := writeRemovedAccounts(removed); err != nil {
		return err
	}
	for _, path := range nativeAccountFiles() {
		creds, _ := loadCredentials(Options{AuthFile: path})
		if accountKey(creds, path) != key {
			continue
		}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.IsDir() {
			return errors.New("account credential is a directory")
		}
		if sharedCLIAuthFile(path) {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove CBL credential: %w", err)
		}
	}
	return nil
}

func accountStatus(err error) string {
	if err == nil {
		return "ready"
	}
	var usage *UsageHTTPError
	if errors.As(err, &usage) {
		if usage.StatusCode == 401 {
			return "auth_required"
		}
		if usage.StatusCode == 429 {
			return "rate_limited"
		}
		return "error"
	}
	message := err.Error()
	if strings.Contains(message, "invalid_jwt") || strings.Contains(message, "invalid_refresh_token") || strings.Contains(message, "tokens.access_token / tokens.refresh_token are missing") {
		return "auth_required"
	}
	var network net.Error
	if errors.As(err, &network) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "connection_error"
	}
	return "error"
}
