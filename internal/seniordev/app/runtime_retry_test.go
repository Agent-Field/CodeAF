//go:build !windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/tool"
)

func retryTurn() turn {
	return turn{ModelID: "test/model", Prompt: "hello", AgentMarkdown: testAgentPrompt}
}

func TestModelCallNeverRetriesInsideTheEngine(t *testing.T) {
	// A provider response belongs to exactly one HTTP request. Transient recovery
	// happens at soloConverse, where it is globally bounded and session-aware.
	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusRequestTimeout,
		http.StatusConflict,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusServiceUnavailable,
	} {
		t.Run(fmt.Sprintf("status-%d", status), func(t *testing.T) {
			requests := 0
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requests++
				encoded, _ := json.Marshal(map[string]any{
					"error": map[string]any{"message": "provider failure"},
				})
				return recordedResponse(request, status, "application/json", string(encoded)), nil
			})}

			backend := &openRouterBackend{apiKey: "test", client: client}
			_, err := backend.Run(context.Background(), retryTurn())
			if err == nil {
				t.Fatal("provider failure returned nil")
			}
			if requests != 1 {
				t.Fatalf("HTTP requests = %d, want exactly 1", requests)
			}
			var failure *modelTurnError
			if !errors.As(err, &failure) || failure.statusCode == nil ||
				*failure.statusCode != uint64(status) {
				t.Fatalf("turn error = %#v, want structured status %d", err, status)
			}
		})
	}
}

func TestInBandProviderFailureReachesRunClassifierWithStatus(t *testing.T) {
	requests := 0
	body := `data: {"error":{"code":502,"message":"Network connection lost.","metadata":{"error_type":"provider_unavailable"}},"choices":[]}` +
		"\n\ndata: [DONE]\n\n"
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return recordedResponse(request, http.StatusOK, "text/event-stream", body), nil
	})}
	backend := &openRouterBackend{apiKey: "test", client: client}
	_, err := backend.Run(context.Background(), retryTurn())
	if err == nil || requests != 1 {
		t.Fatalf("turn error=%v requests=%d, want one failed request", err, requests)
	}
	info, transient := transientTurnError(err)
	if !transient || info.Class != "provider-5xx" || info.StatusCode == nil ||
		*info.StatusCode != 502 || info.ProviderCode != "provider_unavailable" {
		t.Fatalf("run classification = %#v,%v for %v", info, transient, err)
	}
}

type errorAfterBody struct {
	payload []byte
	offset  int
}

func (body *errorAfterBody) Read(target []byte) (int, error) {
	if body.offset >= len(body.payload) {
		return 0, io.ErrUnexpectedEOF
	}
	n := copy(target, body.payload[body.offset:])
	body.offset += n
	return n, nil
}

func (*errorAfterBody) Close() error { return nil }

type errorAfterFile struct {
	payload []byte
	offset  int
	path    string
}

func (body *errorAfterFile) Read(target []byte) (int, error) {
	if body.offset < len(body.payload) {
		n := copy(target, body.payload[body.offset:])
		body.offset += n
		return n, nil
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(body.path); err == nil || time.Now().After(deadline) {
			return 0, io.ErrUnexpectedEOF
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (*errorAfterFile) Close() error { return nil }

func TestFailureAfterToolCallDoesNotReplayRequestOrTool(t *testing.T) {
	requests := 0
	payload := strings.TrimSuffix(toolCallReply("bash", `{"command":"true"}`), "data: [DONE]\n\n")
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       &errorAfterBody{payload: []byte(payload)},
			Request:    request,
		}, nil
	})}
	backend := &openRouterBackend{apiKey: "test", client: client, chunkTimeoutMS: -1}
	var executions atomic.Int32
	_, err := backend.Run(context.Background(), turn{
		Agent: "coder", ModelID: "test/model", Prompt: "use the tool", Workspace: t.TempDir(),
		AgentMarkdown: testAgentPrompt,
		Tools: []steploop.ToolDefinition{{Provider: orclient.Tool{
			Type: "function", Name: "bash", Description: "run a command",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}}},
		Execute: func(context.Context, steploop.ToolCall) (steploop.ToolResult, error) {
			executions.Add(1)
			return steploop.ToolResult{Output: "ok"}, nil
		},
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unexpected eof") {
		t.Fatalf("turn error = %v, want the dropped stream", err)
	}
	if requests != 1 || executions.Load() != 1 {
		t.Fatalf("requests=%d tool executions=%d, want 1 and 1", requests, executions.Load())
	}
}

