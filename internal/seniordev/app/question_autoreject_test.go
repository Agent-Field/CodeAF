//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/question"
)

// A headless run has nothing that answers question.asked, so an unanswered
// question would block the run forever. The senior-dev runtime auto-rejects
// through the service's own reject path, so the call returns promptly with
// the rejection instead of hanging the run.
func TestHeadlessQuestionAutoRejectsInsteadOfHanging(t *testing.T) {
	runtime := newRuntime(t.TempDir(), nil)
	t.Cleanup(runtime.Close)

	input, err := json.Marshal(map[string]any{
		"questions": []map[string]any{{
			"question": "Which storage backend should the service use?",
			"header":   "Storage",
			"options": []map[string]any{
				{"label": "sqlite", "description": "Embedded file database"},
				{"label": "postgres", "description": "Networked relational database"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		result steploop.ToolResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, execErr := runtime.registry.Execute(context.Background(), steploop.ToolCall{
			Name: "question", Input: input,
			ID: "call-q1", SessionID: "ses-headless", MessageID: "msg-q1", Agent: "coder",
		})
		done <- outcome{result: result, err: execErr}
	}()
	select {
	case got := <-done:
		var rejected *question.RejectedError
		if !errors.As(got.err, &rejected) {
			t.Fatalf("question returned (%#v, %v), want the rejection error", got.result, got.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("question tool call hung: headless auto-reject did not fire")
	}
}
