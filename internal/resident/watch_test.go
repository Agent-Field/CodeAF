package resident

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestWatchRestartResumesReservedWakeWithoutDoubleFireOrSkip(t *testing.T) {
	graph := openStore(t)
	charter := residentTestCharter(t, "restart-watch", store.CharterRails{
		PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3,
	}, false)
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	promoteCharterForWatchTest(t, graph, charter.ID)
	stored, _, _ := graph.Charter(charter.ID)
	wakeAt := stored.NextDue.Add(time.Second)
	wakeSeq, err := graph.BeginCharterWake(charter.ID, wakeAt, "poll occurrence", store.CharterWatchState{
		NextDue: wakeAt.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	sentinel := func(_ context.Context, _ SentinelPrompt) (SentinelVerdict, error) {
		calls++
		return SentinelVerdict{Yes: true, Line: "a release landed"}, nil
	}
	reconciler := New(graph, nil, nil).WithWatchEngine(0, sentinel)
	reconciler.now = func() time.Time { return wakeAt }
	pass, err := reconciler.WatchOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || pass.Checked != 1 || pass.Fired != 1 {
		t.Fatalf("first restart pass = %+v calls=%d", pass, calls)
	}

	restarted := New(graph, nil, nil).WithWatchEngine(0, sentinel)
	restarted.now = reconciler.now
	second, err := restarted.WatchOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || second.Fired != 0 || second.Checked != 0 {
		t.Fatalf("second restart pass = %+v calls=%d", second, calls)
	}

	job, found, err := graph.Node(firingPrefix(charter.ID, wakeSeq))
	if err != nil || !found {
		t.Fatalf("firing job found=%t err=%v", found, err)
	}
	if job.Provenance.Origin != store.OriginTrigger || job.Provenance.CharterID != charter.ID ||
		job.Provenance.Intent != charter.Action.Template {
		t.Fatalf("firing provenance = %+v", job.Provenance)
	}
	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	checks, firings := 0, 0
	for _, event := range events {
		if event.Kind == store.EventSentinelChecked {
			checks++
		}
		if event.Kind == store.EventCharterFired {
			firings++
		}
	}
	if checks != 1 || firings != 1 {
		t.Fatalf("journal checks/firings = %d/%d", checks, firings)
	}
}

func TestSentinelNoAndProviderErrorNeverFire(t *testing.T) {
	for _, test := range []struct {
		name    string
		verdict SentinelVerdict
		err     error
	}{
		{name: "no", verdict: SentinelVerdict{Line: "nothing changed"}},
		{name: "provider error", err: errors.New("provider unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph := openStore(t)
			charter := residentTestCharter(t, "sentinel-"+strings.ReplaceAll(test.name, " ", "-"), store.CharterRails{
				PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 2,
			}, false)
			if err := graph.CreateCharter(charter); err != nil {
				t.Fatal(err)
			}
			stored, _, _ := graph.Charter(charter.ID)
			calls := 0
			reconciler := New(graph, nil, nil).WithWatchEngine(0,
				func(_ context.Context, _ SentinelPrompt) (SentinelVerdict, error) {
					calls++
					return test.verdict, test.err
				})
			reconciler.now = func() time.Time { return stored.NextDue.Add(time.Second) }
			pass, err := reconciler.WatchOnce(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || pass.Checked != 1 || pass.Fired != 0 {
				t.Fatalf("pass = %+v calls=%d", pass, calls)
			}
			stored, _, _ = graph.Charter(charter.ID)
			if stored.WakePending || stored.SentinelYes {
				t.Fatalf("failed/no wake stayed pending: %+v", stored)
			}
			events, _ := graph.Events(0, 0)
			checks := 0
			for _, event := range events {
				if event.Kind == store.EventSentinelChecked {
					checks++
				}
			}
			if checks != 1 {
				t.Fatalf("sentinel events = %d, want exactly one", checks)
			}
		})
	}
}

func TestCharterRailsEnforceQuotaExpiryAndDailyPause(t *testing.T) {
	t.Run("quota", func(t *testing.T) {
		graph := openStore(t)
		charter := residentTestCharter(t, "quota-watch", store.CharterRails{
			PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 1,
		}, false)
		if err := graph.CreateCharter(charter); err != nil {
			t.Fatal(err)
		}
		promoteCharterForWatchTest(t, graph, charter.ID)
		calls := 0
		reconciler := New(graph, nil, nil).WithWatchEngine(0,
			func(_ context.Context, _ SentinelPrompt) (SentinelVerdict, error) {
				calls++
				return SentinelVerdict{Yes: true}, nil
			})
		stored, _, _ := graph.Charter(charter.ID)
		reconciler.now = func() time.Time { return stored.NextDue.Add(time.Second) }
		first, err := reconciler.WatchOnce(context.Background())
		if err != nil || first.Fired != 1 {
			t.Fatalf("first firing = %+v err=%v", first, err)
		}
		stored, _, _ = graph.Charter(charter.ID)
		reconciler.now = func() time.Time { return stored.NextDue.Add(time.Second) }
		second, err := reconciler.WatchOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if second.Quota != 1 || second.Fired != 0 || calls != 2 {
			t.Fatalf("quota pass = %+v calls=%d", second, calls)
		}
		firings, err := graph.FiringsToday(charter.ID, time.Now())
		if err != nil || firings != 1 {
			t.Fatalf("firings today = %d err=%v", firings, err)
		}
	})

	t.Run("expiry", func(t *testing.T) {
		graph := openStore(t)
		expires := time.Now().Add(-time.Minute)
		charter := residentTestCharter(t, "expired-watch", store.CharterRails{
			PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 1, ExpiresAt: &expires,
		}, false)
		if err := graph.CreateCharter(charter); err != nil {
			t.Fatal(err)
		}
		if err := graph.SetCharterStatus(charter.ID, store.CharterPaused, store.Ratification{}); err != nil {
			t.Fatal(err)
		}
		calls := 0
		reconciler := New(graph, nil, nil).WithWatchEngine(0,
			func(_ context.Context, _ SentinelPrompt) (SentinelVerdict, error) {
				calls++
				return SentinelVerdict{Yes: true}, nil
			})
		stored, _, _ := graph.Charter(charter.ID)
		reconciler.now = func() time.Time { return stored.NextDue.Add(time.Second) }
		pass, err := reconciler.WatchOnce(context.Background())
		if err != nil || pass.Expired != 1 || calls != 0 {
			t.Fatalf("expiry pass = %+v calls=%d err=%v", pass, calls, err)
		}
		stored, _, _ = graph.Charter(charter.ID)
		if stored.Status != store.CharterRetired {
			t.Fatalf("expired status = %s", stored.Status)
		}
	})

	t.Run("daily rail wait resumes checked yes", func(t *testing.T) {
		graph := openStore(t)
		if err := graph.RecordUsage(store.NodeUsage{NodeID: store.RootID, Cost: 0.95}); err != nil {
			t.Fatal(err)
		}
		charter := residentTestCharter(t, "rail-watch", store.CharterRails{
			PerFiringBudgetUSD: 0.10, MaxFiringsPerDay: 2,
		}, false)
		if err := graph.CreateCharter(charter); err != nil {
			t.Fatal(err)
		}
		promoteCharterForWatchTest(t, graph, charter.ID)
		stored, _, _ := graph.Charter(charter.ID)
		calls := 0
		sentinel := func(_ context.Context, _ SentinelPrompt) (SentinelVerdict, error) {
			calls++
			return SentinelVerdict{Yes: true, Line: "release found"}, nil
		}
		reconciler := New(graph, nil, nil).WithWatchEngine(1, sentinel)
		reconciler.now = func() time.Time { return stored.NextDue.Add(time.Second) }
		pass, err := reconciler.WatchOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if pass.RailWaits != 1 || pass.Fired != 0 || calls != 1 {
			t.Fatalf("rail pass = %+v calls=%d", pass, calls)
		}
		stored, _, _ = graph.Charter(charter.ID)
		if !stored.WakePending || !stored.SentinelYes {
			t.Fatalf("rail did not preserve checked wake: %+v", stored)
		}
		if _, pending, err := graph.PendingDailyRailApproval(1, "charter-session"); err != nil || !pending {
			t.Fatalf("projected rail approval pending=%t err=%v", pending, err)
		}
		if err := graph.RaiseDailyRail(1, "test:user"); err != nil {
			t.Fatal(err)
		}
		restarted := New(graph, nil, nil).WithWatchEngine(1, sentinel)
		restarted.now = reconciler.now
		resumed, err := restarted.WatchOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if resumed.Fired != 1 || calls != 1 {
			t.Fatalf("resumed pass = %+v calls=%d", resumed, calls)
		}
	})
}

func TestSayOnlyCharterPostsAttentionWithoutJob(t *testing.T) {
	graph := openStore(t)
	charter := residentTestCharter(t, "reminder-watch", store.CharterRails{
		PerFiringBudgetUSD: 0.01, MaxFiringsPerDay: 1,
	}, true)
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	promoteCharterForWatchTest(t, graph, charter.ID)
	stored, _, _ := graph.Charter(charter.ID)
	reconciler := New(graph, nil, nil).WithWatchEngine(0,
		func(_ context.Context, _ SentinelPrompt) (SentinelVerdict, error) {
			return SentinelVerdict{Yes: true}, nil
		})
	reconciler.now = func() time.Time { return stored.NextDue.Add(time.Second) }
	pass, err := reconciler.WatchOnce(context.Background())
	if err != nil || pass.Fired != 1 {
		t.Fatalf("say-only pass = %+v err=%v", pass, err)
	}
	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if node.Provenance.Origin == store.OriginTrigger {
			t.Fatalf("say-only charter spliced job %+v", node)
		}
	}
	messages, err := graph.Messages("charter-session", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Role != store.RoleAgent || messages[0].Body != charter.Action.Template {
		t.Fatalf("attention messages = %+v", messages)
	}
}

func promoteCharterForWatchTest(t *testing.T, graph *store.Store, id string) {
	t.Helper()
	if err := graph.PromoteCharter(id, "test fixture exercises tenured watch behavior", false); err != nil {
		t.Fatal(err)
	}
}

func TestFileAndGraphWatchesWakeOnlyOnObservedChange(t *testing.T) {
	t.Run("file mtime", func(t *testing.T) {
		graph := openStore(t)
		path := filepath.Join(t.TempDir(), "watched.txt")
		if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
			t.Fatal(err)
		}
		charter, err := store.NewCharter("file-watch", "Keep the watched file reviewed.", store.WatchSpec{
			Kind: store.WatchFile, File: &store.FileWatch{Glob: path, Cadence: time.Minute},
		}, "Did the file change?", store.CharterAction{Template: "Review the changed file"},
			store.CharterRails{PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3}, store.CharterActive,
			store.Ratification{Origin: store.OriginUser, SessionID: "charter-session", Evidence: "yes"})
		if err != nil {
			t.Fatal(err)
		}
		if err := graph.CreateCharter(charter); err != nil {
			t.Fatal(err)
		}
		calls := 0
		reconciler := New(graph, nil, nil).WithWatchEngine(0,
			func(_ context.Context, _ SentinelPrompt) (SentinelVerdict, error) {
				calls++
				return SentinelVerdict{}, nil
			})
		stored, _, _ := graph.Charter(charter.ID)
		reconciler.now = func() time.Time { return stored.NextDue.Add(time.Second) }
		baseline, err := reconciler.WatchOnce(context.Background())
		if err != nil || calls != 0 || baseline.Checked != 0 {
			t.Fatalf("baseline = %+v calls=%d err=%v", baseline, calls, err)
		}
		stored, _, _ = graph.Charter(charter.ID)
		changedAt := time.Now().Add(2 * time.Hour)
		if err := os.Chtimes(path, changedAt, changedAt); err != nil {
			t.Fatal(err)
		}
		reconciler.now = func() time.Time { return stored.NextDue.Add(time.Second) }
		changed, err := reconciler.WatchOnce(context.Background())
		if err != nil || changed.Checked != 1 || changed.No != 1 || calls != 1 {
			t.Fatalf("changed = %+v calls=%d err=%v", changed, calls, err)
		}
	})

	t.Run("graph settled scope", func(t *testing.T) {
		graph := openStore(t)
		charter, err := store.NewCharter("graph-watch", "Notice parser completion.", store.WatchSpec{
			Kind: store.WatchGraph, Graph: &store.GraphWatch{
				Predicate: store.GraphNodeSettled, Scope: "repo:parser", Cadence: time.Minute,
			},
		}, "Did parser work settle?", store.CharterAction{Template: "Summarize parser completion"},
			store.CharterRails{PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3}, store.CharterActive,
			store.Ratification{Origin: store.OriginUser, SessionID: "charter-session", Evidence: "yes"})
		if err != nil {
			t.Fatal(err)
		}
		if err := graph.CreateCharter(charter); err != nil {
			t.Fatal(err)
		}
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
			ID: "parser-job", Brief: "repair parser", Title: "Parser repair", Stage: 1,
		}}}, store.Provenance{Origin: store.OriginUser, Intent: "repair parser"}); err != nil {
			t.Fatal(err)
		}
		if _, err := graph.RecordFact("parser-job", "repo:parser", store.FactLesson, "parser repair needs the generated fixtures"); err != nil {
			t.Fatal(err)
		}
		claim, won, err := graph.Claim("parser-job", "worker")
		if err != nil || !won {
			t.Fatalf("claim won=%t err=%v", won, err)
		}
		if err := graph.Complete(claim, "parser repaired"); err != nil {
			t.Fatal(err)
		}
		calls := 0
		reconciler := New(graph, nil, nil).WithWatchEngine(0,
			func(_ context.Context, prompt SentinelPrompt) (SentinelVerdict, error) {
				calls++
				if !strings.Contains(prompt.Evidence, "parser-job") {
					t.Errorf("evidence = %q", prompt.Evidence)
				}
				return SentinelVerdict{}, nil
			})
		stored, _, _ := graph.Charter(charter.ID)
		reconciler.now = func() time.Time { return stored.NextDue.Add(time.Second) }
		pass, err := reconciler.WatchOnce(context.Background())
		if err != nil || pass.Checked != 1 || calls != 1 {
			t.Fatalf("graph pass = %+v calls=%d err=%v", pass, calls, err)
		}
	})
}

