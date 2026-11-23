//go:build e2e

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestLiveCrewProposalStreamsBesideARealConversation(t *testing.T) {
	for _, scenario := range []string{"short", "long"} {
		t.Run(scenario, func(t *testing.T) { testLiveCrewProposal(t, scenario) })
	}
}

func testLiveCrewProposal(t *testing.T, scenario string) {
	if os.Getenv("CODEAF_LIVE_CREW_PROPOSAL") != "1" {
		t.Skip("set CODEAF_LIVE_CREW_PROPOSAL=1 to use OpenRouter")
	}
	if liveFactoryKey == "" {
		t.Fatal("no OpenRouter credential configured")
	}
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	profile := v3Profile(t, map[string]any{
		config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: liveFactoryModel,
		"models.roles": "", "models.tiers.low": liveFactoryModel, "models.tiers.high": liveFactoryModel,
		"models.tiers.reflex": liveFactoryModel, "models.tiers.mastermind": liveFactoryModel,
		"models.tiers.worker": liveFactoryModel, "task.model": liveFactoryModel, "models.fallbacks": "",
	})
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("OPENROUTER_API_KEY", liveFactoryKey)
	recorder := newLiveRecorder(t, config.DefaultBaseURL)
	t.Setenv("CODEAF_BASE_URL", recorder.server.URL)
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	freshEngineProcess(t)
	rec := newAppRecorder()
	var mu sync.Mutex
	var active *proposalTrace
	current := func() *proposalTrace { mu.Lock(); defer mu.Unlock(); return active }
	recorder.client = &http.Client{Transport: roundTripper(func(req *http.Request) (*http.Response, error) {
		if trace := current(); trace != nil {
			return trace.transport(http.DefaultTransport).RoundTrip(req)
		}
		return http.DefaultTransport.RoundTrip(req)
	})}
	emit := appProposalLiveEmitter{rec: rec, observe: func(event string, data any) {
		if trace := current(); trace != nil {
			trace.observe(event, data)
		}
	}}
	backend := newAppBackend(emit, appOpenInProcess)
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace, Model: liveFactoryModel})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Send(agent.ID, "We are preparing a community workshop. We need an attendee plan, an accessible venue checklist, and a clear invitation. Give me a short first plan in prose. Do not use tools or start tasks.", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 2*time.Minute, appTurnDone)
	if scenario == "long" {
		for turn := 0; turn < 4; turn++ {
			var message strings.Builder
			fmt.Fprintf(&message, "Workshop planning notes, round %d. Keep these requirements in our conversation. Reply with only a two-sentence acknowledgement. Do not use tools or start tasks.\n", turn+1)
			for row := 0; row < 40; row++ {
				fmt.Fprintf(&message, "Activity %d: reserve an accessible room for %d people, check step-free travel, dietary needs and quiet space, confirm a local host, schedule two practice sessions, gather consent for photographs, and send a plain-language reminder with the venue contact and a weather alternative.\n", turn*40+row+1, 12+row)
			}
			if _, err := backend.Send(agent.ID, message.String(), ""); err != nil {
				t.Fatal(err)
			}
			endings := 0
			rec.waitFor(t, 2*time.Minute, func(event string, data any) bool {
				if event == appwire.EventTurnDone {
					endings++
				}
				return endings == turn+2
			})
		}
	}
	runs := 3
	if value := os.Getenv("CODEAF_PROPOSAL_RUNS"); value != "" {
		var err error
		runs, err = strconv.Atoi(value)
		if err != nil || runs < 1 {
			t.Fatal("invalid CODEAF_PROPOSAL_RUNS")
		}
	}
	for run := 1; run <= runs; run++ {
		trace := &proposalTrace{timing: proposalTiming{Scenario: scenario, Run: run}}
		mu.Lock()
		active = trace
		mu.Unlock()
		rec.mu.Lock()
		mark := len(rec.events)
		rec.mu.Unlock()
		if err := backend.CrewPropose(agent.ID); err != nil {
			t.Fatal(err)
		}
		seen := 0
		i := rec.waitFor(t, 3*time.Minute, func(event string, data any) bool { seen++; return seen > mark && event == appwire.EventAskOpened })
		rec.mu.Lock()
		ask := rec.data[i].(appwire.AskOpened).Ask
		rec.mu.Unlock()
		timing := trace.snapshot()
		raw, _ := json.Marshal(timing)
		t.Logf("PROPOSAL_TRACE %s", raw)
		if err := writeProposalTiming(os.Getenv("CODEAF_PROPOSAL_TRACE_OUT"), timing); err != nil {
			t.Fatal(err)
		}
		if timing.Attempts != 1 {
			t.Fatalf("trace requires one upstream attempt, got %d", timing.Attempts)
		}
		if len(timing.Lines) == 0 || ask.Proposal == nil || len(ask.Proposal.Helpers) == 0 {
			t.Fatalf("no streamed proposal; ask subkind=%s", ask.Subkind)
		}
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	for model := range recorder.requests {
		if model != liveFactoryModel {
			t.Fatalf("unexpected model %q", model)
		}
	}
	for status, count := range recorder.statuses {
		if status != 200 && count > 0 {
			t.Fatalf("provider returned status %d for %d calls", status, count)
		}
	}
	if len(recorder.problems) > 0 {
		t.Fatalf("provider validation = %v", recorder.problems)
	}
}

type appProposalLiveEmitter struct {
	rec     *appRecorder
	observe func(string, any)
}

func (e appProposalLiveEmitter) Emit(event string, data any) {
	e.observe(event, data)
	e.rec.Emit(event, data)
}
