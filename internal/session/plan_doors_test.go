package session

import (
	"context"
	"net/http"
	"testing"
	"time"

	account "github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/modelsource"
	"github.com/Agent-Field/aforge-v2/internal/modelsource/sourcestub"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

func planDoorSource(plan, metered *sourcestub.Server) modelsource.Source {
	return modelsource.Source{
		ID: "z-ai", Written: "z-ai", Name: "Z.ai", KeyEnv: "ZHIPU_API_KEY",
		Doors: []modelsource.Door{
			{ID: "coding-plan", Name: "coding plan", Address: plan.URL(), Models: []string{"glm-5.3-flash"}},
			{ID: "metered", Name: "pay-as-you-go", Address: metered.URL(), Metered: true},
		},
		Listing: modelsource.ListingModels, ProbeModel: "glm-5.3-flash",
		Probe: modelsource.Probe{Address: "/models", Method: http.MethodGet, Accepts: []int{http.StatusOK}},
	}
}

func connectedPlanDoor(source modelsource.Source, outcome modelsource.Outcome, key string) modelsource.Connected {
	connected := modelsource.Connected{
		Source: source, Key: key, Address: outcome.Door.Address, Door: outcome.Door,
		PlanPaused: account.PlanPausedWait,
	}
	if !outcome.Door.Metered {
		for _, door := range source.Doors {
			if door.Metered {
				copy := door
				connected.Overflow = &copy
				break
			}
		}
	}
	return connected
}

func connectThenRunPlanDoor(t *testing.T, source modelsource.Source) modelsource.Outcome {
	t.Helper()
	outcome, err := account.ConnectService(context.Background(), t.TempDir(), account.PersistedSource{
		ID: source.ID, Written: source.Written, Key: "plan-door-test-key", Order: 1,
	}, source, nil)
	if err != nil || outcome.Kind != modelsource.OutcomeConnected {
		t.Fatalf("connect outcome = %v, error=%v", outcome.Kind, err)
	}
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "z-ai/glm-5.3-flash",
		Sources: modelsource.NewSet(
			modelsource.Connected{Source: modelsource.DefaultSource("http://127.0.0.1:1"), Address: "http://127.0.0.1:1"},
			connectedPlanDoor(source, outcome, "plan-door-test-key"),
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	drainTurn(t, agent, "answer from the bound billing door")
	return outcome
}

func TestAPlanKeyBindsToThePlanDoor(t *testing.T) {
	t.Setenv("ZHIPU_API_KEY", "")
	plan, metered := sourcestub.New(), sourcestub.New()
	defer plan.Close()
	defer metered.Close()
	metered.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1113","message":"empty"}`)
	source := planDoorSource(plan, metered)
	outcome := connectThenRunPlanDoor(t, source)
	if outcome.Door.ID != "coding-plan" || len(plan.Requests()) != 2 || len(metered.Requests()) != 0 {
		t.Fatalf("bound=%s requests: plan=%d metered=%d", outcome.Door.ID, len(plan.Requests()), len(metered.Requests()))
	}
	assertRequestsCarry(t, plan.Requests(), "plan-door-test-key")
}

func TestAKeyWithNoPlanFallsBackToPayAsYouGo(t *testing.T) {
	t.Setenv("ZHIPU_API_KEY", "")
	plan, metered := sourcestub.New(), sourcestub.New("glm-5.3-flash")
	defer plan.Close()
	defer metered.Close()
	plan.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1309","message":"plan expired"}`)
	source := planDoorSource(plan, metered)
	outcome := connectThenRunPlanDoor(t, source)
	if outcome.Door.ID != "metered" || len(plan.Requests()) != 1 || len(metered.Requests()) != 3 {
		t.Fatalf("bound=%s requests: plan=%d metered=%d", outcome.Door.ID, len(plan.Requests()), len(metered.Requests()))
	}
	assertRequestsCarry(t, plan.Requests(), "plan-door-test-key")
	assertRequestsCarry(t, metered.Requests(), "plan-door-test-key")
}

func TestAnExhaustedPlanWaitsAndSpendsNothing(t *testing.T) {
	plan, metered := sourcestub.New(), sourcestub.New()
	defer plan.Close()
	defer metered.Close()
	plan.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1316","message":"plan window exhausted"}`)
	connected := connectedPlanDoor(planDoorSource(plan, metered), modelsource.Outcome{
		Door: planDoorSource(plan, metered).Doors[0],
	}, "wait-mode-test-key")
	agent := planDoorAgent(t, connected)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	events, err := agent.Submit(ctx, "wait on the plan I already paid for")
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	if got := metered.Requests(); len(got) != 0 {
		t.Fatalf("wait mode sent %d requests to the metered host: %+v", len(got), got)
	}
	if got := completionRequestsOf(plan); len(got) == 0 {
		t.Fatal("the real agent never reached the bound plan host")
	} else {
		assertRequestsCarry(t, got, "wait-mode-test-key")
	}
}

func TestOverflowGoesToTheMeteredDoorAndSaysSo(t *testing.T) {
	plan, metered := sourcestub.New(), sourcestub.New()
	defer plan.Close()
	defer metered.Close()
	plan.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1316","message":"plan window exhausted"}`)
	source := planDoorSource(plan, metered)
	connected := connectedPlanDoor(source, modelsource.Outcome{Door: source.Doors[0]}, "overflow-test-key")
	connected.PlanPaused = account.PlanPausedUseMeter
	agent := planDoorAgent(t, connected)
	drainTurn(t, agent, "use the metered door I opted into")
	planCalls, meteredCalls := completionRequestsOf(plan), completionRequestsOf(metered)
	if len(planCalls) != 1 || len(meteredCalls) != 1 {
		t.Fatalf("overflow requests: plan=%d metered=%d", len(planCalls), len(meteredCalls))
	}
	assertRequestsCarry(t, planCalls, "overflow-test-key")
	assertRequestsCarry(t, meteredCalls, "overflow-test-key")
}

func TestOnlyAnExplicitReconnectRebindsTheDoor(t *testing.T) {
	plan, metered := sourcestub.New(), sourcestub.New("glm-5.3-flash")
	defer plan.Close()
	defer metered.Close()
	source := planDoorSource(plan, metered)
	key := "rebind-test-key"
	connected := connectedPlanDoor(source, modelsource.Outcome{Door: source.Doors[0]}, key)
	agent := planDoorAgent(t, connected)
	drainTurn(t, agent, "first turn stays on the bound plan")
	planBeforeReconnect := len(plan.Requests())

	plan.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1309","message":"plan expired"}`)
	outcome, err := account.ConnectService(context.Background(), t.TempDir(), account.PersistedSource{
		ID: source.ID, Written: source.Written, Key: key, Order: 1,
	}, source, nil)
	if err != nil || outcome.Door.ID != "metered" {
		t.Fatalf("explicit reconnect = %+v, %v", outcome, err)
	}
	connected = connectedPlanDoor(source, outcome, key)
	agent.SetSources(modelsource.NewSet(
		modelsource.Connected{Source: modelsource.DefaultSource("http://127.0.0.1:1"), Address: "http://127.0.0.1:1"},
		connected,
	))
	planAfterReconnect, meteredAfterReconnect := len(plan.Requests()), len(metered.Requests())
	drainTurn(t, agent, "second turn uses the explicitly rebound door")
	if len(plan.Requests()) != planAfterReconnect || len(metered.Requests()) != meteredAfterReconnect+1 {
		t.Fatalf("turn after reconnect went to the wrong host: plan %d->%d->%d metered %d->%d",
			planBeforeReconnect, planAfterReconnect, len(plan.Requests()), meteredAfterReconnect, len(metered.Requests()))
	}
	assertRequestsCarry(t, plan.Requests(), key)
	assertRequestsCarry(t, metered.Requests(), key)
}

func TestEveryOneDoorServiceKeepsItsHostAndBearer(t *testing.T) {
	for _, testCase := range []struct {
		id, written, key string
		optional         bool
	}{
		{id: "deepseek", written: "deepseek-direct", key: "deepseek-test-key"},
		{id: "ollama", written: "ollama", optional: true},
		{id: "custom", written: "something-local", key: "custom-test-key"},
	} {
		t.Run(testCase.id, func(t *testing.T) {
			host := sourcestub.New("one-model")
			defer host.Close()
			service := modelsource.Connected{
				Source: modelsource.Source{
					ID: testCase.id, Written: testCase.written, Address: host.URL(),
					KeyOptional: testCase.optional, Listing: modelsource.ListingModels,
				},
				Key: testCase.key, Address: host.URL(),
			}
			agent := planDoorAgent(t, service)
			drainTurn(t, agent, "use this one-door service")
			calls := completionRequestsOf(host)
			if len(calls) != 1 {
				t.Fatalf("one-door turn requests = %+v", calls)
			}
			assertRequestsCarry(t, calls, testCase.key)
		})
	}
}

func TestChangingThePlanPauseSettingReplacesTheLiveClient(t *testing.T) {
	plan, metered := sourcestub.New(), sourcestub.New()
	defer plan.Close()
	defer metered.Close()
	source := planDoorSource(plan, metered)
	connected := connectedPlanDoor(source, modelsource.Outcome{Door: source.Doors[0]}, "plan-door-test-key")
	sources := modelsource.NewSet(
		modelsource.Connected{Source: modelsource.DefaultSource("http://127.0.0.1:1"), Address: "http://127.0.0.1:1"},
		connected,
	)
	agent, err := New(Config{Workspace: t.TempDir(), Model: "z-ai/glm-5.3-flash", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	before, _, _, err := agent.clientPool.clientFor("z-ai/glm-5.3-flash")
	if err != nil {
		t.Fatal(err)
	}
	connected.PlanPaused = account.PlanPausedUseMeter
	agent.SetSources(modelsource.NewSet(sources.Default(), connected))
	after, _, _, err := agent.clientPool.clientFor("z-ai/glm-5.3-flash")
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("the live client retained the old plan-pause spending answer")
	}
}

func planDoorAgent(t *testing.T, service modelsource.Connected) *Agent {
	t.Helper()
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: service.Qualify("glm-5.3-flash"),
		Sources: modelsource.NewSet(
			modelsource.Connected{Source: modelsource.DefaultSource("http://127.0.0.1:1"), Address: "http://127.0.0.1:1"},
			service,
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent
}

func completionRequestsOf(server *sourcestub.Server) []sourcestub.Request {
	var calls []sourcestub.Request
	for _, request := range server.Requests() {
		if request.Method == http.MethodPost {
			calls = append(calls, request)
		}
	}
	return calls
}

func assertRequestsCarry(t *testing.T, requests []sourcestub.Request, key string) {
	t.Helper()
	wantBearer := ""
	if key != "" {
		wantBearer = "Bearer " + key
	}
	for _, request := range requests {
		if request.Bearer != wantBearer || request.Agent != provider.DirectUserAgent {
			t.Fatalf("request reached %s with bearer %q and agent %q; want bearer %q and agent %q",
				request.Path, request.Bearer, request.Agent, wantBearer, provider.DirectUserAgent)
		}
	}
}
