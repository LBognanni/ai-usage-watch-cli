// Package provider fetches read-only, private subscription quota endpoints.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aiu/internal/credentials"
	"aiu/internal/quota"
)

type Client struct{ HTTP *http.Client }

func NewClient() Client {
	return Client{HTTP: &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c Client) Fetch(ctx context.Context, id string, cred credentials.Credential, now time.Time) quota.Provider {
	p := quota.Provider{ID: id, Label: cred.Label, Identity: cred.Identity, Plan: title(cred.Plan)}
	endpoints := map[string]string{
		"claude":  "https://api.anthropic.com/api/oauth/usage",
		"openai":  "https://chatgpt.com/backend-api/wham/usage",
		"copilot": "https://api.github.com/copilot_internal/user",
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoints[id], nil)
	if err != nil {
		p.Status = "unavailable"
		p.Message = "Cannot create quota request."
		return p
	}
	request.Header.Set("Authorization", "Bearer "+cred.Token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "aiu/0.1")
	if id == "claude" {
		request.Header.Set("anthropic-beta", "oauth-2025-04-20")
		// The usage endpoint's Claude Code bucket avoids the severe generic-UA
		// throttling observed by existing quota clients.
		request.Header.Set("User-Agent", "claude-code/2.1.296")
	}
	if id == "openai" && cred.AccountID != "" {
		request.Header.Set("ChatGPT-Account-Id", cred.AccountID)
	}
	if id == "copilot" {
		request.Header.Set("X-GitHub-Api-Version", "2025-04-01")
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		p.Status = "unavailable"
		p.Message = "Quota request failed (network or timeout)."
		return p
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case 401:
			p.Status = "auth expired"
			p.Message = "Use OpenCode with this provider to refresh its login, then retry."
			if id == "claude" {
				p.Message = "Open Claude Code to refresh its login, then retry."
			}
		case 403:
			p.Status = "access denied"
			p.Message = "Provider rejected quota access; check login scope or company policy."
		case 429:
			p.Status = "rate limited"
			p.Message = "Using the last reading until the provider allows another request."
			p.NextAttempt = now.Add(retryAfter(response.Header.Get("Retry-After"), now))
		default:
			p.Status = "unavailable"
			p.Message = fmt.Sprintf("Quota endpoint returned HTTP %d.", response.StatusCode)
		}
		return p
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil || len(body) > 1024*1024 {
		p.Status = "unavailable"
		p.Message = "Cannot read quota response."
		return p
	}
	var windows []quota.Window
	var plan string
	switch id {
	case "claude":
		windows, plan, err = DecodeClaude(body)
	case "openai":
		windows, plan, err = DecodeOpenAI(body)
	case "copilot":
		windows, plan, err = DecodeCopilot(body)
	}
	if err != nil {
		p.Status = "unsupported"
		p.Message = "Quota response changed or contains no recognized usage windows."
		return p
	}
	if plan != "" {
		p.Plan = title(plan)
	}
	p.Windows = windows
	p.Status = "live"
	p.CheckedAt = now
	return p
}

func retryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) {
		return time.Duration(math.Min(86400, math.Max(60, seconds))) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return max(time.Minute, min(24*time.Hour, date.Sub(now)))
	}
	return 5 * time.Minute
}

