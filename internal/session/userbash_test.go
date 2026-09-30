package session

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/approval"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestUserBashPersistsLiteralOutputForTheNextModelTurn(t *testing.T) {
	c := &scriptedCompleter{}
	file := filepath.Join(t.TempDir(), "chat.jsonl")
	a, workspace := newTestAgent(t, c, func(cfg *Config) { cfg.SessionFile = file })
	command := `!printf '%s\n' '/task @literal'; pwd; printf 'stderr-token\n' >&2; exit 7`
	ch, err := a.SubmitBash(t.Context(), command)
	if err != nil {
		t.Fatal(err)
	}
	events := collect(t, ch)
	var result Event
	for _, ev := range events {
		if ev.Kind == EventToolFailed {
			result = ev
		}
	}
	for _, want := range []string{"/task @literal", workspace, "stderr-token", "7"} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("output missing %q: %q", want, result.Output)
		}
	}
	if !IsUserBashCall(result.CallID) || c.requests() != 0 || c.asideRequests() != 0 {
		t.Fatalf("command called a model or lost provenance: %+v; %d calls", result, c.requests())
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	replay, err := replaySessionFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(replay.messages) != 3 || messageContentText(replay.messages[0]) != command {
		t.Fatalf("journal did not retain one user command and call/result pair: %+v", replay.messages)
	}
	c.steps = []step{func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		for _, msg := range messages {
			if msg.Role == "tool" && strings.Contains(messageContentText(msg), "stderr-token") {
				return textResponse("I can read that shell output."), nil
			}
		}
		t.Error("resumed model request lost the shell output")
		return textResponse("missing"), nil
	}}
	resumed, err := newAgent(a.config, c)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	ch, err = resumed.Submit(t.Context(), "What did that command print?")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, ch)
	if c.requests() == 0 {
		t.Fatal("follow-up never reached the model")
	}
}

func TestUserBashHasNoStdinAndDoesNotKeepShellState(t *testing.T) {
	a, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	for _, command := range []string{`!if read line; then echo stdin-open; else echo stdin-eof; fi; cd /`, `!pwd`} {
		ch, err := a.SubmitBash(t.Context(), command)
		if err != nil {
			t.Fatal(err)
		}
		events := collect(t, ch)
		want := "stdin-eof"
		if command == "!pwd" {
			want = workspace
		}
		found := false
		for _, ev := range events {
			if ev.Kind == EventToolEnd && strings.Contains(ev.Output, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("command %q did not print %q: %+v", command, want, events)
		}
	}
}

func TestUserBashRefusesEmptyAndBusyAndHonorsCancellation(t *testing.T) {
	a, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	if _, err := a.SubmitBash(t.Context(), "!  "); err == nil {
		t.Fatal("empty command accepted")
	}
	a.mu.Lock()
	a.running = true
	a.mu.Unlock()
	_, err := a.SubmitBash(t.Context(), "!echo do-not-steer")
	a.mu.Lock()
	a.running = false
	queued := len(a.steering)
	a.mu.Unlock()
	if err == nil || queued != 0 {
		t.Fatal("busy command became steering")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ch, err := a.SubmitBash(ctx, "!touch should-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, ch)
	if _, err := os.Stat(filepath.Join(workspace, "should-not-exist")); !os.IsNotExist(err) {
		t.Fatal("cancelled command ran")
	}
}

func TestUserBashStreamsBeforeCompletionAndWaitsForNextMessage(t *testing.T) {
	c := &scriptedCompleter{}
	a, _ := newTestAgent(t, c, func(cfg *Config) { cfg.ApprovalPolicy = promptAll(); cfg.AskConsent = true })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch, err := a.SubmitBash(ctx, "!printf '  first\\n'; mkfifo wait-for-cancel; cat wait-for-cancel")
	if err != nil {
		t.Fatal(err)
	}
	streamed := false
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				if !streamed || c.requests() != 0 || c.asideRequests() != 0 {
					t.Fatalf("streamed=%v, model=%d", streamed, c.requests())
				}
				return
			}
			if ev.Kind == EventToolOutput {
				if !strings.Contains(ev.Text, "  first\n") {
					t.Fatalf("output changed: %q", ev.Text)
				}
				streamed = true
				cancel()
			}
			if ev.Kind == EventConsentRequest {
				t.Fatal("typed command requested duplicate consent")
			}
		case <-deadline:
			t.Fatal("output did not arrive before completion")
		}
	}
}

func TestUserBashExplicitDenyStopsExecution(t *testing.T) {
	a, workspace := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
		cfg.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow, Tools: map[string]approval.Action{"bash": approval.ActionDeny}}
	})
	ch, err := a.SubmitBash(t.Context(), "!touch denied-file")
	if err != nil {
		t.Fatal(err)
	}
	events := collect(t, ch)
	if _, ok := firstOfKind(events, EventToolFailed); !ok {
		t.Fatal("deny did not report failure")
	}
	if _, err := os.Stat(filepath.Join(workspace, "denied-file")); !os.IsNotExist(err) {
		t.Fatal("denied command ran")
	}
}

