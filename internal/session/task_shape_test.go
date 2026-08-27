package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// shaperSettings puts a model on the careful tier, which is where the shaper's
// role sits, and asks it to think — so one assertion covers both the ladder and
// the level riding through.
func shaperSettings() func(string) (string, bool) {
	return func(key string) (string, bool) {
		if key == roles.TierKey(roles.TierHigh) {
			return "careful/model:high", true
		}
		return "", false
	}
}

// isShapeCall reports whether this request is the shaper's, by the one thing
// only it sends: the embedded meta-prompt as its system message.
func isShapeCall(messages []ai.Message) bool {
	return len(messages) > 0 && messages[0].Role == "system" &&
		messageContentText(messages[0]) == shapePrompt
}

// shapeAgent is a session whose task runner does nothing. Admission STARTS a
// node (TaskGraph.runFrontier), and what these tests are about is the brief the
// node was admitted with — not a worktree, a branch and a merge, which would
// also still be writing into the workspace while the test's temp directory was
// being taken away underneath them.
func shapeAgent(t *testing.T, client Completer) (*Agent, <-chan uint64) {
	t.Helper()
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = shaperSettings() })
	ran := make(chan uint64, 4)
	stubbedGraph(agent, func(node *TaskNode) {
		node.graph.complete(node, TaskDone)
		ran <- node.id
	})
	return agent, ran
}

// settled waits for the stubbed runner to have finished with the node, so the
// test ends with nothing of its own still running.
func settled(t *testing.T, ran <-chan uint64) {
	t.Helper()
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the admitted node never ran")
	}
}

const (
	shapedAsk        = "write a blog post about our launch"
	shapedTitle      = "launch post for existing users"
	shapedAcceptance = "The post is at blog/launch.md and names the three features by their shipped spelling."
)

// shapedAnswer is what a shaper that did its job returns: the person's sentence
// quoted whole, the brief written around it, and the name the rail will call it.
var shapedAnswer = `{"title":"` + shapedTitle +
	`","brief":"The ask, in their words: \"` + shapedAsk +
	`\". Write it for people who already use the product. No opening that restates the ` +
	`question, no three-item lists, no sentence that would be true of any launch.",` +
	`"acceptance":"` + shapedAcceptance + `"}`

// THE NODE IS ADMITTED WITH THE SHAPED BRIEF AND THE SHAPED NAME, and the two
// things a person reads underneath are still their own words: the summary under
// the row, and the request the worker is told outranks anything a model wrote.
func TestAPersonsTaskIsAdmittedWithTheShapedBrief(t *testing.T) {
	client := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse(shapedAnswer), nil
	}}}
	agent, ran := shapeAgent(t, client)

	id, title, err := agent.StartTask(t.Context(), shapedAsk)
	if err != nil {
		t.Fatal(err)
	}
	settled(t, ran)

	node := agent.graph().node(id)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	if !strings.Contains(node.spec.brief, "No opening that restates the question") {
		t.Fatalf("the node was not admitted with the shaped brief: %q", node.spec.brief)
	}
	if node.spec.acceptance != shapedAcceptance {
		t.Fatalf("the node's acceptance is %q", node.spec.acceptance)
	}
	// THE PERSON'S WORDS SURVIVE VERBATIM, in both places they are promised:
	// quoted inside the brief the shaper wrote, and whole in the request field
	// that composeBrief prints above it as the thing that wins.
	if !strings.Contains(node.spec.brief, shapedAsk) {
		t.Fatalf("the person's words are not inside the shaped brief: %q", node.spec.brief)
	}
	if node.spec.request != shapedAsk {
		t.Fatalf("the request is %q, want the person's own sentence", node.spec.request)
	}
	// AND THE ROW IS NAMED BY THE SAME ANSWER. The rail draws three words, and
	// the first three words of a typed sentence are how somebody cleared their
	// throat — "write a blog" — so the shaper that has already read the work
	// closely enough to brief a worker about it names it in the same breath.
	if title != shapedTitle || node.spec.title != shapedTitle {
		t.Fatalf("title = %q / %q, want the shaped name %q", title, node.spec.title, shapedTitle)
	}
	if node.spec.summary != shapedAsk {
		t.Fatalf("summary = %q, want the person's words", node.spec.summary)
	}

	if got := client.model(0); got != "careful/model" {
		t.Fatalf("the shaper ran on %q, want the careful tier", got)
	}
	if got := client.efforts[0]; got != provider.EffortHigh {
		t.Fatalf("effort = %q, want the level the tier named", got)
	}
}

