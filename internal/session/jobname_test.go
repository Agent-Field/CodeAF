package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// isJobNameCall reports whether this request is the job namer's, by the one
// thing only it sends: its own system line. The instruction itself rides at
// the END of the user message, where a small model reads it (jobname.go).
func isJobNameCall(messages []ai.Message) bool {
	return len(messages) > 0 && messages[0].Role == "system" &&
		messageContentText(messages[0]) == jobNameSystem
}

// jobNameAsk is a command that is NOT already a name: it has quotes, it is
// longer than [JobNameWords], and it stays running so the tests can look at
// the row before it settles.
const jobNameAsk = `bash -lc "sleep 30"`

// answerTheJobNamerOffTheQueue installs an aside that answers the job namer
// without spending a scripted step, for the same reason
// [answerTheNamerOffTheQueue] exists: the namer is not one of the turn's
// rounds.
func answerTheJobNamerOffTheQueue(completer *scriptedCompleter, name string) {
	completer.aside = func(messages []ai.Message) (*ai.Response, bool) {
		if !isJobNameCall(messages) {
			return nil, false
		}
		return textResponse(name), true
	}
}

// THE PREDICATE IS THE WHOLE ECONOMY OF THIS FEATURE: everything it calls a name
// costs nothing, and everything else costs one cheap call.
func TestAJobThatAlreadyHasALabelIsNeverSentToTheNamer(t *testing.T) {
	for _, c := range []struct {
		name string
		info jobInfo
		want bool
	}{
		{"a watch's own label", jobInfo{label: "app", command: `bash -lc "pytest -q tests/ --maxfail=1"`}, false},
		{"a render's own label", jobInfo{label: "video", command: "a sunset over the harbour at dusk"}, false},
		{"a hand's own label", jobInfo{label: "hand 1", command: "review the diff"}, false},
		{"a task node's own label", jobInfo{label: "task 7", command: "Fix the nil-map crash"}, false},
		{"a name that has already landed", jobInfo{name: "run pytest quietly", command: jobNameAsk}, false},
		{"npm run dev is already a name", jobInfo{command: "npm run dev"}, false},
		{"sleep 30 is already a name", jobInfo{command: "sleep 30"}, false},
		{"four words and no shell", jobInfo{command: "go test -count 1"}, false},
		{"an empty command", jobInfo{command: ""}, false},
		{"a long shell command", jobInfo{command: `bash -lc "pytest -q tests/ --maxfail=1"`}, true},
		{"a pipe is a script", jobInfo{command: "ls | wc"}, true},
		{"five words is a sentence", jobInfo{command: "go test ./internal/session -count 1"}, true},
	} {
		if got := jobNameNeeded(c.info); got != c.want {
			t.Errorf("%s: jobNameNeeded(%q, label %q) = %v, want %v",
				c.name, c.info.command, c.info.label, got, c.want)
		}
	}

	// AND A LABELLED START DOES NOT PAY. startRender mints a label the moment
	// the job exists, so the namer's gate is the whole of why this call is not
	// made — not a special case in the starter.
	client := &scriptedCompleter{}
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })
	started, _, err := agent.jobs.startRender("video", "a sunset over the harbour")
	if err != nil {
		t.Fatalf("could not start the render: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if calls := client.requests(); calls != 0 {
		t.Fatalf("%d call(s) were made to name a job that already had a label", calls)
	}
	if got := started.info().name; got != "" {
		t.Fatalf("a labelled job was named %q", got)
	}
}

// The answer is read back through the session namer's own repair and then held
// to the cap — and an answer that is still not a name is no answer at all.
func TestAJobNameIsCutToJobNameWords(t *testing.T) {
	if JobNameWords != 4 {
		t.Fatalf("JobNameWords = %d, want 4 — the surface reads this figure", JobNameWords)
	}
	for _, c := range []struct{ raw, want string }{
		{"run pytest quietly", "run pytest quietly"},
		{"  Run Pytest Quietly  ", "Run Pytest Quietly"},
		{`"run pytest quietly"`, "run pytest quietly"},
		{"Title: run pytest quietly", "run pytest quietly"},
		{"run the pytest suite extra words here", "run the pytest suite"},
		{"run pytest quietly.", "run pytest quietly"},
		{"run_the_pytest_suite_now", "run the pytest suite"},
		{"", ""},
		{"   ", ""},
		{`bash -lc "pytest`, ""},
	} {
		if got := cleanJobName(c.raw); got != c.want {
			t.Errorf("cleanJobName(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// EVERY FAILURE LEAVES THE JOB NAMED AS IT WAS. There is no state for "a small
// thing did not work", so a namer that could not answer says nothing and the
// surfaces go on drawing the command they drew before.
func TestAJobNamerThatFailsLeavesTheNameUntouched(t *testing.T) {
	offline := func(context.Context, []ai.Message) (*ai.Response, error) { return nil, errors.New("offline") }
	empty := func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(""), nil }
	echo := func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse(jobNamePrompt), nil
	}
	for _, tc := range []struct {
		name string
		step step
	}{
		{"a provider having a bad minute", offline},
		{"nothing at all", empty},
		{"the instruction handed straight back", echo},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// BOTH RUNGS ANSWER THE SAME WAY. An errand that fails falls
			// through the roles ladder once (auxiliary.go), so "never arrives"
			// is the tier's model and then the session's, and a script with one
			// step in it would be testing the fall-through landing rather than
			// the name being left alone.
			client := &scriptedCompleter{steps: []step{tc.step, tc.step}}
			agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })
			id := startJob(t, agent, jobNameAsk)
			if !nameLanded(func() bool { return client.requests() > 0 || client.asideRequests() > 0 }) {
				t.Fatal("the namer was never asked")
			}
			one := agent.jobs.find(id)
			if one == nil {
				t.Fatal("the job vanished")
			}
			if got := one.info().name; got != "" {
				t.Fatalf("a failed namer wrote %q", got)
			}
		})
	}
}

