package cbl

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Optional read-only enrichment. Never sends a redemption request.
func fetchResetCreditDetails(ctx context.Context, client *http.Client, usageRequest *http.Request) *ResetCreditStatus {
	if !strings.HasSuffix(usageRequest.URL.Path, "/usage") {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request := usageRequest.Clone(ctx)
	request.URL.Path = strings.TrimSuffix(request.URL.Path, "/usage") + "/rate-limit-reset-credits"
	request.URL.RawPath = ""
	request.URL.RawQuery = ""
	response, err := client.Do(request)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	if err != nil {
		return nil
	}
	return parseResetCreditDetails(body, time.Now())
}

// Strip provider IDs, profile identifiers, images and untrusted display strings.
func parseResetCreditDetails(body []byte, now time.Time) *ResetCreditStatus {
	var raw struct {
		Available *int `json:"available_count"`
		Credits   []struct {
			ID        string     `json:"id"`
			Type      string     `json:"reset_type"`
			Status    string     `json:"status"`
			Supported bool       `json:"is_supported_by_plan"`
			Expires   *time.Time `json:"expires_at"`
		} `json:"credits"`
	}
	if json.Unmarshal(body, &raw) != nil || raw.Available == nil || *raw.Available < 0 {
		return nil
	}
	result := &ResetCreditStatus{AvailableCount: raw.Available, State: "ready", Offers: []ResetCreditOffer{}}
	seen := map[string]bool{}
	for _, credit := range raw.Credits {
		if credit.ID == "" || seen[credit.ID] || credit.Status != "available" || !credit.Supported || (credit.Expires != nil && !credit.Expires.After(now)) {
			continue
		}
		seen[credit.ID] = true
		scope := "unknown"
		if credit.Type == "codex_rate_limits" {
			scope = "full"
		}
		result.Offers = append(result.Offers, ResetCreditOffer{OfferKey: fmt.Sprintf("%x", sha256.Sum256([]byte(credit.ID))), Scope: scope, ExpiresAt: credit.Expires})
	}
	return result
}