func TestTheShaperCarriesThePlaceNamedInTheRequest(t *testing.T) {
	named := t.TempDir()
	request := "make the change in " + named
	answer := `{"title":"named change","brief":"Make the requested change.","acceptance":"The change is present.","where":"` + named + `"}`
	client := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse(answer), nil
	}}}
	agent, ran := shapeAgent(t, client)
	id, _, err := agent.StartTask(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	settled(t, ran)
	if got := agent.graph().node(id).spec.where; got != named {
		t.Fatalf("shaped where = %q, want the named directory %q", got, named)
	}
}

// IT IS BILLED THE WAY EVERY CALL NOBODY TYPED IS: to the session's total and
// its call count, never to a turn.
func TestShapingIsBilledAsAnAuxiliaryCall(t *testing.T) {
	client := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse(shapedAnswer), nil
	}}}
	agent, ran := shapeAgent(t, client)

	if _, _, err := agent.StartTask(t.Context(), shapedAsk); err != nil {
		t.Fatal(err)
	}
	settled(t, ran)
	usage := agent.Usage()
	if usage.Calls != 1 || usage.Input == 0 || usage.Output == 0 {
		t.Fatalf("usage = %+v, want one billed call with tokens on it", usage)
	}
	if usage.Turns != 0 {
		t.Fatalf("turns = %d, want the shaping call charged to no turn", usage.Turns)
	}
}

// EVERY FAILURE IS THE PERSON'S OWN WORDS. A task must never be blocked, held or
// lost by a call nobody asked for, so each of these starts exactly the task the
// command would have started before shaping existed.
func TestAShaperThatCannotAnswerLetsThePersonsWordsThrough(t *testing.T) {
	garbage := func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("Sure! Here is a brief for you:"), nil
	}
	empty := func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(""), nil }
	offline := func(context.Context, []ai.Message) (*ai.Response, error) { return nil, errors.New("offline") }
	// The deadline's path, without waiting out taskShapeWindow: what a stall
	// reaches this code as is a context that ended, and a call that ends is a
	// call that failed.
	stalled := func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	for _, tc := range []struct {
		name     string
		steps    []step
		requests int
		ctx      func(t *testing.T) context.Context
	}{
		{"prose where JSON was asked for", []step{garbage, garbage}, 2, nil},
		{"nothing at all", []step{empty}, 1, nil},
		{"a provider having a bad minute", []step{offline}, 1, nil},
		{"a call that ran out of time", []step{stalled}, 1, func(t *testing.T) context.Context {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			return ctx
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &scriptedCompleter{steps: tc.steps}
			agent, ran := shapeAgent(t, client)
			ctx := t.Context()
			if tc.ctx != nil {
				ctx = tc.ctx(t)
			}
			id, title, err := agent.StartTask(ctx, shapedAsk)
			if err != nil {
				t.Fatalf("a failed shaper refused the task: %v", err)
			}
			settled(t, ran)
			node := agent.graph().node(id)
			if node == nil {
				t.Fatal("a failed shaper lost the task")
			}
			if node.spec.brief != shapedAsk || node.spec.request != shapedAsk {
				t.Fatalf("brief = %q, request = %q, want the person's sentence untouched",
					node.spec.brief, node.spec.request)
			}
			if node.spec.acceptance != taskPersonAcceptance {
				t.Fatalf("acceptance = %q, want the plain one", node.spec.acceptance)
			}
			// AND THE NAME FALLS BACK TO THEIR OWN OPENING WORDS. There is no
			// shaped title when nothing shaped anything, and the mechanical cut is
			// what this path has always had to stand on.
			if title != shapedAsk {
				t.Fatalf("title = %q, want the person's words", title)
			}
			// AN EMPTY ANSWER IS NOT REPAIRED, and neither is a call that never
			// landed: the second request would ask the identical question of the
			// thing that just failed to answer it.
			//
			// Only the SHAPER'S calls are counted. A task nothing named is named
			// by one cheap call of its own after it has started (taskname.go), and
			// it is a different question on a different role — counting it here
			// would make this assertion about two features at once.
			if got := shapeCalls(client); got != tc.requests {
				t.Fatalf("%d shaping requests, want %d", got, tc.requests)
			}
		})
	}
}

