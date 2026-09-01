package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/getlantern/systray"
)

const (
	waybarURL        = "http://127.0.0.1:18088/waybar"
	statusURL        = "http://127.0.0.1:18088/api/status"
	loginStartURL    = "http://127.0.0.1:18088/api/login/start"
	loginCompleteURL = "http://127.0.0.1:18088/api/login/complete"
)

type statusPayload struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error"`
	Text      string `json:"text"`
	Tooltip   string `json:"tooltip"`
	Class     string `json:"class"`
	Plan      string `json:"plan"`
	AccountID string `json:"account_id"`
	Profile   string `json:"profile"`
	FetchedAt string `json:"fetched_at"`
	Windows   []struct {
		Title     string `json:"title"`
		Remaining int    `json:"remaining"`
		Used      int    `json:"used"`
		Reset     string `json:"reset"`
	} `json:"windows"`
	Credits struct {
		Text string `json:"text"`
		Used int    `json:"used"`
	} `json:"credits"`
}

type loginStartPayload struct {
	OK              bool   `json:"ok"`
	Error           string `json:"error"`
	ID              string `json:"id"`
	VerificationURL string `json:"verification_url"`
	UserCode        string `json:"user_code"`
}

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetTitle("cbl")
	systray.SetTooltip("cbl")
	systray.SetIcon(makeIcon())

	provider := systray.AddMenuItem("Codex", "Current provider")
	provider.Disable()
	updated := systray.AddMenuItem("Updated: —", "Last usage refresh")
	updated.Disable()
	account := systray.AddMenuItem("Account: —", "Active account")
	account.Disable()
	systray.AddSeparator()

	sessionTitle := systray.AddMenuItem("Session", "5 hour limit")
	sessionTitle.Disable()
	sessionBar := systray.AddMenuItem("────────────", "Session usage")
	sessionBar.Disable()
	sessionMeta := systray.AddMenuItem("—", "Session reset")
	sessionMeta.Disable()

	weeklyTitle := systray.AddMenuItem("Weekly", "Weekly limit")
	weeklyTitle.Disable()
	weeklyBar := systray.AddMenuItem("────────────", "Weekly usage")
	weeklyBar.Disable()
	weeklyMeta := systray.AddMenuItem("—", "Weekly reset")
	weeklyMeta.Disable()

	extraTitle := systray.AddMenuItem("Extra usage", "Credit usage")
	extraTitle.Disable()
	extraBar := systray.AddMenuItem("────────────", "Credit usage")
	extraBar.Disable()
	extraMeta := systray.AddMenuItem("—", "Credits")
	extraMeta.Disable()
	systray.AddSeparator()

	addAccount := systray.AddMenuItem("🔑 Add Account…", "Start CBL login from the bar")
	loginCode := systray.AddMenuItem("Code: —", "Device login code")
	loginCode.Disable()
	completeLogin := systray.AddMenuItem("✓ I confirmed login", "Finish CBL login after browser approval")
	completeLogin.Disable()
	refresh := systray.AddMenuItem("↻ Refresh", "Refresh now")
	statusLine := systray.AddMenuItem("Status: local service", "CBL service status")
	statusLine.Disable()
	systray.AddSeparator()
	settings := systray.AddMenuItem("Settings…", "Settings are coming soon")
	settings.Disable()
	about := systray.AddMenuItem("About CBL", "About CBL")
	about.Disable()
	quit := systray.AddMenuItem("Quit", "Exit cbl tray")

	var loginID string
	items := []*systray.MenuItem{updated, account, sessionBar, sessionMeta, weeklyBar, weeklyMeta, extraBar, extraMeta, statusLine}
	setUnavailable := func(err error) {
		label := fmt.Sprintf("cbl unavailable: %v", err)
		systray.SetTooltip(label)
		statusLine.SetTitle(label)
		for _, item := range items[:8] {
			item.SetTitle("—")
		}
	}
	refreshStatus := func() {
		payload, err := fetchStatus()
		if err != nil {
			setUnavailable(err)
			return
		}
		if !payload.OK {
			setUnavailable(fmt.Errorf(payload.Error))
			return
		}
		systray.SetTooltip(payload.Tooltip)
		if payload.Text != "" {
			systray.SetTitle(shortTitle(payload.Text))
		}
		updated.SetTitle("Updated just now")
		if payload.FetchedAt != "" {
			updated.SetTitle("Updated: " + since(payload.FetchedAt))
		}
		acct := payload.AccountID
		if len(acct) > 8 {
			acct = acct[:8] + "…"
		}
		if acct == "" {
			acct = "—"
		}
		plan := payload.Plan
		if plan == "" {
			plan = "unknown"
		}
		account.SetTitle("Plan: " + plan + "     Account: " + acct)
		setWindow(sessionBar, sessionMeta, payload.Windows, 0)
		setWindow(weeklyBar, weeklyMeta, payload.Windows, 1)
		extraBar.SetTitle(progressLine(100-payload.Credits.Used, payload.Credits.Used))
		extraMeta.SetTitle("This month: " + emptyDash(payload.Credits.Text))
		statusLine.SetTitle("Status: OK")
	}

	go func() {
		refreshStatus()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-refresh.ClickedCh:
				refreshStatus()
			case <-addAccount.ClickedCh:
				loginCode.SetTitle("Requesting login code…")
				start, err := startLogin()
				if err != nil {
					loginCode.SetTitle("Login failed: " + err.Error())
					completeLogin.Disable()
					continue
				}
				loginID = start.ID
				loginCode.SetTitle("Code: " + start.UserCode)
				completeLogin.Enable()
				openURL(start.VerificationURL)
			case <-completeLogin.ClickedCh:
				if loginID == "" {
					continue
				}
				loginCode.SetTitle("Waiting for confirmation…")
				if err := finishLogin(loginID); err != nil {
					loginCode.SetTitle("Login failed: " + err.Error())
					continue
				}
				loginID = ""
				loginCode.SetTitle("Login saved")
				completeLogin.Disable()
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

func fetchStatus() (statusPayload, error) {
	resp, err := http.Get(statusURL)
	if err != nil {
		return statusPayload{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var payload statusPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return statusPayload{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if payload.Error == "" {
			payload.Error = strings.TrimSpace(string(data))
		}
		return payload, nil
	}
	return payload, nil
}

func startLogin() (loginStartPayload, error) {
	resp, err := http.Post(loginStartURL, "application/json", nil)
	if err != nil {
		return loginStartPayload{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var payload loginStartPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return loginStartPayload{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 || !payload.OK {
		if payload.Error == "" {
			payload.Error = strings.TrimSpace(string(data))
		}
		return loginStartPayload{}, fmt.Errorf(payload.Error)
	}
	return payload, nil
}

func finishLogin(id string) error {
	req, err := http.NewRequest(http.MethodPost, loginCompleteURL+"?id="+id, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var payload struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &payload)
		if payload.Error == "" {
			payload.Error = strings.TrimSpace(string(data))
		}
		return fmt.Errorf(payload.Error)
	}
	return nil
}

func setWindow(bar, meta *systray.MenuItem, windows []struct {
	Title     string `json:"title"`
	Remaining int    `json:"remaining"`
	Used      int    `json:"used"`
	Reset     string `json:"reset"`
}, idx int) {
	if len(windows) <= idx {
		bar.SetTitle("────────────")
		meta.SetTitle("—")
		return
	}
	w := windows[idx]
	bar.SetTitle(progressLine(w.Remaining, w.Used))
	reset := ""
	if w.Reset != "" {
		reset = "     Resets: " + w.Reset
	}
	meta.SetTitle(fmt.Sprintf("%d%% used%s", w.Used, reset))
}

func progressLine(remaining, used int) string {
	used = clamp(used, 0, 100)
	remaining = clamp(remaining, 0, 100)
	filled := clamp((used+4)/8, 0, 12)
	if used > 0 && filled == 0 {
		filled = 1
	}
	return strings.Repeat("●", filled) + strings.Repeat("─", 12-filled) + fmt.Sprintf("  %d%% used", used)
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func since(value string) string {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return "just now"
	}
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}

func shortTitle(text string) string {
	text = strings.TrimPrefix(text, "Codex ")
	if len(text) > 48 {
		return text[:48] + "…"
	}
	return text
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func openURL(url string) {
	_ = exec.Command("xdg-open", url).Start()
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
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
