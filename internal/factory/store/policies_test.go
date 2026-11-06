package store

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// P9, P10, P11: unset reads are empty, writes survive reopening, and rejected
// replacements leave both the saved policy and its version alone.
func TestPoliciesPersist(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	empty, err := st.Policy("g")
	if err != nil || empty.Group != "g" || empty.Stopped || empty.Version != 0 || empty.Allowances == nil {
		t.Fatalf("empty = %+v, %v", empty, err)
	}
	p, err := st.ChangePolicy("g", func(p *factory.GroupPolicy) error { p.Stopped = true; return nil })
	if err != nil || p.Version != 1 {
		t.Fatalf("write = %+v, %v", p, err)
	}
	other, err := Open(st.Root())
	if err != nil {
		t.Fatal(err)
	}
	p, err = other.Policy("g")
	if err != nil || !p.Stopped || p.Version != 1 {
		t.Fatalf("restart = %+v, %v", p, err)
	}
	refused := errors.New("refused")
	if _, err := other.ChangePolicy("g", func(p *factory.GroupPolicy) error { p.Stopped = false; return refused }); !errors.Is(err, refused) {
		t.Fatal(err)
	}
	p, err = st.Policy("g")
	if err != nil || !p.Stopped || p.Version != 1 {
		t.Fatalf("refusal wrote = %+v, %v", p, err)
	}
	if jobs, err := st.List(); err != nil || len(jobs) != 0 {
		t.Fatalf("policy became a job: %+v, %v", jobs, err)
	}
}

// P11: independent stores serialize replacements of different groups.
func TestPoliciesShareLock(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, group := range []string{"g1", "g2", "g3"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other, err := Open(st.Root())
			if err == nil {
				_, err = other.ChangePolicy(group, func(p *factory.GroupPolicy) error { p.Stopped = true; return nil })
			}
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, group := range []string{"g1", "g2", "g3"} {
		if p, err := st.Policy(group); err != nil || !p.Stopped || p.Version != 1 {
			t.Fatalf("lost %s: %+v, %v", group, p, err)
		}
	}
}

// P13: malformed settings refuse reads and writes instead of disappearing.
func TestPoliciesUnreadable(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"{", "null", `{"g":{"group":"g","allowances":[{"id":"m2","jobs":[1],"cap_usd":0,"ends_at":"2026-11-12T00:00:00Z"}]}}`} {
		if err := os.WriteFile(filepath.Join(st.Root(), policiesFile), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Policy("g"); err == nil {
			t.Fatalf("read accepted %s", raw)
		}
		called := false
		if _, err := st.ChangePolicy("g", func(*factory.GroupPolicy) error { called = true; return nil }); err == nil || called {
			t.Fatalf("change accepted %s: %v", raw, err)
		}
		if after, err := os.ReadFile(filepath.Join(st.Root(), policiesFile)); err != nil || string(after) != raw {
			t.Fatalf("refusal wrote %s, %v", after, err)
		}
	}
}
