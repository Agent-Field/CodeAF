package session

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/connect"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// armNotion puts the served account's tools on the belt, the way a conversation
// that picked it up is already holding them.
func armNotion(t *testing.T, agent *Agent) string {
	t.Helper()
	reply := agent.armServed(context.Background(), notionStatus(true), "Notion is connected", "")
	if !strings.Contains(reply, "notion_") {
		t.Fatalf("nothing was armed: %q", reply)
	}
	return reply
}

// servedTurn is the script for one turn that calls one armed tool and then says
// something.
func servedTurn(tool, args string) []step {
	return []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", tool, args), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}
}

// manyServed is an account with more tools than a conversation carries.
func manyServed(count int) []connect.MCPTool {
	tools := make([]connect.MCPTool, 0, count)
	for index := 1; index <= count; index++ {
		tools = append(tools, connect.MCPTool{
			Name:        "tool-" + strconv.Itoa(index),
			Description: "Does the " + strconv.Itoa(index) + "th thing.",
			Schema:      json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			ReadOnly:    true,
		})
	}
	return tools
}

// ── the arming ──────────────────────────────────────────────────────────────

// AN ACCOUNT NOBODY WROTE TOOLS FOR IS ASKED WHAT IT BRINGS, and what it says
// becomes the belt: its own names folded into names a belt can carry, its own
// sentences, its own argument shapes.
func TestAServedAccountArmsWhatItSays(t *testing.T) {
	hub := servedHub(servedList())
	completer := &scriptedCompleter{steps: useServiceTurn("notion")}
	agent := connectAgent(t, completer, hub, true)

	collected := collect(t, mustSubmit(t, agent, "look in notion"))
	output := lastToolOutput(t, collected)

	for _, want := range []string{"notion_search_pages", "notion_create_page"} {
		if !strings.Contains(output, want) {
			t.Errorf("%s is missing from the arming reply: %q", want, output)
		}
		if !hasTool(agent, want) {
			t.Errorf("%s never reached the belt: %v", want, beltNames(agent))
		}
	}

	// The account's own sentence stands, and what is added is what the account
	// cannot know: that an answer arriving here is bounded, and that a person
	// stands between a call that changes something and the far end.
	for _, tool := range agent.beltTools() {
		switch tool.Name {
		case "notion_search_pages":
			if !strings.HasPrefix(tool.Description, "Search the person's workspace") {
				t.Errorf("the account's own sentence was rewritten: %q", tool.Description)
			}
			if !strings.Contains(tool.Description, "Long answers are shortened") {
				t.Errorf("nothing says the answer is bounded: %q", tool.Description)
			}
			if strings.Contains(tool.Description, "asked before") {
				t.Errorf("a read promises a question nobody will be asked: %q", tool.Description)
			}
		case "notion_create_page":
			if !strings.Contains(tool.Description, "the person is asked before it goes") {
				t.Errorf("a call that acts does not say so: %q", tool.Description)
			}
			// The account's schema is passed through, not rewritten.
			if !strings.Contains(string(tool.Schema), `"title"`) {
				t.Errorf("the account's own argument shape was lost: %q", tool.Schema)
			}
		}
	}
}

// A LIST NOBODY HERE WROTE MUST NEVER FAIL THE WHOLE ARMING. One tool whose
// argument shape does not parse is one tool left off — named in the reply, so
// the model is not left planning around a hand it does not have — and the rest
// arrive.
func TestAServedToolWithAnUnreadableShapeIsLeftOffAndSaidSo(t *testing.T) {
	served := append(servedList(), connect.MCPTool{
		Name:        "broken-thing",
		Description: "Does something.",
		Schema:      json.RawMessage(`{"type":"object",`),
	})
	hub := servedHub(served)
	agent := connectAgent(t, &scriptedCompleter{}, hub, true)

	reply := armNotion(t, agent)

	if !strings.Contains(reply, "broken-thing") {
		t.Errorf("a tool was dropped without a word: %q", reply)
	}
	if !strings.Contains(reply, "cannot read") {
		t.Errorf("the reply does not say why it was left off: %q", reply)
	}
	if hasTool(agent, "notion_broken_thing") {
		t.Error("a tool with an unreadable shape reached the belt")
	}
	for _, want := range []string{"notion_search_pages", "notion_create_page"} {
		if !hasTool(agent, want) {
			t.Errorf("one broken tool took %s down with it: %v", want, beltNames(agent))
		}
	}
}

