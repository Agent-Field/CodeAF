package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestAppendRejectsMissingContentWithoutCreatingFiles(t *testing.T) {
	for _, fields := range []string{`"text":"wrong field"`, `"content":null`} {
		t.Run(fields, func(t *testing.T) {
			scripted := &scriptedCompleter{steps: []step{
				func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
					return toolResponse("badwrite", "write", `{"path":"new/file.txt","append":true,`+fields+`}`), nil
				},
				func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
					if !strings.Contains(lastToolText(messages), "content") {
						t.Error("missing actionable content error")
					}
					return textResponse("done"), nil
				},
			}}
			agent, _ := newTestAgent(t, scripted, nil)
			collect(t, mustSubmit(t, agent, "write the file"))
			if _, err := os.Stat(filepath.Join(agent.config.Workspace, "new")); !os.IsNotExist(err) {
				t.Errorf("invalid append created directory: %v", err)
			}
		})
	}
}