func TestGraphWatchFailedAndSpendThresholdPredicates(t *testing.T) {
	t.Run("failed node", func(t *testing.T) {
		graph := openStore(t)
		charter := residentGraphCharter(t, "failed-graph-watch", store.GraphWatch{
			Predicate: store.GraphNodeFailed, Title: "database", Cadence: time.Minute,
		})
		if err := graph.CreateCharter(charter); err != nil {
			t.Fatal(err)
		}
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
			ID: "database-job", Brief: "repair database", Title: "Database repair", Stage: 1,
		}}}, store.Provenance{Origin: store.OriginUser, Intent: "repair database"}); err != nil {
			t.Fatal(err)
		}
		claim, won, err := graph.Claim("database-job", "worker")
		if err != nil || !won {
			t.Fatalf("claim won=%t err=%v", won, err)
		}
		if err := graph.Fail(claim, "database unavailable"); err != nil {
			t.Fatal(err)
		}
		calls := 0
		reconciler := New(graph, nil, nil).WithWatchEngine(0,
			func(_ context.Context, prompt SentinelPrompt) (SentinelVerdict, error) {
				calls++
				if !strings.Contains(prompt.Evidence, string(store.EventNodeFailed)) {
					t.Errorf("failed evidence = %q", prompt.Evidence)
				}
				return SentinelVerdict{}, nil
			})
		stored, _, _ := graph.Charter(charter.ID)
		reconciler.now = func() time.Time { return stored.NextDue.Add(time.Second) }
		pass, err := reconciler.WatchOnce(context.Background())
		if err != nil || pass.Checked != 1 || calls != 1 {
			t.Fatalf("failed pass = %+v calls=%d err=%v", pass, calls, err)
		}
	})

	t.Run("daily spend crossing wakes once", func(t *testing.T) {
		graph := openStore(t)
		charter := residentGraphCharter(t, "spend-graph-watch", store.GraphWatch{
			Predicate: store.GraphSpendThreshold, ThresholdUSD: 0.50, Cadence: time.Minute,
		})
		if err := graph.CreateCharter(charter); err != nil {
			t.Fatal(err)
		}
		if err := graph.RecordUsage(store.NodeUsage{NodeID: store.RootID, Cost: 0.60}); err != nil {
			t.Fatal(err)
		}
		calls := 0
		reconciler := New(graph, nil, nil).WithWatchEngine(0,
			func(_ context.Context, prompt SentinelPrompt) (SentinelVerdict, error) {
				calls++
				if !strings.Contains(prompt.Evidence, "$0.60") {
					t.Errorf("spend evidence = %q", prompt.Evidence)
				}
				return SentinelVerdict{}, nil
			})
		stored, _, _ := graph.Charter(charter.ID)
		reconciler.now = func() time.Time { return stored.NextDue.Add(time.Second) }
		first, err := reconciler.WatchOnce(context.Background())
		if err != nil || first.Checked != 1 || calls != 1 {
			t.Fatalf("first spend pass = %+v calls=%d err=%v", first, calls, err)
		}
		stored, _, _ = graph.Charter(charter.ID)
		reconciler.now = func() time.Time { return stored.NextDue.Add(time.Second) }
		second, err := reconciler.WatchOnce(context.Background())
		if err != nil || second.Checked != 0 || calls != 1 {
			t.Fatalf("repeated spend pass = %+v calls=%d err=%v", second, calls, err)
		}
	})
}

