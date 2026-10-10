// Package ui renders quota without leaking raw provider payloads or control codes.
package ui

import (
	"fmt"
	"math"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"aiu/internal/engine"
	"aiu/internal/quota"
)

// Clean removes terminal escapes, controls, and bidi formatting from labels.
func Clean(s string) string {
	var b strings.Builder
	count := 0
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		if count >= 120 {
			break
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}

func duration(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	minutes := int(d / time.Minute)
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	hours := minutes / 60
	if hours < 24 {
		return fmt.Sprintf("%dh %02dm", hours, minutes%60)
	}
	return fmt.Sprintf("%dd %dh", hours/24, hours%24)
}

func age(p quota.Provider, now time.Time) string {
	if p.CheckedAt.IsZero() {
		return "never updated"
	}
	return "updated " + duration(max(0, now.Sub(p.CheckedAt))) + " ago"
}

func validWindow(w quota.Window, now time.Time) bool {
	return w.ResetsAt.IsZero() || now.Before(w.ResetsAt)
}

func Display(s quota.Snapshot, ids []string, now time.Time, maxAge time.Duration, color bool) string {
	var b strings.Builder
	for index, id := range ids {
		if index > 0 {
			b.WriteString("\n")
		}
		p := engine.Find(s, id)
		header := quota.Name(id)
		if p.Plan != "" {
			header += " · " + Clean(p.Plan)
		}
		if p.Label != "" {
			header += " · " + Clean(p.Label)
		}
		fmt.Fprintln(&b, header)
		stale := !p.Fresh(now, maxAge)
		for _, w := range p.Windows {
			if !validWindow(w, now) {
				fmt.Fprintf(&b, "  %-22s window elapsed; refresh needed\n", Clean(w.Label))
				continue
			}
			used := math.Max(0, math.Min(100, w.Used))
			filled := int(math.Round(used / 5))
			bar := strings.Repeat("█", filled) + strings.Repeat("░", 20-filled)
			bar = renderUsageBar([]rune(bar), filled, used, color, false)
			reset := "reset unknown"
			if !w.ResetsAt.IsZero() {
				if w.ResetsAt.Sub(now) < 24*time.Hour {
					reset = "resets in " + duration(w.ResetsAt.Sub(now))
				} else {
					reset = "resets " + w.ResetsAt.Local().Format("Mon Jan 2 15:04 MST")
				}
			}
			prefix := ""
			if stale {
				prefix = "~"
			}
			fmt.Fprintf(&b, "  %-22s %s  %s%5.1f%% used  %s\n", Clean(w.Label), bar, prefix, used, reset)
		}
		if p.Status != "live" {
			fmt.Fprintf(&b, "  %s", Clean(p.Status))
			if p.Message != "" {
				fmt.Fprintf(&b, " — %s", Clean(p.Message))
			}
			b.WriteString("\n")
		}
		if len(p.Windows) > 0 && stale {
			b.WriteString("  stale · ")
			b.WriteString(age(p, now))
			b.WriteString("\n")
		}
	}
	if footer := freshnessFooter(s, ids, now); footer != "" {
		b.WriteString("\n" + footer)
	}
	return b.String()
}

// Compact shows only provider/plan names and the main quota bars. Countdown
// text overlays the bar; no percentages, extra windows, or footer are emitted.
func Compact(s quota.Snapshot, ids []string, now time.Time, maxAge time.Duration, color bool) string {
	var b strings.Builder
	rows := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	for _, id := range ids {
		p := engine.Find(s, id)
		header := quota.Name(id)
		if id == "copilot" {
			header = "Copilot"
		}
		if p.Plan != "" {
			header += " " + Clean(p.Plan)
		}
		var cells []string
		stale := !p.Fresh(now, maxAge)
		labels := []string{"5-hour", "Weekly"}
		if id == "copilot" {
			labels = []string{"Monthly"}
		}
		for _, label := range labels {
			var window *quota.Window
			for i := range p.Windows {
				if p.Windows[i].Label == label {
					window = &p.Windows[i]
					break
				}
			}
			bar := compactBar(window, now, stale, color)
			cells = append(cells, bar)
		}
		fmt.Fprintf(rows, "%s\t%s\n", header, strings.Join(cells, " "))
	}
	_ = rows.Flush()
	return b.String()
}

func compactBar(w *quota.Window, now time.Time, stale, color bool) string {
	const width = 20
	bar := []rune(strings.Repeat("░", width))
	text := "?"
	filled := 0
	used := 0.0
	if w != nil && validWindow(*w, now) {
		used = w.Used
		filled = int(math.Round(math.Max(0, math.Min(100, used)) * width / 100))
		for i := 0; i < filled; i++ {
			bar[i] = '█'
		}
		if !w.ResetsAt.IsZero() {
			d := w.ResetsAt.Sub(now)
			if d < 24*time.Hour {
				minutes := int(d / time.Minute)
				text = fmt.Sprintf("%d:%02d", minutes/60, minutes%60)
			} else {
				text = duration(d)
			}
		}
		if stale {
			text = "~" + text
		}
	}
	label := []rune(text)
	if len(label) > width {
		label = label[:width]
	}
	start := (width - len(label)) / 2
	copy(bar[start:], label)
	return renderUsageBar(bar, filled, used, color, w == nil || !validWindow(*w, now))
}

// Both views use exactly the same cell backgrounds and palette. Text replaces
// only the foreground character, never the progress underneath it.
func renderUsageBar(bar []rune, filled int, used float64, color, unknown bool) string {
	if !color {
		return string(bar)
	}
	var b strings.Builder
	previous := ""
	bright, dark := usagePalette(used)
	if unknown {
		dark = "43;43;43"
	}
	for i, cell := range bar {
		// Every cell retains its underlying usage background, including text
		// cells. Solid spaces avoid a different texture behind the countdown.
		fg, bg := 97, dark
		if i < filled {
			fg, bg = 30, bright
		}
		if cell == '█' || cell == '░' {
			cell = ' '
		}
		style := fmt.Sprintf("\x1b[%d;48;2;%sm", fg, bg)
		if style != previous {
			b.WriteString(style)
			previous = style
		}
		b.WriteRune(cell)
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

// Truecolor backgrounds are independent of the terminal's ANSI theme. The
// unused portion is a dark tint of the filled portion, not a separate gray box.
func usagePalette(used float64) (bright, dark string) {
	switch {
	case used >= 90:
		return "230;85;78", "58;23;21"
	case used >= 70:
		return "229;192;54", "57;47;15"
	default:
		return "176;224;48", "43;53;13"
	}
}

func freshnessFooter(s quota.Snapshot, ids []string, now time.Time) string {
	var oldest time.Time
	for _, id := range ids {
		p := engine.Find(s, id)
		if len(p.Windows) > 0 && !p.CheckedAt.IsZero() && (oldest.IsZero() || p.CheckedAt.Before(oldest)) {
			oldest = p.CheckedAt
		}
	}
	if oldest.IsZero() {
		return ""
	}
	return age(quota.Provider{CheckedAt: oldest}, now) + " (oldest reading)\n"
}

func Status(s quota.Snapshot, ids []string, now time.Time, maxAge time.Duration) string {
	var parts []string
	var oldest time.Time
	for _, id := range ids {
		p := engine.Find(s, id)
		name := quota.Name(id)
		if id == "copilot" {
			name = "Copilot"
		}
		find := func(label string) string {
			for _, w := range p.Windows {
				if w.Label == label && validWindow(w, now) {
					return fmt.Sprintf("%.0f", w.Used)
				}
			}
			return "?"
		}
		values := find("5-hour") + "/" + find("Weekly") + "%"
		if id == "copilot" {
			values = find("Monthly") + "%"
		}
		if len(p.Windows) == 0 {
			values = Clean(p.Status)
		} else {
			if !p.Fresh(now, maxAge) {
				values = "~" + values
				if p.Status != "live" {
					values += " (" + Clean(p.Status) + ")"
				}
			}
			if !p.CheckedAt.IsZero() && (oldest.IsZero() || p.CheckedAt.Before(oldest)) {
				oldest = p.CheckedAt
			}
		}
		parts = append(parts, name+" "+values)
	}
	if !oldest.IsZero() {
		parts = append(parts, "updated "+duration(max(0, now.Sub(oldest)))+" ago")
	}
	return strings.Join(parts, " · ") + "\n"
}
