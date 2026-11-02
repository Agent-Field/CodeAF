package run

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// checkRig runs write (a chat), then unit (a check, until green, max rounds),
// then proof. The check fails its first `fails` runs. It answers the rig and
// the order steps ran in, with the notes each write was given.
func checkRig(t *testing.T, fails, max int) (*rig, int, func() []string, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var ran, notes []string
	checks := 0
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			mu.Lock()
			ran = append(ran, job.Stage.Name)
			if job.Stage.Name == "write" {
				notes = append(notes, strings.Join(job.Notes, " | "))
			}
			mu.Unlock()
			return done(job.Stage.Name + " went fine"), nil
		}),
		factory.StageCheck: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			mu.Lock()
			defer mu.Unlock()
			ran = append(ran, job.Stage.Name)
			checks++
			if checks <= fails {
				return factory.StageResult{Done: true, Exit: 1, Findings: 1, Output: "--- FAIL: TestLogin\nwant 200, got 500"}, nil
			}
			return factory.StageResult{Done: true, Exit: 0, Output: "ok"}, nil
		}),
	}, func(o *Options) { o.Benches = 0 })
	unit := factory.Stage{Name: "unit", Kind: factory.StageCheck, Ask: "go test ./...", Until: factory.UntilGreen, Max: max}
	id := g.add("loop", chat("write"), unit, chat("proof"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	get := func() []string { mu.Lock(); defer mu.Unlock(); return append([]string{}, ran...) }
	gotNotes := func() []string { mu.Lock(); defer mu.Unlock(); return append([]string{}, notes...) }
	return g, id, get, gotNotes
}

// A FAILING CHECK SENDS THE WORK BACK TO THE CHAT BEFORE IT, with the failure,
// and runs again after it: the loop write → check goes round until the check
// passes, within the check's max.
func TestAFailingCheckSendsTheWorkBackToTheChatBeforeIt(t *testing.T) {
	g, id, ran, notes := checkRig(t, 2, 3)
	g.waitState(id, factory.StateLanded)
	if got := strings.Join(ran(), ","); got != "write,unit,write,unit,write,unit,proof" {
		t.Fatalf("ran %s", got)
	}
	n := notes()
	if len(n) != 3 || !strings.Contains(n[1], "the check `go test ./...` failed") || !strings.Contains(n[1], "want 200, got 500") {
		t.Fatalf("notes %q", n)
	}
}

// ITS MAX BOUNDS THE LOOP: a check that keeps failing goes round its max
// times, then asks the person, as a step out of rounds always has.
func TestACheckThatKeepsFailingAsksThePersonAfterItsMax(t *testing.T) {
	g, id, ran, _ := checkRig(t, 99, 2)
	it := g.wait(id, "out of rounds", func(it factory.Item) bool {
		return it.State == factory.StateNeedsYou && it.QKind == factory.QKindOutOfAttempts
	})
	if got := strings.Join(ran(), ","); got != "write,unit,write,unit" {
		t.Fatalf("ran %s", got)
	}
	if !strings.Contains(it.Question, "unit") {
		t.Fatalf("question %q", it.Question)
	}
}

// A CHECK WITH NO CHAT BEFORE IT runs again on its own, as before.
func TestACheckFirstInItsPipelineRunsAgainOnItsOwn(t *testing.T) {
	var mu sync.Mutex
	runs := 0
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) { return done("ok"), nil }),
		factory.StageCheck: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			mu.Lock()
			defer mu.Unlock()
			runs++
			if runs == 1 {
				return factory.StageResult{Done: true, Exit: 1, Findings: 1, Output: "flaky"}, nil
			}
			return factory.StageResult{Done: true, Exit: 0, Output: "ok"}, nil
		}),
	}, func(o *Options) { o.Benches = 0 })
	id := g.add("first", factory.Stage{Name: "unit", Kind: factory.StageCheck, Ask: "go test ./...", Until: factory.UntilGreen, Max: 3}, chat("proof"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitState(id, factory.StateLanded)
	if runs != 2 {
		t.Fatalf("%d runs", runs)
	}
}
