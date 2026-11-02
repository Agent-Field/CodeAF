package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/session"
)

// freshEngineFactoryStore keeps an in-process engine's factory store, opened
// once per process, from outliving the temporary home of the test that opened it.
func freshEngineFactoryStore(t *testing.T) {
	t.Helper()
	reset := func() {
		engineFactoryStore.once = sync.Once{}
		engineFactoryStore.store = nil
	}
	reset()
	t.Cleanup(reset)
}

// Retry and giving back replace execution; only an explicit Stop is a stop.
// The real solo worker runs a failing command and reports into the manager
// conversation read by the job page. No provider or forge is contacted.
func TestFactoryRetryActionsDoNotReportStopped(t *testing.T) {
	for _, action := range []string{"try again", "send back to Write", "give back", "stop job", "stop step"} {
		t.Run(action, func(t *testing.T) {
			s := newLocalSetup(t)
			s.backend.emit = s.server
			t.Setenv(factoryBenchesEnv, "1")
			model := newStageModel(t)
			t.Setenv("OPENROUTER_API_KEY", "test-key")
			t.Setenv("CODEAF_BASE_URL", model.server.URL)
			g := newRunRig(t)
			freshEngineProcess(t)
			freshEngineFactoryStore(t)
			s.backend.open = appOpenInProcess
			s.backend.local.parent = func() session.Config {
				return session.Config{Workspace: g.workspace, Model: "test/model", OneModel: true, APIKey: "test-key", BaseURL: model.server.URL}
			}
			st := s.start()
			floor := s.backend.local.host.floor
			if err := floor.SetCheckout("acme/api", g.workspace); err != nil {
				t.Fatal(err)
			}
			id, err := floor.Add(context.Background(), factory.Item{
				Repo: "acme/api", Num: 7, Kind: factory.KindIssue, Tier: factory.TierOwner, Cap: 10, Title: "Retry the failing check",
				Stages: []factory.Stage{
					{Name: "write", Kind: factory.StageCheck, On: true, Ask: "true", Until: factory.UntilGreen, Max: 1},
					{Name: "test", Kind: factory.StageCheck, On: true, Ask: "false", Until: factory.UntilGreen, Max: 1},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			read := func() api.Job {
				var job api.Job
				s.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &job)
				return job
			}
			act := func(method string, extra map[string]any) {
				job := read()
				p := map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": job.Version}}
				for k, v := range extra {
					p[k] = v
				}
				s.must(method, p, &appwire.FactoryJobActed{})
			}
			wait := func(what string, ok func(api.Job) bool) {
				t.Helper()
				deadline := time.Now().Add(25 * time.Second)
				for {
					job := read()
					if ok(job) {
						return
					}
					if time.Now().After(deadline) {
						t.Fatalf("%s: state=%s", what, job.State)
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			var chats appwire.FactoryJobChats
			entries := func() []appwire.Entry {
				var page appwire.EntriesResult
				s.must("agents.entries", map[string]any{"id": chats.Agent, "limit": 200}, &page)
				return page.Entries
			}
			act("factory.job_start", nil)
			wait("failed command waits for Try again", func(j api.Job) bool { return j.State == api.StateNeedsYou })
			s.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": id}, &chats)
			before, err := floor.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if before.Stream == nil || before.Stream.Cur != 1 || before.Stream.Phases[1].Result == nil || before.Stream.Phases[1].Result.Exit == 0 {
				t.Fatalf("did not reproduce a failed command: question=%q stream=%+v", before.Question, before.Stream)
			}
			initialWaiting := false
			for _, e := range entries() {
				t.Logf("initial manager entry: kind=%s text=%q progress=%+v", e.Kind, e.Text, e.Progress)
				if e.Progress != nil && e.Progress.Event == "waiting" {
					initialWaiting = true
				}
			}
			if !initialWaiting {
				t.Fatal("job page manager does not show the failed step waiting")
			}
			var turn appwire.TurnResult
			s.must("agents.send", map[string]any{"id": chats.Agent, "text": "Say ready in one word."}, &turn)
			wait("real manager turn finishes", func(api.Job) bool {
				s.mu.Lock()
				defer s.mu.Unlock()
				for _, line := range s.lines {
					var ev struct {
						Event string           `json:"event"`
						Data  appwire.TurnDone `json:"data"`
					}
					if json.Unmarshal([]byte(line), &ev) == nil && ev.Event == appwire.EventTurnDone && ev.Data.Agent == chats.Agent && ev.Data.Turn == turn.Turn {
						if !ev.Data.OK {
							t.Fatalf("manager turn failed: %+v", ev.Data)
						}
						return true
					}
				}
				return false
			})
			if action == "give back" {
				act("factory.job_control", map[string]any{"mode": "take_over"})
				// Takeover is a separate action; wait for the withdrawn worker before
				// measuring what giving the job back writes.
				wait("takeover is manual", func(j api.Job) bool { return j.Manual })
				time.Sleep(time.Second)
			}
			baseline := map[string]bool{}
			for _, e := range entries() {
				baseline[e.ID] = true
			}
			s.mu.Lock()
			eventStart := len(s.lines)
			s.mu.Unlock()
			switch action {
			case "try again":
				act("factory.job_step_rerun", map[string]any{"step": "test"})
			case "send back to Write":
				act("factory.job_step_rerun", map[string]any{"step": "write"})
			case "give back":
				act("factory.job_control", map[string]any{"mode": "resume_pipeline"})
			case "stop job":
				act("factory.job_stop", nil)
			case "stop step":
				act("factory.job_step_stop", map[string]any{"step": "test"})
			}
			stopping := strings.HasPrefix(action, "stop ")
			if stopping {
				wait("Stop records the person’s stop", func(j api.Job) bool {
					if j.Failure == nil || j.Failure.Code != factory.CodeStopped {
						return false
					}
					for _, e := range entries() {
						if !baseline[e.ID] && e.Text == "stopped" {
							return true
						}
					}
					return false
				})
			} else {
				wait("replacement reports into the manager conversation", func(api.Job) bool {
					for _, e := range entries() {
						if !baseline[e.ID] && (e.Text == "resumed" || strings.HasPrefix(e.Text, "trying ")) {
							return true
						}
					}
					return false
				})
			}
			// Reporting notices are asynchronous; the wire read below also drains
			// all events already sent before its reply.
			time.Sleep(600 * time.Millisecond)
			stopped, trying := false, false
			for _, e := range entries() {
				if baseline[e.ID] {
					continue
				}
				t.Logf("%s: manager entry kind=%s error_kind=%s text=%q progress=%+v", action, e.Kind, e.ErrorKind, e.Text, e.Progress)
				if e.ErrorKind == appwire.ErrorStopped || e.Progress != nil && e.Progress.Event == string(factory.ProgressStopped) || e.Text == "stopped" {
					stopped = true
				}
				if e.Text == "trying test again" && action == "try again" || e.Text == "trying write again" && action == "send back to Write" {
					trying = true
				}
			}
			s.mu.Lock()
			lines := append([]string(nil), s.lines[eventStart:]...)
			s.mu.Unlock()
			for _, line := range lines {
				var ev struct {
					Event string `json:"event"`
					Data  struct {
						Agent     string        `json:"agent"`
						ErrorKind string        `json:"error_kind"`
						Entry     appwire.Entry `json:"entry"`
					} `json:"data"`
				}
				if json.Unmarshal([]byte(line), &ev) != nil {
					t.Fatal("invalid wire event")
				}
				if ev.Event == appwire.EventTurnDone && ev.Data.Agent == chats.Agent && ev.Data.ErrorKind == appwire.ErrorStopped {
					stopped = true
					t.Logf("stopped manager turn: %s", line)
				}
				if ev.Event == appwire.EventEntryAdded && ev.Data.Entry.Agent == chats.Agent && (ev.Data.Entry.ErrorKind == appwire.ErrorStopped || ev.Data.Entry.Progress != nil && ev.Data.Entry.Progress.Event == string(factory.ProgressStopped)) {
					stopped = true
				}
			}
			if stopped != stopping {
				t.Errorf("%s: stopped=%v, want %v", action, stopped, stopping)
			}
			if !stopping && action != "give back" && !trying {
				t.Errorf("%s: missing manager action line trying <step> again", action)
			}
		})
	}
}
