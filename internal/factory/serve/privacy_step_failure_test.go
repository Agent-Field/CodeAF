package serve

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// FS0/FS15: StampFailure copies a failure, machine included, onto the Stream
// phase it belongs to, and the wire serializes that as steps[i].failure. Every
// body that carries steps (job, queue, step-edits) must withhold a machine the
// caller cannot prove is theirs, searched over the WHOLE response bytes.
func TestStepFailureNeverNamesAnotherPersonsMachine(t *testing.T) {
	c := capacitySetup(t)
	c.attach(t, c.priya, "priya", "priya-box", 0)
	c.attach(t, c.priya, "priya", "mbp", 0)
	raj := c.attach(t, c.raj, "raj", "raj-box", 3)
	c.attach(t, c.raj, "raj", "mbp", 0)

	stamp := func(assignee, machine string) factory.Item {
		it := factory.Item{Title: "step failure", Repo: "acme/api", State: factory.StateQueued, Assignee: assignee, Changed: c.now,
			Stream: &factory.Stream{Phases: []factory.Phase{{Name: "code", State: factory.PhaseRunning}}}}
		factory.StampFailure(&it, factory.NewFailure("code", factory.CodeToolError, "red", machine, factory.FailureStopped, 1, c.now))
		it.Failure = nil // only the step carries it: the top-level redaction cannot cover for the step
		made, err := c.st.Create(it)
		if err != nil {
			t.Fatal(err)
		}
		return made
	}
	// First, so raj's pull takes it and the hold (held_by) is live for the others.
	held := stamp("raj", "raj-box")
	handedOver := stamp("priya", "raj-box") // assignee priya proves nothing about raj's id
	shared := stamp("raj", "mbp")           // an id both registered is nobody's to name
	var pull api.RunnerPull
	c.request(t, "GET", "/v1/runners/pull", raj.Credential, nil, 200, &pull)
	if pull.Job.ID != held.ID {
		t.Fatalf("raj pulled %d, want %d", pull.Job.ID, held.ID)
	}

	stepFailure := func(steps []api.Step) *api.Failure {
		for _, s := range steps {
			if s.Failure != nil {
				return s.Failure
			}
		}
		return nil
	}
	type door struct {
		name, path string
		steps      func(body string) []api.Step
	}
	jobSteps := func(body string) []api.Step { var j api.Job; mustJSON(t, body, &j); return j.Steps }
	id := func(it factory.Item) string { return strconv.Itoa(it.ID) }
	doors := func(it factory.Item) []door {
		return []door{
			{"job", "/v1/jobs/" + id(it), jobSteps},
			{"step-edits", "/v1/jobs/" + id(it) + "/step-edits", func(body string) []api.Step { var e api.StepEdits; mustJSON(t, body, &e); return e.Steps }},
		}
	}
	for _, who := range []struct{ name, token string }{{"priya", c.priya}, {"operator", "operator-secret"}, {"raj", c.raj}} {
		ownsRajBox := who.name == "raj"
		for _, it := range []factory.Item{held, handedOver} {
			for _, d := range doors(it) {
				body := c.request(t, "GET", d.path, who.token, nil, 200, nil)
				f := stepFailure(d.steps(body))
				if f == nil || f.Detail != "red" || f.Step != "code" {
					t.Fatalf("%s %s lost the step failure itself: %s", who.name, d.name, body)
				}
				if has := strings.Contains(body, "raj-box"); has != ownsRajBox {
					t.Fatalf("%s %s: raj-box present=%v, want %v: %s", who.name, d.name, has, ownsRajBox, body)
				}
				if ownsRajBox && (f.Machine != "raj-box" || f.Runner != "raj-box") {
					t.Fatalf("raj lost his own machine on %s: %s", d.name, body)
				}
			}
		}
		for _, d := range doors(shared) {
			if body := c.request(t, "GET", d.path, who.token, nil, 200, nil); strings.Contains(body, `"mbp"`) {
				t.Fatalf("%s %s names an id two people registered: %s", who.name, d.name, body)
			}
		}
		body := c.request(t, "GET", "/v1/queue", who.token, nil, 200, nil)
		if has := strings.Contains(body, "raj-box"); has != ownsRajBox {
			t.Fatalf("%s /v1/queue: raj-box present=%v, want %v: %s", who.name, has, ownsRajBox, body)
		}
		if strings.Contains(body, `"mbp"`) {
			t.Fatalf("%s /v1/queue names an id two people registered: %s", who.name, body)
		}
	}
}

func mustJSON(t *testing.T, body string, out any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), out); err != nil {
		t.Fatal(err)
	}
}