func title(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "_", " "))
	if len(s) == 0 {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func percent(p *float64) bool {
	return p != nil && !math.IsNaN(*p) && !math.IsInf(*p, 0) && *p >= 0 && *p <= 100
}

func unsupported() error { return fmt.Errorf("unsupported usage response") }

func DecodeClaude(raw []byte) ([]quota.Window, string, error) {
	type window struct {
		Used  *float64 `json:"utilization"`
		Reset string   `json:"resets_at"`
	}
	var doc struct {
		Five   *window `json:"five_hour"`
		Week   *window `json:"seven_day"`
		Opus   *window `json:"seven_day_opus"`
		Sonnet *window `json:"seven_day_sonnet"`
		Limits []struct {
			Kind  string   `json:"kind"`
			Used  *float64 `json:"percent"`
			Reset string   `json:"resets_at"`
			Scope struct {
				Model struct {
					Name string `json:"display_name"`
				} `json:"model"`
			} `json:"scope"`
		} `json:"limits"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil, "", unsupported()
	}
	var windows []quota.Window
	for _, entry := range []struct {
		label string
		w     *window
	}{{"5-hour", doc.Five}, {"Weekly", doc.Week}, {"Weekly · Opus", doc.Opus}, {"Weekly · Sonnet", doc.Sonnet}} {
		if entry.w == nil || !percent(entry.w.Used) {
			continue
		}
		reset, _ := time.Parse(time.RFC3339Nano, entry.w.Reset)
		windows = append(windows, quota.Window{Label: entry.label, Used: *entry.w.Used, ResetsAt: reset})
	}
	for _, limit := range doc.Limits {
		if limit.Kind != "weekly_scoped" || limit.Scope.Model.Name == "" || !percent(limit.Used) {
			continue
		}
		label := "Weekly · " + limit.Scope.Model.Name
		duplicate := false
		for _, w := range windows {
			if w.Label == label {
				duplicate = true
			}
		}
		if duplicate {
			continue
		}
		reset, _ := time.Parse(time.RFC3339Nano, limit.Reset)
		windows = append(windows, quota.Window{Label: label, Used: *limit.Used, ResetsAt: reset})
	}
	if len(windows) == 0 {
		return nil, "", unsupported()
	}
	return windows, "", nil
}

func DecodeOpenAI(raw []byte) ([]quota.Window, string, error) {
	type window struct {
		Used    *float64 `json:"used_percent"`
		Seconds int64    `json:"limit_window_seconds"`
		Reset   int64    `json:"reset_at"`
	}
	type limit struct {
		Primary   *window `json:"primary_window"`
		Secondary *window `json:"secondary_window"`
	}
	var doc struct {
		Plan       string `json:"plan_type"`
		Rate       limit  `json:"rate_limit"`
		Additional []struct {
			Name string `json:"limit_name"`
			Rate limit  `json:"rate_limit"`
		} `json:"additional_rate_limits"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil, "", unsupported()
	}
	var windows []quota.Window
	add := func(w *window, prefix string) {
		if w == nil || !percent(w.Used) || w.Seconds <= 0 {
			return
		}
		label := fmt.Sprintf("%d-hour", w.Seconds/3600)
		switch w.Seconds {
		case 18000:
			label = "5-hour"
		case 604800:
			label = "Weekly"
		case 86400:
			label = "Daily"
		}
		var reset time.Time
		if w.Reset > 0 && w.Reset < 253402300800 {
			reset = time.Unix(w.Reset, 0).UTC()
		}
		windows = append(windows, quota.Window{Label: prefix + label, Used: *w.Used, ResetsAt: reset})
	}
	add(doc.Rate.Primary, "")
	add(doc.Rate.Secondary, "")
	for _, extra := range doc.Additional {
		if extra.Name != "" {
			add(extra.Rate.Primary, extra.Name+" · ")
			add(extra.Rate.Secondary, extra.Name+" · ")
		}
	}
	if len(windows) == 0 {
		return nil, "", unsupported()
	}
	return windows, doc.Plan, nil
}

func DecodeCopilot(raw []byte) ([]quota.Window, string, error) {
	type window struct {
		Remaining      *float64 `json:"percent_remaining"`
		RemainingCamel *float64 `json:"percentRemaining"`
		Entitlement    *float64 `json:"entitlement"`
		Unlimited      bool     `json:"unlimited"`
	}
	type snapshots struct {
		Premium      *window `json:"premium_interactions"`
		PremiumCamel *window `json:"premiumInteractions"`
		Chat         *window `json:"chat"`
	}
	var doc struct {
		Plan           string     `json:"copilot_plan"`
		PlanCamel      string     `json:"copilotPlan"`
		Reset          string     `json:"quota_reset_date"`
		ResetCamel     string     `json:"quotaResetDate"`
		Snapshots      *snapshots `json:"quota_snapshots"`
		SnapshotsCamel *snapshots `json:"quotaSnapshots"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil, "", unsupported()
	}
	if doc.Plan == "" {
		doc.Plan = doc.PlanCamel
	}
	if doc.Reset == "" {
		doc.Reset = doc.ResetCamel
	}
	if doc.Snapshots == nil {
		doc.Snapshots = doc.SnapshotsCamel
	}
	if doc.Snapshots == nil {
		return nil, "", unsupported()
	}
	reset, err := time.Parse(time.RFC3339Nano, doc.Reset)
	if err != nil {
		reset, _ = time.Parse("2006-01-02", doc.Reset)
	}
	var windows []quota.Window
	premium := doc.Snapshots.Premium
	if premium == nil {
		premium = doc.Snapshots.PremiumCamel
	}
	for _, entry := range []struct {
		label string
		w     *window
	}{{"Monthly", premium}, {"Monthly · Chat", doc.Snapshots.Chat}} {
		w := entry.w
		if w == nil || w.Unlimited || (w.Entitlement != nil && *w.Entitlement <= 0) {
			continue
		}
		remaining := w.Remaining
		if remaining == nil {
			remaining = w.RemainingCamel
		}
		if !percent(remaining) {
			continue
		}
		windows = append(windows, quota.Window{Label: entry.label, Used: math.Round((100-*remaining)*1e6) / 1e6, ResetsAt: reset})
	}
	if len(windows) == 0 {
		return nil, "", unsupported()
	}
	return windows, doc.Plan, nil
}
