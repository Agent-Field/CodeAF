package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
)

// R1: a display snapshot must not let its caller rewrite the live journal's
// presentation, for jobs and services alike.
func TestToolProcessDisplaySnapshotsDoNotAliasPresentation(t *testing.T) {
	for name, want := range map[string]ToolProcess{
		"job":     {Job: 3, Started: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)},
		"service": {Service: "kept-service"},
	} {
		t.Run(name, func(t *testing.T) {
			process := want
			result := ai.Message{Role: "tool", ToolCallID: "snapshot", Content: []ai.ContentPart{{Type: "text", Text: "running"}}}
			presentation := &presentationIndex{}
			presentation.remember(result, &messagePresentation{Audience: "tool", ToolInvoked: true, ToolProcess: &process})
			messages := []ai.Message{
				{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "snapshot", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"worker"}`}}}},
				result,
			}
			journal := &sessionFile{presentation: presentation}
			for range 2 {
				got := toolEntryFor(t, shapeEntries(messages, journal), "snapshot").ToolProcess
				if got == nil || got.Job != want.Job || !got.Started.Equal(want.Started) || got.Service != want.Service {
					t.Fatalf("display snapshot changed the presentation's process: %+v, want %+v", got, want)
				}
				got.Job = 99
				got.Started = time.Time{}
				got.Service = "edited-service"
			}
		})
	}
}

// processHeldCommand stays alive until its owner closes the session. The
// one-millisecond yield keeps promotion independent of a guessed process age.
func processHeldCommand(t *testing.T) string {
	t.Helper()
	return "while [ ! -f " + shellQuoted(filepath.Join(t.TempDir(), "release")) + " ]; do sleep 0.001; done"
}

// E2 and E4: every real start/adoption road goes through dispatch, sends the
// registry's identity on the end event, and journals it on the result itself.
// The background clock is fired by hand; a steer advances the existing age
// seam; the timeout is one millisecond. No product-length clock is waited out.
func TestToolProcessE2EveryJobRoadPersistsTheLiveIdentity(t *testing.T) {
	for _, road := range []string{"background", "clock", "timeout", "steer", "declined_keep", "approval_then_clock", "kept_service"} {
		t.Run(road, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			command := processHeldCommand(t)
			arguments := map[string]any{"command": command}
			supervisor, _ := keepWorld(t)
			clock := make(chan func(), 1)
			config := func(c *Config) {
				c.SessionFile = path
				c.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
				if strings.Contains(road, "keep") || strings.Contains(road, "service") {
					c.Services, c.AskConsent = supervisor, true
				}
				if road == "clock" || road == "approval_then_clock" {
					c.BashBackgroundAfterSeconds = 30
					c.bashAfter = func(_ time.Duration, fire func()) *time.Timer {
						clock <- fire
						return time.NewTimer(24 * time.Hour)
					}
				}
				if road == "approval_then_clock" {
					c.AskConsent = true
					c.ApprovalPolicy = &approval.Policy{Default: approval.ActionPrompt}
				}
			}
			switch road {
			case "background":
				arguments["background"] = true
			case "timeout":
				arguments["timeout"] = 0.001
			case "declined_keep", "kept_service":
				arguments["keep_running"] = map[string]any{"health": map[string]any{"cmd": "true"}}
			}
			raw, err := json.Marshal(arguments)
			if err != nil {
				t.Fatal(err)
			}
			provider := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return toolResponse("process-call", "bash", string(raw)), nil
				},
				finalText("done"),
			}}
			answerTheReadingsOffTheQueue(provider)
			agent, _ := newTestAgent(t, provider, config)
			turn := mustSubmit(t, agent, "run the command")
			if road == "steer" {
				waitFor(t, "the command to become steerable", func() bool { return agent.inFlightBash.find("process-call") != nil })
				advanceSteerAge(agent, steerBashAge+steerGraceMargin)
				steered, err := agent.Steer("continue with the answer while the command runs")
				if err != nil {
					t.Fatal(err)
				}
				defer collect(t, steered)
			}
			if road == "clock" {
				select {
				case fire := <-clock:
					fire()
				case <-time.After(10 * time.Second):
					t.Fatal("the session clock was not armed")
				}
			}
			var events []Event
			approved, fired := false, road == "clock"
			deadline := time.After(10 * time.Second)
		reading:
			for {
				select {
				case fire := <-clock:
					if road != "approval_then_clock" || !approved {
						t.Fatal("the command started before its approval")
					}
					fire()
					fired = true
				case event, open := <-turn:
					if !open {
						break reading
					}
					events = append(events, event)
					if event.Kind == EventConsentRequest {
						approved = true
						agent.ResolveConsentRemember(event.ID, true, ConsentOnce)
					}
					if event.Kind == EventKeepRunning && event.Keep != nil && event.Keep.Decided == "" {
						key := KeepAllowKey
						if road == "declined_keep" {
							key = KeepDenyKey
						}
						if err := agent.ResolveQuestion(Answer{Kind: QuestionKeep, ID: event.Keep.ID, Key: key, DecidedBy: DecidedByPerson}); err != nil {
							t.Fatal(err)
						}
					}
				case <-deadline:
					t.Fatal("the tool turn never ended")
				}
			}
			if road == "approval_then_clock" && (!approved || !fired) {
				t.Fatal("the approval and promotion path was not taken")
			}
			var end *ToolProcess
			for _, event := range events {
				if event.CallID != "process-call" {
					continue
				}
				if event.Kind == EventToolEnd {
					end = event.ToolProcess
				} else if event.ToolProcess != nil {
					t.Fatalf("a non-end event acquired a process: %+v", event)
				}
			}
			if end == nil {
				t.Fatal("the successful tool end did not name its process")
			}
			check := func(label string, entries []DisplayEntry) {
				t.Helper()
				if got := toolEntryFor(t, entries, "process-call").ToolProcess; got == nil || got.Job != end.Job || got.Service != end.Service || !got.Started.Equal(end.Started) {
					t.Fatalf("%s identity = %+v, live = %+v", label, got, end)
				}
			}
			check("live transcript", agent.Transcript())
			check("detached read", ReadTranscript(path).Entries)
			if end.Service != "" {
				rows := supervisor.List(agent.keepOwner())
				if len(rows) != 1 || rows[0].ID != end.Service || end.Job != 0 {
					t.Fatalf("service identity = %+v, supervisor = %+v", end, rows)
				}
			} else {
				rows, err := agent.JobNotices()
				if err != nil || len(rows) != 1 || rows[0].ID != end.Job || !rows[0].Started.Equal(end.Started) || rows[0].ToolCallID != "process-call" {
					t.Fatalf("process list = %+v, %v; live identity = %+v", rows, err, end)
				}
			}
			if err := agent.Close(); err != nil {
				t.Fatal(err)
			}
			check("closed journal", ReadTranscript(path).Entries)
			check("reopened transcript", reopen(t, path).Transcript())
		})
	}
}

// E3: literal engine-looking sentences printed by a real foreground process
// are output, including when that process fails, is refused, or is interrupted.
func TestToolProcessE3OrdinaryFailedRefusedRejectedAndInterruptedCallsStayAbsent(t *testing.T) {
	for _, kind := range []string{"foreground", "failed", "refused", "rejected", "interrupted"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			args := `{"command":"printf 'still running as job 3; log at invented\\njob 3 started; log at invented\\n'"}`
			if kind == "failed" {
				args = `{"command":"printf 'job 3 started; log at invented'; exit 3"}`
			}
			if kind == "rejected" {
				args = `{"command":`
			}
			if kind == "interrupted" {
				raw, _ := json.Marshal(map[string]string{"command": processHeldCommand(t)})
				args = string(raw)
			}
			provider := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return toolResponse("no-job", "bash", args), nil
				},
				finalText("done"),
			}}
			agent, _ := newTestAgent(t, provider, func(c *Config) {
				c.SessionFile = path
				if kind == "refused" {
					c.ApprovalPolicy = &approval.Policy{Default: approval.ActionDeny}
				}
			})
			turn := mustSubmit(t, agent, "run it")
			if kind == "interrupted" {
				waitFor(t, "the foreground command to start", func() bool { return agent.inFlightBash.find("no-job") != nil })
				agent.Interrupt()
			}
			ended := false
			for _, event := range collect(t, turn) {
				ended = ended || event.CallID == "no-job" && (event.Kind == EventToolEnd || event.Kind == EventToolFailed)
				if event.ToolProcess != nil {
					t.Fatalf("a call that made no job named one: %+v", event)
				}
			}
			if !ended {
				t.Fatal("the non-job call produced no end event")
			}
			if err := agent.Close(); err != nil {
				t.Fatal(err)
			}
			entries := ReadTranscript(path).Entries
			_ = toolEntryFor(t, entries, "no-job")
			for _, entry := range entries {
				if entry.ToolProcess != nil {
					t.Fatalf("history invented a process: %+v", entry)
				}
			}
		})
	}
}

// E4 and E5: a repeated provider id cannot lend a job to a later foreground
// call, and older result messages with only the engine's sentence stay unknown.
func TestToolProcessE4ReusedIDsAndE5LegacyRecordsKeepTheirOwnFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	raw, _ := json.Marshal(map[string]any{"command": processHeldCommand(t), "background": true})
	provider := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("same", "bash", string(raw)), nil
		},
		finalText("started"),
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("same", "bash", `{"command":"printf 'job 1 started; log at invented'"}`), nil
		},
		finalText("done"),
	}}
	answerTheReadingsOffTheQueue(provider)
	agent, _ := newTestAgent(t, provider, func(c *Config) { c.SessionFile = path })
	collect(t, mustSubmit(t, agent, "first"))
	collect(t, mustSubmit(t, agent, "second"))
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	var marks []*ToolProcess
	for _, entry := range ReadTranscript(path).Entries {
		if entry.Role == "tool" && entry.CallID == "same" {
			marks = append(marks, entry.ToolProcess)
		}
	}
	if len(marks) != 2 || marks[0] == nil || marks[1] != nil {
		t.Fatalf("same-id occurrences = %+v", marks)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var legacy strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var record map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		var mark map[string]json.RawMessage
		if json.Unmarshal(record["presentation"], &mark) == nil {
			delete(mark, "tool_process")
			record["presentation"], _ = json.Marshal(mark)
		}
		encoded, _ := json.Marshal(record)
		legacy.Write(encoded)
		legacy.WriteByte('\n')
	}
	old := filepath.Join(t.TempDir(), "legacy.jsonl")
	if err := os.WriteFile(old, []byte(legacy.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, entry := range ReadTranscript(old).Entries {
		if entry.ToolProcess != nil {
			t.Fatal("an older record's words were parsed into a process")
		}
	}
	for _, value := range []any{Event{Kind: EventToolEnd}, DisplayEntry{Role: "tool"}} {
		raw, err := json.Marshal(value)
		if err != nil || strings.Contains(string(raw), "ToolProcess") {
			t.Fatalf("E5 absent fact changed the wire: %s, %v", raw, err)
		}
	}
}
