package session

// THE SHELF, PROVED THROUGH THE DOOR THE MODEL ACTUALLY USES.
//
// The claim shelving makes is narrow and easy to break quietly: a tool held back
// is a tool the model can still have, still approved the same way, still charged
// the same way, and still absent where the build never had it. So these tests
// drive a scripted provider loop rather than calling the loader directly — the
// question is not "does armFamily append", which connect.go's tests already
// answer, but "does a turn that loads a group then get to CALL what it loaded,
// with the arguments the provider was told about".

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	configpkg "github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/aforge-v2/internal/video"
)

// blockRecorder is a completer that keeps the TOOL BLOCK of every request
// beside the messages, which [scriptedCompleter] does not: the whole point of
// loading is what the next request's definitions carry, and that rides in the
// options rather than in the transcript.
type blockRecorder struct {
	mu     sync.Mutex
	steps  []step
	blocks [][]ai.ToolDefinition
	sent   [][]ai.Message
	index  int
}

func (b *blockRecorder) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	b.mu.Lock()
	// The narrator arms half a second into every tool batch and is no fixture's
	// business (agent_test.go states this at length); answering it here keeps it
	// off the step counter.
	if isCaptionCall(messages) {
		b.mu.Unlock()
		return textResponse(""), nil
	}
	block := make([]ai.ToolDefinition, len(request.Tools))
	copy(block, request.Tools)
	b.blocks = append(b.blocks, block)
	// AND THE TRANSCRIPT BESIDE IT, because a loaded tool that never runs is
	// half the road: the request after the call is where its real result is.
	seen := make([]ai.Message, len(messages))
	copy(seen, messages)
	b.sent = append(b.sent, seen)
	var next step
	if b.index < len(b.steps) {
		next = b.steps[b.index]
	}
	b.index++
	b.mu.Unlock()
	if next == nil {
		return textResponse("done"), nil
	}
	return next(ctx, messages)
}

// blockAt is the tool block of one request, by the names in it and by the whole
// definition, so a test can ask both "was it there" and "what was it told".
func (b *blockRecorder) blockAt(t *testing.T, index int) map[string]ai.ToolDefinition {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if index >= len(b.blocks) {
		t.Fatalf("the loop made %d requests, so there is no request %d to read", len(b.blocks), index)
	}
	block := make(map[string]ai.ToolDefinition, len(b.blocks[index]))
	for _, definition := range b.blocks[index] {
		block[definition.Function.Name] = definition
	}
	return block
}

// resultAt is the text of the tool result answering one call id, in the
// transcript of one request.
func (b *blockRecorder) resultAt(t *testing.T, index int, callID string) string {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if index >= len(b.sent) {
		t.Fatalf("the loop made %d requests, so there is no request %d to read", len(b.sent), index)
	}
	for _, message := range b.sent[index] {
		if message.ToolCallID == callID {
			return messageText(message)
		}
	}
	t.Fatalf("request %d carries no result for call %s", index, callID)
	return ""
}

func (b *blockRecorder) requests() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.blocks)
}

// shelfAgent is the shipping conversation's shape, which is the one that has
// something on every shelf.
func shelfAgent(t *testing.T, completer Completer, mutate func(*Config)) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.BashBackgroundAfterSeconds = configpkg.DefaultBashBackgroundAfter
		config.HarnessStore = subharness.At(t.TempDir())
		config.RunHarness = func(context.Context, string, string, string, func(subharness.Trail)) (string, subharness.Usage, error) {
			return "", subharness.Usage{}, nil
		}
		config.OrchestrateRunner = func(context.Context, string, string, float64) (string, error) { return "", nil }
		if mutate != nil {
			mutate(config)
		}
	})
	return agent
}

