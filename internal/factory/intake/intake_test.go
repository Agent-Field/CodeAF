package intake_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/triage"
)

type source struct{ items []factory.Item }

func (s source) Name() string { return "fake" }
func (s source) Read(context.Context, string) ([]factory.Item, string, error) {
	return s.items, "next", nil
}
func (s source) Write(context.Context, factory.Action) (factory.Receipt, error) {
	panic("intake must not post")
}

func TestPollThenTriageStartsOrAsksFirst(t *testing.T) {
	cases := []struct {
		name, policy, body, reply, reason string
		start                             bool
	}{
		{"small labeled issue", "start:\n  labels: [agent-ready]\n", "fix docs/readme.md", `{"read":"Fix docs", "size":"S", "est_usd":1}`, "", true},
		{"auth path", "start:\n  labels: [agent-ready]\n  never: [auth/**]\n", "fix auth/keys.go", `{"read":"Fix auth", "size":"S", "est_usd":1}`, "auth/**", false},
		{"auth directory", "start:\n  labels: [agent-ready]\n  never: [auth/**]\n", "update auth/", `{"read":"Fix auth", "size":"S"}`, "auth/**", false},
		{"cost", "start:\n  labels: [agent-ready]\n  max_cost: 2\n", "fix docs", `{"read":"Fix docs", "size":"S", "est_usd":3}`, "over $2", false},
		{"large", "start:\n  labels: [agent-ready]\n  max_size: M\n", "fix docs", `{"read":"Fix docs", "size":"L"}`, "this one is L", false},
		{"matched sentence", "start:\n  labels: [agent-ready]\n  ask_first: [changes secrets]\n", "change stored secrets", `{"read":"Fix secrets", "size":"S", "ask_first_matched":["changes secrets"]}`, "changes secrets", false},
		{"omitted judgment", "start:\n  labels: [agent-ready]\n  ask_first: [changes secrets]\n", "fix docs", `{"read":"Fix docs", "size":"S"}`, "not judged yet", false},
		{"cleared sentences", "start:\n  labels: [agent-ready]\n  ask_first: [changes secrets]\n", "fix docs", `{"read":"Fix docs", "size":"S", "ask_first_matched":[]}`, "", true},
		{"no start block", "rules: [keep changes small]\n", "fix docs", `{"read":"Fix docs", "size":"S"}`, "", false},
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
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, ".codeaf"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, pipeline.RepoFile), []byte(tc.policy), 0600); err != nil {
				t.Fatal(err)
			}
			src := source{[]factory.Item{{Repo: "owner/repo", Num: 1, Kind: factory.KindIssue, Tier: factory.TierOwner, Title: "Incoming job", Body: tc.body, Labels: []string{"agent-ready"}}}}
			if _, err := github.PollOnce(ctx, src, st, "", func() time.Time { return time.Unix(100, 0) }); err != nil {
				t.Fatal(err)
			}
			history, err := record.Open(st, record.Options{})
			if err != nil {
				t.Fatal(err)
			}
			started := 0
			in := intake.New(st, intake.Options{Record: history, Load: func(string) (pipeline.File, error) { return pipeline.Load(dir) }, Launch: func(id int) error {
				started++
				return st.Update(id, func(it *factory.Item) error { it.State = factory.StateQueued; return nil })
			}})
			if err := in.Once(ctx); err != nil {
				t.Fatal(err)
			}
			if started != 0 {
				t.Fatal("started before triage")
			}
			triage.ReadOne(ctx, st, func(_ context.Context, prompt string) (string, error) {
				if strings.Contains(tc.policy, "ask_first:") && !strings.Contains(prompt, "changes secrets") {
					t.Fatal("triage did not receive the rule")
				}
				return tc.reply, nil
			}, triage.Options{AskFirst: in.AskFirst})
			for range 2 {
				if err := in.Once(ctx); err != nil {
					t.Fatal(err)
				}
			}
			items, err := st.List()
			if err != nil {
				t.Fatal(err)
			}
			it := items[0]
			events, err := history.Since(ctx, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.start:
				if started != 1 || it.State != factory.StateQueued || len(events) != 1 || events[0].Kind != record.AutoStarted {
					t.Fatalf("started %d, item %+v, events %+v", started, it, events)
				}
			case tc.reason != "":
				if started != 0 || it.State != factory.StateNeedsYou || it.QKind != factory.QKindAskFirst || !strings.Contains(it.Question, "asked first: ") || !strings.Contains(it.Question, tc.reason) || len(events) != 1 || events[0].Kind != record.AskedFirst {
					t.Fatalf("item %+v, events %+v", it, events)
				}
				if api.FromItem(it).AskedFirstReason != it.AskedFirstReason {
					t.Fatal("browser loses the matched rule")
				}
			default:
				if started != 0 || it.State != factory.StateNew || len(events) != 0 {
					t.Fatalf("no start block: %+v, %+v", it, events)
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
