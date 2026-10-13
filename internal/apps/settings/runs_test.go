package settings

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/realtime"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type snapshotStore struct{ *testStore }

func (s snapshotStore) Definition(ctx context.Context, app string) (*spec.Bundle, error) {
	def, err := s.testStore.Definition(ctx, app)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(def)
	if err != nil {
		return nil, err
	}
	var copy spec.Bundle
	err = json.Unmarshal(raw, &copy)
	return &copy, err
}

type observedGate struct {
	svc.TriggerGate
	fires chan bool
}

func (g observedGate) Allow(app, trigger string, at time.Time) bool {
	allowed := g.TriggerGate.Allow(app, trigger, at)
	g.fires <- allowed
	return allowed
}

type pricedModel struct{}

func (pricedModel) Complete(context.Context, svc.ModelCall) (svc.ModelReply, error) {
	return svc.ModelReply{Text: `{}`, Cost: .2}, nil
}

func realRuns(t *testing.T, s *Service, store *testStore) (context.Context, svc.Hub) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub := realtime.New(svc.Config{}, nil)
	s.all.Store = snapshotStore{store}
	s.all.Hub = hub
	s.all.Model = pricedModel{}
	if s.all.Crews.TriggerGate == nil {
		s.all.Crews.TriggerGate = s
	}
	s.all.Crews.BudgetCap = s
	s.all.Runs = runs.New(svc.Config{Root: s.root, Ctx: ctx}, s.all)
	return ctx, hub
}

func TestRunsPausedTriggerSkipsWithoutReplay(t *testing.T) {
	for _, paused := range []bool{false, true} {
		name := "active"
		if paused {
			name = "paused"
		}
		t.Run(name, func(t *testing.T) {
			s, store, scope, _ := fixture(t)
			store.bundle.App.Triggers = store.bundle.App.Triggers[:1]
			fires := make(chan bool, 2)
			s.all.Crews.TriggerGate = observedGate{TriggerGate: s, fires: fires}
			ctx, hub := realRuns(t, s, store)
			events, cancel := hub.Subscribe(bridge.TopicRuns("crm"))
			defer cancel()
			_, err := s.PauseTrigger(ctx, scope, bridge.PauseTriggerParams{ID: "new-deal", Paused: paused})
			requireCode(t, err, "")
			publish := func(id string) {
				hub.Publish(bridge.TopicRecords("crm", "deal"), bridge.RecordDiff{
					Op: bridge.DiffUpsert, Type: "deal", ID: id,
					Record: &bridge.Record{ID: id, Type: "deal", Version: 1},
				})
			}
			publish("first")
			select {
			case allowed := <-fires:
				if allowed == paused {
					t.Fatalf("gate allowed = %v, paused = %v", allowed, paused)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("record event never reached the settings gate")
			}
			if !paused {
				waitRunDone(t, events)
			}
			_, err = s.PauseTrigger(ctx, scope, bridge.PauseTriggerParams{ID: "new-deal", Paused: false})
			requireCode(t, err, "")
			page, err := s.all.Runs.List(ctx, scope, bridge.ListRunsParams{})
			requireCode(t, err, "")
			want := 1
			if paused {
				want = 0
			}
			if len(page.Runs) != want {
				t.Fatalf("after resume: runs = %d, want %d", len(page.Runs), want)
			}
			publish("second")
			waitRunDone(t, events)
			page, err = s.all.Runs.List(ctx, scope, bridge.ListRunsParams{})
			requireCode(t, err, "")
			if len(page.Runs) != want+1 || page.Runs[0].Trigger != "new-deal" {
				t.Fatalf("resumed trigger = %+v", page)
			}
			out, err := s.ListTriggers(ctx, scope)
			requireCode(t, err, "")
			if out.Triggers[0].LastFired == "" {
				t.Fatal("runs hook did not record last_fired")
			}
		})
	}
}

func waitRunDone(t *testing.T, events <-chan svc.Event) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-events:
			if state, ok := event.Data.(bridge.RunState); ok && state.Status == bridge.RunDone {
				return
			}
		case <-timer.C:
			t.Fatal("trigger did not finish its run")
		}
	}
}

func TestRunsBudgetOverrideChangesCap(t *testing.T) {
	for _, tt := range []struct {
		name  string
		month float64
		code  bridge.ErrorCode
	}{
		{"lower cap refuses new run", .1, bridge.CodeBudget},
		{"raised cap admits new run", 50, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, store, scope, _ := fixture(t)
			store.bundle.App.Triggers = nil
			store.bundle.App.Budget.PerRun = .5
			store.bundle.Runs["costed"] = &spec.Run{Run: "costed", Steps: []*spec.Step{{ID: "draft", Agent: "CRM agent", Prompt: "draft"}}}
			ctx, _ := realRuns(t, s, store)
			before, err := s.all.Runs.Start(ctx, scope, bridge.StartRunParams{Run: "costed"})
			requireCode(t, err, "")
			if before.Status != bridge.RunDone || before.Cost.Spent != .2 {
				t.Fatalf("first run = %+v", before)
			}
			started, err := time.Parse(time.RFC3339, before.StartedAt)
			if err != nil {
				t.Fatal(err)
			}
			s.SetClock(func() time.Time { return started })
			info, err := s.SetBudget(ctx, scope, bridge.SetBudgetParams{Month: tt.month})
			requireCode(t, err, "")
			if info.Spent != .2 || info.Month != tt.month {
				t.Fatalf("settings did not read engine spending: %+v", info)
			}
			_, err = s.all.Runs.Start(ctx, scope, bridge.StartRunParams{Run: "costed"})
			requireCode(t, err, tt.code)
			if store.bundle.App.Budget.Month != 20 {
				t.Fatal("override edited the app definition")
			}
		})
	}
}
