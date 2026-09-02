package session

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// isNameCall reports whether this request is the namer's, by the one thing only
// it sends: its own system line. The instruction itself rides at the END of the
// user message, where a small model reads it (taskname.go).
func isNameCall(messages []ai.Message) bool {
	return len(messages) > 0 && messages[0].Role == "system" &&
		messageContentText(messages[0]) == taskNameSystem
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
			// BOTH RUNGS ANSWER THE SAME WAY. An errand that fails falls
			// through the roles ladder once (auxiliary.go), so "never arrives"
			// is the tier's model and then the session's, and a script with one
			// step in it would be testing the fall-through landing rather than
			// the row being left alone.
			client := &scriptedCompleter{steps: []step{tc.step, tc.step}}
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
	// THE PROVIDER ANSWERS BY WHAT IT WAS ASKED, NEVER BY HOW MANY CALLS CAME
	// BEFORE IT. Admitting the node starts two things that both reach this one
	// completer and neither of which the code orders against the other: the
	// naming errand, and the node itself, whose finish posts `while you worked:
	// task 1 finished` and wakes the conversation into a turn of its own. This
	// test used to script three steps by index and assert that the third was
	// the namer's; on a loaded machine the woken turn takes the third slot
	// perhaps one run in ten, the namer falls off the end of the script, and
	// the row is called "(unscripted)". The identity of a call is a fact about
	// its messages, so it is read from its messages.
	var mu sync.Mutex
	shaped, named := 0, 0
	client := answeringCompleter(func(messages []ai.Message) string {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case isShapeCall(messages):
			// Prose where a brief was asked for, which is what shaping failing
			// looks like: the shaper reads JSON and there is none.
			shaped++
			return "Sure! Here is a brief for you:"
		case isNameCall(messages):
			named++
			return "parser recon"
		}
		return "(nothing else was scripted)"
	})
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
	mu.Lock()
	defer mu.Unlock()
	// The shaping really was tried and really did fail, and the name really did
	// cost one call — which is the whole claim the discarded call-index script
	// was standing in for.
	if shaped != 2 {
		t.Fatalf("the shaper was asked %d times, want the two attempts it is allowed", shaped)
	}
	if named != 1 {
		t.Fatalf("the namer was asked %d times, want exactly one", named)
	}
}

// answeringCompleter answers a request from the request itself, which is the
// only thing a provider shared by several agents at once can safely be scripted
// on. A list indexed by call number is a script for one caller, and this
// package's tests rarely have only one.
type answeringCompleter func(messages []ai.Message) string

func (a answeringCompleter) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	// The end of a turn asks a reader whether the person's ask is finished and
	// re-opens the turn when it is not, so THAT question is answered with the
	// remains contract's own token — otherwise a test that only cares about one
	// errand runs to the meter's ceiling. Same reasoning as the past-the-script
	// branch of [scriptedCompleter].
	if len(messages) > 0 && strings.Contains(messageText(messages[len(messages)-1]), "[still asked]") {
		return textResponse(checkpointNothingLeft), nil
	}
	return textResponse(a(messages)), nil
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

// ── THE LADDER FALLS THROUGH ONCE ───────────────────────────────────────────
//
// A role pinned to a model that is down used to cost the errand outright, with
// the model the person is talking to sitting there able to do it. It now costs
// one rung.

func TestAnErrandFallsThroughToTheNextRungAndBillsTheModelThatAnswered(t *testing.T) {
	const sentence = "read /Users/me/src and say what the parser does"
	var asked []string
	record := func(model string, fail bool) step {
		return func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			asked = append(asked, model)
			if fail {
				return nil, errors.New("that model is down")
			}
			return textResponse("parser recon"), nil
		}
	}
	client := &scriptedCompleter{steps: []step{
		record("cheap/model", true),
		record("test/model", false),
	}}
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
	if !nameLanded(func() bool { return graph.node(id).title() == "parser recon" }) {
		t.Fatalf("the name never landed; the row reads %q", graph.node(id).title())
	}
	// THE RUNGS, IN ORDER: the tier's model, then the model the conversation is
	// on — and never a third, because one fall-through is the whole budget.
	if len(asked) != 2 || client.model(0) != "cheap/model" || client.model(1) != "test/model" {
		t.Fatalf("the calls rode %v; want the tier's model then the session's",
			[]string{client.model(0), client.model(1)})
	}
}

