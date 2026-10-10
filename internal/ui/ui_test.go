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
	got := Compact(s, quota.IDs, now, 5*time.Minute, false)
	for _, want := range []string{"Claude Pro", "2:00", "3d 0h", "OpenAI Plus", "Copilot", "?"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	if strings.Count(got, "[") != 5 || strings.Count(got, "\n") != 3 || strings.ContainsAny(got, "%\x1b") {
		t.Fatal(got)
	}
	for _, forbidden := range []string{"updated", "Claude Code", "default", "Spark", "not connected", "reset", "week"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("unexpected %q in %s", forbidden, got)
		}
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
	got := Compact(s, []string{"claude"}, now, 5*time.Minute, false)
	for _, want := range []string{"?", "~?"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "OpenAI") || strings.Contains(got, "rate limited") || strings.Contains(got, "updated") {
		t.Fatal(got)
	}
	if got := Compact(quota.Snapshot{}, []string{"copilot"}, now, time.Minute, false); strings.Contains(got, "updated") {
		t.Fatal(got)
	}
}

func TestCompactBar(t *testing.T) {
	now := time.Now()
	for _, used := range []float64{0, 50, 100} {
		w := quota.Window{Used: used, ResetsAt: now.Add(3*time.Hour + 33*time.Minute)}
		bar := compactBar(&w, now, false)
		if len([]rune(bar)) != 22 || !strings.Contains(bar, "3:33") {
			t.Fatal(bar)
		}
		if used == 0 && strings.Contains(bar, "█") {
			t.Fatal(bar)
		}
		if used == 100 && strings.Contains(bar, "░") {
			t.Fatal(bar)
		}
		if used == 50 && (!strings.Contains(bar, "█") || !strings.Contains(bar, "░")) {
			t.Fatal(bar)
		}
	}
	w := quota.Window{Used: 50, ResetsAt: now.Add(75 * time.Hour)}
	if bar := compactBar(&w, now, true); !strings.Contains(bar, "~3d 3h") {
		t.Fatal(bar)
	}
	w.ResetsAt = now.Add(-time.Second)
	if bar := compactBar(&w, now, true); strings.Contains(bar, "█") || !strings.Contains(bar, "?") {
		t.Fatal(bar)
	}
}

func TestCompactColorsMatchFullView(t *testing.T) {
	now := time.Now()
	for _, tt := range []struct {
		used float64
		code string
	}{{20, "32"}, {75, "33"}, {95, "31"}} {
		s := quota.Snapshot{Providers: []quota.Provider{{ID: "claude", Status: "live", CheckedAt: now, Windows: []quota.Window{{Label: "5-hour", Used: tt.used, ResetsAt: now.Add(time.Hour)}}}}}
		for _, output := range []string{Compact(s, []string{"claude"}, now, time.Minute, true), Display(s, []string{"claude"}, now, time.Minute, true)} {
			if !strings.Contains(output, "\x1b["+tt.code+"m") || !strings.Contains(output, "\x1b[0m") {
				t.Fatal(output)
			}
		}
		plain := Compact(s, []string{"claude"}, now, time.Minute, false)
		if strings.Contains(plain, "\x1b") || strings.Contains(plain, "-") || !strings.Contains(plain, "░") {
			t.Fatal(plain)
		}
	}
}
