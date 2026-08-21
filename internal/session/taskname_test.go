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

// isNameCall reports whether this request is the namer's, by the one thing only
// it sends: its own instruction as the system message.
func isNameCall(messages []ai.Message) bool {
	return len(messages) > 0 && messages[0].Role == "system" &&
		messageContentText(messages[0]) == taskNamePrompt
}

// nameSettings puts a model on the cheap tier, which is where the namer's role
// sits, so one assertion covers the ladder as well as the answer.
func nameSettings() func(string) (string, bool) {
	return func(key string) (string, bool) {
		if key == roles.TierKey(roles.TierLow) {
			return "cheap/model", true
		}
		return "", false
	}
}

// THE PREDICATE IS THE WHOLE ECONOMY OF THIS FEATURE: everything it calls a name
// costs nothing, and everything else costs one cheap call.
func TestTaskNameNeededTellsANameFromASentenceOrAPath(t *testing.T) {
	for _, c := range []struct {
		title string
		want  bool
	}{
		{"", true},
		{"   ", true},
		{"nil-map crash fix", false},
		{"frieren pdf summary", false},
		{"launch post", false},
		// Four words is past the column, so it is a sentence as far as the row is
		// concerned.
		{"launch post for existing users", true},
		{"read the parser and tell me what it does", true},
		// A PATH FAILS HOWEVER SHORT IT IS. The one thing a name must not be is
		// the machine's own filing, and this is the shape the complaint arrived
		// as: a row named after a temporary directory.
		{"/var/folders/j7/59f75dnd", true},
		{"read /Users/me", true},
		{`C:\Users\me`, true},
	} {
		if got := taskNameNeeded(c.title); got != c.want {
			t.Errorf("taskNameNeeded(%q) = %v, want %v", c.title, got, c.want)
		}
	}
}

// The answer is read back through the session namer's own repair and then held
// to the cap — and an answer that is still not a name is no answer at all.
func TestCleanTaskNameCutsToThreeWordsAndRefusesWhatIsNotAName(t *testing.T) {
	for _, c := range []struct{ raw, want string }{
		{"nil-map crash fix", "nil-map crash fix"},
		{"  Launch Post  ", "Launch Post"},
		{`"launch post"`, "launch post"},
		{"Title: launch post", "launch post"},
		{"launch post for existing users", "launch post for"},
		{"launch post.", "launch post"},
		// A ONE-TOKEN SLUG IS WORDS WELDED TOGETHER, and title.go's cleaner
		// unwelds it before this one counts them.
		{"launch_post_for_users", "launch post for"},
		// Nothing, and a path echoed back at us, are both refusals.
		{"", ""},
		{"   ", ""},
		{"/var/folders/j7/59f75dnd", ""},
	} {
		if got := cleanTaskName(c.raw); got != c.want {
			t.Errorf("cleanTaskName(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// THE WHOLE FEATURE, END TO END: a task admitted under a sentence is renamed by
// one cheap call, the new name is what every surface is told, and it is in the
// checkpoint so a restart keeps it.
func TestWorkAdmittedUnderASentenceIsNamedByTheCheapModel(t *testing.T) {
	client := &scriptedCompleter{steps: []step{func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		if !isNameCall(messages) {
			t.Errorf("the first call was not the namer's: %q", messageContentText(messages[0]))
		}
		return textResponse("parser recon"), nil
	}}}
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })
	ran := make(chan uint64, 4)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node.id })

	updates := agent.TaskUpdates()
	id := graph.reserve()
	graph.admit(id, taskSpec{
		title:      "read /Users/me/src and say what the parser does",
		summary:    "read the parser",
		brief:      "Read the parser under /Users/me/src and write down what it does.",
		acceptance: "a note saying what the parser does",
	})
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the admitted node never ran")
	}

	// THE NAME LANDS ON THE NODE, and it is the title from then on.
	if !nameLanded(func() bool { return graph.node(id).title() == "parser recon" }) {
		t.Fatalf("the node is still called %q", graph.node(id).title())
	}
	// AND THE WORLD IS TOLD. A row already on screen under the sentence learns
	// the name from an ordinary update.
	if !waitForNamedRow(updates, id, "parser recon") {
		t.Fatal("no update carried the new name")
	}
	// AND IT IS KEPT. The checkpoint is what a resumed session reads its roster
	// back out of, so a name that only lived in this process would be a row that
	// went back to its sentence after a restart.
	if got := graph.document().Nodes[0].Title; got != "parser recon" {
		t.Fatalf("the checkpoint says %q", got)
	}
	if got := client.model(0); got != "cheap/model" {
		t.Fatalf("the namer ran on %q, want the cheap tier", got)
	}
}

