package codeaf

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/project"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/observer"
)

func TestObserverRunSiteHonorsFlagAndTracksEntryPayload(t *testing.T) {
	observer.ClearCurrent()
	t.Cleanup(observer.ClearCurrent)

	for _, value := range []string{"", "0"} {
		t.Run("disabled-"+value, func(t *testing.T) {
			t.Setenv("CODEAF_OBSERVER", value)
			workspace := validityTestRepo(t)
			runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
				Events: newEventWriter(io.Discard), Notes: io.Discard,
			})
			defer runner.runtime.Close()
			runner.entryAgent = "coder"
			runner.startObserver(context.Background(), "Fix it")
			if runner.observer != nil || observer.Current() != nil || runner.runtime.reminders != nil {
				t.Fatalf("disabled observer constructed: runner=%p current=%p reminders=%p",
					runner.observer, observer.Current(), runner.runtime.reminders)
			}
		})
	}

	t.Setenv("CODEAF_OBSERVER", "1")
	workspace := validityTestRepo(t)
	now := time.UnixMilli(1700000000000)
	var notes bytes.Buffer
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: &notes, Now: func() time.Time { return now },
		ObserverDispatcher: observer.DispatcherFunc(func(
			context.Context, observer.ObservedSession, string, string,
		) (observer.Verdict, bool, error) {
			return observer.Verdict{Reasoning: "progress"}, false, nil
		}),
	})
	defer runner.runtime.Close()
	runner.entryAgent = "root-orchestrator"
	if err := runner.runtime.ensureRootSession(
		context.Background(), runner.sessionID, "Fix it", runner.entryAgent,
	); err != nil {
		t.Fatal(err)
	}
	runner.startObserver(context.Background(), "Fix it")
	if runner.observer == nil || observer.Current() != runner.observer {
		t.Fatalf("enabled registry runner=%p current=%p", runner.observer, observer.Current())
	}
	list := runner.observer.List()
	if len(list) != 1 {
		t.Fatalf("tracked sessions=%d", len(list))
	}
	observed := list[0].Observed
	if observed.SessionID != runner.sessionID || observed.AgentRole != "root-orchestrator" ||
		observed.TaskSummary != "Fix it" || observed.StartedAt != 1700000000000 ||
		observed.Workspace != workspace || observed.ParentSessionID != runner.sessionID {
		t.Fatalf("root observer payload=%#v", observed)
	}
	if !strings.Contains(notes.String(), "observer sidecar enabled (CODEAF_OBSERVER=1)") {
		t.Fatalf("notes=%q", notes.String())
	}
	runner.stopObserver()
	if observer.Current() != nil || runner.runtime.reminders != nil {
		t.Fatal("observer teardown left live process state")
	}
}

func TestObserverSessionQueueReceivesIntervention(t *testing.T) {
	workspace := validityTestRepo(t)
	runtime := newRuntime(workspace, nil)
	defer runtime.Close()
	confidence := 0.9
	reminder := "  run the focused test now  "
	service := &observer.Service{
		Reminders: runtime.durable.sessions,
		Now:       func() time.Time { return time.UnixMilli(900000) },
	}
	result, err := service.Fire(context.Background(), observer.FireInput{
		Observed: observer.ObservedSession{SessionID: "ses-observed", AgentRole: "coder"},
		Trigger:  observer.TriggerKind{Kind: "time", ElapsedMS: 900000}, StartedAt: 0,
		Override: func(observer.ObserverContext) (observer.Verdict, error) {
			return observer.Verdict{
				Intervene: true, Reasoning: "stuck", Reminder: &reminder, Confidence: &confidence,
			}, nil
		},
	})
	if err != nil || !result.Intervened {
		t.Fatalf("fire=%#v err=%v", result, err)
	}
	if got := runtime.durable.sessions.DrainReminders("ses-observed"); len(got) != 1 || got[0] != "run the focused test now" {
		t.Fatalf("queued reminders=%v", got)
	}
}

