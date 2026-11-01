package vaultsync

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// This source keeps authored values separate from received display values.
// Applying a remote window cannot change what the next capture publishes.
type windowSource struct {
	local, received map[string]string
	live            map[string]bool
}

func (s *windowSource) Values() (map[string]string, error)   { return s.local, nil }
func (s *windowSource) Holds(name string) bool               { return s.live[name] }
func (s *windowSource) Apply(values map[string]string) error { s.received = values; return nil }

func TestDeviceWindowsRateLimitOnlyOwnTrafficAndDeleteOnTheNextRound(t *testing.T) {
	home := t.TempDir()
	v, err := keys.Open(home)
	must(t, err)
	s := &windowSource{local: map[string]string{"alpha": "first"}, live: map[string]bool{"alpha": true}}
	w := Windows("traffic", "a", s, filepath.Join(home, "traffic.ledger"), teams.TrafficSyncEvery)
	now := time.Unix(100, 0)
	w.now = func() time.Time { return now }
	must(t, w.capture(v))
	s.local["alpha"] = "second"
	now = now.Add(teams.TrafficSyncEvery - time.Nanosecond)
	must(t, w.capture(v))
	e, err := v.Get(w.scope + "/alpha/a")
	must(t, err)
	if e.Value != "first" {
		t.Fatal("published before 30 seconds")
	}
	now = now.Add(time.Nanosecond)
	must(t, w.capture(v))
	e, err = v.Get(w.scope + "/alpha/a")
	must(t, err)
	if e.Value != "second" {
		t.Fatal("missed first eligible round")
	}
	must(t, v.PutAt(w.scope+"/alpha/b", keys.Entry{Name: "alpha/b", Scope: w.scope, Value: "remote"}, now))
	must(t, w.restore(v))
	if s.received["alpha/b"] != "remote" {
		t.Fatal("lost remote window")
	}
	before, _ := os.ReadFile(filepath.Join(home, "vault.enc"))
	s.received = nil
	must(t, w.capture(v))
	if s.received["alpha/b"] != "remote" {
		t.Fatal("quiet capture skipped display refresh when the shared object did not change")
	}
	after, _ := os.ReadFile(filepath.Join(home, "vault.enc"))
	if string(before) != string(after) {
		t.Fatal("quiet capture wrote or republished remote lines")
	}
	delete(s.live, "alpha")
	delete(s.local, "alpha")
	must(t, w.capture(v))
	if deleted, err := v.Deleted(w.scope + "/alpha/a"); err != nil || !deleted {
		t.Fatal("deletion waited for rate limit")
	}
	if _, err := v.Get(w.scope + "/alpha/b"); err != nil {
		t.Fatal("writer removed another writer's value")
	}
}
