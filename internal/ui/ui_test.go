package ui

import (
	"strings"
	"testing"
	"time"

	"aiu/internal/quota"
)

func TestStatusOrderingAndMissingWindows(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	s := quota.Snapshot{Version: 1, Providers: []quota.Provider{{ID: "openai", Status: "live", CheckedAt: now, Windows: []quota.Window{{Label: "Weekly", Used: 31}, {Label: "5-hour", Used: 7}}}, {ID: "claude", Status: "live", CheckedAt: now, Windows: []quota.Window{{Label: "Weekly", Used: 42}}}}}
	got := Status(s, quota.IDs, now, 5*time.Minute)
	want := "Claude ?/42% · OpenAI 7/31% · Copilot not connected · updated <1m ago\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestStaleAndElapsed(t *testing.T) {
	now := time.Now()
	s := quota.Snapshot{Providers: []quota.Provider{{ID: "claude", Status: "live", CheckedAt: now.Add(-time.Hour), Windows: []quota.Window{{Label: "5-hour", Used: 42, ResetsAt: now.Add(-time.Minute)}, {Label: "Weekly", Used: 80}}}}}
	got := Status(s, []string{"claude"}, now, 5*time.Minute)
	if !strings.Contains(got, "~?/80%") || strings.Contains(got, "~~") {
		t.Fatal(got)
	}
	full := Display(s, []string{"claude"}, now, 5*time.Minute, false)
	if !strings.Contains(full, "window elapsed") || !strings.Contains(full, "stale") || strings.Contains(full, "\x1b") {
		t.Fatal(full)
	}
}

func TestTerminalLabelSafety(t *testing.T) {
	got := Clean("work\x1b\n\r\x00\u202eaccount")
	if got != "workaccount" {
		t.Fatalf("unsafe label %q", got)
	}
}

func TestCompact(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	s := quota.Snapshot{Providers: []quota.Provider{
		{ID: "claude", Plan: "Pro", Label: "Claude Code", Status: "live", CheckedAt: now.Add(-time.Minute), Windows: []quota.Window{{Label: "5-hour", Used: 34, ResetsAt: now.Add(2 * time.Hour)}, {Label: "Weekly", Used: 53, ResetsAt: now.Add(3 * 24 * time.Hour)}}},
		{ID: "openai", Plan: "Plus", Status: "live", CheckedAt: now.Add(-2 * time.Minute), Windows: []quota.Window{{Label: "Weekly", Used: 39}, {Label: "Spark · Weekly", Used: 7}}},
	}}
	got := Compact(s, quota.IDs, now, 5*time.Minute)
	for _, want := range []string{"Claude (Pro · Claude Code)", "5h 34.0% (2h 00m)", "week 53.0% (3d 0h)", "Spark · Weekly 7.0%", "GitHub Copilot", "not connected", "updated 2m ago (oldest reading)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	if strings.Count(got, "updated") != 1 || strings.ContainsAny(got, "█░\x1b") {
		t.Fatal(got)
	}
	full := Display(s, quota.IDs, now, 5*time.Minute, false)
	if strings.Count(full, "updated") != 1 {
		t.Fatal(full)
	}
}

func TestCompactStaleElapsedAndSelection(t *testing.T) {
	now := time.Now()
	s := quota.Snapshot{Providers: []quota.Provider{
		{ID: "claude", Status: "rate limited", CheckedAt: now.Add(-time.Hour), Windows: []quota.Window{{Label: "5-hour", Used: 42, ResetsAt: now.Add(-time.Minute)}, {Label: "Weekly", Used: 80}}},
		{ID: "openai", Status: "live", CheckedAt: now.Add(-24 * time.Hour), Windows: []quota.Window{{Label: "Weekly", Used: 3}}},
	}}
	got := Compact(s, []string{"claude"}, now, 5*time.Minute)
	for _, want := range []string{"5h ? (elapsed)", "week ~80.0%", "stale: rate limited", "updated 1h 00m ago (oldest reading)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "OpenAI") || strings.Contains(got, "24h") {
		t.Fatal(got)
	}
	if got := Compact(quota.Snapshot{}, []string{"copilot"}, now, time.Minute); strings.Contains(got, "updated") {
		t.Fatal(got)
	}
}
