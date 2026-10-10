package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aiu/internal/cache"
	"aiu/internal/quota"
)

func TestFlags(t *testing.T) {
	for _, args := range [][]string{{"--watch", "--status"}, {"--interval=0"}, {"--provider=wrong"}, {"--timeout=-1s"}, {"extra"}, {"--watch", "--interval=1s"}} {
		var out, err bytes.Buffer
		if run(context.Background(), args, &out, &err) != 2 {
			t.Fatal(args)
		}
	}
}

func TestCachedStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	path := filepath.Join(dir, "aiu", "snapshot.json")
	s := quota.Snapshot{Version: 1, Providers: []quota.Provider{{ID: "openai", Status: "live", CheckedAt: time.Now(), Windows: []quota.Window{{Label: "5-hour", Used: 12}, {Label: "Weekly", Used: 34}}}}}
	if err := cache.Write(path, s); err != nil {
		t.Fatal(err)
	}
	var out, err bytes.Buffer
	if run(context.Background(), []string{"--status", "--cached", "--provider=openai"}, &out, &err) != 0 {
		t.Fatal(err.String())
	}
	if !strings.HasPrefix(out.String(), "OpenAI 12/34%") {
		t.Fatal(out.String())
	}
}

func TestHelpAndVersion(t *testing.T) {
	for _, arg := range []string{"--help", "--version"} {
		var out, err bytes.Buffer
		if run(context.Background(), []string{arg}, &out, &err) != 0 {
			t.Fatal(arg)
		}
	}
}
