package cbl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

type usageResponse struct {
	AccountID string `json:"account_id"`
	PlanType  string `json:"plan_type"`
	RateLimit struct {
		PrimaryWindow   *apiWindow `json:"primary_window"`
		SecondaryWindow *apiWindow `json:"secondary_window"`
		IndividualLimit *apiLimit   `json:"individual_limit"`
	} `json:"rate_limit"`
	Credits struct {
		HasCredits bool   `json:"has_credits"`
		Unlimited  bool   `json:"unlimited"`
		Balance    string `json:"balance"`
	} `json:"credits"`
	AdditionalRateLimits []struct {
		LimitName string     `json:"limit_name"`
		RateLimit *apiWindow `json:"rate_limit"`
	} `json:"additional_rate_limits"`
}

type apiWindow struct {
	UsedPercent       int   `json:"used_percent"`
	ResetAt           int64 `json:"reset_at"`
	LimitWindowSeconds int   `json:"limit_window_seconds"`
}

type apiLimit struct {
	Used             json.Number `json:"used"`
	Limit            json.Number `json:"limit"`
	RemainingPercent json.Number `json:"remaining_percent"`
	ResetsAt         *int64      `json:"resets_at"`
}

func Load(ctx context.Context, opts Options) (UsageSnapshot, error) {
	if fixture := strings.TrimSpace(opts.Fixture); fixture != "" {
		return loadFromFixture(fixture)
	}
	if fixture := strings.TrimSpace(os.Getenv("CBL_FIXTURE")); fixture != "" {
		return loadFromFixture(fixture)
	}
	creds, err := loadCredentials(opts)
	if err != nil {
		return UsageSnapshot{}, err
	}
	baseURL := loadBaseURL(opts)
	return fetchUsage(ctx, creds, baseURL)
}

func loadFromFixture(path string) (UsageSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return UsageSnapshot{}, err
	}
	return mapFromBytes(data, "fixture")
}

