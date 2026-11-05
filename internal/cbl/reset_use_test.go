package cbl

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResetUseExplicitlyUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	registerUIHandlers(mux, &cache{}, Options{})
	for _, tc := range []struct {
		method, origin string
		want           int
	}{
		{"POST", "", 501}, {"GET", "", 405}, {"POST", "https://untrusted.example", 403},
	} {
		r := httptest.NewRequest(tc.method, "http://127.0.0.1:18088/api/reset-credits/use", strings.NewReader(`{"account_key":"synthetic","offer_key":"synthetic"}`))
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		localOnly(mux).ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s: status%d, want%d", tc.method, w.Code, tc.want)
		}
		if tc.want == 501 && !strings.Contains(w.Body.String(), `"error_code":"reset_unavailable"`) {
			t.Fatal("must explicitly report unavailable, never fake success")
		}
	}
}
