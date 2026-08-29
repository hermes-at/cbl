package cbl

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func resolvedProxy(raw string) string {
	if raw = strings.TrimSpace(raw); raw != "" {
		return raw
	}
	return strings.TrimSpace(os.Getenv("CBL_PROXY"))
}

func newHTTPClient(proxy string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSHandshakeTimeout = 15 * time.Second
	transport.ExpectContinueTimeout = 1 * time.Second
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.Proxy = http.ProxyFromEnvironment

	proxy = resolvedProxy(proxy)
	if proxy == "" {
		return &http.Client{Transport: transport, Timeout: 30 * time.Second}, nil
	}

	parsed, err := parseProxyURL(proxy)
	if err != nil {
		return nil, err
	}

	switch parsed.Scheme {
	case "http", "https":
		transport.Proxy = http.ProxyURL(parsed)
	case "socks5", "socks5h":
		transport.Proxy = nil
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialSOCKS5(ctx, parsed.Host, addr, parsed.Scheme == "socks5h")
		}
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", parsed.Scheme)
	}

	return &http.Client{Transport: transport, Timeout: 30 * time.Second}, nil
}

func parseProxyURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("proxy is empty")
	}
	if strings.Contains(raw, "://") {
		return url.Parse(raw)
	}
	for _, scheme := range []string{"socks5h", "socks5", "http", "https"} {
		prefix := scheme + ":"
		if strings.HasPrefix(strings.ToLower(raw), prefix) {
			return url.Parse(scheme + "://" + raw[len(prefix):])
		}
	}
	return nil, fmt.Errorf("proxy must include a scheme, e.g. socks5h://127.0.0.1:2080")
}

func dialSOCKS5(ctx context.Context, proxyAddr, target string, remoteDNS bool) (net.Conn, error) {
	d := &net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	var hello [2]byte
	if _, err := io.ReadFull(conn, hello[:]); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if hello[0] != 0x05 || hello[1] != 0x00 {
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 proxy rejected no-auth handshake")
	}

	host, port, err := net.SplitHostPort(target)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	addr, err := encodeSOCKS5Addr(host, port, remoteDNS)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	req := append([]byte{0x05, 0x01, 0x00}, addr...)
	if _, err := conn.Write(req); err != nil {
		_ = conn.Close()
		return nil, err
	}
	var resp [4]byte
	if _, err := io.ReadFull(conn, resp[:]); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if resp[1] != 0x00 {
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 proxy connect failed: rep=%d", resp[1])
	}
	if err := discardSOCKS5Addr(conn, resp[3]); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func encodeSOCKS5Addr(host, port string, remoteDNS bool) ([]byte, error) {
	if remoteDNS {
		if len(host) > 255 {
			return nil, fmt.Errorf("proxy host too long")
		}
		p := make([]byte, 0, 2+len(host)+2)
		p = append(p, 0x03, byte(len(host)))
		p = append(p, host...)
		p = append(p, mustPortBytes(port)...)
		return p, nil
	}

	ip := net.ParseIP(host)
	if ip == nil {
		resolved, err := net.LookupIP(host)
		if err != nil {
			return nil, err
		}
		if len(resolved) == 0 {
			return nil, fmt.Errorf("no IPs resolved for %q", host)
		}
		ip = resolved[0]
	}
	if v4 := ip.To4(); v4 != nil {
		return append([]byte{0x01}, append(v4, mustPortBytes(port)...)...), nil
	}
	if v6 := ip.To16(); v6 != nil {
		return append([]byte{0x04}, append(v6, mustPortBytes(port)...)...), nil
	}
	return nil, fmt.Errorf("invalid IP %q", host)
}

func mustPortBytes(port string) []byte {
	n, err := net.LookupPort("tcp", port)
	if err != nil {
		return []byte{0, 0}
	}
	return []byte{byte(n >> 8), byte(n)}
}

func discardSOCKS5Addr(r io.Reader, atyp byte) error {
	switch atyp {
	case 0x01:
		_, err := io.CopyN(io.Discard, r, 6)
		return err
	case 0x03:
		var l [1]byte
		if _, err := io.ReadFull(r, l[:]); err != nil {
			return err
		}
		_, err := io.CopyN(io.Discard, r, int64(l[0])+2)
		return err
	case 0x04:
		_, err := io.CopyN(io.Discard, r, 18)
		return err
	default:
		return fmt.Errorf("unknown socks5 address type %d", atyp)
	}
}