// THE FLOOR STILL ASKS for a command the person typed, even when the policy
// otherwise allows bash. A harmless missing program precedes the marker so
// an accidental execution is visible without touching anything valuable.
func TestUserBashCriticalFloorAsksUnderAllow(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{false: "refuse", true: "allow"}[allow], func(t *testing.T) {
			a, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
				cfg.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
				cfg.AskConsent = true
			})
			marker := filepath.Join(t.TempDir(), "ran")
			ch, err := a.SubmitBash(t.Context(), "!mkfs.codeaf-floor-probe; touch "+marker)
			if err != nil {
				t.Fatal(err)
			}
			events := drainAnswering(t, ch, func(request Event) {
				if request.Rule != `critical command "mkfs*"` {
					t.Errorf("question rule = %q", request.Rule)
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Errorf("command ran before answer: %v", err)
				}
				a.ResolveConsent(request.ID, allow)
			})
			if countKind(events, EventConsentRequest) != 1 || events[len(events)-1].Kind != EventTurnDone {
				t.Fatalf("question or turn end missing: %v", kinds(events))
			}
			if allow {
				if _, err := os.Stat(marker); err != nil {
					t.Fatalf("allowed command did not run: %v", err)
				}
			} else {
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatalf("refused command ran: %v", err)
				}
				failed, ok := firstOfKind(events, EventToolFailed)
				if !ok || !strings.Contains(failed.Output, `denied by the person: critical command "mkfs*"`) {
					t.Fatalf("refusal = %+v", failed)
				}
			}
		})
	}
}

func TestUserBashCriticalFloorWithoutResolverRefuses(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
		cfg.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		cfg.AskConsent = false
	})
	marker := filepath.Join(t.TempDir(), "ran")
	ch, err := a.SubmitBash(t.Context(), "!mkfs.codeaf-floor-probe; touch "+marker)
	if err != nil {
		t.Fatal(err)
	}
	events := collect(t, ch)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("unattended command ran: %v", err)
	}
	failed, ok := firstOfKind(events, EventToolFailed)
	if !ok || !strings.Contains(failed.Output, `needs approval but no resolver is attached: critical command "mkfs*"`) {
		t.Fatalf("refusal = %+v", failed)
	}
}

func TestUserBashCriticalFloorInterruptRefuses(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
		cfg.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		cfg.AskConsent = true
	})
	marker := filepath.Join(t.TempDir(), "ran")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch, err := a.SubmitBash(ctx, "!mkfs.codeaf-floor-probe; touch "+marker)
	if err != nil {
		t.Fatal(err)
	}
	events := drainAnswering(t, ch, func(request Event) { cancel() })
	if countKind(events, EventConsentRequest) != 1 {
		t.Fatalf("question missing: %v", kinds(events))
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("interrupted command ran: %v", err)
	}
	failed, ok := firstOfKind(events, EventToolFailed)
	if !ok || !strings.Contains(failed.Output, "not approved: ended before an answer") {
		t.Fatalf("refusal = %+v", failed)
	}
}

func TestUserBashOrdinaryCommandNeverAsks(t *testing.T) {
	for _, policy := range []approval.Action{approval.ActionAllow, approval.ActionPrompt} {
		t.Run(string(policy), func(t *testing.T) {
			a, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
				cfg.ApprovalPolicy = &approval.Policy{Default: policy}
				cfg.AskConsent = true
			})
			ch, err := a.SubmitBash(t.Context(), "!echo ordinary-ok")
			if err != nil {
				t.Fatal(err)
			}
			events := collect(t, ch)
			if countKind(events, EventConsentRequest) != 0 {
				t.Fatalf("ordinary command asked: %v", kinds(events))
			}
			end, ok := firstOfKind(events, EventToolEnd)
			if !ok || !strings.Contains(end.Output, "ordinary-ok") {
				t.Fatalf("ordinary command did not run: %+v", end)
			}
		})
	}
}

func TestUserBashOrdinarySubmitNeverInterpretsBang(t *testing.T) {
	c := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("literal text"), nil }}}
	a, workspace := newTestAgent(t, c, nil)
	ch, err := a.Submit(t.Context(), "!touch automated-file")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, ch)
	if c.requests() == 0 {
		t.Fatal("ordinary text bypassed model")
	}
	if _, err := os.Stat(filepath.Join(workspace, "automated-file")); !os.IsNotExist(err) {
		t.Fatal("ordinary message executed shell")
	}
}

func TestUserBashRetainsOutputBeyondToolPreview(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	ch, err := a.SubmitBash(t.Context(), "!printf '%05000d-END' 1")
	if err != nil {
		t.Fatal(err)
	}
	events := collect(t, ch)
	end, ok := firstOfKind(events, EventToolEnd)
	if !ok || len(end.Output) < 5000 || !strings.HasSuffix(end.Output, "-END") {
		t.Fatalf("output was clipped: %d bytes", len(end.Output))
	}
	for _, e := range a.Transcript() {
		if IsUserBashCall(e.CallID) && e.Output == end.Output {
			return
		}
	}
	t.Fatal("replay clipped shell output")
}