func fetchUsage(ctx context.Context, creds Credentials, baseURL string) (UsageSnapshot, error) {
	usageURL, err := resolveUsageURL(baseURL)
	if err != nil {
		return UsageSnapshot{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
	if err != nil {
		return UsageSnapshot{}, err
	}
	request.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "cbl")
	if creds.AccountID != "" {
		request.Header.Set("ChatGPT-Account-Id", creds.AccountID)
		request.Header.Set("ChatGPT-Account-ID", creds.AccountID)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(request)
	if err != nil {
		return UsageSnapshot{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return UsageSnapshot{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return UsageSnapshot{}, fmt.Errorf("codex usage request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return mapFromBytes(body, baseURL)
}

func mapFromBytes(data []byte, baseURL string) (UsageSnapshot, error) {
	var resp usageResponse
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&resp); err != nil {
		return UsageSnapshot{}, fmt.Errorf("decode usage response: %w", err)
	}
	return mapSnapshot(resp, baseURL), nil
}

func mapSnapshot(resp usageResponse, baseURL string) UsageSnapshot {
	now := time.Now().UTC()
	snap := UsageSnapshot{
		AccountID: resp.AccountID,
		PlanType:  resp.PlanType,
		FetchedAt: now,
		Source:    "oauth",
		BaseURL:   baseURL,
	}
	if resp.RateLimit.PrimaryWindow != nil {
		win := mapWindow(*resp.RateLimit.PrimaryWindow, "5h")
		snap.PrimaryWindow = &win
	}
	if resp.RateLimit.SecondaryWindow != nil {
		win := mapWindow(*resp.RateLimit.SecondaryWindow, "weekly")
		snap.SecondaryWindow = &win
	}
	if resp.RateLimit.IndividualLimit != nil {
		snap.IndividualLimit = mapLimit(*resp.RateLimit.IndividualLimit, now)
	}
	if bal := strings.TrimSpace(resp.Credits.Balance); bal != "" {
		if v, err := parseFloat(bal); err == nil {
			snap.CreditsBalance = &v
		}
	}
	for _, extra := range resp.AdditionalRateLimits {
		if extra.RateLimit == nil {
			continue
		}
		snap.AdditionalRates = append(snap.AdditionalRates, NamedWindow{
			Name:   extra.LimitName,
			Window: mapWindow(*extra.RateLimit, extra.LimitName),
		})
	}
	return snap
}

func mapWindow(raw apiWindow, label string) UsageWindow {
	var resetAt *time.Time
	if raw.ResetAt > 0 {
		t := time.Unix(raw.ResetAt, 0).UTC()
		resetAt = &t
	}
	return UsageWindow{
		UsedPercent:       clamp(raw.UsedPercent, 0, 100),
		ResetAt:           resetAt,
		LimitWindowSeconds: raw.LimitWindowSeconds,
		Label:             label,
		RemainingPercent:  clamp(100-raw.UsedPercent, 0, 100),
	}
}

func mapLimit(raw apiLimit, now time.Time) *CreditLimit {
	used, _ := raw.Used.Float64()
	limit, _ := raw.Limit.Float64()
	remaining := limit - used
	remainingPct, _ := raw.RemainingPercent.Float64()
	var resetAt *time.Time
	if raw.ResetsAt != nil && *raw.ResetsAt > 0 {
		t := time.Unix(*raw.ResetsAt, 0).UTC()
		resetAt = &t
	}
	return &CreditLimit{
		Used:             used,
		Limit:            limit,
		Remaining:        remaining,
		RemainingPercent: remainingPct,
		ResetsAt:         resetAt,
		UpdatedAt:        now,
	}
}

func resolveUsageURL(base string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if strings.Contains(parsed.String(), "/backend-api") {
		parsed.Path = path.Join(parsed.Path, "wham", "usage")
	} else {
		parsed.Path = path.Join(parsed.Path, "api", "codex", "usage")
	}
	if !strings.HasPrefix(parsed.Path, "/") {
		parsed.Path = "/" + parsed.Path
	}
	return parsed.String(), nil
}

func parseFloat(s string) (float64, error) {
	var v float64
	_, err := fmt.Sscan(strings.TrimSpace(s), &v)
	if err != nil {
		return 0, err
	}
	return v, nil
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

func (s UsageSnapshot) summaryLine() string {
	parts := make([]string, 0, 4)
	if s.PrimaryWindow != nil {
		parts = append(parts, fmt.Sprintf("5h %d%% left", s.PrimaryWindow.Remaining()))
	}
	if s.SecondaryWindow != nil {
		parts = append(parts, fmt.Sprintf("weekly %d%% left", s.SecondaryWindow.Remaining()))
	}
	if s.IndividualLimit != nil && s.IndividualLimit.Limit > 0 {
		parts = append(parts, fmt.Sprintf("credits %.0f/%.0f left", s.IndividualLimit.Remaining, s.IndividualLimit.Limit))
	} else if s.CreditsBalance != nil {
		parts = append(parts, fmt.Sprintf("credits %.2f", *s.CreditsBalance))
	}
	if len(parts) == 0 {
		return "Codex status unavailable"
	}
	return "Codex " + strings.Join(parts, " • ")
}

func (s UsageSnapshot) tooltip() string {
	lines := []string{s.summaryLine()}
	if s.PlanType != "" {
		lines = append(lines, "plan: "+s.PlanType)
	}
	if s.AccountID != "" {
		lines = append(lines, "account: "+s.AccountID)
	}
	if s.PrimaryWindow != nil && s.PrimaryWindow.ResetAt != nil {
		lines = append(lines, "5h reset: "+s.PrimaryWindow.ResetAt.Local().Format(time.RFC1123))
	}
	if s.SecondaryWindow != nil && s.SecondaryWindow.ResetAt != nil {
		lines = append(lines, "weekly reset: "+s.SecondaryWindow.ResetAt.Local().Format(time.RFC1123))
	}
	if s.IndividualLimit != nil && s.IndividualLimit.ResetsAt != nil {
		lines = append(lines, fmt.Sprintf("credits remaining: %.0f", s.IndividualLimit.Remaining))
		lines = append(lines, "credit reset: "+s.IndividualLimit.ResetsAt.Local().Format(time.RFC1123))
	}
	for _, extra := range s.AdditionalRates {
		lines = append(lines, fmt.Sprintf("%s: %d%% left", extra.Name, extra.Window.Remaining()))
	}
	lines = append(lines, "fetched: "+s.FetchedAt.Local().Format(time.RFC1123))
	return strings.Join(lines, "\n")
}

func (s UsageSnapshot) waybarClass() string {
	if s.PrimaryWindow == nil && s.SecondaryWindow == nil && s.IndividualLimit == nil && s.CreditsBalance == nil {
		return "error"
	}
	worst := 100
	if s.PrimaryWindow != nil && s.PrimaryWindow.Remaining() < worst {
		worst = s.PrimaryWindow.Remaining()
	}
	if s.SecondaryWindow != nil && s.SecondaryWindow.Remaining() < worst {
		worst = s.SecondaryWindow.Remaining()
	}
	if s.IndividualLimit != nil && s.IndividualLimit.RemainingPercent < float64(worst) {
		worst = int(s.IndividualLimit.RemainingPercent)
	}
	switch {
	case worst <= 10:
		return "critical"
	case worst <= 30:
		return "warning"
	default:
		return "good"
	}
}
