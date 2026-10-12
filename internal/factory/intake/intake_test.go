package intake_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

type source struct{ items []factory.Item }

func (s source) Name() string { return "fake" }
func (s source) Read(context.Context, string) ([]factory.Item, string, error) {
	return s.items, "next", nil
}
func (s source) Write(context.Context, factory.Action) (factory.Receipt, error) {
	panic("intake must not post")
}

func TestPollUsesLabelSizeAndBudgetWithoutAnAIRead(t *testing.T) {
	cases := []struct {
		name, policy, body, size, reason string
		start                            bool
	}{
		{"small labeled issue", "start:\n  labels: [agent-ready]\n", "fix docs/readme.md", "S", "", true},
		{"auth path", "start:\n  labels: [agent-ready]\n  never: [auth/**]\n", "fix auth/keys.go", "S", "auth/**", false},
		{"auth directory", "start:\n  labels: [agent-ready]\n  never: [auth/**]\n", "update auth/", "S", "auth/**", false},
		{"budget", "default_budget: 3\nstart:\n  labels: [agent-ready]\n  max_cost: 2\n", "fix docs", "S", "job budget $3 is over $2", false},
		{"large", "start:\n  labels: [agent-ready]\n  max_size: M\n", "fix docs", "L", "this one is L", false},
		{"path rule", "start:\n  labels: [agent-ready]\n  ask_first: [touches auth/**]\n", "fix auth/keys.go", "S", "touches auth/**", false},
		{"unknown rule asks", "start:\n  labels: [agent-ready]\n  ask_first: [changes secrets]\n", "fix docs", "S", "changes secrets", false},
		{"unmatched path", "start:\n  labels: [agent-ready]\n  ask_first: [touches auth/**]\n", "fix docs/readme.md", "S", "", true},
		{"missing size", "start:\n  labels: [agent-ready]\n", "fix docs", "", "no size label", false},
		{"no start block", "rules: [keep changes small]\n", "fix docs", "S", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := st.SetRepos([]string{"owner/repo"}); err != nil {
				t.Fatal(err)
			}
			f, err := pipeline.Parse(tc.policy)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name != "budget" {
				f.DefaultBudget = 1
			}
			labels := []string{"agent-ready"}
			if tc.size != "" {
				labels = append(labels, "size:"+tc.size)
			}
			src := source{[]factory.Item{{Repo: "owner/repo", Num: 1, Kind: factory.KindIssue, Tier: factory.TierOwner, Title: "Incoming job", Body: tc.body, Labels: labels, Triage: factory.Triage{Size: "XL", Est: 999, Priority: 1, StartJudged: true, StartMatched: []string{}}}}}
			if _, err := github.PollOnce(ctx, src, st, "", func() time.Time { return time.Unix(100, 0) }); err != nil {
				t.Fatal(err)
			}
			history, err := record.Open(st, record.Options{})
			if err != nil {
				t.Fatal(err)
			}
			started := 0
			in := intake.New(st, intake.Options{Record: history, Load: func(string) (pipeline.File, error) { return f, nil }, Launch: func(id int) error {
				started++
				return st.Update(id, func(it *factory.Item) error { it.State = factory.StateQueued; return nil })
			}})
			for range 2 {
				if err := in.Once(ctx); err != nil {
					t.Fatal(err)
				}
			}
			items, err := st.List()
			if err != nil || len(items) != 1 {
				t.Fatalf("items %+v %v", items, err)
			}
			it := items[0]
			events, err := history.Since(ctx, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.start:
				if started != 1 || it.State != factory.StateQueued || len(events) != 1 || events[0].Kind != record.AutoStarted {
					t.Fatalf("started %d item %+v events %+v", started, it, events)
				}
			case tc.reason != "":
				if started != 0 || it.State != factory.StateNeedsYou || it.QKind != factory.QKindAskFirst || !strings.Contains(it.Question, "Approval needed: start") || !strings.Contains(it.Question, tc.reason) || len(events) != 1 || events[0].Kind != record.AskedFirst {
					t.Fatalf("item %+v events %+v", it, events)
				}
				if api.FromItem(it).Approval.Matched != it.AskedFirstReason {
					t.Fatal("matched rule lost")
				}
			default:
				if started != 0 || it.State != factory.StateNew || len(events) != 0 {
					t.Fatalf("no automatic start %+v %+v", it, events)
				}
			}
		})
	}
}

