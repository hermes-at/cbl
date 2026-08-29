package cbl

import "time"

type Options struct {
	AuthFile   string
	ConfigFile string
	BaseURL    string
	Proxy      string
	Fixture    string
	JSON       bool
	Waybar     bool
}

type Credentials struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	AccountID    string
	AuthFile     string
	Source       string
	IsAPIKey     bool
}

type UsageWindow struct {
	UsedPercent        int
	ResetAt            *time.Time
	LimitWindowSeconds int
	Label              string
	RemainingPercent   int
}

type CreditLimit struct {
	Used             float64
	Limit            float64
	Remaining        float64
	RemainingPercent float64
	ResetsAt         *time.Time
	UpdatedAt        time.Time
}

type UsageSnapshot struct {
	AccountID       string
	ProfileName     string
	PlanType        string
	PrimaryWindow   *UsageWindow
	SecondaryWindow *UsageWindow
	IndividualLimit *CreditLimit
	CreditsBalance  *float64
	AdditionalRates []NamedWindow
	FetchedAt       time.Time
	Source          string
	BaseURL         string
	Proxy           string
	Raw             any
}

type NamedWindow struct {
	Name   string
	Window UsageWindow
}

func (w UsageWindow) Remaining() int {
	if w.RemainingPercent > 0 {
		return w.RemainingPercent
	}
	return 100 - w.UsedPercent
}
