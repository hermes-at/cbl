package cbl

import (
	"encoding/json"
	"testing"
)

func TestResetCreditSummaryPreservesUnknownAndInventory(t *testing.T) {
	for _, tc := range []struct {
		name, raw             string
		present               bool
		available, applicable *int
	}{
		{"absent", ``, false, nil, nil},
		{"null", `,"rate_limit_reset_credits":null`, false, nil, nil},
		{"zero", `,"rate_limit_reset_credits":{"available_count":0,"applicable_available_count":0}`, true, intPtrReset(0), intPtrReset(0)},
		{"inventory is not applicability", `,"rate_limit_reset_credits":{"available_count":2,"applicable_available_count":0}`, true, intPtrReset(2), intPtrReset(0)},
		{"unknown", `,"rate_limit_reset_credits":{}`, true, nil, nil},
		{"invalid counter", `,"rate_limit_reset_credits":{"available_count":"bad"}`, true, nil, nil},
		{"negative", `,"rate_limit_reset_credits":{"available_count":-1}`, true, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap, err := mapFromBytes([]byte(`{"rate_limit":{"primary_window":{"used_percent":25}}`+tc.raw+`}`), "fixture")
			if err != nil {
				t.Fatal(err)
			}
			bytes, err := json.Marshal(accountCard(snap))
			if err != nil {
				t.Fatal(err)
			}
			var account map[string]any
			if err = json.Unmarshal(bytes, &account); err != nil {
				t.Fatal(err)
			}
			value, ok := account["reset_credits"]
			if !tc.present {
				if ok {
					t.Fatalf("unexpected reset data: %v", value)
				}
				return
			}
			summary, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("missing summary: %s", bytes)
			}
			for name, want := range map[string]*int{"available_count": tc.available, "applicable_available_count": tc.applicable} {
				got := summary[name]
				if want == nil {
					if got != nil {
						t.Fatalf("%s = %v, want unknown", name, got)
					}
				} else if got != float64(*want) {
					t.Fatalf("%s = %v, want %d", name, got, *want)
				}
			}
			if summary["state"] != "unavailable" {
				t.Fatalf("summary without grant details must not claim ready: %v", summary)
			}
			if _, ok := summary["offers"].([]any); !ok {
				t.Fatalf("offers must be an array: %v", summary)
			}
		})
	}
}
func intPtrReset(value int) *int { return &value }
