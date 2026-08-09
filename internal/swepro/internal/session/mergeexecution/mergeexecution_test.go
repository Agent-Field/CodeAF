package mergeexecution

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func runnablePortfolio() ([]AttemptResult, *AttemptResult) {
	best := AttemptResult{
		Tag: "a", Workspace: "/winner", GateStatus: "pass",
		ClauseCoverage: []ClauseCoverage{
			{Clause: "Winner clause", Evidence: "winner evidence"},
		},
	}
	loser := AttemptResult{
		Tag: "b", Workspace: "/loser", GateStatus: "fail",
		ClauseCoverage: []ClauseCoverage{
			{Clause: "Loser clause", Evidence: "loser evidence"},
		},
	}
	return []AttemptResult{best, loser}, &best
}

func TestRunReconciliationSkipDoesNotTouchEffects(t *testing.T) {
	attempts, best := runnablePortfolio()
	var logs []string
	outcome, err := RunReconciliation(RunOptions{
		Attempts: attempts, Best: best, NoReconcile: true,
		Deps: ReconcileDeps{
			CheckDisk: func(string) (DiskEnvelope, error) {
				t.Fatal("disk check should not run")
				return DiskEnvelope{}, nil
			},
			Log: func(line string) { logs = append(logs, line) },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Ran || outcome.Skip != "disabled" ||
		outcome.Reason != "reconciliation disabled (--no-reconcile)" {
		t.Fatalf("outcome = %#v", outcome)
	}
	if !reflect.DeepEqual(logs, []string{
		"[codeaf] reconcile: skipped — reconciliation disabled (--no-reconcile)",
	}) {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestRunReconciliationDiskFloor(t *testing.T) {
	attempts, best := runnablePortfolio()
	var logs []string
	envelope := DiskEnvelope{
		FreeBytes: 1331439861, FreeGB: 1.239999999, FloorGB: 5, OK: false,
	}
	outcome, err := RunReconciliation(RunOptions{
		Attempts: attempts, Best: best,
		Deps: ReconcileDeps{
			CheckDisk: func(path string) (DiskEnvelope, error) {
				if path != "/winner" {
					t.Fatalf("disk path = %q", path)
				}
				return envelope, nil
			},
			SpawnChild: func(string, string) (int, error) {
				t.Fatal("spawn should not run")
				return 0, nil
			},
			Log: func(line string) { logs = append(logs, line) },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantReason := "disk below floor before reconciliation spawn " +
		"(free=1.24GB floor=5GB) — skipping"
	if outcome.Ran || outcome.Skip != "disk-floor" ||
		outcome.Reason != wantReason ||
		outcome.Envelope == nil || *outcome.Envelope != envelope {
		t.Fatalf("outcome = %#v", outcome)
	}
	if !reflect.DeepEqual(logs, []string{"[codeaf] reconcile: " + wantReason}) {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestRunReconciliationRunsOneChildAndRereadsVerdict(t *testing.T) {
	attempts, best := runnablePortfolio()
	var logs []string
	var spawnedMessage string
	post := map[string]any{"verdict": "pass", "extra": 7.0}
	envelope := DiskEnvelope{FreeBytes: 10, FreeGB: 10, FloorGB: 5, OK: true}
	outcome, err := RunReconciliation(RunOptions{
		Attempts: attempts, Best: best,
		Deps: ReconcileDeps{
			CheckDisk: func(path string) (DiskEnvelope, error) {
				return envelope, nil
			},
			SpawnChild: func(workspace, message string) (int, error) {
				if workspace != "/winner" {
					t.Fatalf("spawn workspace = %q", workspace)
				}
				spawnedMessage = message
				return 17, nil
			},
			ReadVerdict: func(workspace string) (any, error) {
				if workspace != "/winner" {
					t.Fatalf("verdict workspace = %q", workspace)
				}
				return post, nil
			},
			Log: func(line string) { logs = append(logs, line) },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Ran || outcome.ChildCode != 17 ||
		outcome.PostGateStatus != "pass" ||
		!reflect.DeepEqual(outcome.PostVerdict, post) ||
		outcome.Message != spawnedMessage ||
		!strings.Contains(outcome.Message, "### From attempt b (/loser)") {
		t.Fatalf("outcome = %#v", outcome)
	}
	if len(logs) != 2 ||
		!strings.Contains(logs[0], "launching child on winner /winner") ||
		logs[1] != "[codeaf] reconcile: child exited code=17; post-reconciliation gate=pass" {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestRunReconciliationPropagatesInjectedFailures(t *testing.T) {
	attempts, best := runnablePortfolio()
	want := errors.New("disk rejected")
	_, err := RunReconciliation(RunOptions{
		Attempts: attempts, Best: best,
		Deps: ReconcileDeps{
			CheckDisk: func(string) (DiskEnvelope, error) {
				return DiskEnvelope{}, want
			},
		},
	})
	if err != want {
		t.Fatalf("error = %v, want identity %v", err, want)
	}
}

func TestWithStaggerDelaysBeforePositiveIndex(t *testing.T) {
	var events []string
	launch := WithStagger(
		func(tag string, index float64) (string, error) {
			events = append(events, "launch:"+tag)
			return tag, nil
		},
		StaggerDeps{
			Sleep: func(ms float64) error {
				events = append(events, "sleep:"+jsNumber(ms))
				return nil
			},
			Rand: func() float64 { return 0.5 },
		},
	)
	if got, err := launch("first", 0); err != nil || got != "first" {
		t.Fatalf("first got=%q err=%v", got, err)
	}
	if got, err := launch("second", 1); err != nil || got != "second" {
		t.Fatalf("second got=%q err=%v", got, err)
	}
	if !reflect.DeepEqual(events, []string{
		"launch:first", "sleep:3000", "launch:second",
	}) {
		t.Fatalf("events = %#v", events)
	}
}

func TestWithStaggerSleepFailurePreventsLaunch(t *testing.T) {
	want := errors.New("timer failed")
	called := false
	launch := WithStagger(
		func(string, float64) (int, error) {
			called = true
			return 1, nil
		},
		StaggerDeps{
			Sleep: func(float64) error { return want },
			Rand:  func() float64 { return 0 },
		},
	)
	got, err := launch("b", 1)
	if err != want || got != 0 || called {
		t.Fatalf("got=%d err=%v called=%v", got, err, called)
	}
}

func jsNumber(value float64) string {
	if value == 3000 {
		return "3000"
	}
	return "unexpected"
}