// TWO NAMES THAT FOLD TO ONE ARE ONE NAME HERE. The first stands, the second is
// named in the reply, and the model is never handed two tools it cannot tell
// apart.
func TestTwoServedNamesThatFoldToOneArmOnce(t *testing.T) {
	served := append(servedList(), connect.MCPTool{
		Name:        "Create Page",
		Description: "The same thing, spelled differently.",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
	})
	hub := servedHub(served)
	agent := connectAgent(t, &scriptedCompleter{}, hub, true)

	reply := armNotion(t, agent)

	if !strings.Contains(reply, "Create Page") {
		t.Errorf("the doubled name was dropped without a word: %q", reply)
	}
	if !strings.Contains(reply, "already taken") {
		t.Errorf("the reply does not say why: %q", reply)
	}
	armed := 0
	for _, name := range beltNames(agent) {
		if name == "notion_create_page" {
			armed++
		}
	}
	if armed != 1 {
		t.Errorf("notion_create_page is on the belt %d times, want once", armed)
	}
	if record, served := agent.servedRecord("notion_create_page"); !served || record.tool != "create-page" {
		t.Errorf("the name the account knows it by is %q, want create-page", record.tool)
	}
}

// AN OFF HALF TAKES ITS TOOLS OFF THE BELT, and the tools of a served account
// divide on the one thing the account said about each: whether it only looks.
func TestTheServedHalvesArmSeparately(t *testing.T) {
	cases := []struct {
		name    string
		read    connect.CapabilityState
		act     connect.CapabilityState
		armed   []string
		unarmed []string
	}{
		{"both on", connect.StateYes, connect.StateAsk,
			[]string{"notion_search_pages", "notion_create_page"}, nil},
		{"acting is off", connect.StateYes, connect.StateOff,
			[]string{"notion_search_pages"}, []string{"notion_create_page"}},
		{"reading is off", connect.StateOff, connect.StateAsk,
			[]string{"notion_create_page"}, []string{"notion_search_pages"}},
	}
	for _, c := range cases {
		hub := servedHub(servedList())
		hub.says("notion", connect.CapabilityRead, c.read)
		hub.says("notion", connect.CapabilityAct, c.act)
		agent := connectAgent(t, &scriptedCompleter{}, hub, true)

		reply := agent.armServed(context.Background(), notionStatus(true), "Notion is connected", "")

		for _, want := range c.armed {
			if !hasTool(agent, want) || !strings.Contains(reply, want) {
				t.Errorf("%s: %s is missing (%q)", c.name, want, reply)
			}
		}
		for _, gone := range c.unarmed {
			if hasTool(agent, gone) {
				t.Errorf("%s: %s reached the belt", c.name, gone)
			}
			if strings.Contains(reply, gone) {
				t.Errorf("%s: the reply offers %s (%q)", c.name, gone, reply)
			}
		}
	}
}

// Everything off is its own fact and gets its own sentence: the account IS
// connected and there is nothing to do with it.
func TestAServedAccountWithBothHalvesOffArmsNothing(t *testing.T) {
	hub := servedHub(servedList())
	hub.says("notion", connect.CapabilityRead, connect.StateOff)
	hub.says("notion", connect.CapabilityAct, connect.StateOff)
	agent := connectAgent(t, &scriptedCompleter{}, hub, true)

	reply := agent.armServed(context.Background(), notionStatus(true), "Notion is connected", "")

	if !strings.Contains(reply, "turned off everything it can do") {
		t.Errorf("the model was not told why there is nothing: %q", reply)
	}
	if strings.Contains(reply, "no tools for it") {
		t.Errorf("a person's answer was reported as a missing feature: %q", reply)
	}
	if names := beltNames(agent); len(names) > 0 {
		for _, name := range names {
			if strings.HasPrefix(name, "notion_") {
				t.Fatalf("%s reached the belt", name)
			}
		}
	}
}

