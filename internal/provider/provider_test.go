package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"aiu/internal/credentials"
	"aiu/internal/quota"
)

func TestDecoders(t *testing.T) {
	tests := []struct {
		name, raw string
		decode    func([]byte) ([]quota.Window, string, error)
		labels    []string
		used      []float64
	}{
		{"claude", `{"five_hour":{"utilization":42,"resets_at":"2026-10-12T12:00:00Z"},"seven_day":{"utilization":0},"seven_day_opus":null,"limits":[{"kind":"weekly_scoped","percent":12,"scope":{"model":{"display_name":"Sonnet"}}}]}`, DecodeClaude, []string{"5-hour", "Weekly", "Weekly · Sonnet"}, []float64{42, 0, 12}},
		{"openai reversed", `{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":31,"limit_window_seconds":604800,"reset_at":1791806400},"secondary_window":{"used_percent":7,"limit_window_seconds":18000}}}`, DecodeOpenAI, []string{"Weekly", "5-hour"}, []float64{31, 7}},
		{"openai weekly only", `{"rate_limit":{"primary_window":{"used_percent":31,"limit_window_seconds":604800},"secondary_window":null}}`, DecodeOpenAI, []string{"Weekly"}, []float64{31}},
		{"copilot", `{"copilot_plan":"business","quota_reset_date":"2026-11-01","quota_snapshots":{"premium_interactions":{"percent_remaining":75,"entitlement":300},"chat":{"percent_remaining":100,"unlimited":true}}}`, DecodeCopilot, []string{"Monthly"}, []float64{25}},
		{"copilot camel", `{"copilotPlan":"business","quotaResetDate":"2026-11-01","quotaSnapshots":{"premiumInteractions":{"percentRemaining":31.17,"entitlement":300}}}`, DecodeCopilot, []string{"Monthly"}, []float64{68.83}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			windows, _, err := tt.decode([]byte(tt.raw))
			if err != nil {
				t.Fatal(err)
			}
			if len(windows) != len(tt.labels) {
				t.Fatal(windows)
			}
			for i, w := range windows {
				if w.Label != tt.labels[i] || w.Used != tt.used[i] {
					t.Fatal(w)
				}
			}
		})
	}
}

func TestUnsupportedSchemas(t *testing.T) {
	for _, decode := range []func([]byte) ([]quota.Window, string, error){DecodeClaude, DecodeOpenAI, DecodeCopilot} {
		for _, raw := range []string{`{`, `{}`, `null`, `{"five_hour":{"utilization":101}}`, `{"quota_snapshots":{"premium_interactions":{"entitlement":0,"percent_remaining":0}}}`} {
			if _, _, err := decode([]byte(raw)); err == nil {
				t.Fatalf("accepted %s", raw)
			}
		}
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHTTPFailuresAreSanitized(t *testing.T) {
	now := time.Now()
	for _, code := range []int{401, 403, 429, 500} {
		client := Client{HTTP: &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Authorization") != "Bearer sensitive-token" {
				t.Fatal("missing auth")
			}
			return &http.Response{StatusCode: code, Header: http.Header{"Retry-After": []string{"120"}}, Body: io.NopCloser(strings.NewReader("sensitive-token"))}, nil
		})}}
		got := client.Fetch(context.Background(), "openai", credentials.Credential{Token: "sensitive-token"}, now)
		if got.Status == "live" || strings.Contains(got.Message, "sensitive-token") {
			t.Fatal(got)
		}
		if code == 429 && !got.NextAttempt.Equal(now.Add(2*time.Minute)) {
			t.Fatal(got.NextAttempt)
		}
	}
}

func TestRetryAfter(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tt := range []struct {
		value string
		want  time.Duration
	}{{"", 5 * time.Minute}, {"0", time.Minute}, {"nan", 5 * time.Minute}, {"120", 2 * time.Minute}, {now.Add(3 * time.Minute).Format(http.TimeFormat), 3 * time.Minute}} {
		if got := retryAfter(tt.value, now); got != tt.want {
			t.Fatalf("%q: %v", tt.value, got)
		}
	}
}

func TestNoRedirectCredentialForwarding(t *testing.T) {
	client := NewClient()
	calls := 0
	client.HTTP.Transport = roundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "chatgpt.com" {
			t.Fatal("credential sent to another host")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://example.com/steal"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	got := client.Fetch(context.Background(), "openai", credentials.Credential{Token: "secret"}, time.Now())
	if calls != 1 || got.Status != "unavailable" {
		t.Fatal("redirect followed")
	}
}

func TestHTTPSuccess(t *testing.T) {
	client := NewClient()
	client.HTTP.Transport = roundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("ChatGPT-Account-Id") != "account" {
			t.Fatal("missing account header")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"plan_type":"plus","email":"private@example.com","rate_limit":{"primary_window":{"used_percent":42,"limit_window_seconds":18000}}}`))}, nil
	})
	got := client.Fetch(context.Background(), "openai", credentials.Credential{Token: "secret", AccountID: "account", Label: "default", Identity: "hash"}, time.Now())
	if got.Status != "live" || got.Plan != "Plus" || len(got.Windows) != 1 || got.Windows[0].Used != 42 {
		t.Fatal(got)
	}
}
