package cbl

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDashboardPayloadIncludesBarsAndCredits(t *testing.T) {
	snap := UsageSnapshot{
		AccountID:       "acct-1",
		PlanType:        "plus",
		FetchedAt:       time.Unix(0, 0).UTC(),
		PrimaryWindow:   &UsageWindow{UsedPercent: 19, RemainingPercent: 81},
		SecondaryWindow: &UsageWindow{UsedPercent: 13, RemainingPercent: 87},
		CreditsBalance:  floatPtr(0),
	}
	payload := dashboardPayload(snap, nil)
	if payload["ok"] != true {
		t.Fatalf("unexpected ok: %#v", payload["ok"])
	}
	cards := payload["windows"].([]map[string]any)
	if len(cards) != 2 {
		t.Fatalf("cards = %#v", cards)
	}
	if cards[0]["remaining"] != 81 || cards[1]["remaining"] != 87 {
		t.Fatalf("unexpected cards: %#v", cards)
	}
	credits := payload["credits"].(map[string]any)
	if credits["text"] != "0.00" {
		t.Fatalf("credits = %#v", credits)
	}
}

func TestDashboardPayloadIncludesMultipleAccounts(t *testing.T) {
	snaps := []UsageSnapshot{
		{AccountID: "acct-1", AccountEmail: "max@example.com", PlanType: "plus", PrimaryWindow: &UsageWindow{UsedPercent: 10}, SecondaryWindow: &UsageWindow{UsedPercent: 20}},
		{AccountID: "acct-2", PlanType: "pro", PrimaryWindow: &UsageWindow{UsedPercent: 30}, SecondaryWindow: &UsageWindow{UsedPercent: 40}},
	}
	payload := dashboardPayloadAll(snaps, nil)
	accounts := payload["accounts"].([]map[string]any)
	if len(accounts) != 2 {
		t.Fatalf("accounts = %#v", accounts)
	}
	if accounts[0]["account_id"] != "acct-1" || accounts[1]["account_id"] != "acct-2" {
		t.Fatalf("unexpected accounts: %#v", accounts)
	}
	if accounts[0]["account_label"] != "max@example.com" {
		t.Fatalf("missing account label: %#v", accounts[0])
	}
}

func TestDashboardPayloadWithNoAccountsIsEmptyState(t *testing.T) {
	payload := dashboardPayloadAll(nil, nil)
	if payload["ok"] != true {
		t.Fatalf("ok = %#v", payload["ok"])
	}
	accounts := payload["accounts"].([]map[string]any)
	if len(accounts) != 0 {
		t.Fatalf("accounts = %#v", accounts)
	}
	if payload["class"] != "empty" {
		t.Fatalf("class = %#v", payload["class"])
	}
}

func TestDashboardRouteServesLoginUI(t *testing.T) {
	mux := http.NewServeMux()
	state := &cache{}
	registerUIHandlers(mux, state, Options{})
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	body := res.Body.String()
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", res.Code, body)
	}
	for _, want := range []string{"Баланс", "Add Account / Login", "Осталось кредитов", "api/login/start"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}
}