// THE WORK NEVER WAITS FOR THE NAME. The node is admitted, on the frontier and
// running before the call is made, which is what lets the call be slow, wrong or
// absent without costing anything but a good name.
func TestTheWorkStartsBeforeTheNameIsAskedFor(t *testing.T) {
	naming := make(chan struct{})
	release := make(chan struct{})
	client := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		close(naming)
		<-release
		return textResponse("parser recon"), nil
	}}}
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })
	ran := make(chan uint64, 4)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node.id })

	id := graph.reserve()
	graph.admit(id, taskSpec{
		title: "read /Users/me/src and say what the parser does", summary: "read the parser",
		brief: "Read the parser.", acceptance: "a note",
	})
	// admit has already returned and the node has already run, with the namer
	// still blocked inside the provider.
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the work waited for the name")
	}
	<-naming
	if got := graph.node(id).title(); got != "read /Users/me/src and say what the parser does" {
		t.Fatalf("the row is drawn as %q before the name lands", got)
	}
	close(release)
}

// EVERY FAILURE LEAVES THE ROW AS IT WAS. There is no state for "a small thing
// did not work", so a namer that could not answer says nothing and the surfaces
// go on drawing the fallback they drew before.
func TestANameThatNeverArrivesLeavesTheRowAsItWas(t *testing.T) {
	const sentence = "read /Users/me/src and say what the parser does"
	offline := func(context.Context, []ai.Message) (*ai.Response, error) { return nil, errors.New("offline") }
	empty := func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(""), nil }
	apath := func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("/var/folders/j7/59f75dnd"), nil
	}
	for _, tc := range []struct {
		name string
		step step
	}{
		{"a provider having a bad minute", offline},
		{"nothing at all", empty},
		{"the path handed straight back", apath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &scriptedCompleter{steps: []step{tc.step}}
			agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })
			ran := make(chan uint64, 4)
			graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node.id })
			id := graph.reserve()
			graph.admit(id, taskSpec{
				title: sentence, summary: "read the parser", brief: "Read the parser.", acceptance: "a note",
			})
			select {
			case <-ran:
			case <-time.After(5 * time.Second):
				t.Fatal("the admitted node never ran")
			}
			if !nameLanded(func() bool { return client.requests() > 0 }) {
				t.Fatal("the namer was never asked")
			}
			if got := graph.node(id).title(); got != sentence {
				t.Fatalf("a failed namer wrote %q", got)
			}
		})
	}
}

// WORK A MODEL ALREADY NAMED IS NOT NAMED AGAIN, and a title that is already
// short is not either. Both are the same rule said twice: the namer exists to
// turn a sentence into a name, and there is no call to make where there is no
// sentence.
func TestWorkThatAlreadyHasANameCostsNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec taskSpec
	}{
		{"a name the grooming model wrote", taskSpec{
			title: "Fix the nil-map crash in the reconciler", named: true,
			summary: "the crash", brief: "Fix it.", acceptance: "it does not crash",
		}},
		{"a title that is already two words", taskSpec{
			title: "parser recon", summary: "the parser", brief: "Read it.", acceptance: "a note",
		}},
		{"a design, which keeps its own title", taskSpec{
			title: "harness · write a weekly digest of the repository",
			brief: "write a weekly digest", acceptance: "a page",
			design: &harnessDesignSpec{goal: "write a weekly digest"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &scriptedCompleter{}
			agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })
			ran := make(chan uint64, 4)
			graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node.id })
			id := graph.reserve()
			want := tc.spec.title
			graph.admit(id, tc.spec)
			select {
			case <-ran:
			case <-time.After(5 * time.Second):
				t.Fatal("the admitted node never ran")
			}
			// Nothing to wait for is the assertion, so the window is a short one
			// spent proving no call was made.
			time.Sleep(150 * time.Millisecond)
			if calls := client.requests(); calls != 0 {
				t.Fatalf("%d call(s) were made to rename work that already had a name", calls)
			}
			if got := graph.node(id).title(); got != want {
				t.Fatalf("the title moved to %q", got)
			}
		})
	}
}

