package cbl

import (
	"bytes"
	"encoding/json"
	"time"
)

// ResetCreditStatus keeps inventory distinct from per-offer details and
// applicability. Missing counters are unknown, not a verified zero balance.
type ResetCreditStatus struct {
	AvailableCount           *int               `json:"available_count"`
	ApplicableAvailableCount *int               `json:"applicable_available_count"`
	State                    string             `json:"state"`
	Offers                   []ResetCreditOffer `json:"offers"`
}

type ResetCreditOffer struct {
	OfferKey  string     `json:"offer_key"`
	Scope     string     `json:"scope"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func parseResetCreditSummary(raw json.RawMessage) *ResetCreditStatus {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	result := &ResetCreditStatus{State: "unavailable", Offers: []ResetCreditOffer{}}
	var counts struct {
		Available  *int `json:"available_count"`
		Applicable *int `json:"applicable_available_count"`
	}
	if json.Unmarshal(raw, &counts) != nil {
		return result
	}
	if counts.Available != nil && *counts.Available >= 0 {
		result.AvailableCount = counts.Available
	}
	if counts.Applicable != nil && *counts.Applicable >= 0 {
		result.ApplicableAvailableCount = counts.Applicable
	}
	return result
}