// AND THE ANSWERING MODEL IS WHAT THE CALLER BILLS. Every caller of callRole
// hands the returned id to addAuxiliaryUsage, so a charge on the row of a model
// no request ever reached is a bill nobody could reconcile.
func TestCallRoleReportsTheModelThatAnswered(t *testing.T) {
	client := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return nil, errors.New("down") },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("ok"), nil },
	}}
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })

	response, answered, err := agent.callRole(context.Background(), roles.RoleTaskName, "test/model",
		[]ai.Message{textMessage("user", "name this")})
	if err != nil || response == nil {
		t.Fatalf("the fall-through did not land: %v", err)
	}
	if answered != "test/model" {
		t.Fatalf("the call reports %q, want the rung that actually answered", answered)
	}
	// AND ONE RUNG IS THE WHOLE BUDGET: a ladder walked to the bottom on every
	// errand would turn one bad minute at a provider into three charges.
	failing := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return nil, errors.New("down") },
		func(context.Context, []ai.Message) (*ai.Response, error) { return nil, errors.New("down") },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("late"), nil },
	}}
	stubborn, _ := newTestAgent(t, failing, func(c *Config) {
		c.RolesSource = func(key string) (string, bool) {
			switch key {
			case roles.PinKey(roles.RoleTaskName):
				return "pinned/model", true
			case roles.TierKey(roles.TierLow):
				return "cheap/model", true
			}
			return "", false
		}
	})
	if _, _, err := stubborn.callRole(context.Background(), roles.RoleTaskName, "test/model",
		[]ai.Message{textMessage("user", "name this")}); err == nil {
		t.Fatal("a third rung answered; one fall-through is the whole budget")
	}
	if failing.requests() != 2 {
		t.Fatalf("requests = %d, want the resolved rung and exactly one below it", failing.requests())
	}
}

// THE NAME IS ON THE TASK THE FIRST TIME A PERSON READS ABOUT IT. A road that
// starts work on its own asks for the name when it decides to, and the
// told-after line and the node carry it — as a name a model wrote, so nobody
// asks again.
func TestANameAskedAheadIsOnTheTaskWhenItIsAnnounced(t *testing.T) {
	client := &scriptedCompleter{steps: []step{func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		if !isNameCall(messages) {
			t.Errorf("the call was not the namer's: %q", messageContentText(messages[0]))
		}
		return textResponse("issue 252 check"), nil
	}}}
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })
	graph := stubbedGraph(agent, func(*TaskNode) {})

	const asked = "https://github.com/x/y/issues/252 Can you look at this and tell me whether it is done"
	ahead := agent.nameAhead(asked)
	if got := ahead.wait(); got != "issue 252 check" {
		t.Fatalf("the name asked ahead is %q", got)
	}
	said, id := agent.launchRouteTask(newEventHub(), routeVerdict{Work: true, Goal: "say whether issue 252 is done", Why: "an audit"}, asked, drawnDivision{}, ahead)
	if !strings.HasSuffix(said, ": issue 252 check") {
		t.Fatalf("the told-after line is %q; want it to end in the name", said)
	}
	if got := graph.node(id).title(); got != "issue 252 check" {
		t.Fatalf("the node is called %q", got)
	}
	// AND NOBODY ASKS AGAIN: the graph's own namer sees a name a model wrote.
	time.Sleep(200 * time.Millisecond)
	if got := client.requests(); got != 1 {
		t.Fatalf("%d calls were made to name one task", got)
	}
}

// A NAME STILL IN FLIGHT AT ADMISSION IS WAITED FOR, NOT ASKED FOR AGAIN. The
// line and the row carry the sentence until it lands, and then the row changes.
func TestANameStillInFlightAtAdmissionIsWaitedFor(t *testing.T) {
	release := make(chan struct{})
	client := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return textResponse("issue 252 check"), nil
	}}}
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })
	graph := stubbedGraph(agent, func(*TaskNode) {})

	const asked = "look at issue 252 and tell me whether it is done"
	ahead := agent.nameAhead(asked)
	said, id := agent.launchRouteTask(newEventHub(), routeVerdict{Work: true, Goal: asked, Why: "an audit"}, asked, drawnDivision{}, ahead)
	if strings.Contains(said, "issue 252 check") {
		t.Fatalf("the told-after line %q waited for a name that had not landed", said)
	}
	close(release)
	if !nameLanded(func() bool { return graph.node(id).title() == "issue 252 check" }) {
		t.Fatalf("the node is still called %q", graph.node(id).title())
	}
	time.Sleep(200 * time.Millisecond)
	if got := client.requests(); got != 1 {
		t.Fatalf("%d calls were made to name one task", got)
	}
}

// A ROAD THAT DECLINES LETS THE NAME GO. The call was made for work that never
// started, and a released, unclaimed one is cancelled rather than left holding
// a provider slot. And every method is safe on nil, because a road with nothing
// to name from hands the doors a nil.
func TestARoadThatDeclinesLetsTheNameGo(t *testing.T) {
	cancelled := make(chan struct{})
	client := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}}}
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })

	ahead := agent.nameAhead("look at issue 252 and tell me whether it is done")
	ahead.release()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the released call was not let go")
	}
	if got := ahead.wait(); got != "" {
		t.Fatalf("a released call answered %q", got)
	}

	var none *nameAhead
	none.claim()
	none.release()
	if name, landed := none.ready(); landed || name != "" {
		t.Fatalf("a nil name is %q, %v", name, landed)
	}
	if got := none.wait(); got != "" {
		t.Fatalf("a nil name waited into %q", got)
	}
	if agent.nameAhead("   ") != nil {
		t.Fatal("nothing to name from made a call")
	}
}