// An account that serves nothing at all ends on the sentence it always ended
// on, rather than on a report about a list.
func TestAnAccountThatServesNothingSaysSo(t *testing.T) {
	hub := &fakeHub{servedService: true, servedConnected: true}
	agent := connectAgent(t, &scriptedCompleter{}, hub, true)

	reply := agent.armServed(context.Background(), notionStatus(true), "Notion is connected", "")

	if !strings.Contains(reply, "no tools for it") {
		t.Errorf("an empty account was not reported plainly: %q", reply)
	}
}

// An account that will not say what it brings is a failure the model can act
// on, not a turn that ends.
func TestAnAccountThatWillNotSayWhatItBringsIsReported(t *testing.T) {
	hub := servedHub(servedList())
	hub.servedErr = errors.New("the workspace did not answer")
	agent := connectAgent(t, &scriptedCompleter{}, hub, true)

	reply := agent.armServed(context.Background(), notionStatus(true), "Notion is connected", "")

	if !strings.Contains(reply, "could not be listed") || !strings.Contains(reply, "did not answer") {
		t.Errorf("the reply hides what went wrong: %q", reply)
	}
}

// ── the ceiling ─────────────────────────────────────────────────────────────

// OVER THE CEILING NOTHING IS ARMED AND THE WHOLE LIST IS SAID. Trimming to the
// first thirty-two would leave the model planning around a list it was never
// told was cut; this way it reads the list and asks for the three it needs.
func TestAnAccountThatServesTooManyArmsNoneAndLists(t *testing.T) {
	hub := servedHub(manyServed(mcpToolCeiling + 1))
	agent := connectAgent(t, &scriptedCompleter{}, hub, true)

	reply := agent.armServed(context.Background(), notionStatus(true), "Notion is connected", "")

	if !strings.Contains(reply, strconv.Itoa(mcpToolCeiling+1)) {
		t.Errorf("the reply does not say how many there are: %q", reply)
	}
	if !strings.Contains(reply, "Nothing was loaded") {
		t.Errorf("the reply does not say that nothing arrived: %q", reply)
	}
	if !strings.Contains(reply, "tools naming") {
		t.Errorf("the reply does not say how to ask again: %q", reply)
	}
	for _, want := range []string{"tool-1", "tool-" + strconv.Itoa(mcpToolCeiling+1)} {
		if !strings.Contains(reply, want) {
			t.Errorf("%s is missing from the list the model has to choose from: %q", want, reply)
		}
	}
	for _, name := range beltNames(agent) {
		if strings.HasPrefix(name, "notion_") {
			t.Fatalf("%s was armed over the ceiling", name)
		}
	}
}

// Exactly at the ceiling everything arrives: the rule is "more than", not
// "close to".
func TestAnAccountAtTheCeilingArmsEverything(t *testing.T) {
	hub := servedHub(manyServed(mcpToolCeiling))
	agent := connectAgent(t, &scriptedCompleter{}, hub, true)

	armNotion(t, agent)

	armed := 0
	for _, name := range beltNames(agent) {
		if strings.HasPrefix(name, "notion_") {
			armed++
		}
	}
	if armed != mcpToolCeiling {
		t.Errorf("%d tools were armed, want %d", armed, mcpToolCeiling)
	}
}

// Naming the tools is how a model gets past the ceiling, and it may name them in
// either spelling — the account's own, or the belt's.
func TestNamingToolsArmsOnlyThose(t *testing.T) {
	hub := servedHub(manyServed(mcpToolCeiling + 1))
	agent := connectAgent(t, &scriptedCompleter{}, hub, true)

	reply := agent.armServed(context.Background(), notionStatus(true), "Notion is connected",
		"tool-7, notion_tool_9, tool-404")

	for _, want := range []string{"notion_tool_7", "notion_tool_9"} {
		if !hasTool(agent, want) {
			t.Errorf("%s was named and never arrived: %v", want, beltNames(agent))
		}
	}
	if hasTool(agent, "notion_tool_1") {
		t.Error("a tool nobody named was armed")
	}
	if !strings.Contains(reply, "nothing called") || !strings.Contains(reply, "tool-404") {
		t.Errorf("a name that matches nothing was swallowed: %q", reply)
	}
}