func TestPlanPathsReachTheNeverRule(t *testing.T) {
	f, err := pipeline.Parse("start:\n  labels: [ready]\n  never: [auth/**]\n")
	if err != nil {
		t.Fatal(err)
	}
	it := factory.Item{Kind: factory.KindIssue, Labels: []string{"ready"}, Triage: factory.Triage{Size: "S"}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "plan", Result: &factory.StageResult{Output: "Change internal/auth/keys.go"}}}}}
	if d := f.Decide(intake.Job(it)); !d.Ask || !strings.Contains(strings.Join(d.Reasons, " "), "auth/**") {
		t.Fatalf("decision: %+v", d)
	}
}

func TestStoppedJobStaysStoppedUntilPersonRunsIt(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	it, err := st.Create(factory.Item{Repo: "owner/repo", Num: 1, Kind: factory.KindIssue,
		State: factory.StateNew, Labels: []string{"agent-ready", "size:S"},
		Triage: factory.Triage{TriagedAt: now, Size: "S", Est: 1}})
	if err != nil {
		t.Fatal(err)
	}
	f, err := pipeline.Parse("default_budget: 1\nstart:\n  labels: [agent-ready]\n")
	if err != nil {
		t.Fatal(err)
	}
	var history *record.Record
	var desk *lease.Desk
	var in *intake.Intake
	var handler *serve.Handler
	open := func() {
		t.Helper()
		st, err = store.Open(root)
		if err != nil {
			t.Fatal(err)
		}
		history, err = record.Open(st, record.Options{Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		desk, err = lease.Open(st, lease.Options{Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		doors := lease.NewDoors(desk)
		in = intake.New(st, intake.Options{Record: history, Load: func(string) (pipeline.File, error) { return f, nil }, Launch: doors.Launch})
		seam := factory.LocalSeam(st, now, factory.WithRunner(doors))
		handler = serve.New(seam, serve.Config{Store: st, Record: history, AccessToken: "test-stop-token"})
	}
	tick := func(want factory.State) {
		t.Helper()
		if err := in.Once(ctx); err != nil {
			t.Fatal(err)
		}
		got, err := st.Get(it.ID)
		if err != nil || got.State != want {
			t.Fatalf("job state = %s, want %s; error = %v", got.State, want, err)
		}
	}
	post := func(verb string) {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v1/jobs/%d/%s", it.ID, verb), nil)
		req.Header.Set("Authorization", "Bearer test-stop-token")
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", verb, w.Code, w.Body)
		}
	}
	open()
	tick(factory.StateQueued)
	post("stop")
	for range 5 {
		tick(factory.StateNew)
	}
	open()
	for range 5 {
		tick(factory.StateNew)
	}
	// A changed rule must not turn the person's stop into another question.
	f.Start.MaxCost = 0.5
	tick(factory.StateNew)
	events, err := history.Since(ctx, time.Time{})
	if err != nil || len(events) != 2 || events[0].Kind != record.AutoStarted || events[1].Kind != record.Stopped {
		t.Fatalf("stop history = %+v; error = %v", events, err)
	}
	post("run")
	tick(factory.StateQueued)
	if _, err := desk.Pull(lease.Worker{ID: "w1", Handle: "sam", TakesUnassigned: map[string]bool{lease.EveryRepo: true}}); err != nil {
		t.Fatal(err)
	}
	tick(factory.StateRunning)
}
