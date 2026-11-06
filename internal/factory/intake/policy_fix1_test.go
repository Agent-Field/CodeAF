package intake_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// P2: intake neither starts nor asks about grouped releases without an allowance.
// The listing guard also avoids loading rules or scouting those jobs.
func TestIntakePolicyGroupGuards(t *testing.T) {
	for _, duringLoad := range []bool{false, true} {
		for _, ask := range []bool{false, true} {
			t.Run(fmt.Sprintf("during_load=%t/ask=%t", duringLoad, ask), func(t *testing.T) {
				st, err := store.Open(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				group := "g"
				if duringLoad {
					group = ""
				}
				job, err := st.Create(factory.Item{Repo: "owner/api", Kind: factory.KindIssue, State: factory.StateNew, Tier: factory.TierOwner, Group: group, WaitsOn: []int{99}, WaitsReleased: true, Labels: []string{"agent-ready", "size:S"}})
				if err != nil {
					t.Fatal(err)
				}
				history, err := record.Open(st, record.Options{})
				if err != nil {
					t.Fatal(err)
				}
				rules := "default_budget: 1\nstart:\n  labels: [agent-ready]\n"
				if ask {
					rules += "  ask_first: [changes secrets]\n"
				}
				file, err := pipeline.Parse(rules)
				if err != nil {
					t.Fatal(err)
				}
				loaded, resume := make(chan struct{}), make(chan struct{})
				loads, scouts, launches := 0, 0, 0
				in := intake.New(st, intake.Options{Record: history, Load: func(string) (pipeline.File, error) {
					loads++
					if duringLoad {
						close(loaded)
						<-resume
					}
					return file, nil
				}, Scout: func(context.Context, int) error { scouts++; return nil }, Launch: func(int) error { launches++; return nil }})
				before, err := st.Get(job.ID)
				if err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() { done <- in.Once(context.Background()) }()
				if duringLoad {
					select {
					case <-loaded:
					case err := <-done:
						t.Fatalf("intake returned before Load: %v", err)
					case <-time.After(5 * time.Second):
						t.Fatal("intake did not reach Load")
					}
					err := st.Update(job.ID, func(it *factory.Item) error { it.Group = "g"; return nil })
					if err == nil {
						before, err = st.Get(job.ID)
					}
					close(resume)
					if err != nil {
						<-done
						t.Fatal(err)
					}
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				after, err := st.Get(job.ID)
				if err != nil {
					t.Fatal(err)
				}
				events, err := history.Since(context.Background(), time.Time{})
				if err != nil {
					t.Fatal(err)
				}
				if launches != 0 || after.State != factory.StateNew || after.PolicyRun != "" || after.QKind != "" || after.AskedFirstReason != "" || !reflect.DeepEqual(before, after) || len(events) != 0 {
					t.Fatalf("intake admitted grouped job: launches=%d job=%+v events=%+v", launches, after, events)
				}
				if !duringLoad && (loads != 0 || scouts != 0) {
					t.Fatalf("grouped job read rules %d times and scouted %d times", loads, scouts)
				}
			})
		}
	}
}
