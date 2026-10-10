package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"aiu/internal/quota"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aiu", "snapshot.json")
	s := quota.Snapshot{Version: 1, Providers: []quota.Provider{{ID: "claude", Status: "live", CheckedAt: time.Now().UTC(), Windows: []quota.Window{{Label: "5-hour", Used: 42}}}}}
	if err := Write(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Providers[0].Windows[0].Used != 42 {
		t.Fatal(got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v", info.Mode())
	}
	if err := Write(path, s); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	for _, raw := range []string{`{`, `{"version":99}`} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(path); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
