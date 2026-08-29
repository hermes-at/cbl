package cbl

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestHTTPClientUsesSOCKS5HProxy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer target.Close()

	targetAddr := target.Listener.Addr().String()
	host, port, err := net.SplitHostPort(targetAddr)
	if err != nil {
		t.Fatal(err)
	}
	_ = host

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		var hdr [2]byte
		if _, err := io.ReadFull(conn, hdr[:]); err != nil {
			t.Error(err)
			return
		}
		if hdr[0] != 0x05 {
			t.Errorf("bad socks version: %d", hdr[0])
			return
		}
		methods := make([]byte, int(hdr[1]))
		if _, err := io.ReadFull(conn, methods); err != nil {
			t.Error(err)
			return
		}
		if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
			t.Error(err)
			return
		}

		var reqHdr [4]byte
		if _, err := io.ReadFull(conn, reqHdr[:]); err != nil {
			t.Error(err)
			return
		}
		if reqHdr[0] != 0x05 || reqHdr[1] != 0x01 || reqHdr[3] != 0x03 {
			t.Errorf("unexpected socks request header: %v", reqHdr)
			return
		}
		var hostLen [1]byte
		if _, err := io.ReadFull(conn, hostLen[:]); err != nil {
			t.Error(err)
			return
		}
		hostBytes := make([]byte, int(hostLen[0]))
		if _, err := io.ReadFull(conn, hostBytes); err != nil {
			t.Error(err)
			return
		}
		portBytes := make([]byte, 2)
		if _, err := io.ReadFull(conn, portBytes); err != nil {
			t.Error(err)
			return
		}
		if string(hostBytes) != "example.test" {
			t.Errorf("proxy saw host %q, want example.test", hostBytes)
			return
		}
		wantPort := mustPortInt(port)
		gotPort := int(portBytes[0])<<8 | int(portBytes[1])
		if gotPort != wantPort {
			t.Errorf("proxy saw port %d, want %d", gotPort, wantPort)
			return
		}

		backend, err := net.Dial("tcp", targetAddr)
		if err != nil {
			t.Error(err)
			return
		}
		defer backend.Close()
		if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
			t.Error(err)
			return
		}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = io.Copy(backend, conn)
		}()
		go func() {
			defer wg.Done()
			_, _ = io.Copy(conn, backend)
		}()
		wg.Wait()
	}()

	client, err := newHTTPClient("socks5h://" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, "http://example.test:"+port+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Close = true
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}
	<-done
}

func mustPortInt(port string) int {
	var n int
	_, err := fmt.Sscanf(port, "%d", &n)
	if err != nil {
		return -1
	}
	return n
}