// THE WORK NEVER WAITS FOR THE NAME. The job is registered, running and
// announced before the call is made, which is what lets the call be slow,
// wrong or absent without costing anything but a good name.
func TestTheJobStartsBeforeTheNameIsAskedFor(t *testing.T) {
	naming := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	client := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		close(naming)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return textResponse("run pytest quietly"), nil
	}}}
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })

	id := startJob(t, agent, jobNameAsk)
	// startJob has already returned and the process has already forked, with
	// the namer still blocked inside the provider.
	one := agent.jobs.find(id)
	if one == nil || !one.running() {
		t.Fatal("the job waited for the name")
	}
	if got := one.info().name; got != "" {
		t.Fatalf("the job is already called %q before the name lands", got)
	}
	select {
	case <-naming:
	case <-time.After(5 * time.Second):
		t.Fatal("the namer was never asked")
	}
	close(release)
}

// A NAME ARRIVING IS NEWS. The first notice goes out under the command, and a
// second EventJobUpdate carries the name so a surface that already drew the
// row can redraw it.
func TestANameArrivingPublishesASecondJobUpdate(t *testing.T) {
	const named = "run pytest quietly"
	client := &scriptedCompleter{}
	answerTheJobNamerOffTheQueue(client, named)
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })
	lane, stop := agent.WatchTaskUpdates()
	t.Cleanup(stop)

	id := startJob(t, agent, jobNameAsk)

	var first, second *JobNotice
	deadline := time.After(5 * time.Second)
	for second == nil {
		select {
		case event, open := <-lane:
			if !open {
				t.Fatal("the lane closed before the name arrived")
			}
			if event.Kind != EventJobUpdate || event.Job == nil || event.Job.ID != id {
				continue
			}
			notice := *event.Job
			if first == nil {
				first = &notice
				if first.Name != "" {
					t.Fatalf("the first notice already carried a name: %q", first.Name)
				}
				continue
			}
			if notice.Name == named {
				second = &notice
			}
		case <-deadline:
			t.Fatal("timed out waiting for a second EventJobUpdate carrying the name")
		}
	}
	if first.Command == "" {
		t.Fatal("the first notice did not carry the command")
	}
	if second.ID != first.ID {
		t.Fatalf("the named notice is job %d and the first was job %d", second.ID, first.ID)
	}
}

// THE NAMER IS ON THE CHEAP CLASS, and the role is registered — a role that is
// not is an auxiliary call that resolves to nothing and never fires.
func TestTheJobNamerIsRegisteredOnTheCheapClass(t *testing.T) {
	tier, ok := roles.TierOf(roles.RoleJobName)
	if !ok {
		t.Fatal("the jobname role is not registered")
	}
	if tier != roles.TierLow {
		t.Fatalf("the jobname role sits on %q, want the cheap class", tier)
	}
	if strings.TrimSpace(roles.Describe(roles.RoleJobName)) == "" {
		t.Fatal("the jobname role has no line under its name in the settings list")
	}
}

// THE SUBJECT IS THE COMMAND AND THE DIRECTORY, clipped, so a giant heredoc
// does not become a giant prompt.
func TestTheJobNamerIsShownTheCommandAndTheWorkingDirectory(t *testing.T) {
	var seen []ai.Message
	client := &scriptedCompleter{}
	client.aside = func(messages []ai.Message) (*ai.Response, bool) {
		if !isJobNameCall(messages) {
			return nil, false
		}
		seen = messages
		return textResponse("run pytest quietly"), true
	}
	agent, workspace := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })
	id := startJob(t, agent, jobNameAsk)
	if !nameLanded(func() bool { return agent.jobs.find(id).info().name == "run pytest quietly" }) {
		t.Fatal("the name never landed")
	}
	if len(seen) < 2 {
		t.Fatal("the namer was not asked with a user message")
	}
	user := messageContentText(seen[1])
	if !strings.Contains(user, jobNameAsk) {
		t.Fatalf("the namer was not shown the command: %q", user)
	}
	if !strings.Contains(user, workspace) {
		t.Fatalf("the namer was not shown the working directory %q: %q", workspace, user)
	}
	if !strings.HasSuffix(strings.TrimSpace(user), jobNamePrompt) {
		t.Fatalf("the instruction was not last in the user message: %q", user)
	}
}