func residentTestCharter(t *testing.T, id string, rails store.CharterRails, sayOnly bool) store.Charter {
	t.Helper()
	charter, err := store.NewCharter(id, "Keep releases documented.", store.WatchSpec{
		Kind: store.WatchPoll, Poll: &store.PollWatch{Condition: "look for releases", Cadence: time.Minute},
	}, "Did a release occur?", store.CharterAction{Template: "Update release documentation", SayOnly: sayOnly},
		rails, store.CharterActive, store.Ratification{
			Origin: store.OriginUser, SessionID: "charter-session", Evidence: "yes, stand this up",
		})
	if err != nil {
		t.Fatal(err)
	}
	return charter
}

func residentGraphCharter(t *testing.T, id string, watch store.GraphWatch) store.Charter {
	t.Helper()
	charter, err := store.NewCharter(id, "Notice the graph condition.", store.WatchSpec{
		Kind: store.WatchGraph, Graph: &watch,
	}, "Did the graph condition occur?", store.CharterAction{Template: "Respond to the graph condition"},
		store.CharterRails{PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3}, store.CharterActive,
		store.Ratification{Origin: store.OriginUser, SessionID: "charter-session", Evidence: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	return charter
}

// Wake evidence is read by a model asked whether a condition occurred, never
// how many nanoseconds ago. A nanosecond stamp is a string no two checks can
// share; the minute says the same true thing in reusable bytes, and the
// fingerprint that actually detects the change keeps its full resolution.
func TestFileWatchEvidenceIsRoundedToTheMinute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watched.txt")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	fingerprint, detail, err := fileWatchFingerprint(path)
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint == "" || !strings.Contains(detail, "mtime ") {
		t.Fatalf("fingerprint=%q detail=%q", fingerprint, detail)
	}
	stamp := detail[strings.Index(detail, "mtime ")+len("mtime "):]
	parsed, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		t.Fatalf("evidence stamp %q is not a plain RFC3339 time: %v", stamp, err)
	}
	if parsed.Second() != 0 || parsed.Nanosecond() != 0 {
		t.Fatalf("evidence stamp %q carries sub-minute precision", stamp)
	}
}

