package main

import (
	"fmt"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
)

func TestProjectTurnResultPopulatesTestEvidenceContract(t *testing.T) {
	// Validation contract C3: the latest persisted test exit status becomes the
	// scheduler-visible TestPassed signal for both pass and failure.
	for _, test := range []struct {
		name string
		code int
		want bool
	}{{"pass", 0, true}, {"fail", 2, false}} {
		t.Run(test.name, func(t *testing.T) {
			metadata := msgmodel.RawObject([]byte(`{"exitCode":` + fmt.Sprint(test.code) + `}`))
			messages := []msgmodel.WithParts{{
				Info: msgmodel.Assistant{MessageBase: msgmodel.MessageBase{ID: "assistant"}},
				Parts: msgmodel.Parts{msgmodel.ToolPart{
					Tool: "bash",
					State: msgmodel.CompletedToolState(
						msgmodel.RawObject(`{"command":"go test ./..."}`), "output", "test", metadata, 1, 2, nil,
					),
				}},
			}}
			result := projectTurnResult("session", messages, nil)
			if result.TestPassed == nil || *result.TestPassed != test.want {
				t.Fatalf("TestPassed = %v, want %v", result.TestPassed, test.want)
			}
		})
	}
}
