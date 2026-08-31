package cbl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type cache struct {
	mu    sync.RWMutex
	snap  UsageSnapshot
	snaps []UsageSnapshot
	err   error
}

func (c *cache) set(s UsageSnapshot, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snap = s
	c.snaps = []UsageSnapshot{s}
	c.err = err
}

func (c *cache) setAll(snaps []UsageSnapshot, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snaps = append([]UsageSnapshot(nil), snaps...)
	if len(snaps) > 0 {
		c.snap = snaps[0]
	} else {
		c.snap = UsageSnapshot{}
	}
	c.err = err
}

func (c *cache) get() (UsageSnapshot, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snap, c.err
}

func (c *cache) getAll() ([]UsageSnapshot, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]UsageSnapshot(nil), c.snaps...), c.err
}

func Watch(ctx context.Context, interval time.Duration, opts Options, out ioWriter) error {
	refresh := func() error {
		snap, err := Load(ctx, opts)
		if err != nil {
			return err
		}
		return Render(out, snap, RenderOptions{Waybar: opts.Waybar})
	}
	if err := refresh(); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := refresh(); err != nil {
				return err
			}
		}
	}
}

func Serve(ctx context.Context, addr string, interval time.Duration, opts Options) error {
	state := &cache{}
	refresh := func() {
		snaps, err := LoadAll(ctx, opts)
		state.setAll(snaps, err)
	}
	refresh()
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				refresh()
			}
		}
	}()

	mux := http.NewServeMux()
	registerUIHandlers(mux, state, opts)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		snap, err := state.get()
		payload := map[string]any{"ok": err == nil}
		if err != nil {
			payload["error"] = err.Error()
		} else {
			payload["updated_at"] = snap.FetchedAt
		}
		_ = json.NewEncoder(w).Encode(payload)
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		snap, err := state.get()
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(snap)
	})
	mux.HandleFunc("/text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		snap, err := state.get()
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, err.Error())
			return
		}
		fmt.Fprintln(w, snap.summaryLine())
	})
	mux.HandleFunc("/waybar", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		snap, err := state.get()
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"text": "Codex error", "tooltip": err.Error(), "class": "error"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"text": snap.summaryLine(), "tooltip": snap.tooltip(), "class": snap.waybarClass()})
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

type ioWriter interface{ Write([]byte) (int, error) }