func TestObserverPromptDrainPersistsSyntheticPartAfterPlanDB(t *testing.T) {
	workspace := validityTestRepo(t)
	runtime := newRuntime(workspace, nil)
	defer runtime.Close()
	const sessionID = "ses-prompt"
	if err := runtime.ensureRootSession(context.Background(), sessionID, "goal", "coder"); err != nil {
		t.Fatal(err)
	}
	runtime.durable.sessions.QueueReminder(sessionID, "observer correction")
	_, err := persistTurnPromptWithReminders(
		context.Background(), runtime.durable, sessionID, "msg-prompt",
		turn{Prompt: "goal", Agent: "coder", PlanDB: &project.PlanDB{
			ProjectID: "project", RootTaskID: "root", DBPath: "/repo/.plandb.db",
		}}, runtime.durable.sessions,
	)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := runtime.durable.Messages(context.Background(), sessionID)
	if err != nil || len(messages) != 1 || len(messages[0].Parts) != 3 {
		t.Fatalf("messages=%#v err=%v", messages, err)
	}
	planPart := messages[0].Parts[1].(msgmodel.TextPart)
	observerPart := messages[0].Parts[2].(msgmodel.TextPart)
	if !strings.Contains(planPart.Text, "PlanDB has already been initialized") ||
		observerPart.Text != "observer correction" ||
		observerPart.Synthetic == nil || !*observerPart.Synthetic {
		t.Fatalf("parts=%#v", messages[0].Parts)
	}
	if pending := runtime.durable.sessions.DrainReminders(sessionID); len(pending) != 0 {
		t.Fatalf("prompt did not drain queue: %v", pending)
	}
}

func TestObserverDispatcherUsesLowTierTransientArtifactSink(t *testing.T) {
	workspace := validityTestRepo(t)
	configuredPath := filepath.Join(workspace, ".codeaf", "observer", "ses-observed-900000.json")
	var seen turn
	backend := backendFunc(func(_ context.Context, request turn) (turnResult, error) {
		seen = request
		if err := os.WriteFile(configuredPath, []byte(
			`{"intervene":false,"reasoning":"making progress","reminder":null,"confidence":0.8}`,
		), 0o600); err != nil {
			return turnResult{}, err
		}
		return turnResult{SessionID: request.SessionID, Text: "observed"}, nil
	})
	runtime := newRuntime(workspace, backend)
	defer runtime.Close()
	runtime.pool = poolResolver{low: []string{"provider/low-model"}}
	service := &observer.Service{
		History: runtime.durable, Reminders: runtime.durable.sessions,
		Dispatcher: runtimeObserverDispatcher{runtime: runtime},
		Now:        func() time.Time { return time.UnixMilli(900000) },
	}
	result, err := service.Fire(context.Background(), observer.FireInput{
		Observed: observer.ObservedSession{
			SessionID: "ses-observed", AgentRole: "coder", TaskSummary: "fix",
			Workspace: workspace, ParentSessionID: "ses-parent",
		},
		Trigger: observer.TriggerKind{Kind: "time", ElapsedMS: 900000}, StartedAt: 0,
	})
	if err != nil || result.Verdict == nil || result.Verdict.Intervene || result.Intervened {
		t.Fatalf("fire=%#v err=%v", result, err)
	}
	if seen.Agent != "observer" || seen.ParentSessionID != "ses-parent" ||
		seen.ProviderID != "provider" || seen.ModelID != "low-model" ||
		!strings.Contains(seen.Prompt, "Write your verdict as JSON to: "+configuredPath) {
		t.Fatalf("observer dispatch=%#v", seen)
	}
	if _, statErr := os.Stat(configuredPath); !os.IsNotExist(statErr) {
		t.Fatalf("TS-compatible consumed artifact remains at %s: %v", configuredPath, statErr)
	}
}

func TestObserverDoesNotChangeTerminalStatus(t *testing.T) {
	statuses := map[string]string{}
	for _, value := range []string{"0", "1"} {
		t.Run("observer-"+value, func(t *testing.T) {
			t.Setenv("CODEAF_OBSERVER", value)
			t.Setenv("CODEAF_VALIDITY", "0")
			t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")
			t.Setenv("CODEAF_AUDITOR", "0")
			t.Setenv("CODEAF_TAMPER", "0")
			t.Setenv("CODEAF_SPEC_IDS", "0")
			t.Setenv("CODEAF_AUTORESUME", "0")
			workspace := validityTestRepo(t)
			t.Setenv("PLANDB_DB", filepath.Join(workspace, ".plandb.db"))
			plandb.ResetPlanDBForTesting()
			t.Cleanup(plandb.ResetPlanDBForTesting)
			runner := newPipeline(cliArgs{
				High: "provider/high", Low: "provider/low", EntryAgent: "coder",
			}, workspace, pipelineDeps{
				Backend: backendFunc(func(_ context.Context, request turn) (turnResult, error) {
					return turnResult{SessionID: request.SessionID, Text: "done"}, nil
				}),
				Events: newEventWriter(io.Discard), Notes: io.Discard,
			})
			defer runner.runtime.Close()
			result, err := runner.run(context.Background(), "Make the requested change", pipelineOptions{})
			if err != nil {
				t.Fatal(err)
			}
			statuses[value] = result.Status
		})
	}
	if statuses["0"] != statuses["1"] || statuses["0"] != "pass" {
		t.Fatalf("terminal statuses=%v", statuses)
	}
}