// IT IS BILLED THE WAY EVERY CALL NOBODY TYPED IS: to the session's total and
// its call count, never to a turn.
func TestNamingIsBilledAsAnAuxiliaryCall(t *testing.T) {
	client := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("parser recon"), nil
	}}}
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })
	ran := make(chan uint64, 4)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node.id })
	id := graph.reserve()
	graph.admit(id, taskSpec{
		title: "read /Users/me/src and say what the parser does", summary: "read the parser",
		brief: "Read the parser.", acceptance: "a note",
	})
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the admitted node never ran")
	}
	if !nameLanded(func() bool { return graph.node(id).title() == "parser recon" }) {
		t.Fatal("the name never landed")
	}
	usage := agent.Usage()
	if usage.Calls != 1 || usage.Input == 0 || usage.Output == 0 {
		t.Fatalf("usage = %+v, want one billed call with tokens on it", usage)
	}
	if usage.Turns != 0 {
		t.Fatalf("turns = %d, want the naming call charged to no turn", usage.Turns)
	}
}

// THE NAMER IS ON THE CHEAP CLASS, and the role is registered — a role that is
// not is an auxiliary call that resolves to nothing and never fires.
func TestTheNamerIsRegisteredOnTheCheapClass(t *testing.T) {
	tier, ok := roles.TierOf(roles.RoleTaskName)
	if !ok {
		t.Fatal("the taskname role is not registered")
	}
	if tier != roles.TierLow {
		t.Fatalf("the taskname role sits on %q, want the cheap class", tier)
	}
	if strings.TrimSpace(roles.Describe(roles.RoleTaskName)) == "" {
		t.Fatal("the taskname role has no line under its name in the settings list")
	}
}

// A `/task` WHOSE SHAPING COULD NOT RUN IS STILL NAMED. That is the exact case
// the complaint arrived as: with no shaper, the row falls back to the person's
// own first eight words, and the column shows the first three of them — "read
// /Users/me/src" — which names the work after the path they pasted.
func TestATaskWhoseShapingFailedIsNamedAnyway(t *testing.T) {
	const ask = "read /Users/me/src and say what the parser does and where it is weakest"
	garbage := func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("Sure! Here is a brief for you:"), nil
	}
	// Two attempts at the shaper, then the namer: the shaping calls are made and
	// exhausted before the task is admitted, so the order is fixed.
	client := &scriptedCompleter{steps: []step{garbage, garbage,
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if !isNameCall(messages) {
				t.Errorf("the third call was not the namer's")
			}
			return textResponse("parser recon"), nil
		}}}
	agent, ran := shapeAgent(t, client)

	id, title, err := agent.StartTask(t.Context(), ask)
	if err != nil {
		t.Fatal(err)
	}
	settled(t, ran)
	// WHAT THE COMMAND ANSWERS WITH IS THE FALLBACK, because the name does not
	// exist yet and nothing waits for it.
	if title != taskPersonTitle(ask) {
		t.Fatalf("StartTask answered with %q, want the fallback", title)
	}
	if !nameLanded(func() bool { return agent.graph().node(id).title() == "parser recon" }) {
		t.Fatalf("the row is still called %q", agent.graph().node(id).title())
	}
}

// nameLanded spins on a condition for as long as a naming call can take to land.
func nameLanded(done func() bool) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return done()
}

// waitForNamedRow drains task updates until one names the node.
func waitForNamedRow(updates <-chan Event, id uint64, name string) bool {
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event, open := <-updates:
			if !open {
				return false
			}
			if event.Task != nil && event.Task.ID == id && event.Task.Title == name {
				return true
			}
		case <-deadline:
			return false
		}
	}
}