// A model that names nothing the account has gets the list rather than a
// shrug.
func TestNamingNothingTheAccountHasReturnsTheList(t *testing.T) {
	hub := servedHub(servedList())
	agent := connectAgent(t, &scriptedCompleter{}, hub, true)

	reply := agent.armServed(context.Background(), notionStatus(true), "Notion is connected", "invoices")

	if !strings.Contains(reply, "invoices") || !strings.Contains(reply, "Search Pages") {
		t.Errorf("the reply does not show what there is instead: %q", reply)
	}
	if hasTool(agent, "notion_search_pages") {
		t.Error("a tool nobody named was armed")
	}
}

// ── the call ────────────────────────────────────────────────────────────────

// THE CALL GOES BACK OUT UNDER THE ACCOUNT'S OWN NAME. The belt's name is a
// thing this program invented so a model could say it; the account has never
// heard of it.
func TestAServedCallGoesOutUnderTheAccountsOwnName(t *testing.T) {
	hub := servedHub(servedList())
	completer := &scriptedCompleter{steps: servedTurn("notion_search_pages", `{"query":"invoices"}`)}
	agent := connectAgent(t, completer, hub, true)
	armNotion(t, agent)

	collected := collect(t, mustSubmit(t, agent, "search notion"))
	output := lastToolOutput(t, collected)

	made := hub.servedMade()
	if len(made) != 1 {
		t.Fatalf("the account was called %d times, want once: %v", len(made), made)
	}
	if !strings.HasPrefix(made[0], "notion Search Pages ") {
		t.Errorf("the call went out as %q, want the account's own name", made[0])
	}
	if !strings.Contains(made[0], `"invoices"`) {
		t.Errorf("the arguments did not reach the account: %q", made[0])
	}
	if !strings.Contains(output, "done") {
		t.Errorf("the account's answer did not reach the model: %q", output)
	}
}

// A capability turned off after the belt was armed is answered at the CALL, in
// the person's own words, and nothing runs. Arming cannot un-arm (connect.go's
// append law), so this is the whole of what protects a tool the person has taken
// away mid-conversation.
func TestAServedCallOnAnOffHalfIsRefused(t *testing.T) {
	hub := servedHub(servedList())
	completer := &scriptedCompleter{steps: servedTurn("notion_create_page", `{"title":"Weekly notes"}`)}
	agent := connectAgent(t, completer, hub, true)
	armNotion(t, agent)
	hub.says("notion", connect.CapabilityAct, connect.StateOff)

	collected := collect(t, mustSubmit(t, agent, "write it up in notion"))
	output := lastToolOutput(t, collected)

	if len(hub.servedMade()) != 0 {
		t.Fatalf("a call the person turned off reached the account: %v", hub.servedMade())
	}
	if !strings.Contains(output, "turned off") || !strings.Contains(output, "Notion") {
		t.Errorf("the refusal is not in the person's words: %q", output)
	}
	if !strings.Contains(output, "Nothing was done") {
		t.Errorf("the model was not told the call did not run: %q", output)
	}
}

// ── the floor ───────────────────────────────────────────────────────────────