// A poll charter's wake evidence is the constant condition string, so without
// a memory the sentinel is asked the identical question every hour and gives
// the identical answer — including after the firing it caused was declined.
func TestTheSentinelIsShownWhatItLastDecidedAndHowItTurnedOut(t *testing.T) {
	graph := openStore(t)
	charter := residentTestCharter(t, "repeat-watch", store.CharterRails{
		PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 5,
	}, false)
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	promoteCharterForWatchTest(t, graph, charter.ID)

	var prompts []SentinelPrompt
	sentinel := func(_ context.Context, prompt SentinelPrompt) (SentinelVerdict, error) {
		prompts = append(prompts, prompt)
		return SentinelVerdict{Yes: false, Line: "nothing has changed since the last look"}, nil
	}
	reconciler := New(graph, nil, nil).WithWatchEngine(0, sentinel)
	for pass := 1; pass <= 2; pass++ {
		stored, _, _ := graph.Charter(charter.ID)
		at := stored.NextDue.Add(time.Second)
		reconciler.now = func() time.Time { return at }
		if _, err := reconciler.WatchOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(prompts) != 2 {
		t.Fatalf("the sentinel ran %d times, want two wakes", len(prompts))
	}
	if len(prompts[0].Previous) != 0 {
		t.Fatalf("the first wake was shown a history it does not have: %v", prompts[0].Previous)
	}
	if len(prompts[1].Previous) != 1 {
		t.Fatalf("second wake previous = %v, want the first judgment", prompts[1].Previous)
	}
	if !strings.Contains(prompts[1].Previous[0], "nothing has changed since the last look") {
		t.Fatalf("the sentinel was not shown its own reasoning: %q", prompts[1].Previous[0])
	}
	if !strings.HasPrefix(prompts[1].Previous[0], "no") {
		t.Fatalf("the sentinel was not shown which way it went: %q", prompts[1].Previous[0])
	}
}
