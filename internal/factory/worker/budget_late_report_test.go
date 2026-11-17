package worker_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestDroppedLateCapReportResumesAndStopsAtRaisedCap(t *testing.T) {
	frozen, deliver, resumed, spend := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var deliverOnce, spendOnce, freezeOnce sync.Once
	// Unblock requests before the rig waits for its worker during cleanup.
	release := func() { deliverOnce.Do(func() { close(deliver) }); spendOnce.Do(func() { close(spend) }) }
	gReports := func() []string { return nil }
	wait := func(ch <-chan struct{}, why string) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s; reports=%s", why, gReports())
		}
	}
	var calls atomic.Int32
	g := newRigWithDelivery(t, nil, func(o *run.Options) {
		o.Exec = map[factory.StageKind]run.Executor{factory.StageCheck: run.ExecutorFunc(func(ctx context.Context, j run.Job) (factory.StageResult, error) {
			calls.Add(1)
			if j.Stage.Name == "second" {
				close(resumed)
				select {
				case <-spend:
				case <-ctx.Done():
					return factory.StageResult{}, ctx.Err()
				}
			}
			return factory.StageResult{Done: true, Spent: 5}, nil
		})}
	}, 20*time.Millisecond, func(w http.ResponseWriter, r *http.Request) bool {
		if isReport(r) && strings.Contains(peek(r), "frozen cap report") {
			freezeOnce.Do(func() { close(frozen) })
			select {
			case <-deliver:
			case <-r.Context().Done():
				return true
			}
		}
		return false
	}, nil, 1, "me")
	gReports = g.seen.all
	defer release()
	id := g.add(t, factory.Stage{Name: "first", Kind: factory.StageCheck, On: true}, factory.Stage{Name: "second", Kind: factory.StageCheck, On: true}, factory.Stage{Name: "third", Kind: factory.StageCheck, On: true})
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.Cap, it.Autonomy, it.Assignee = 5, factory.AutonomyAuto, "me"
		it.Notes = []string{"initial"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.doors.Launch(id); err != nil {
		t.Fatal(err)
	}
	initial := g.waitFor(t, id, factory.StateNeedsYou)
	if initial.QKind != "cap" {
		t.Fatalf("initial question=%q kind=%q", initial.Question, initial.QKind)
	}
	// A parked update freezes a real worker report with its old cap question.
	if err := g.mirror.Update(id, func(it *factory.Item) error { it.Notes = append(it.Notes, "frozen cap report"); return nil }); err != nil {
		t.Fatal(err)
	}
	wait(frozen, "parked worker did not freeze its cap report")
	g.raise(t, id, 10)
	deliverOnce.Do(func() { close(deliver) })
	wait(resumed, "raised worker did not enter second step")
	read := func() api.Job {
		t.Helper()
		resp, err := http.Get(fmt.Sprintf("%s/v1/jobs/%d", g.base, id))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("read job: HTTP %d", resp.StatusCode)
		}
		var got api.Job
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		got := read()
		local := api.ToItem(got)
		if slices.Contains(local.Notes, "frozen cap report") {
			if got.State != api.StateRunning || got.SpendingLimit != nil || got.LifetimeCost != 5 {
				t.Fatalf("dropped report left job stuck: state=%s limit=%+v lifetime=%g", got.State, got.SpendingLimit, got.LifetimeCost)
			}
			for _, q := range got.Questions {
				if q.State == "open" {
					t.Fatalf("dropped cap question remains open: %+v", q)
				}
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("frozen report did not apply: reports=%s", g.seen.all())
		}
		time.Sleep(5 * time.Millisecond)
	}
	spendOnce.Do(func() { close(spend) })
	g.waitFor(t, id, factory.StateNeedsYou)
	// Observe more report/heartbeat cycles to catch an incorrectly admitted third step.
	deadline = time.Now().Add(300 * time.Millisecond)
	for {
		got := read()
		if got.State != api.StateNeedsYou || got.SpendingLimit == nil || got.SpendingLimit.Limit != 10 || got.SpendingLimit.Spent != 10 || got.LifetimeCost != 10 || calls.Load() != 2 {
			t.Fatalf("raised cap did not stop paid work: state=%s limit=%+v lifetime=%g calls=%d reports=%s", got.State, got.SpendingLimit, got.LifetimeCost, calls.Load(), g.seen.all())
		}
		open := 0
		for _, q := range got.Questions {
			if q.State == "open" {
				open++
				if q.Kind != "cap" || !strings.Contains(q.Text, "budget of $10 reached") {
					t.Fatalf("wrong raised-cap question: %+v", q)
				}
			}
		}
		local := api.ToItem(got)
		if open != 1 || local.Stream.Phases[2].Round != 0 {
			t.Fatalf("cap question or unstarted third step lost: questions=%+v phases=%+v", got.Questions, local.Stream.Phases)
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}
