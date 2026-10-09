package remote

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// recordingAgent is a fake engine agent that also takes the structured door, the
// way the real session agent does.
type recordingAgent struct {
	*fakeAgent
	words []string
	paths [][]string
}

func (r *recordingAgent) SubmitAttached(_ context.Context, text string, files []string, _ []session.Image) (<-chan session.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.words = append(r.words, text)
	r.paths = append(r.paths, files)
	return r.open(), nil
}

// An agent that records files structurally is handed the person's own words and
// the paths the engine wrote, not the model-facing sentence the engine would have
// composed, so the journal can keep the two apart.
func TestAnAttachedFileReachesTheStructuredDoorWithItsPath(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	folder := filepath.Join(root, "session")
	for _, dir := range []string{workspace, folder} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	agent := &recordingAgent{fakeAgent: &fakeAgent{model: "m"}}
	place := session.Place{Dir: folder, Workspace: workspace}
	s := &server{out: io.Discard, session: NewSession(&Engine{Agent: agent, Workspace: workspace, Place: place}, false)}

	if _, err := submitFilesCall(t, s, SubmitFilesArgs{
		Text:  "why is this failing",
		Files: []WireFile{{Name: "server.log", MIME: "text/plain", Bytes: []byte("panic\n")}},
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(agent.words) != 1 || agent.words[0] != "why is this failing" {
		t.Fatalf("words = %q, want the person's own", agent.words)
	}
	if len(agent.paths) != 1 || len(agent.paths[0]) != 1 || filepath.Base(agent.paths[0][0]) == "" ||
		filepath.Dir(agent.paths[0][0]) != filepath.Join(folder, attachmentsDirectory) {
		t.Fatalf("paths = %v", agent.paths)
	}
}

// The outcome receipts cross the wire the way the other reads do, and an agent
// without them answers none.
func TestReceiptReadsAreGetters(t *testing.T) {
	if classify(MethodRecentQuestionOutcomes) != classGetter {
		t.Fatalf("a receipt read is a getter, got class %v", classify(MethodRecentQuestionOutcomes))
	}
}
