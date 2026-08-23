package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"time"

	"github.com/getlantern/systray"
)

const waybarURL = "http://127.0.0.1:18088/waybar"

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetTitle("cbl")
	systray.SetTooltip("cbl")
	systray.SetIcon(makeIcon())

	status := systray.AddMenuItem("cbl starting…", "Codex status")
	refresh := systray.AddMenuItem("Refresh", "Refresh now")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit", "Exit cbl tray")

	refreshStatus := func() {
		text, tooltip, err := fetch()
		if err != nil {
			label := fmt.Sprintf("cbl unavailable: %v", err)
			status.SetTitle(label)
			systray.SetTooltip(label)
			return
		}
		status.SetTitle(text)
		systray.SetTooltip(tooltip)
	}

	go func() {
		refreshStatus()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-refresh.ClickedCh:
				refreshStatus()
			case <-ticker.C:
				refreshStatus()
			case <-quit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func onExit() {}

func fetch() (string, string, error) {
	resp, err := http.Get(waybarURL)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("status %s: %s", resp.Status, string(body))
	}
	var payload struct {
		Text    string `json:"text"`
		Tooltip string `json:"tooltip"`
		Class   string `json:"class"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", err
	}
	if payload.Text == "" {
		payload.Text = "cbl"
	}
	if payload.Tooltip == "" {
		payload.Tooltip = payload.Text
	}
	return payload.Text, payload.Tooltip, nil
}

func makeIcon() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	bg := color.RGBA{R: 0x11, G: 0x74, B: 0xea, A: 0xff}
	fg := color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, bg)
		}
	}
	for y := 4; y < 12; y++ {
		for x := 4; x < 12; x++ {
			if x == 4 || x == 11 || y == 4 || y == 11 || x == y || x+y == 15 {
				img.Set(x, y, fg)
			}
		}
	}
	var buf []byte
	out := &byteWriter{buf: &buf}
	_ = png.Encode(out, img)
	return buf
}

type byteWriter struct{ buf *[]byte }

func (w *byteWriter) Write(p []byte) (int, error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}
