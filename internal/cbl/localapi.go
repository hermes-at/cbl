package cbl

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// A loopback bind alone does not protect a local API from browser DNS rebinding.
// Native desktop clients have no Origin; browser requests must be same-origin.
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		name := host
		if h, _, err := net.SplitHostPort(host); err == nil {
			name = h
		}
		name = strings.Trim(name, "[]")
		ip := net.ParseIP(name)
		if name != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "local requests only", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Scheme != "http" || u.Host != host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
				http.Error(w, "cross-origin request forbidden", http.StatusForbidden)
				return
			}
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			http.Error(w, "cross-site request forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
