package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

// C2: a /task shape that asks to work in place inside a repository is given the
// same branch isolation as a model proposal, and its start note says so once.
func TestC2APersonsShapedTaskCannotBePutInARepositoryCheckoutByTheShaper(t *testing.T) {
	repo := newTestRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}
	answer := `{"title":"isolated note","brief":"write isolated.txt","acceptance":"isolated.txt exists","where":"in place"}`
	client := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse(answer), nil
	}}}
	agent, _ := newTestAgent(t, client, func(config *Config) {
		config.Workspace = repo
		config.Place = place
		config.RolesSource = shaperSettings()
	})
	world, release := heldTaskWorld(t, agent, "isolated.txt", "only in the task copy\n")
	id, _, note, err := agent.StartTask(t.Context(), "write the isolated note")
	if err != nil {
		t.Fatal(err)
	}
	tree := <-world
	want := whereRedirectSentence("in place", canonicalPath(repo))
	if strings.Count(note, want) != 1 {
		t.Fatalf("start note does not carry the redirect once: %q", note)
	}
	if tree.root != canonicalPath(repo) || !withinDir(place.Trees(), tree.dir) || !strings.HasPrefix(tree.branch, "task/") {
		t.Fatalf("tree = %+v, want a task branch under the session trees", tree)
	}
	if _, err := os.Stat(filepath.Join(repo, "isolated.txt")); !os.IsNotExist(err) {
		t.Fatalf("the repository checkout was written: %v", err)
	}
	release()
	waitDoneNode(t, agent.graph().node(id))
}

// THE NODE IS ADMITTED WITH THE SHAPED BRIEF AND THE SHAPED NAME, and the two
// things a person reads underneath are still their own words: the summary under
// the row, and the request the worker is told outranks anything a model wrote.
func TestAPersonsTaskIsAdmittedWithTheShapedBrief(t *testing.T) {
	client := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse(shapedAnswer), nil
	}}}
	agent, ran := shapeAgent(t, client)

	id, title, _, err := agent.StartTask(t.Context(), shapedAsk)
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

