package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"aiu/internal/cache"
	"aiu/internal/engine"
	"aiu/internal/quota"
	"aiu/internal/ui"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, out, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("aiu", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	watch := flags.Bool("watch", false, "watch quota bars; refresh every interval")
	status := flags.Bool("status", false, "compact line; refresh stale cache (2s default timeout)")
	compact := flags.Bool("compact", false, "one row per provider with usage and reset countdowns")
	providers := flags.String("provider", "claude,openai,copilot", "comma-separated providers")
	interval := flags.Duration("interval", 5*time.Minute, "watch network refresh interval")
	maxAge := flags.Duration("max-age", 5*time.Minute, "maximum cache age for status output")
	timeout := flags.Duration("timeout", 0, "overall refresh timeout (default 10s, status 2s)")
	noColor := flags.Bool("no-color", false, "disable color")
	cached := flags.Bool("cached", false, "read cache only, with no credential or network calls")
	version := flags.Bool("version", false, "print version")
	flags.Usage = func() {
		fmt.Fprintln(diagnostics, "aiu — live AI subscription quotas\n\nUsage: aiu [flags]\n\nDefault: fetch live quotas and print bars. --status shows 5-hour/weekly used percentages.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *version {
		fmt.Fprintln(out, "aiu 0.1.0")
		return 0
	}
	if flags.NArg() != 0 || (*watch && *status) || (*compact && *status) || (*watch && *cached) || *interval <= 0 || *maxAge <= 0 || *timeout < 0 {
		fmt.Fprintln(diagnostics, "aiu: invalid arguments; use --help")
		return 2
	}
	if *watch && *interval < time.Minute {
		fmt.Fprintln(diagnostics, "aiu: watch interval must be at least 1m to protect quota endpoints")
		return 2
	}
	ids, err := parseProviders(*providers)
	if err != nil {
		fmt.Fprintln(diagnostics, "aiu:", err)
		return 2
	}
	if *timeout == 0 {
		*timeout = 10 * time.Second
		if *status {
			*timeout = 2 * time.Second
		}
	}
	path, err := cache.Path()
	if err != nil {
		fmt.Fprintln(diagnostics, "aiu: cannot locate cache directory")
		return 1
	}
	snapshot, _ := cache.Read(path) // Missing/corrupt cache is recoverable.
	e := engine.New()
	refresh := func(previous quota.Snapshot, staleOnly bool) quota.Snapshot {
		if staleOnly {
			needed := false
			now := time.Now()
			for _, id := range ids {
				p := engine.Find(previous, id)
				if !p.Fresh(now, *maxAge) && !now.Before(p.NextAttempt) {
					needed = true
				}
			}
			if !needed {
				return previous
			}
		}
		requestCtx, cancel := context.WithTimeout(ctx, *timeout)
		defer cancel()
		unlock, err := cache.Lock(requestCtx, path)
		if err != nil {
			if requestCtx.Err() == nil {
				fmt.Fprintln(diagnostics, "aiu: cannot lock quota cache")
			}
			if latest, readErr := cache.Read(path); readErr == nil {
				return latest
			}
			if len(previous.Providers) == 0 {
				previous.Version = 1
				for _, id := range ids {
					previous.Providers = append(previous.Providers, quota.Provider{ID: id, Status: "unavailable", Message: "Quota refresh is busy or cache is inaccessible; retry shortly."})
				}
			}
			return previous
		}
		defer unlock()
		if latest, err := cache.Read(path); err == nil {
			previous = latest
		}
		next := e.Refresh(requestCtx, previous, ids, time.Now(), *maxAge, staleOnly)
		if err := cache.Write(path, next); err != nil {
			fmt.Fprintln(diagnostics, "aiu: cannot save latest quota cache")
		}
		return next
	}
	if !*cached {
		snapshot = refresh(snapshot, *status)
	}
	terminal := isTerminal(out)
	color := terminal && !*noColor && os.Getenv("NO_COLOR") == ""
	if *status {
		fmt.Fprint(out, ui.Status(snapshot, ids, time.Now(), *maxAge))
		return 0
	}
	render := func() {
		if *watch && terminal {
			fmt.Fprint(out, "\x1b[H\x1b[2J")
		}
		if *compact {
			fmt.Fprint(out, ui.Compact(snapshot, ids, time.Now(), *maxAge))
		} else {
			fmt.Fprint(out, ui.Display(snapshot, ids, time.Now(), *maxAge, color))
		}
		if *watch {
			fmt.Fprintf(out, "\nRefreshing every %s · Ctrl-C to exit\n", *interval)
		}
	}
	if !*watch {
		render()
		return 0
	}
	if terminal {
		fmt.Fprint(out, "\x1b[?25l")
		defer fmt.Fprint(out, "\x1b[?25h")
	}
	render()
	ticks := time.NewTicker(time.Second)
	defer ticks.Stop()
	polls := time.NewTicker(*interval)
	defer polls.Stop()
	results := make(chan quota.Snapshot, 1)
	busy := false
	for {
		select {
		case <-ctx.Done():
			if busy {
				<-results
			}
			return 0
		case <-ticks.C:
			if terminal {
				render()
			}
		case <-polls.C:
			if !busy {
				busy = true
				previous := snapshot
				go func() { results <- refresh(previous, false) }()
			}
		case next := <-results:
			snapshot = next
			busy = false
			render()
		}
	}
}

func parseProviders(value string) ([]string, error) {
	var ids []string
	seen := map[string]bool{}
	for _, id := range strings.Split(value, ",") {
		id = strings.TrimSpace(id)
		if id != "claude" && id != "openai" && id != "copilot" {
			return nil, fmt.Errorf("unknown provider %q", id)
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	return ids, nil
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0 && os.Getenv("TERM") != "dumb"
}
