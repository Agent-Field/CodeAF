package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestFactoryLifetimeCostUsesTaggedLedgerWithoutLoweringKnownSpend(t *testing.T) {
	s := newLocalSetup(t)
	st := s.start()
	floor := s.backend.local.host.floor
	it, err := floor.Create(factory.Item{Repo: "acme/api", Num: 7, Title: "Keep lifetime", Kind: factory.KindIssue, State: factory.StateNew, Stream: &factory.Stream{Spent: .030189}})
	if err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(t.TempDir(), session.UsageLedgerName)
	old := appLedgerPath
	appLedgerPath = func() string { return ledger }
	t.Cleanup(func() { appLedgerPath = old })
	read := func(want float64) {
		t.Helper()
		var job api.Job
		s.must("factory.job", map[string]any{"factory": st.Factory, "job": it.ID}, &job)
		if math.Abs(job.LifetimeCost-want) > 1e-9 || job.Cost != .030189 {
			t.Fatalf("job lifetime=%g run=%g, want %g / .030189", job.LifetimeCost, job.Cost, want)
		}
		var queue api.Queue
		s.must("factory.queue", map[string]any{"factory": st.Factory}, &queue)
		for _, row := range queue.Jobs {
			if row.ID == it.ID {
				if math.Abs(row.LifetimeCost-want) > 1e-9 {
					t.Fatalf("queue lifetime=%g, want %g", row.LifetimeCost, want)
				}
				return
			}
		}
		t.Fatal("job missing from queue")
	}
	read(.030189) // No ledger: show only known money.
	write := func(costs ...float64) {
		t.Helper()
		var data []byte
		for _, cost := range costs {
			row, err := json.Marshal(session.UsageLine{At: time.Now(), FactoryJob: factory.LedgerName(it), USD: cost})
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, append(row, '\n')...)
		}
		if err := os.WriteFile(ledger, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(.138363, .030189)
	read(.168552)
	read(.168552) // Unchanged metadata reuses the ledger reading.
	if err := floor.Annotate(it.ID, func(it *factory.Item) error { it.SpentBefore = .2; return nil }); err != nil {
		t.Fatal(err)
	}
	read(.230189) // A smaller ledger cannot erase stored money.
	write(.01)
	read(.230189)
}
