package serve

import (
	"strconv"
	"strings"
	"testing"
	"time"

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

// The manager conversation's session path (Desk.Talk) is as runner-local as a
// step's chat: no reader, the owner included, is handed it, while the thread
// and the other desk facts stay, and the worker's own pull keeps it.
const deskTalkPath = "/home/raj-secret/.codeaf/sessions/manager-3.jsonl"

func TestNoReaderIsHandedARunnersManagerTalkPath(t *testing.T) {
	c := capacitySetup(t)
	raj := c.attach(t, c.raj, "raj", "mbp", 0)
	job, err := c.st.Create(factory.Item{Title: "talk it through", Repo: "acme/api", State: factory.StateQueued, Assignee: "raj",
		Talk:   deskTalkPath,
		Thread: []factory.Message{{From: "manager", Text: "see /repo/internal/x.go for the fix"}},
		Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "code", State: factory.PhaseRunning}}}})
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.Itoa(job.ID)
	for _, who := range []struct{ name, token string }{{"priya", c.priya}, {"raj", c.raj}, {"operator", "operator-secret"}} {
		for _, path := range []string{"/v1/jobs/" + id, "/v1/queue"} {
			var out map[string]any
			body := c.request(t, "GET", path, who.token, nil, 200, &out)
			if strings.Contains(body, "manager-3.jsonl") || strings.Contains(body, `"talk"`) {
				t.Fatalf("%s reads the manager session path from %s: %s", who.name, path, body)
			}
			if path == "/v1/jobs/"+id && !strings.Contains(body, "see /repo/internal/x.go for the fix") {
				t.Fatalf("%s lost the thread with the path: %s", who.name, body)
			}
		}
	}
	var pull api.RunnerPull
	c.request(t, "GET", "/v1/runners/pull", raj.Credential, nil, 200, &pull)
	if pull.Job.Desk == nil || pull.Job.Desk.Talk != deskTalkPath {
		t.Fatalf("the worker lost its manager session: %+v", pull.Job.Desk)
	}
	stored, err := c.st.Get(job.ID)
	if err != nil || stored.Talk != deskTalkPath {
		t.Fatalf("stored talk changed: %q, %v", stored.Talk, err)
	}
}

// An earlier round's conversation id is the same kind of path as its step's,
// and is withheld from every reader the same way; when the round began is not
// a path and stays. The worker holding the lease keeps each round's chat.
const roundChatPath = "/home/raj-secret/.codeaf/sessions/stage-7-round-1.jsonl"

func TestNoReaderIsHandedARoundsConversationPathButEveryoneReadsWhenItBegan(t *testing.T) {
	c := capacitySetup(t)
	raj := c.attach(t, c.raj, "raj", "mbp", 0)
	t0 := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	job, err := c.st.Create(factory.Item{Title: "review it twice", Repo: "acme/api", State: factory.StateQueued, Assignee: "raj",
		Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "code", State: factory.PhaseRunning, Round: 2,
			Started: t0.Add(5 * time.Minute), Chat: stepChatPath,
			Rounds: []factory.RoundLine{{Round: 1, Summary: "2 findings", Started: t0, At: t0.Add(4 * time.Minute), Chat: roundChatPath}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.Itoa(job.ID)
	for _, who := range []struct{ name, token string }{{"priya", c.priya}, {"raj", c.raj}, {"operator", "operator-secret"}} {
		for _, path := range []string{"/v1/jobs/" + id, "/v1/queue"} {
			var out map[string]any
			body := c.request(t, "GET", path, who.token, nil, 200, &out)
			if strings.Contains(body, "raj-secret") || strings.Contains(body, `"chat"`) {
				t.Fatalf("%s reads a conversation path from %s: %s", who.name, path, body)
			}
			if path == "/v1/jobs/"+id && (!strings.Contains(body, `"started_at":"2026-10-19T09:05:00Z"`) || !strings.Contains(body, `"started_at":"2026-10-19T09:00:00Z"`)) {
				t.Fatalf("%s lost when the step or its earlier round began: %s", who.name, body)
			}
		}
	}
	var pull api.RunnerPull
	c.request(t, "GET", "/v1/runners/pull", raj.Credential, nil, 200, &pull)
	if pull.Job.ID != job.ID || len(pull.Job.Steps) == 0 || len(pull.Job.Steps[0].Rounds) != 1 || pull.Job.Steps[0].Rounds[0].Chat != roundChatPath {
		t.Fatalf("the worker lost its round's chat: %+v", pull.Job.Steps)
	}
	stored, err := c.st.Get(job.ID)
	if err != nil || stored.Stream.Phases[0].Rounds[0].Chat != roundChatPath {
		t.Fatalf("stored round chat changed: %+v, %v", stored.Stream, err)
	}
}
