package lease

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func policyReportFixture(t *testing.T, repo string) (*store.Store, *Desk, Lease, *time.Time) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	job, err := st.Create(factory.Item{Repo: repo, Kind: factory.KindIssue, State: factory.StateQueued, Assignee: "owner", Group: "g", PolicyRun: "m2", Cap: 20, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", State: factory.PhaseRunning, Round: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ChangePolicy("g", func(p *factory.GroupPolicy) error {
		p.Allowances = []factory.Allowance{{ID: "m2", Jobs: []int{job.ID, 2}, CapUSD: 10, EndsAt: now.Add(time.Hour).Format(time.RFC3339)}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	desk, err := Open(st, Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	held, err := desk.Pull(Worker{ID: "w", Handle: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	return st, desk, held, &now
}

// P5: resolving an alias after leasing must not make a report lock itself.
func TestPolicyReportDoesNotMigrateRepository(t *testing.T) {
	st, desk, held, now := policyReportFixture(t, "api")
	if err := st.SetRepos([]string{"owner/api"}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Second)
	done := make(chan error, 1)
	go func() {
		done <- desk.Apply(held.ID, "w", false, false, func(it *factory.Item) error { it.Stream.Spent = 1; return nil })
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("report deadlocked after repository alias became resolvable")
	}
	it, err := st.ReadOnly(held.Job)
	if err != nil || it.Repo != "owner/api" || it.Stream.Spent != 1 {
		t.Fatalf("report did not persist normally: %+v, %v", it, err)
	}
}

// P5: only the allowance's other documents are read, once per two seconds.
// An unrelated floor of 1,000 jobs must leave that count unchanged.
func TestPolicyReportDocumentWork(t *testing.T) {
	st, desk, held, now := policyReportFixture(t, "owner/api")
	reads := 0
	desk.policySpend.read = func(id int) (factory.Item, error) {
		reads++
		return st.ReadOnly(id)
	}
	report := func(want int) {
		t.Helper()
		reads = 0
		if err := desk.Apply(held.ID, "w", false, false, func(*factory.Item) error { return nil }); err != nil {
			t.Fatal(err)
		}
		if reads != want {
			t.Fatalf("read %d documents, want %d", reads, want)
		}
	}
	*now = now.Add(2 * time.Second)
	report(1)
	for id := 3; id < 1003; id++ {
		if err := st.Save(factory.Item{ID: id, Repo: "owner/api", Kind: factory.KindIssue, State: factory.StateNew}); err != nil {
			t.Fatal(err)
		}
	}
	*now = now.Add(2 * time.Second)
	report(1)
	*now = now.Add(2*time.Second - time.Nanosecond)
	report(0)
	*now = now.Add(time.Nanosecond)
	report(1)
	if _, err := st.ChangePolicy("g", func(p *factory.GroupPolicy) error {
		p.Allowances[0].Jobs = append(p.Allowances[0].Jobs, 3)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	report(2)
	if _, err := st.ChangePolicy("g", func(p *factory.GroupPolicy) error { p.Stopped = true; return nil }); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Second)
	report(0)
	it, err := st.ReadOnly(held.Job)
	if err != nil || !it.StopAfterStep {
		t.Fatalf("stopped report did not arm boundary: %+v, %v", it, err)
	}
}

// P5, P9: unreadable listed jobs count as zero; the locked spend is current
// even when the allowance snapshot was populated before the latest report.
func TestPolicyReportCurrentSpendAndUnreadableJob(t *testing.T) {
	st, desk, held, now := policyReportFixture(t, "owner/api")
	if err := os.WriteFile(st.ItemPath(2), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Second)
	if err := desk.Apply(held.ID, "w", false, false, func(it *factory.Item) error { it.Stream.Spent = 9; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := desk.Apply(held.ID, "w", false, false, func(it *factory.Item) error { it.Stream.Spent = 10; return nil }); !errors.Is(err, ErrWithdrawn) {
		t.Fatalf("cached own spend escaped cap: %v", err)
	}
}

// P5, P9: the lease gate and policy read agree at a fractional-dollar cap.
func TestPolicyReportDecimalCap(t *testing.T) {
	st, desk, held, _ := policyReportFixture(t, "owner/api")
	if err := st.Save(factory.Item{ID: 2, Repo: "owner/api", ChatSpent: 0.10}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ChangePolicy("g", func(p *factory.GroupPolicy) error {
		p.Allowances[0].CapUSD = 0.80
		p.Allowances[0].ID = "decimal"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	err := desk.Apply(held.ID, "w", false, false, func(it *factory.Item) error {
		it.PolicyRun, it.Stream.Spent = "decimal", 0.70
		return nil
	})
	if !errors.Is(err, ErrWithdrawn) {
		t.Fatalf("exact decimal cap = %v", err)
	}
	it, err := st.ReadOnly(held.Job)
	if err != nil || it.Question != "allowance decimal reached its $0.80 cap" || it.Stream.Spent != 0.70 {
		t.Fatalf("cap hold = %+v, %v", it, err)
	}
}

// P7: a running report for the same saved step is progress, whereas a new
// phase or round is admission even before the previous phase reports done.
func TestPolicyStopAdmitsOnlySavedSteps(t *testing.T) {
	for _, kind := range []string{"progress", "phase", "round"} {
		t.Run(kind, func(t *testing.T) {
			st, desk, held, now := policyReportFixture(t, "owner/api")
			if _, err := st.ChangePolicy("g", func(p *factory.GroupPolicy) error { p.Stopped = true; return nil }); err != nil {
				t.Fatal(err)
			}
			err := desk.Apply(held.ID, "w", false, false, func(it *factory.Item) error {
				it.Stream.Spent = 0.25
				it.Stream.Phases[0].Started = now.Add(-time.Hour)
				switch kind {
				case "phase":
					it.Stream.Phases = append(it.Stream.Phases, factory.Phase{Name: "next", State: factory.PhaseRunning, Round: 1})
					it.Stream.Cur = 1
				case "round":
					it.Stream.Phases[0].Round++
				}
				return nil
			})
			it, readErr := st.ReadOnly(held.Job)
			if readErr != nil || factory.JobLifetimeSpent(it) != 0.25 {
				t.Fatalf("report lost spend: %+v, %v", it, readErr)
			}
			if kind == "progress" {
				if err != nil || !it.StopAfterStep || it.State != factory.StateRunning {
					t.Fatalf("admitted step was cut: %+v, %v", it, err)
				}
			} else if !errors.Is(err, ErrWithdrawn) || it.Blocked == nil || it.Blocked.Reason != "its group is stopped" {
				t.Fatalf("new admission escaped stop: %+v, %v", it, err)
			}
		})
	}
}
