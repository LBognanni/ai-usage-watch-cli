// Package quota defines the secret-free, provider-independent usage snapshot.
package quota

import "time"

type Window struct {
	Label    string    `json:"label"`
	Used     float64   `json:"used_percent"`
	ResetsAt time.Time `json:"resets_at,omitempty"`
}

type Provider struct {
	ID          string    `json:"id"`
	Plan        string    `json:"plan,omitempty"`
	Label       string    `json:"label,omitempty"`
	Status      string    `json:"status"`
	Message     string    `json:"message,omitempty"`
	CheckedAt   time.Time `json:"checked_at,omitempty"`
	NextAttempt time.Time `json:"next_attempt,omitempty"`
	Windows     []Window  `json:"windows,omitempty"`
	// Identity is a hash, not a token or account ID. It prevents a cached quota
	// from being displayed for another account after a credential switch.
	Identity string `json:"identity,omitempty"`
}

type Snapshot struct {
	Version   int        `json:"version"`
	Providers []Provider `json:"providers"`
}

var IDs = []string{"claude", "openai", "copilot"}

func Name(id string) string {
	switch id {
	case "claude":
		return "Claude"
	case "openai":
		return "OpenAI"
	case "copilot":
		return "GitHub Copilot"
	default:
		return id
	}
}

func (p Provider) Fresh(now time.Time, maxAge time.Duration) bool {
	if p.Status != "live" || p.CheckedAt.IsZero() || now.Before(p.CheckedAt) || now.Sub(p.CheckedAt) >= maxAge {
		return false
	}
	for _, w := range p.Windows {
		if !w.ResetsAt.IsZero() && !now.Before(w.ResetsAt) {
			return false
		}
	}
	return true
}