// A PASTED ISSUE'S BACKTICKED REPRODUCTION NEVER REACHES THE DOOR IT STARTED
// THROUGH.
//
// The measured ask named `chmod 000 tox.ini` in the punctuation nearly every
// bug report uses, and the door once treated it as the work's check. Driving
// the ask through StartTask proves the admitted node keeps the account boundary;
// reading that node's real door and the file's mode proves the command was
// neither offered nor run. The shaper writes prose, so its backticked suggestion
// does not declare a repeatable verification contract either.
func TestAPastedIssuesBacktickedCommandNeverReachesTheDoorItStartedThrough(t *testing.T) {
	tree := t.TempDir()
	tox := writeCheckFile(t, tree, "tox.ini", "[tox]\n", 0o644)
	writeCheckFile(t, tree, "check.sh", "#!/bin/sh\nexit 0\n", 0o755)
	ask := "Pasted issue body. Repro: `chmod 000 tox.ini`, then watch the suite fail."
	answer := "{\"title\":\"repair tox\",\"brief\":\"Repair the tox configuration and check it with `check.sh`.\",\"acceptance\":\"`check.sh` passes.\"}"
	client := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse(answer), nil
	}}}
	agent, _ := newTestAgent(t, client, func(config *Config) {
		config.Workspace = tree
		config.TaskAudit = true
		config.RolesSource = shaperSettings()
	})
	ran := make(chan uint64, 1)
	stubbedGraph(agent, func(node *TaskNode) {
		node.graph.complete(node, TaskDone)
		ran <- node.id
	})

	id, _, _, err := agent.StartTask(t.Context(), ask)
	if err != nil {
		t.Fatal(err)
	}
	settled(t, ran)
	node := agent.graph().node(id)
	if node == nil {
		t.Fatal("the pasted issue did not produce an admitted node")
	}
	door := auditDoorFor(node, tree)
	if len(door.checks) != 0 {
		t.Fatalf("the admitted node harvested prose as checks: %q", door.checks)
	}
	if containsWord(door.allowed, "chmod 000 tox.ini") {
		t.Fatalf("the pasted reproduction entered the admitted node's door: %q", door.allowed)
	}
	if strings.Contains(door.offer(), "chmod 000 tox.ini") {
		t.Fatalf("the admitted node offered the pasted reproduction:\n%s", door.offer())
	}
	if _, ok := auditRefusal("chmod 000 tox.ini", door.allowed); ok {
		t.Fatal("the admitted node's gate allowed the pasted reproduction")
	}
	info, err := os.Stat(tox)
	if err != nil {
		t.Fatalf("stat tox.ini after the task started: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o644 {
		t.Fatalf("tox.ini mode after the task started = %04o, want 0644", mode)
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
	id, _, _, err := agent.StartTask(t.Context(), request)
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

	if _, _, _, err := agent.StartTask(t.Context(), shapedAsk); err != nil {
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
	// The deadline's path, without waiting out TaskShapeWindow: what a stall
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
			id, title, _, err := agent.StartTask(ctx, shapedAsk)
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

// ── the brief, watched while it is written ──────────────────────────────────

// A CALLER THAT ASKS TO WATCH SEES THE ANSWER ARRIVE. The gap this closes is
// thirteen seconds of `shaping the brief…` and nothing else, in front of a
// person who has just typed a command; the words exist the whole time, and this
// is the door they leave by.
//
// What arrives is the ACCUMULATED text, raw and partial — a prefix of a JSON
// object — and [PartialString] is what reads one field of it. Nothing here
// unmarshals anything, which is the whole contract.
func TestTheShaperReportsTheBriefToACallerThatAsksToWatch(t *testing.T) {
	fragments := []string{`{"title":"launch post"`, `,"brief":"Write it for people`, ` who already use the product."}`}
	client := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		whole := ""
		for _, part := range fragments {
			whole += part
			provider.Emit(ctx, provider.StreamDelta, part)
		}
		// The reasoning the model did on the way rides its own side of the watch
		// and never joins the answer.
		provider.Emit(ctx, provider.StreamReasoning, "thinking about the audience")
		return textResponse(whole + `,"acceptance":"It is at blog/launch.md."}`), nil
	}}}
	agent, ran := shapeAgent(t, client)
	var seen []string
	ctx := WithBriefWatch(context.Background(), func(answer, _ string) { seen = append(seen, answer) })

	shaped := agent.shapeBrief(ctx, shapedAsk)
	// One reading per delta of either kind — the three answer fragments and the
	// reasoning event after them, which reports the same answer again beside a
	// think the caller is free to ignore.
	if len(seen) != len(fragments)+1 {
		t.Fatalf("the watch saw %d readings, want one per delta: %q", len(seen), seen)
	}
	if seen[0] != fragments[0] {
		t.Fatalf("the first reading is not the first fragment: %q", seen[0])
	}
	// EVERY READING IS THE WHOLE ANSWER SO FAR, which is what lets a surface
	// that missed one draw the right thing from the next.
	if want := fragments[0] + fragments[1]; seen[1] != want {
		t.Fatalf("the second reading is not accumulated: %q, want %q", seen[1], want)
	}
	if brief, ok := PartialString(seen[1], "brief"); !ok || !strings.HasPrefix(brief, "Write it for people") {
		t.Fatalf("the partial answer does not read as a brief: %q ok=%v", brief, ok)
	}
	if last := seen[len(seen)-1]; strings.Contains(last, "thinking about the audience") {
		t.Fatalf("THE REASONING REACHED THE ANSWER: %q", last)
	}
	if !strings.Contains(shaped.Brief, "already use the product") {
		t.Fatalf("watching the call changed what it returned: %q", shaped.Brief)
	}
	_ = ran
}

// AND A CALLER THAT DID NOT ASK HEARS NOTHING AT ALL, which is every headless
// run and every session with nobody in front of it: the conversation's own
// observer is taken off this call, so the shaper cannot type its housekeeping
// into a room where somebody is reading a reply.
func TestTheShaperIsSilentToAnObserverThatIsNotItsOwn(t *testing.T) {
	client := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		provider.Emit(ctx, provider.StreamDelta, shapedAnswer)
		return textResponse(shapedAnswer), nil
	}}}
	agent, _ := shapeAgent(t, client)
	var heard []string
	ctx := provider.WithStreamObserver(context.Background(), func(event provider.StreamEvent) {
		heard = append(heard, event.Delta)
	})
	agent.shapeBrief(ctx, shapedAsk)
	if len(heard) != 0 {
		t.Fatalf("THE SHAPER TYPED INTO THE ROOM: %q", heard)
	}
}