// THE NAME COSTS NO SECOND CALL, and it is bounded and cleaned by the same hand
// that cleans the session's own ([cleanTitle]): a model asked for a short
// lowercase name answers "Title: …", or quotes it, or welds it into a slug, at
// the same rates whichever prompt asked.
func TestTheShapedNameIsCleanedAndIsSurvivableWhenAbsent(t *testing.T) {
	for _, tc := range []struct {
		name, answer, want string
	}{
		{"a plain name", `{"title":"launch post","brief":"do it","acceptance":"checkable"}`, "launch post"},
		{"a labelled one", `{"title":"Title: launch post","brief":"do it","acceptance":"c"}`, "launch post"},
		{"a quoted one", `{"title":"\"launch post\"","brief":"do it","acceptance":"c"}`, "launch post"},
		{"one that stopped", `{"title":"launch post.","brief":"do it","acceptance":"c"}`, "launch post"},
		{"a welded one", `{"title":"launch_post_draft","brief":"do it","acceptance":"c"}`, "launch post draft"},
		// NO NAME IS NOT A FAILED ANSWER. The brief is the field no default can
		// stand in for; a name has [taskPersonTitle] behind it, so a shaper that
		// answered with two fields where three were asked for costs a good name
		// and nothing else.
		{"none at all", `{"brief":"do it","acceptance":"checkable"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shaped, ok := parseShapedBrief(tc.answer)
			if !ok {
				t.Fatalf("%q was not read as an answer", tc.answer)
			}
			if shaped.Title != tc.want {
				t.Fatalf("title = %q, want %q", shaped.Title, tc.want)
			}
		})
	}
	// AND A MISSING NAME REACHES THE RAIL AS THEIR OWN OPENING WORDS, cut the way
	// this path has always cut them, at BOTH doors — a single task and an adaptive
	// run started from one sentence must never end up under two different names.
	long := "please have a look at why the nightly build keeps falling over on port b"
	if got := taskName("", long); got != "please have a look at why the nightly" {
		t.Fatalf("the fallback name is %q", got)
	}
	if got := taskName("  port b nightly build failures  ", long); got != "port b nightly build failures" {
		t.Fatalf("the shaped name is %q", got)
	}
}

// A shaped brief that came back with nothing in it is not an answer: admitting a
// blank brief would lose the work outright, which is the one thing this path is
// written not to do.
func TestAnEmptyShapedBriefIsNotAnAnswer(t *testing.T) {
	if _, ok := parseShapedBrief(`{"brief":"   ","acceptance":"checkable"}`); ok {
		t.Fatal("a blank brief was taken as a shaped one")
	}
	// An empty ACCEPTANCE is survivable, because there is a line to stand in for
	// it and none to stand in for the work.
	shaped, ok := parseShapedBrief("```json\n{\"brief\":\"do the thing\"}\n```")
	if !ok || shaped.Brief != "do the thing" || shaped.Acceptance != taskPersonAcceptance {
		t.Fatalf("fenced answer = %+v ok=%v", shaped, ok)
	}
}

// shapeCalls counts the requests that were the shaper's, by the one thing only
// it sends.
func shapeCalls(client *scriptedCompleter) int {
	count := 0
	for i := 0; i < client.requests(); i++ {
		if isShapeCall(client.request(i)) {
			count++
		}
	}
	return count
}
