package engine

import (
	"context"
	"testing"
	"time"

	"aiu/internal/credentials"
	"aiu/internal/quota"
)

func TestFreshCacheSkipsCredentials(t *testing.T) {
	now := time.Now()
	e := Engine{Load: func(context.Context, []string) map[string]credentials.Result {
		t.Fatal("credential lookup for fresh cache")
		return nil
	}}
	old := quota.Snapshot{Version: 1, Providers: []quota.Provider{{ID: "openai", Status: "live", CheckedAt: now, Windows: []quota.Window{{Label: "Weekly", Used: 42}}}}}
	got := e.Refresh(context.Background(), old, []string{"openai"}, now, 5*time.Minute, true)
	if Find(got, "openai").Windows[0].Used != 42 {
		t.Fatal(got)
	}
}

func TestPartialFailureAndAccountSwitch(t *testing.T) {
	now := time.Now()
	old := quota.Snapshot{Version: 1, Providers: []quota.Provider{{ID: "claude", Identity: "same", Status: "live", CheckedAt: now.Add(-time.Hour), Windows: []quota.Window{{Label: "5-hour", Used: 42}}}, {ID: "copilot", Identity: "other", Windows: []quota.Window{{Label: "Monthly", Used: 75}}}}}
	e := Engine{
		Load: func(context.Context, []string) map[string]credentials.Result {
			return map[string]credentials.Result{
				"claude":  {Credential: credentials.Credential{Token: "a", Identity: "same"}},
				"openai":  {Credential: credentials.Credential{Token: "b", Identity: "new"}},
				"copilot": {Credential: credentials.Credential{Token: "c", Identity: "new"}},
			}
		},
		Fetch: func(_ context.Context, id string, c credentials.Credential, _ time.Time) quota.Provider {
			p := quota.Provider{ID: id, Identity: c.Identity, Status: "unavailable"}
			if id == "openai" {
				p.Status = "live"
				p.CheckedAt = now
				p.Windows = []quota.Window{{Label: "Weekly", Used: 20}}
			}
			return p
		},
	}
	got := e.Refresh(context.Background(), old, quota.IDs, now, 5*time.Minute, false)
	if len(Find(got, "claude").Windows) != 1 || Find(got, "claude").Status != "unavailable" {
		t.Fatal("no stale fallback")
	}
	if len(Find(got, "copilot").Windows) != 0 {
		t.Fatal("old account quota retained")
	}
	if Find(got, "openai").Status != "live" {
		t.Fatal("healthy provider lost")
	}
}

func TestBackoff(t *testing.T) {
	now := time.Now()
	e := Engine{Load: func(context.Context, []string) map[string]credentials.Result { t.Fatal("backoff ignored"); return nil }}
	old := quota.Snapshot{Version: 1, Providers: []quota.Provider{{ID: "claude", Status: "rate limited", NextAttempt: now.Add(time.Minute)}}}
	e.Refresh(context.Background(), old, []string{"claude"}, now, 5*time.Minute, true)
}
