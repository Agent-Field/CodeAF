package serve

import (
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A chat stage's conversation id is the session FILE PATH on the machine that
// ran it. No reader of a job may be handed it; only the worker holding the
// lease gets its own phase chat back, because that is how a pause resumes.
const stepChatPath = "/home/raj-secret/.codeaf/sessions/stage-7.jsonl"

func chatJob(t *testing.T, c capacityRig, handle string) factory.Item {
	t.Helper()
	it, err := c.st.Create(factory.Item{Title: "talk it through", Repo: "acme/api", State: factory.StateQueued, Assignee: handle,
		Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "code", State: factory.PhaseRunning, Chat: stepChatPath, Handle: "mentor"}}}})
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func TestNoReaderIsHandedARunnersConversationPath(t *testing.T) {
	c := capacitySetup(t)
	raj := c.attach(t, c.raj, "raj", "mbp", 0)
	job := chatJob(t, c, "raj")
	id := strconv.Itoa(job.ID)
	for _, who := range []struct{ name, token string }{{"priya", c.priya}, {"raj", c.raj}, {"operator", "operator-secret"}} {
		for _, path := range []string{"/v1/jobs/" + id, "/v1/queue", "/v1/jobs/" + id + "/step-edits"} {
			var out map[string]any
			body := c.request(t, "GET", path, who.token, nil, 200, &out)
			if strings.Contains(body, "raj-secret") || strings.Contains(body, `"chat"`) {
				t.Fatalf("%s reads a conversation path from %s: %s", who.name, path, body)
			}
			if path == "/v1/jobs/"+id && !strings.Contains(body, `"handle":"mentor"`) {
				t.Fatalf("%s lost the stage's handle with the path: %s", who.name, body)
			}
		}
	}

	// The lease holder still resumes its own conversation, and the stored
	// phase was never touched by the readers above.
	var pull api.RunnerPull
	c.request(t, "GET", "/v1/runners/pull", raj.Credential, nil, 200, &pull)
	if pull.Job.ID != job.ID || len(pull.Job.Steps) == 0 || pull.Job.Steps[0].Chat != stepChatPath {
		t.Fatalf("the worker lost its resume chat: %+v", pull.Job.Steps)
	}
	if got := api.ToItem(pull.Job).Stream.Phases[0].Chat; got != stepChatPath {
		t.Fatalf("the worker's decoded phase carries chat %q", got)
	}
	stored, err := c.st.Get(job.ID)
	if err != nil || stored.Stream.Phases[0].Chat != stepChatPath {
		t.Fatalf("stored phase chat changed: %+v, %v", stored.Stream, err)
	}
}