func TestSoloRecoveryCrossesThePersistedEngineBoundaryWithoutReplayingToolEffects(t *testing.T) {
	const effect = "RECOVERY_SIDE_EFFECT_48291"
	workspace, base := guardWorkspace(t)
	if err := writeFile(
		filepath.Join(workspace, ".senior-dev", "checklist.md"),
		"[x] preserve completed tool effects across recovery\n",
	); err != nil {
		t.Fatal(err)
	}

	var requestBodies [][]byte
	var events bytes.Buffer
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		requestBodies = append(requestBodies, raw)

		var reply string
		switch len(requestBodies) {
		case 1:
			reply = toolCallReply("bash", `{"command":"printf '`+effect+`\\n' > recovered.txt"}`)
		case 2:
			reply = strings.Replace(
				toolCallReply("submit", `{"reason":"recovered safely","evidence":"workspace effect inspected","checklist_satisfied":true}`),
				"call-1", "call-submit", 1,
			)
			return recordedResponse(request, http.StatusOK, "text/event-stream", reply), nil
		case 3:
			return recordedResponse(request, http.StatusOK, "text/event-stream", chatReply("done", 10)), nil
		default:
			t.Fatalf(
				"unexpected model request %d (recovery=%v nudge=%v)",
				len(requestBodies), strings.Contains(string(raw), soloRecoveryPrompt()),
				strings.Contains(string(raw), "You stopped without calling submit"),
			)
		}
		// Only the first response drops after executing a tool. The recovered
		// turn is healthy and can submit the preserved workspace normally.
		payload := strings.TrimSuffix(reply, "data: [DONE]\n\n")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: &errorAfterFile{
				payload: []byte(payload), path: filepath.Join(workspace, "recovered.txt"),
			},
			Request: request,
		}, nil
	})}
	backend := &openRouterBackend{
		apiKey: "test", client: client, totalTimeoutMS: -1, chunkTimeoutMS: -1,
	}
	runner := newPipeline(cliArgs{High: "openrouter/test/model"}, workspace, pipelineDeps{
		Backend: backend, Events: newEventWriter(&events), Notes: discardWriter{},
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	t.Cleanup(runner.runtime.Close)
	// The adaptive router is orthogonal to this test. Keeping its single
	// candidate out of cooldown lets the fresh turn start immediately.
	backend.router = nil
	state := &soloState{baseSHA: base}
	runner.runtime.registry.SetSubmitFreezer(
		func(_ context.Context, submission tool.Submission) (string, error) {
			return runner.soloFreezeWithContext(context.Background(), state, submission)
		},
	)

	outcome := soloOutcome{}
	const goal = "Create recovered.txt and submit the result."
	if err := runner.soloConverse(context.Background(), goal, state, &outcome); err != nil {
		t.Fatalf("solo recovery: %v", err)
	}
	if outcome.TerminalTrigger != "submitted" || state.candidate() == nil {
		t.Fatalf("outcome=%#v candidate=%#v, want submitted", outcome, state.candidate())
	}
	if len(requestBodies) != 3 {
		t.Fatalf("model requests=%d, want failed request plus one recovered tool cycle", len(requestBodies))
	}
	if got, err := os.ReadFile(filepath.Join(workspace, "recovered.txt")); err != nil ||
		strings.TrimSpace(string(got)) != effect {
		t.Fatalf("completed tool effect=%q err=%v", got, err)
	}
	second := string(requestBodies[1])
	if !strings.Contains(second, goal) || !strings.Contains(second, soloRecoveryPrompt()) {
		t.Fatalf("fresh request lost the task or recovery prompt: %s", second)
	}
	if strings.Contains(second, effect) {
		t.Fatalf("failed assistant/tool payload leaked into fresh context: %s", second)
	}
	retries := 0
	for _, event := range soloStageEvents(t, &events, "implement") {
		if event["status"] == "transport-retry" {
			retries++
		}
	}
	if retries != 1 {
		t.Fatalf("outer recovery turns=%d, want exactly 1", retries)
	}
}