// A SERVED TOOL THAT ACTS HITS THE SAME FLOOR gmail_send HITS. The account said
// the tool is not read-only, so it runs under the sentence about acting in the
// person's name, which is "ask first" until they say otherwise — and a blanket
// allow does not vouch for it.
func TestAServedToolThatActsIsAskedAbout(t *testing.T) {
	hub := servedHub(servedList())
	completer := &scriptedCompleter{steps: servedTurn("notion_create_page", `{"title":"Weekly notes","body":"..."}`)}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.connectHub = hub
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.AskConsent = true
	})
	armNotion(t, agent)

	collected := drainAnswering(t, mustSubmit(t, agent, "write it up in notion"), func(event Event) {
		agent.ResolveConsent(event.ID, true)
	})

	request, asked := firstOfKind(collected, EventConsentRequest)
	if !asked {
		t.Fatalf("a call that acts in the person's name ran unasked: %v", kinds(collected))
	}
	// The memo says which account, which of its tools, and what the call is
	// about — the last from the argument the account itself marked required.
	for _, want := range []string{"Notion", "create-page", "Weekly notes"} {
		if !strings.Contains(request.Hint, want) {
			t.Errorf("the question does not say %q: %q", want, request.Hint)
		}
	}
	if !strings.Contains(request.Rule, "act in this account in your name") {
		t.Errorf("the question does not say why it is being asked: %q", request.Rule)
	}
	if len(hub.servedMade()) != 1 {
		t.Fatalf("the call ran %d times, want once", len(hub.servedMade()))
	}
}

// A served tool that only looks is not asked about under a blanket allow: a read
// that was not wanted costs a moment.
func TestAServedToolThatOnlyLooksIsNotAskedAbout(t *testing.T) {
	hub := servedHub(servedList())
	completer := &scriptedCompleter{steps: servedTurn("notion_search_pages", `{"query":"invoices"}`)}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.connectHub = hub
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.AskConsent = true
	})
	armNotion(t, agent)

	collected := collect(t, mustSubmit(t, agent, "search notion"))

	if _, asked := firstOfKind(collected, EventConsentRequest); asked {
		t.Error("a read was put to the person")
	}
	if len(hub.servedMade()) != 1 {
		t.Fatalf("the read ran %d times, want once", len(hub.servedMade()))
	}
}

// NOBODY STANDS IN FOR THE PERSON on a served tool that acts. The guardian's
// list is internal/approval's, which cannot hold a name nobody wrote down, so
// the arming record answers for these.
func TestTheGuardianNeverVouchesForAServedToolThatActs(t *testing.T) {
	hub := servedHub(servedList())
	agent := connectAgent(t, &scriptedCompleter{}, hub, true)
	armNotion(t, agent)

	if !agent.actsInThePersonsName("notion_create_page", nil) {
		t.Error("a tool the account said is not read-only reads as a read")
	}
	if agent.actsInThePersonsName("notion_search_pages", nil) {
		t.Error("a read reads as something that leaves in the person's name")
	}
	if agent.actsInThePersonsName("read", nil) {
		t.Error("an ordinary tool was read as an account's")
	}
}

// ── the mid-chat "always" ───────────────────────────────────────────────────

// ONE VOCABULARY, ONE STORE — and it holds for a tool whose name nobody wrote
// down. "Always" on a served call is the same sentence the settings sheet
// writes, and it lands in the same place.
func TestAlwaysOnAServedToolWritesTheSetting(t *testing.T) {
	hub := servedHub(servedList())
	completer := &scriptedCompleter{steps: servedTurn("notion_create_page", `{"title":"Weekly notes"}`)}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.connectHub = hub
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.AskConsent = true
	})
	armNotion(t, agent)

	drainAnswering(t, mustSubmit(t, agent, "write it up in notion"), func(event Event) {
		agent.ResolveConsentRemember(event.ID, true, ConsentToolSession)
	})

	if got := hub.CapabilityState("notion", connect.CapabilityAct); got != connect.StateYes {
		t.Errorf("the answer did not reach the setting: got %q, want %q", got, connect.StateYes)
	}
	if _, remembered := agent.rememberedConsent("notion_create_page"); remembered {
		t.Error("the answer was written twice — the session memo and the setting will drift")
	}
	if len(hub.servedMade()) != 1 {
		t.Fatalf("the call ran %d times, want once", len(hub.servedMade()))
	}
}

// ── the list that changed ───────────────────────────────────────────────────

