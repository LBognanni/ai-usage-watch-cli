package quota

import (
	"testing"
	"time"
)

func TestFresh(t *testing.T) {
	now := time.Now()
	p := Provider{Status: "live", CheckedAt: now.Add(-time.Minute)}
	if !p.Fresh(now, 5*time.Minute) {
		t.Fatal("fresh snapshot rejected")
	}
	if p.Fresh(now, time.Minute) {
		t.Fatal("old snapshot accepted")
	}
	p.Windows = []Window{{ResetsAt: now.Add(-time.Second)}}
	if p.Fresh(now, 5*time.Minute) {
		t.Fatal("expired window accepted")
	}
	p.Windows = nil
	p.Status = "stale"
	if p.Fresh(now, 5*time.Minute) {
		t.Fatal("stale state accepted")
	}
}