// AND THE REASONING IS CARRIED TOO, KEPT APART FROM THE ANSWER.
//
// THE GAP THIS CLOSES WAS FOUND AGAINST A REAL ENDPOINT, not here: the shaper
// sits on the careful tier and is allowed to think, and a measured run produced
// 437 stream events across the whole twenty-five-second window without a single
// answer delta among them. A watch given only the answer therefore hears nothing
// at all for exactly the wait it exists for — so both halves go out, and the
// caller decides what to do with each.
func TestTheShaperReportsItsReasoningApartFromItsAnswer(t *testing.T) {
	client := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		provider.Emit(ctx, provider.StreamReasoning, "who is this for")
		provider.Emit(ctx, provider.StreamReasoning, " — people who already use it")
		provider.Emit(ctx, provider.StreamDelta, shapedAnswer)
		return textResponse(shapedAnswer), nil
	}}}
	agent, _ := shapeAgent(t, client)
	type reading struct{ answer, thinking string }
	var seen []reading
	ctx := WithBriefWatch(context.Background(), func(answer, thinking string) {
		seen = append(seen, reading{answer, thinking})
	})
	agent.shapeBrief(ctx, shapedAsk)

	if len(seen) != 3 {
		t.Fatalf("the watch saw %d readings, want one per delta of either kind: %+v", len(seen), seen)
	}
	// WHILE IT IS THINKING THERE IS NO ANSWER, and that is the state the whole
	// thing exists for.
	if seen[0].answer != "" || seen[0].thinking != "who is this for" {
		t.Fatalf("the first reading is not the think alone: %+v", seen[0])
	}
	if seen[1].thinking != "who is this for — people who already use it" {
		t.Fatalf("the reasoning did not accumulate: %q", seen[1].thinking)
	}
	// AND THE TWO ARE NEVER MIXED: the answer arrives on its own side, with the
	// reasoning still readable beside it and not spliced into it.
	if seen[2].answer != shapedAnswer {
		t.Fatalf("the answer is not the answer: %q", seen[2].answer)
	}
	if strings.Contains(seen[2].answer, "who is this for") {
		t.Fatalf("THE REASONING WAS SPLICED INTO THE ANSWER: %q", seen[2].answer)
	}
}

// THE CUT IS SAID, AND ONLY WHEN A SHAPER ACTUALLY RAN (path (a) of issue
// #133). "No shaper configured" is the documented silent pass-through and
// stays silent; but a shaper that was genuinely invoked and came back cut —
// the deadline among the reasons — keeps the person's words AS the brief and
// carries one dim line to the surface: brief kept as you wrote it.
func TestAWindowCutShaperKeepsTheBriefAndSaysSo(t *testing.T) {
	// The deadline's path, without waiting out TaskShapeWindow: what a stall
	// reaches this code as is a context that ended, and a call that ends is a
	// call that failed.
	stalled := func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	client := &scriptedCompleter{steps: []step{stalled}}
	agent, ran := shapeAgent(t, client)

	// A context that has already ended is the shape a window cut arrives in.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	id, title, note, err := agent.StartTask(ctx, shapedAsk)
	if err != nil {
		t.Fatal(err)
	}
	settled(t, ran)

	if note != TaskShapeFallbackNote {
		t.Fatalf("fallback note = %q, want %q", note, TaskShapeFallbackNote)
	}
	// DISPLAY-ONLY: the brief delivered is still the person's own words.
	node := agent.graph().node(id)
	if node == nil {
		t.Fatal("a cut shaper lost the task")
	}
	if node.spec.brief != shapedAsk || node.spec.request != shapedAsk {
		t.Fatalf("brief = %q, request = %q, want the person's sentence untouched",
			node.spec.brief, node.spec.request)
	}
	if title != shapedAsk {
		t.Fatalf("title = %q, want the person's words", title)
	}
	if shapeCalls(client) != 1 {
		t.Fatalf("%d shaping requests, want 1: the cut call was made", shapeCalls(client))
	}
}

// AND THE SILENT PATHS STAY SILENT. No shaper configured is the documented
// pass-through; a shaper that answered whole and merely failed to parse is
// not a cut. Neither one carries the line.
func TestTheFallbackNoteStaysSilentWhenNoShaperRan(t *testing.T) {
	// NO SHAPER CONFIGURED: the pass-through, and the note with it.
	agent, ran := shapeAgentNoShaper(t, &scriptedCompleter{})
	if _, _, note, err := agent.StartTask(t.Context(), shapedAsk); err != nil {
		t.Fatal(err)
	} else if note != "" {
		t.Fatalf("no-shaper note = %q, want silence", note)
	}
	settled(t, ran)

	// A WHOLE ANSWER THAT FAILED TO PARSE: a real response came back, so this
	// is not a cut and the line is not said.
	garbage := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("Sure! Here is a brief for you:"), nil
	}}}
	agent, ran = shapeAgent(t, garbage)
	if _, _, note, err := agent.StartTask(t.Context(), shapedAsk); err != nil {
		t.Fatal(err)
	} else if note != "" {
		t.Fatalf("parse-failure note = %q, want silence", note)
	}
	settled(t, ran)
}

// shapeAgentNoShaper is [shapeAgent] without anything on the careful tier, so
// no model is ever resolved for the shaper and the request passes through
// untouched — the documented absence of the capability.
func shapeAgentNoShaper(t *testing.T, client Completer) (*Agent, <-chan uint64) {
	t.Helper()
	agent, _ := newTestAgent(t, client, nil)
	ran := make(chan uint64, 4)
	stubbedGraph(agent, func(node *TaskNode) {
		node.graph.complete(node, TaskDone)
		ran <- node.id
	})
	return agent, ran
}
