package cbl

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestConfiguredClientDirectModeIgnoresEnvironmentProxy(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"proxy":"socks5h://127.0.0.1:2080","proxy_enabled":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CBL_PROXY", "socks5h://127.0.0.1:2080")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	client, err := newConfiguredHTTPClient(Options{ConfigFile: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	transport := client.Transport.(*http.Transport)
	if transport.Proxy != nil {
		t.Fatal("direct mode unexpectedly retained an environment proxy")
	}
}
