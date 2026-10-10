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
