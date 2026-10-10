// Package engine coordinates independent providers and stale cache fallback.
package engine

import (
	"context"
	"sync"
	"time"

	"aiu/internal/credentials"
	"aiu/internal/provider"
	"aiu/internal/quota"
)

type Loader func(context.Context, []string) map[string]credentials.Result
type Fetcher func(context.Context, string, credentials.Credential, time.Time) quota.Provider

type Engine struct {
	Load  Loader
	Fetch Fetcher
}

func New() Engine {
	client := provider.NewClient()
	return Engine{Load: load, Fetch: client.Fetch}
}

func load(ctx context.Context, ids []string) map[string]credentials.Result {
	result := make(map[string]credentials.Result)
	var wg sync.WaitGroup
	var mu sync.Mutex
	needsOpenCode := false
	for _, id := range ids {
		if id != "claude" {
			needsOpenCode = true
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := credentials.Claude(credentials.ClaudePath())
			mu.Lock()
			result["claude"] = r
			mu.Unlock()
		}()
	}
	if needsOpenCode {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := credentials.OpenCode(ctx)
			mu.Lock()
			for id, v := range r {
				result[id] = v
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return result
}

func Find(s quota.Snapshot, id string) quota.Provider {
	for _, p := range s.Providers {
		if p.ID == id {
			return p
		}
	}
	return quota.Provider{ID: id, Status: "not connected"}
}

// Refresh only updates requested providers. With staleOnly it avoids both
// credential lookups and HTTP calls when all requested providers are fresh.
func (e Engine) Refresh(ctx context.Context, previous quota.Snapshot, ids []string, now time.Time, maxAge time.Duration, staleOnly bool) quota.Snapshot {
	next := quota.Snapshot{Version: 1, Providers: append([]quota.Provider(nil), previous.Providers...)}
	var requested []string
	for _, id := range ids {
		old := Find(previous, id)
		if staleOnly && (old.Fresh(now, maxAge) || now.Before(old.NextAttempt)) {
			continue
		}
		requested = append(requested, id)
	}
	if len(requested) == 0 {
		return next
	}
	creds := e.Load(ctx, requested)
	updates := make(chan quota.Provider, len(requested))
	var wg sync.WaitGroup
	for _, id := range requested {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			old := Find(previous, id)
			r := creds[id]
			var p quota.Provider
			if r.Status != "" {
				p = quota.Provider{ID: id, Status: r.Status, Message: r.Message}
			} else if r.Credential.Token == "" {
				p = quota.Provider{ID: id, Status: "not connected", Message: "No active subscription OAuth credential."}
			} else if now.Before(old.NextAttempt) && old.Identity == r.Credential.Identity {
				updates <- old
				return
			} else {
				p = e.Fetch(ctx, id, r.Credential, now)
			}
			if p.Status != "live" {
				if p.NextAttempt.IsZero() {
					p.NextAttempt = now.Add(30 * time.Second)
				}
				if p.Status == "not connected" {
					p.NextAttempt = now.Add(maxAge)
				}
				// Never retain account-specific data after a known account switch.
				// Missing/expired credentials can still display a clearly stale reading.
				if len(old.Windows) > 0 && (p.Identity == "" || p.Identity == old.Identity) {
					p.Windows = old.Windows
					p.CheckedAt = old.CheckedAt
					p.Plan = old.Plan
					p.Label = old.Label
					p.Identity = old.Identity
				}
			}
			updates <- p
		}(id)
	}
	wg.Wait()
	close(updates)
	for p := range updates {
		found := false
		for i := range next.Providers {
			if next.Providers[i].ID == p.ID {
				next.Providers[i] = p
				found = true
				break
			}
		}
		if !found {
			next.Providers = append(next.Providers, p)
		}
	}
	return next
}