func shelfBeltNames(agent *Agent) []string {
	tools := agent.beltTools()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func drain(t *testing.T, events <-chan Event) {
	t.Helper()
	for range events {
	}
}

// ── the whole road, in one turn ─────────────────────────────────────────────

// THE ACCEPTANCE. A model that wants a shelved verb loads its group and then
// calls it, and the second request is where the schema it needs to call it with
// arrives.
func TestATurnLoadsAGroupAndThenCallsTheToolItLoaded(t *testing.T) {
	recorder := &blockRecorder{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", loadCapabilityToolName, `{"group":"settings"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c2", "settings", `{"search":"budget"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("here is what that row says"), nil
		},
	}}
	agent := shelfAgent(t, recorder, func(config *Config) {
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		// The catalog assertion below exercises every group. Supply media
		// explicitly so the fixture does not depend on a local FFmpeg install.
		config.Media = &scriptedMedia{}
		config.MediaModel = allMediaModels()
	})

	// THE FIRST REQUEST DOES NOT CARRY THE PAIR, which is the saving. It carries
	// the one verb that can fetch them.
	events, err := agent.Submit(context.Background(), "what is my daily budget set to?")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	drain(t, events)

	if recorder.requests() < 3 {
		t.Fatalf("the loop made %d requests, so it never got past loading", recorder.requests())
	}
	first := recorder.blockAt(t, 0)
	if _, carried := first["settings"]; carried {
		t.Error("the first request carried `settings`: nothing was shelved and nothing was saved")
	}
	loader, offered := first[loadCapabilityToolName]
	if !offered {
		t.Fatal("the first request carried no load_capability, so a shelved verb is unreachable")
	}
	// THE CATALOG IS IN THE DESCRIPTION AND THE ENUM, so a model can load on the
	// turn it first needs something rather than spending a round trip listing.
	for _, group := range []string{"media", "settings", "harnesses"} {
		if !strings.Contains(loader.Function.Description, group) {
			t.Errorf("load_capability's description never names the %s group, so the model cannot know to ask for it", group)
		}
	}
	// AND EVERY GROUP LINE IS ITS OWN SURVIVING TOOL NAMES, which is both the
	// only truthful way to describe a group whose membership depends on the
	// machine, and how a model that meets an unfamiliar name in its own history
	// finds the group it is in.
	for _, name := range agent.shelvedNames() {
		if !strings.Contains(loader.Function.Description, name) {
			t.Errorf("the catalog never names the shelved `%s`, so nothing says which group fetches it:\n%s",
				name, loader.Function.Description)
		}
	}
	for _, absent := range []string{"generate_image", "generate_music", "generate_video"} {
		if agent.offers(absent) {
			continue
		}
		if strings.Contains(loader.Function.Description, absent) {
			t.Errorf("the catalog names `%s`, which this build does not have", absent)
		}
	}

	// AND THE SECOND REQUEST CARRIES THE PAIR IN FULL, as provider-native
	// definitions — which is the answer to why the tool result need not echo a
	// schema at all.
	second := recorder.blockAt(t, 1)
	for _, want := range []string{"settings", "change_setting"} {
		definition, arrived := second[want]
		if !arrived {
			t.Fatalf("the request after loading does not carry `%s`", want)
		}
		if strings.TrimSpace(definition.Function.Description) == "" {
			t.Errorf("`%s` arrived with no description", want)
		}
		if definition.Function.Parameters == nil {
			t.Errorf("`%s` arrived with no parameters, so the model is told it takes no arguments", want)
		}
	}
	if properties, ok := second["settings"].Function.Parameters["properties"].(map[string]interface{}); !ok {
		t.Error("`settings` arrived without an object schema")
	} else if _, hasSearch := properties["search"]; !hasSearch {
		t.Error("`settings` arrived without its own `search` field, so this is not the tool tools.go built")
	}
	if !agent.hasTool("settings") {
		t.Fatal("the belt does not carry `settings` after a load that reported success")
	}

	// AND THE LOADED TOOL ACTUALLY RAN. This is the half a schema assertion
	// cannot reach: the third request carries the settings sheet the real tool
	// read off disk, dispatched by the ordinary machinery, and not a refusal.
	loaded := recorder.resultAt(t, 1, "c1")
	if !strings.HasPrefix(loaded, "Loaded: ") {
		t.Errorf("the loading call was answered %q", loaded)
	}
	answer := recorder.resultAt(t, 2, "c2")
	for _, refusal := range []string{"Unknown tool", "Invalid arguments", "could not be loaded", "no group called"} {
		if strings.Contains(answer, refusal) {
			t.Fatalf("the loaded `settings` call was answered with an error: %q", answer)
		}
	}
	if !strings.Contains(answer, `settings mentioning "budget"`) || !strings.Contains(answer, configpkg.KeyDailyBudget) {
		t.Errorf("the third request does not carry the real settings sheet, so nothing proves the loaded tool ran:\n%s", answer)
	}
}

// ── absence survives ────────────────────────────────────────────────────────

// A GROUP IS NOT A PROMISE. Where the build never had the tools, the word for
// them does not exist either — which is absent-not-broken read from the new
// direction shelving opened.
func TestAGroupWhoseToolsThisBuildLacksDoesNotExist(t *testing.T) {
	// No media seams at all, so [Agent.imageTools] and its four siblings build
	// nothing. edit_video needs only ffmpeg, so the group survives on a machine
	// that has it — the assertion is therefore about the four, and about what
	// the loader says when the whole group is gone.
	agent := shelfAgent(t, &scriptedCompleter{}, nil)
	for _, verb := range []string{"generate_image", "speak", "generate_music", "generate_video"} {
		if agent.offers(verb) {
			t.Fatalf("a build with no media seams offers %s", verb)
		}
	}
	said, failed := agent.loadCapability("media")
	if video.Available() {
		if failed {
			t.Errorf("loading the surviving media group reported a failure: %q", said)
		}
		if !strings.Contains(said, "edit_video") {
			t.Errorf("a machine with ffmpeg loaded the media group and got %q, which never names edit_video", said)
		}
		for _, gone := range []string{"generate_image", "generate_video"} {
			if strings.Contains(said, gone) {
				t.Errorf("loading media on a machine with no media models named %s: %q", gone, said)
			}
		}
		return
	}
	if strings.Contains(said, "Loaded") {
		t.Errorf("a machine with no media at all loaded a media group: %q", said)
	}
	if !failed {
		t.Errorf("loading a group this build has none of reported success: %q", said)
	}
	if !strings.Contains(said, "no group called") {
		t.Errorf("the refusal for a group this build has none of reads %q", said)
	}
	// AND A MISTYPED GROUP IS ANSWERED WITH THE REAL ONES rather than a bare no.
	mistyped, mistypedFailed := agent.loadCapability("mediaa")
	if !mistypedFailed {
		t.Errorf("a mistyped group reported success: %q", mistyped)
	}
	if !strings.Contains(mistyped, "settings") {
		t.Error("a mistyped group is not answered with the groups this build does have")
	}
}

// AND A BUILD WITH NOTHING TO SHELVE CARRIES NO LOADING VERB AT ALL, which is
// the same law applied to the verb itself: a model handed a loader over an empty
// cupboard would spend a call finding out there is nothing in it.
// It is asserted twice because the machine decides which half runs: `edit_video`
// needs only ffmpeg, so on a developer's laptop the media group survives every
// other seam being absent. The first half is the law over a belt with nothing
// shelvable on it, which is true everywhere; the second is a real shape, where
// there is one.
func TestABeltWithNothingShelvableCarriesNoLoadingVerb(t *testing.T) {
	agent := shelfAgent(t, &scriptedCompleter{}, nil)
	plain := []bare.Tool{{Name: "read"}, {Name: "bash"}, {Name: "manual"}}
	carried := agent.shelveDeferred(plain)
	if len(carried) != len(plain) {
		t.Fatalf("shelving a belt with nothing shelvable on it returned %d of %d tools", len(carried), len(plain))
	}
	for index, tool := range plain {
		if carried[index].Name != tool.Name {
			t.Fatalf("position %d was %s and is now %s", index, tool.Name, carried[index].Name)
		}
	}
	if len(agent.shelvedNames()) != 0 {
		t.Fatalf("a belt with nothing shelvable shelved %v", agent.shelvedNames())
	}
}

func TestABuildWithNothingShelvedHasNoLoadingVerb(t *testing.T) {
	if video.Available() {
		t.Skip("ffmpeg is on PATH, so edit_video keeps the media group alive on this machine")
	}
	// A task node on the floor of the tree: no settings pair, no harness
	// machines, no media.
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.InTask = true
		config.taskID = 7
		config.taskDepth = taskDepthLimit
	})
	if agent.hasTool(loadCapabilityToolName) {
		t.Fatalf("a belt with an empty shelf carries a loading verb: %v", shelfBeltNames(agent))
	}
	if len(agent.shelvedNames()) != 0 {
		t.Fatalf("that shape shelved %v", agent.shelvedNames())
	}
}

// ── the append law ──────────────────────────────────────────────────────────

// LOADING TWICE COSTS ONE CHEAP LINE AND MOVES NOTHING. The definition block
// rides in front of the whole transcript, so a definition that shifts re-bills
// every token behind it (connect.go's append law, which this door is bound by
// because it is the same door).
func TestLoadingAGroupTwiceAddsNothingAndReordersNothing(t *testing.T) {
	agent := shelfAgent(t, &scriptedCompleter{}, nil)
	before := shelfBeltNames(agent)

	first, failed := agent.loadCapability("harnesses")
	if failed {
		t.Fatalf("the first load reported a failure: %q", first)
	}
	if !strings.Contains(first, "Loaded") {
		t.Fatalf("the first load reported %q", first)
	}
	once := shelfBeltNames(agent)
	if len(once) != len(before)+2 {
		t.Fatalf("loading harnesses took the belt from %d tools to %d, want %d", len(before), len(once), len(before)+2)
	}
	// APPENDED AT THE TAIL, with everything that was there already exactly where
	// it was.
	for index, name := range before {
		if once[index] != name {
			t.Fatalf("loading moved the belt: position %d was %s and is now %s", index, name, once[index])
		}
	}

	second, secondFailed := agent.loadCapability("harnesses")
	if secondFailed {
		t.Errorf("loading a group twice is idempotent, not a failure: %q", second)
	}
	if !strings.Contains(second, "Already loaded") {
		t.Errorf("the second load reported %q, want the cheap already-loaded line", second)
	}
	twice := shelfBeltNames(agent)
	if len(twice) != len(once) {
		t.Fatalf("the second load changed the belt from %d tools to %d", len(once), len(twice))
	}
	for index, name := range once {
		if twice[index] != name {
			t.Fatalf("the second load moved the belt: position %d was %s and is now %s", index, name, twice[index])
		}
	}
	// AND NOTHING IS ON IT TWICE.
	seen := map[string]bool{}
	for _, name := range twice {
		if seen[name] {
			t.Fatalf("%s is on the belt twice", name)
		}
		seen[name] = true
	}
}

// TWO GOROUTINES LOADING THE SAME GROUP ARM IT ONCE. A model may batch two calls
// in one round and the loop runs a batch concurrently, so this is a real
// ordering and not a hypothetical one. Run under -race.
func TestConcurrentLoadsOfOneGroupArmItOnce(t *testing.T) {
	agent := shelfAgent(t, &scriptedCompleter{}, nil)
	before := len(shelfBeltNames(agent))

	const racers = 8
	var wait sync.WaitGroup
	said := make([]string, racers)
	start := make(chan struct{})
	for index := 0; index < racers; index++ {
		wait.Add(1)
		go func(slot int) {
			defer wait.Done()
			<-start
			said[slot], _ = agent.loadCapability("harnesses")
		}(index)
	}
	close(start)
	wait.Wait()

	after := shelfBeltNames(agent)
	if len(after) != before+2 {
		t.Fatalf("%d concurrent loads left %d tools on a belt of %d, want %d", racers, len(after), before, before+2)
	}
	seen := map[string]bool{}
	for _, name := range after {
		if seen[name] {
			t.Fatalf("%s is on the belt twice after concurrent loads", name)
		}
		seen[name] = true
	}
	// EXACTLY ONE CALLER IS TOLD IT LOADED THEM. The rest read the already-loaded
	// line, which is the answer that keeps a model from calling a third time.
	loaded := 0
	for _, line := range said {
		if strings.HasPrefix(line, "Loaded: ") {
			loaded++
		}
	}
	if loaded != 1 {
		t.Fatalf("%d of %d concurrent loads reported arming the group, want exactly 1", loaded, racers)
	}
}

// ── loading is not permission ───────────────────────────────────────────────

// A TOOL THE PERSON'S RULES REFUSE IS REFUSED AFTER LOADING EXACTLY AS BEFORE.
// This is the one thing a shelf could quietly break — a loading door that armed
// tools past the gate would be an approval bypass wearing a saving's clothes —
// so it is asserted through [Agent.executeTool], the door every call goes
// through, and not through the policy in isolation.
func TestLoadingAGroupDoesNotLoosenTheGateOverIt(t *testing.T) {
	agent := shelfAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ApprovalPolicy = &approval.Policy{
			Default: approval.ActionAllow,
			Tools:   map[string]approval.Action{"change_setting": approval.ActionDeny},
		}
	})
	if said, failed := agent.loadCapability("settings"); failed {
		t.Fatalf("the settings group is not on this shape's shelf: %q", said)
	}
	if !agent.hasTool("change_setting") {
		t.Fatal("change_setting did not arrive")
	}

	decision, governed := agent.decide(gateCall("change_setting", `{"key":"daily_budget_usd","value":"5"}`))
	if !governed || decision.Action != approval.ActionDeny {
		t.Fatalf("after loading, the gate over change_setting answers %s (governed=%v), want deny", decision, governed)
	}
	result := agent.executeTool(context.Background(), agent.newEpisode(), nil,
		gateCall("change_setting", `{"key":"daily_budget_usd","value":"5"}`), "")
	if !result.isError {
		t.Fatalf("a denied tool succeeded after being loaded: %q", result.text)
	}
	// AND THE ALLOWED HALF OF THE SAME GROUP IS STILL ALLOWED: the gate keys on
	// the tool NAME and loading a group is not one answer for two acts
	// (tools_settings.go states why the pair is two tools).
	if decision, _ := agent.decide(gateCall("settings", `{}`)); decision.Action != approval.ActionAllow {
		t.Fatalf("loading the group changed the answer over `settings` to %s", decision)
	}
}

// ── a narrowed belt inherits no cupboard ────────────────────────────────────

// A HAND HAS THE SHELF ITS BELT HAS, WHICH IS NONE. `forkBelt` is an allowlist
// and never names the loading verb, so the shelf is unreachable in any case —
// this asserts the cupboard is empty as well, because a narrowing meant to be
// total that left one standing would be a capability surviving by accident.
func TestAHandInheritsNeitherTheLoadingVerbNorTheShelf(t *testing.T) {
	caller, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.System = "" })
	seed, page := caller.forkSeed()
	hand, err := caller.newHandAgent(forkPart{Role: "one", Scope: []string{"a"}}, seed, page, &handLeash{limit: forkRounds})
	if err != nil {
		t.Fatalf("newHandAgent: %v", err)
	}
	t.Cleanup(func() { _ = hand.Close() })

	if hand.hasTool(loadCapabilityToolName) {
		t.Fatalf("a hand carries the loading verb: %v", shelfBeltNames(hand))
	}
	if shelved := hand.shelvedNames(); len(shelved) != 0 {
		t.Fatalf("a hand inherited a shelf holding %v", shelved)
	}
	for _, never := range []string{"settings", "change_setting", "build_harness", "generate_image"} {
		if hand.offers(never) {
			t.Fatalf("a hand offers %s, which its allowlist never named", never)
		}
	}
	// AND THE CALLER STILL HAS ITS OWN. Clearing the hand's shelf must not reach
	// through to the mind that forked it.
	if len(caller.shelvedNames()) == 0 {
		t.Fatal("forking emptied the caller's own shelf")
	}
}

// ── a load that cannot happen is a failure ──────────────────────────────────

// A CALL THAT LOADED NOTHING IS ANSWERED AS AN ERROR, THROUGH THE REAL DOOR.
// A refusal dressed as a success is the worst answer this verb can give: the
// model reads "here you are", reaches for a tool that never arrived, and spends
// the round trip the whole design was bought to save. So these go through
// [Agent.executeTool] — the door every call is dispatched by — and read the flag
// the loop actually acts on.
func TestALoadThatFetchesNothingIsAToolError(t *testing.T) {
	agent := shelfAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	})
	before := shelfBeltNames(agent)

	for _, probe := range []struct{ name, args, says string }{
		{"a group this build has no word for", `{"group":"nope"}`, "no group called"},
		// THE ENUM IS THE SPELLING. A near miss is told about, never guessed at.
		{"a group spelled nearly right", `{"group":"Settings"}`, "no group called"},
		{"arguments that are not JSON", `{"group":`, "not valid JSON"},
	} {
		result := agent.executeTool(context.Background(), agent.newEpisode(), nil,
			gateCall(loadCapabilityToolName, probe.args), "")
		if !result.isError {
			t.Errorf("%s was reported as a success: %q", probe.name, result.text)
		}
		if !strings.Contains(result.text, probe.says) {
			t.Errorf("%s was answered %q, which never says %q", probe.name, result.text, probe.says)
		}
	}

	// AND NOTHING WAS ARMED BY ANY OF THEM.
	after := shelfBeltNames(agent)
	if len(after) != len(before) {
		t.Fatalf("failed loads took the belt from %d tools to %d", len(before), len(after))
	}
}

// AND AN ARMING THAT FAILS IS A FAILURE TOO, rather than a line claiming tools
// that are not there. [Agent.armFamily] refuses a tool whose schema will not
// parse (tools.go's toolDefinitions), which is the one way the append can fail.
func TestAnArmingThatFailsIsReportedAsAFailure(t *testing.T) {
	agent := shelfAgent(t, &scriptedCompleter{}, nil)
	agent.armMu.Lock()
	agent.shelf["broken"] = []bare.Tool{{Name: "broken_tool", Schema: json.RawMessage("{oops")}}
	agent.shelfOrder = append(agent.shelfOrder, "broken")
	agent.armMu.Unlock()

	said, failed := agent.loadCapability("broken")
	if !failed {
		t.Errorf("an arming that could not happen reported success: %q", said)
	}
	if !strings.Contains(said, "could not be loaded") {
		t.Errorf("the failure reads %q", said)
	}
	if agent.hasTool("broken_tool") {
		t.Error("a tool that could not be armed is on the belt anyway")
	}
}

// ── what this build offers, counted once ────────────────────────────────────

// A LOADED TOOL IS ON THE BELT AND STILL ON THE CONSTRUCTION-TIME SHELF, so the
// two halves must be joined once and not concatenated. What [Agent.offeredTools]
// answers is whether this build HAS a verb; a name appearing twice would make
// every gate that counts what it walks read a duplicate as a second capability.
func TestWhatThisBuildOffersHoldsEveryToolExactlyOnce(t *testing.T) {
	agent := shelfAgent(t, &scriptedCompleter{}, nil)
	if said, failed := agent.loadCapability("harnesses"); failed {
		t.Fatalf("loading the harnesses group reported %q", said)
	}

	seen := map[string]int{}
	for _, tool := range agent.offeredTools() {
		seen[tool.Name]++
	}
	for name, count := range seen {
		if count != 1 {
			t.Errorf("`%s` is offered %d times, and offering is a question about capability rather than a count", name, count)
		}
	}
	// AND THE MEANING IS UNCHANGED: everything carried and everything still
	// waiting is offered, whether it has been loaded or not.
	for _, name := range []string{"read", "build_harness", "settings"} {
		if !agent.offers(name) {
			t.Errorf("this build no longer offers `%s`", name)
		}
	}
}

// ── a shape that carries its tools is never told to fetch them ──────────────

// NOTHING IS SHELVED IN A TASK, so nothing in a task's page may name the verb
// that fetches a shelf. This is the forward law of beltfacts.go read from the
// side shelving opened: a worker told to call `load_capability` — which is not
// on its belt at all — is the `Unknown tool` defect written the other way round.
func TestAShapeThatCarriesItsToolsIsNeverToldToLoadThem(t *testing.T) {
	for _, shape := range beltShapes {
		if agentConfigFor(t, shape).shelvesCapabilities() {
			continue
		}
		minted := beltShapeAgent(t, shape)
		if len(minted.shelved) != 0 {
			t.Errorf("%s: shelved %v though it carries its tools directly", shape.name, beltNameSet(minted.shelved))
		}
		for _, tool := range minted.belt {
			if tool.Name == loadCapabilityToolName {
				t.Errorf("%s: carries `%s` though it shelves nothing", shape.name, loadCapabilityToolName)
			}
		}
		// A hand opens on its caller's page word for word, and the caller is a
		// conversation that does shelve — so what this shape answers for is the
		// tail fork.go appends (prompt_belt_test.go states the same law).
		page := strings.TrimPrefix(minted.page, minted.inherited)
		if strings.Contains(page, loadCapabilityToolName) {
			t.Errorf("%s: its page names `%s`, which is not on its belt", shape.name, loadCapabilityToolName)
		}
	}
}

// ── the page tells the truth ────────────────────────────────────────────────

// A CONVERSATION IS NEVER TOLD IT HAS A VERB IT IS NOT CARRYING AND CANNOT
// FETCH. prompt_belt_test.go holds this over every shape and both directions;
// this is the same law asserted over the page a live conversation is actually
// holding, which is what [Agent.refreshSystemLocked] rebuilds on every turn.
// The reopen road is a different question and is [TestAReopenedConversationHoldsTheGroupsItAlreadyLoaded].
func TestAConversationIsPromisedNoVerbItCannotReach(t *testing.T) {
	agent := shelfAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.refreshSystemLocked()
	page := agent.system
	agent.mu.Unlock()

	carried := map[string]bool{}
	for _, name := range shelfBeltNames(agent) {
		carried[name] = true
	}
	for _, name := range agent.shelvedNames() {
		if !strings.Contains(page, "`"+name+"`") {
			continue
		}
		if !strings.Contains(page, "`"+loadCapabilityToolName+"`") {
			t.Errorf("the page names the shelved `%s` and never names the verb that fetches it", name)
		}
		if group := capabilityGroupOf(name); group == "" || !strings.Contains(page, "`"+group+"`") {
			t.Errorf("the page names the shelved `%s` without naming its `%s` group", name, group)
		}
	}
	// AND THE VERB IS NOT PROMISED WHERE IT IS NOT CARRIED.
	if strings.Contains(page, "`"+loadCapabilityToolName+"`") && !carried[loadCapabilityToolName] {
		t.Error("the page names load_capability and the belt does not carry it")
	}
}

// ── reopening ───────────────────────────────────────────────────────────────

// A reopened conversation restores groups named by load calls still in its
// saved transcript.
//
// A new process rebuilds the belt from scratch, so the groups go back on the
// shelf — while the transcript it just replayed still says "Loaded:
// build_harness, list_harnesses". Without the re-arm the model would reach for
// what its own history says it holds and be answered `Unknown tool`, so this
// drives a real turn through the provider loop, closes the session, and opens
// the SAME journal again through the ordinary door.
func TestAReopenedConversationHoldsTheGroupsItAlreadyLoaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	recorder := &blockRecorder{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", loadCapabilityToolName, `{"group":"harnesses"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the recipe is saved"), nil
		},
	}}
	agent := shelfAgent(t, recorder, func(config *Config) {
		config.SessionFile = path
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	})
	saved := agent.config

	events, err := agent.Submit(context.Background(), "save this shape of work as a recipe")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	drain(t, events)
	if !agent.hasTool("build_harness") {
		t.Fatal("the turn never loaded the harnesses group, so there is nothing to reopen onto")
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := newAgent(saved, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	for _, want := range []string{"build_harness", "list_harnesses"} {
		if !reopened.hasTool(want) {
			t.Errorf("a reopened conversation whose own transcript says it loaded `%s` does not carry it: %v",
				want, shelfBeltNames(reopened))
		}
	}
	// AND ONLY WHAT IT LOADED. A reopen must not arm a group nobody asked for.
	if reopened.hasTool("settings") {
		t.Error("reopening armed the settings group, which this conversation never loaded")
	}
	// AND ONCE. The re-arm goes through the same append law as the live door.
	seen := map[string]bool{}
	for _, name := range shelfBeltNames(reopened) {
		if seen[name] {
			t.Fatalf("%s is on the reopened belt twice", name)
		}
		seen[name] = true
	}
	if !reopened.hasTool(loadCapabilityToolName) {
		t.Error("the reopened belt lost the loading verb, so the groups it has not loaded are unreachable")
	}
}

// ── the accounts door and this one grow the same belt ───────────────────────

// TWO DOORS APPEND TO ONE BELT AND NEITHER MOVES THE OTHER'S TOOLS. A connected
// account arms a family through [Agent.armFamily]; so does a loaded group. They
// are the same door and this is the proof that using both leaves one belt in the
// order the two arrivals happened.
func TestAServedFamilyAndALoadedGroupBothAppendWithoutMovingAnything(t *testing.T) {
	agent := shelfAgent(t, &scriptedCompleter{}, nil)
	before := shelfBeltNames(agent)

	arriving := []bare.Tool{{
		Name:        "example_request",
		Description: "one account's own tool",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			return "", false, nil
		},
	}}
	if _, err := agent.armFamily(arriving); err != nil {
		t.Fatalf("armFamily: %v", err)
	}
	if said, failed := agent.loadCapability("harnesses"); failed {
		t.Fatalf("loading after an account arrived reported %q", said)
	}

	after := shelfBeltNames(agent)
	if len(after) != len(before)+3 {
		t.Fatalf("the belt went from %d to %d tools, want %d", len(before), len(after), len(before)+3)
	}
	for index, name := range before {
		if after[index] != name {
			t.Fatalf("position %d was %s and is now %s", index, name, after[index])
		}
	}
	if after[len(before)] != "example_request" {
		t.Fatalf("the account's tool is at position %d rather than first of the arrivals", indexOfName(after, "example_request"))
	}
	// And the loaded group is still callable: arming twice through two doors
	// leaves one belt, not two halves.
	if !agent.hasTool("build_harness") || !agent.hasTool("list_harnesses") {
		t.Fatal("the loaded group is not on the belt the account's family also landed on")
	}
}

func indexOfName(names []string, want string) int {
	for index, name := range names {
		if name == want {
			return index
		}
	}
	return -1
}

// ── the table itself ────────────────────────────────────────────────────────

// THE TABLE IS NAMES AND NOTHING ELSE, and it must not claim a name twice or
// claim one no belt in this package builds — the first would make which group
// owns a tool depend on table order, and the second is a word in the model's
// catalog standing for nothing.
func TestTheCapabilityTableIsWellFormed(t *testing.T) {
	owner := map[string]string{}
	for _, group := range capabilityGroups {
		if strings.TrimSpace(group.name) == "" {
			t.Errorf("a capability group has no name: %+v", group)
		}
		if len(group.members) == 0 {
			t.Errorf("the %s group names no tools", group.name)
		}
		for _, member := range group.members {
			if had, taken := owner[member]; taken {
				t.Errorf("%s is claimed by both %s and %s, so which group owns it depends on table order", member, had, group.name)
			}
			owner[member] = group.name
		}
	}

	// EVERY NAME IS A TOOL SOME BELT IN THIS PACKAGE BUILDS. The wide shape has
	// every seam a conversation can be handed, so a name it does not produce is a
	// name nothing produces.
	built := map[string]bool{}
	wide := shelfAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Media = &scriptedMedia{}
		config.MediaModel = allMediaModels()
		config.Memory = openTestBrain(t)
		config.Standing = &Standing{}
		config.standingItems = &fakeStanding{}
		config.Subharnesses = registryWith(t, &fakeGeneralist{}, &fakeRunner{manifest: theProgram()})
		config.HarnessCards = true
	})
	for _, tool := range wide.offeredTools() {
		built[tool.Name] = true
	}
	for member, group := range owner {
		if member == "edit_video" && !video.Available() {
			continue
		}
		if !built[member] {
			t.Errorf("the %s group claims %s, which no belt in this package builds", group, member)
		}
	}
	// AND THE LOADING VERB IS NOT ITSELF SHELVEABLE.
	if _, claimed := owner[loadCapabilityToolName]; claimed {
		t.Error("the loading verb is claimed by a group, so loading it would need loading it")
	}
}

