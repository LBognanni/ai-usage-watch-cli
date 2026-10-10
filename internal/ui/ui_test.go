package ui

import (
	"regexp"
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

func TestCountdownBackgroundCrossesFillBoundary(t *testing.T) {
	now := time.Now()
	w := quota.Window{Used: 50, ResetsAt: now.Add(3*time.Hour + 33*time.Minute)}
	bar := compactBar(&w, now, false, true)
	// At 50%, the first two countdown characters sit on the consumed green
	// region; the last two sit on the remaining dark region.
	if !strings.Contains(bar, "\x1b[30;48;2;176;224;48m") || !strings.Contains(bar, "3:\x1b[97;48;2;43;53;13m33") {
		t.Fatal(bar)
	}
	strip := regexp.MustCompile(`\x1b\[[0-9;]+m`)
	plain := strings.Map(func(r rune) rune {
		if r == '█' || r == '░' {
			return ' '
		}
		return r
	}, compactBar(&w, now, false, false))
	if strip.ReplaceAllString(bar, "") != plain {
		t.Fatal("color changed visible content or width")
	}
	for _, tt := range []struct {
		used  float64
		style string
	}{{0, "\x1b[97;48;2;43;53;13m"}, {100, "\x1b[30;48;2;230;85;78m"}} {
		w.Used = tt.used
		if bar := compactBar(&w, now, false, true); !strings.Contains(bar, tt.style) {
			t.Fatal(bar)
		}
	}
}

func TestColoredCompactBarHasUniformBackgroundUnderText(t *testing.T) {
	now := time.Now()
	w := quota.Window{Used: 20, ResetsAt: now.Add(time.Hour)}
	bar := compactBar(&w, now, false, true)
	// Exactly one background transition: lime consumed cells, then a single
	// olive background spanning the remaining cells AND the countdown.
	if strings.Count(bar, "48;2;") != 2 || !strings.Contains(bar, "1:00") || strings.ContainsAny(bar, "█░") {
		t.Fatal(bar)
	}
	if strings.Contains(bar, ";40m") || strings.Contains(bar, ";100m") {
		t.Fatal("separate gray/black text background")
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
	if strings.Count(got, "\n") != 3 || strings.ContainsAny(got, "[]%\x1b") {
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
		bar := compactBar(&w, now, false, false)
		if len([]rune(bar)) != 20 || !strings.Contains(bar, "3:33") {
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
	if bar := compactBar(&w, now, true, false); !strings.Contains(bar, "~3d 3h") {
		t.Fatal(bar)
	}
	w.ResetsAt = now.Add(-time.Second)
	if bar := compactBar(&w, now, true, false); strings.Contains(bar, "█") || !strings.Contains(bar, "?") {
		t.Fatal(bar)
	}
}

func TestCompactColorsMatchFullView(t *testing.T) {
	now := time.Now()
	for _, tt := range []struct {
		used   float64
		bright string
		dark   string
	}{{20, "176;224;48", "43;53;13"}, {69.9, "176;224;48", "43;53;13"}, {70, "229;192;54", "57;47;15"}, {89.9, "229;192;54", "57;47;15"}, {90, "230;85;78", "58;23;21"}, {95, "230;85;78", "58;23;21"}} {
		s := quota.Snapshot{Providers: []quota.Provider{{ID: "claude", Status: "live", CheckedAt: now, Windows: []quota.Window{{Label: "5-hour", Used: tt.used, ResetsAt: now.Add(time.Hour)}}}}}
		compact := Compact(s, []string{"claude"}, now, time.Minute, true)
		if !strings.Contains(compact, "48;2;"+tt.bright+"m") || !strings.Contains(compact, "48;2;"+tt.dark+"m") || !strings.Contains(compact, "\x1b[0m") {
			t.Fatal(compact)
		}
		full := Display(s, []string{"claude"}, now, time.Minute, true)
		if !strings.Contains(full, "48;2;"+tt.bright+"m") || !strings.Contains(full, "48;2;"+tt.dark+"m") || !strings.Contains(full, "\x1b[0m") {
			t.Fatal(full)
		}
		// The shared renderer must preserve identical style transitions at the
		// fill boundary, whether or not a countdown overlays the cells.
		styles := regexp.MustCompile(`\x1b\[[0-9;]+m`)
		compactBarOnly := compactBar(&s.Providers[0].Windows[0], now, false, true)
		if strings.Join(styles.FindAllString(compactBarOnly, -1), "") != strings.Join(styles.FindAllString(full, -1), "") {
			t.Fatal("view palettes or fill boundaries differ")
		}
		plain := Compact(s, []string{"claude"}, now, time.Minute, false)
		if strings.Contains(plain, "\x1b") || strings.Contains(plain, "-") || !strings.Contains(plain, "░") {
			t.Fatal(plain)
		}
	}
}
