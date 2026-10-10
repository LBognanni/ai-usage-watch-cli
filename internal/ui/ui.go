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
			if color {
				code := "32"
				if used >= 90 {
					code = "31"
				} else if used >= 70 {
					code = "33"
				}
				bar = "\x1b[" + code + "m" + bar + "\x1b[0m"
			}
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

// Compact keeps all reported windows and account labels, but removes bars and
// per-provider healthy timestamps. Parentheses contain reset countdowns.
func Compact(s quota.Snapshot, ids []string, now time.Time, maxAge time.Duration) string {
	var b strings.Builder
	rows := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	for _, id := range ids {
		p := engine.Find(s, id)
		header := quota.Name(id)
		var labels []string
		if p.Plan != "" {
			labels = append(labels, Clean(p.Plan))
		}
		if p.Label != "" {
			labels = append(labels, Clean(p.Label))
		}
		if len(labels) > 0 {
			header += " (" + strings.Join(labels, " · ") + ")"
		}
		var cells []string
		stale := !p.Fresh(now, maxAge)
		for _, w := range p.Windows {
			label := Clean(w.Label)
			switch label {
			case "5-hour":
				label = "5h"
			case "Weekly":
				label = "week"
			case "Monthly":
				label = "month"
			}
			if !validWindow(w, now) {
				cells = append(cells, label+" ? (elapsed)")
				continue
			}
			prefix := ""
			if stale {
				prefix = "~"
			}
			reset := "reset unknown"
			if !w.ResetsAt.IsZero() {
				reset = duration(w.ResetsAt.Sub(now))
			}
			cells = append(cells, fmt.Sprintf("%s %s%.1f%% (%s)", label, prefix, w.Used, reset))
		}
		if stale && len(p.Windows) > 0 {
			state := "stale"
			if p.Status != "live" {
				state += ": " + Clean(p.Status)
			}
			cells = append(cells, state+" · "+age(p, now))
		} else if p.Status != "live" {
			state := Clean(p.Status)
			if p.Message != "" {
				state += " — " + Clean(p.Message)
			}
			cells = append(cells, state)
		}
		fmt.Fprintf(rows, "%s\t%s\n", header, strings.Join(cells, " · "))
	}
	_ = rows.Flush()
	if footer := freshnessFooter(s, ids, now); footer != "" {
		b.WriteString("\n" + footer)
	}
	return b.String()
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