// THE EVERYDAY BELT IS UNTOUCHED. The saving is only worth having if the tools a
// conversation reaches for on the turn it needs them are still in front of it,
// so this pins the list that must never move onto a shelf.
func TestTheEverydayVerbsAreStillCarried(t *testing.T) {
	agent := shelfAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Media = &scriptedMedia{}
		config.MediaModel = allMediaModels()
		config.Memory = openTestBrain(t)
	})
	for _, everyday := range []string{
		"read", "write", "edit", "bash", "grep", "find", "ls",
		"read_document", "jobs", "watch", "manual",
		"propose_task", "tasks", "fork", "track", "commit", "recall",
		"remember", "search_conversations", "view_image",
	} {
		if !agent.hasTool(everyday) {
			t.Errorf("%s is not carried: an everyday verb costs a round trip to discover", everyday)
		}
	}
}

// AND THE SAVING IS REAL, measured the way prefixbudget_test.go measures.
// A grouping that did not pay for itself would be a round trip bought for
// nothing, which is the one outcome that makes this whole design not worth
// having.
func TestShelvingTakesMoreOffTheToolBlockThanItPutsOn(t *testing.T) {
	agent := shelfAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Media = &scriptedMedia{}
		config.MediaModel = allMediaModels()
		config.Memory = openTestBrain(t)
	})
	carried, err := json.Marshal(agent.beltDefinitions())
	if err != nil {
		t.Fatal(err)
	}
	shelved, err := toolDefinitions(agent.shelvedTools())
	if err != nil {
		t.Fatal(err)
	}
	held, err := json.Marshal(shelved)
	if err != nil {
		t.Fatal(err)
	}
	loader := 0
	for _, definition := range agent.beltDefinitions() {
		if definition.Function.Name != loadCapabilityToolName {
			continue
		}
		encoded, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		loader = len(encoded)
	}
	if loader == 0 {
		t.Fatal("this shape shelved nothing, so there is no saving to weigh")
	}
	// Encode the complete comparison block, including its actual array framing.
	// Adding two separately encoded array lengths overcounts the saving.
	complete := make([]ai.ToolDefinition, 0, len(agent.beltDefinitions())+len(shelved))
	for _, definition := range agent.beltDefinitions() {
		if definition.Function.Name != loadCapabilityToolName {
			complete = append(complete, definition)
		}
	}
	complete = append(complete, shelved...)
	before, err := json.Marshal(complete)
	if err != nil {
		t.Fatal(err)
	}
	saved := len(before) - len(carried)
	t.Logf("the full tool block is %d bytes; discovery carries %d, saving %d after its %d-byte loading verb",
		len(before), len(carried), saved, loader)
	// A FLOOR AND NOT THE MEASUREMENT. What is pinned is that the trade is worth
	// making by a wide margin; the exact figure is in the change entry, and a
	// group that stopped paying its way would fall through this.
	if saved < 4*loader {
		t.Fatalf("shelving holds back %d bytes and spends %d to do it, which is not a trade worth a round trip", len(held), loader)
	}
}