// THE RECORDS ARE REBUILT FROM WHAT THE ACCOUNT SAYS TODAY. Nothing about a
// served tool is journaled, so a conversation resumed after the account changed
// what it serves arms today's list and holds nothing of yesterday's.
func TestAResumedConversationArmsWhatIsServedNow(t *testing.T) {
	hub := servedHub(servedList())
	first := connectAgent(t, &scriptedCompleter{}, hub, true)
	armNotion(t, first)
	if !hasTool(first, "notion_search_pages") {
		t.Fatalf("yesterday's belt is wrong: %v", beltNames(first))
	}

	hub.serves([]connect.MCPTool{{
		Name:        "find-pages",
		Description: "Search the workspace, renamed.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
		ReadOnly:    true,
	}})

	second := connectAgent(t, &scriptedCompleter{}, hub, true)
	armNotion(t, second)

	if !hasTool(second, "notion_find_pages") {
		t.Errorf("today's list was not armed: %v", beltNames(second))
	}
	if hasTool(second, "notion_search_pages") {
		t.Errorf("a name nobody serves any more came back: %v", beltNames(second))
	}
	if _, served := second.servedRecord("notion_search_pages"); served {
		t.Error("a record outlived the belt it was written for")
	}
}

// ── the other door ──────────────────────────────────────────────────────────

// AN ACCOUNT CONNECTED FROM THE SURFACE GETS THE SAME BELT. The ask reaches the
// far end, and the surface calling this is redrawing itself, so it happens on a
// goroutine of its own and the note follows it — but what arrives is what a tool
// call would have armed, or a person would have two different sets of hands
// depending on which door connected the account.
func TestAnAccountConnectedFromTheSurfaceArmsWhatItServes(t *testing.T) {
	hub := servedHub(servedList())
	agent := connectAgent(t, &scriptedCompleter{}, hub, true)

	agent.NoteConnected("notion", "you@example.test")

	deadline := time.Now().Add(2 * time.Second)
	for !hasTool(agent, "notion_create_page") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !hasTool(agent, "notion_search_pages") || !hasTool(agent, "notion_create_page") {
		t.Fatalf("the surface's door armed a different belt: %v", beltNames(agent))
	}
	if _, served := agent.servedRecord("notion_create_page"); !served {
		t.Error("a tool reached the belt with no record of whose it is")
	}
}

// ── the folding ─────────────────────────────────────────────────────────────

// The fold is lossy on purpose, which is why every armed tool keeps the
// account's own name beside the belt's.
func TestFoldingANameForTheBelt(t *testing.T) {
	cases := []struct{ tool, want string }{
		{"Search Pages", "notion_search_pages"},
		{"create-page", "notion_create_page"},
		{"create.page", "notion_create_page"},
		{"API_v2__query", "notion_api_v2_query"},
		{"  spaced  out  ", "notion_spaced_out"},
		{"…", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := servedName("notion", c.tool); got != c.want {
			t.Errorf("servedName(notion, %q) = %q, want %q", c.tool, got, c.want)
		}
	}
	long := strings.Repeat("verylongname", 10)
	if got := servedName("notion", long); len(got) > mcpNameLimit {
		t.Errorf("a long name was armed as %d characters, over the %d ceiling", len(got), mcpNameLimit)
	}
}

// A tool that takes nothing is not a tool with an unreadable shape: plenty of
// them take nothing, and the belt needs a shape that says so.
func TestAServedToolWithNoShapeIsStillArmed(t *testing.T) {
	hub := servedHub([]connect.MCPTool{{Name: "whoami", Description: "Say who this is.", ReadOnly: true}})
	agent := connectAgent(t, &scriptedCompleter{}, hub, true)

	armNotion(t, agent)

	if !hasTool(agent, "notion_whoami") {
		t.Fatalf("a tool that takes nothing was left off: %v", beltNames(agent))
	}
	for _, tool := range agent.beltTools() {
		if tool.Name != "notion_whoami" {
			continue
		}
		var shape map[string]any
		if err := json.Unmarshal(tool.Schema, &shape); err != nil {
			t.Errorf("the belt carries a shape that does not parse: %v", err)
		}
	}
}
