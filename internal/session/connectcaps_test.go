package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/connect"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// googleStatus is the account as a conversation that has already picked it up
// sees it.
func googleStatus() connectStatus {
	return connectStatus{ID: "google", Name: "Google", Auth: "browser", Connected: true}
}

// armKeyed puts a key account's one tool on the belt, the way a conversation
// that picked it up is already holding it.
func armKeyed(t *testing.T, agent *Agent) {
	t.Helper()
	if _, err := agent.armFamily(agent.familyTools(keyStatus(true))); err != nil {
		t.Fatalf("arm the family: %v", err)
	}
}

// ── the arming ──────────────────────────────────────────────────────────────

// AN OFF CAPABILITY TAKES ITS TOOLS OFF THE BELT: absent from the tool list,
// absent from the sentence that says what arrived.
func TestAnOffCapabilityIsNeverArmed(t *testing.T) {
	hub := &fakeHub{connected: true, account: "you@example.test"}
	hub.says("google", "mail-send", connect.StateOff)
	completer := &scriptedCompleter{steps: useServiceTurn("google")}
	agent := connectAgent(t, completer, hub, true)

	collected := collect(t, mustSubmit(t, agent, "read my mail"))
	output := lastToolOutput(t, collected)

	if strings.Contains(output, "gmail_send") {
		t.Errorf("the arming reply offers a tool the person turned off: %q", output)
	}
	for _, want := range []string{"gmail_search", "gmail_read", "calendar_list", "calendar_create"} {
		if !strings.Contains(output, want) {
			t.Errorf("%s is missing from the arming reply: %q", want, output)
		}
	}
	for _, tool := range agent.beltTools() {
		if tool.Name == "gmail_send" {
			t.Fatal("gmail_send reached the belt")
		}
	}
}

// Everything off is a different fact from "this build has no tools", and it gets
// its own sentence — the account IS connected and there is nothing to do with it.
func TestAnAccountWithEverythingOffArmsNothing(t *testing.T) {
	hub := &fakeHub{connected: true, account: "you@example.test"}
	for _, capability := range []string{"mail-read", "mail-send", "calendar-read", "calendar-write"} {
		hub.says("google", capability, connect.StateOff)
	}
	completer := &scriptedCompleter{steps: useServiceTurn("google")}
	agent := connectAgent(t, completer, hub, true)

	collected := collect(t, mustSubmit(t, agent, "read my mail"))
	output := lastToolOutput(t, collected)

	if !strings.Contains(output, "turned off everything it can do") {
		t.Errorf("the model was not told why there is nothing: %q", output)
	}
	if strings.Contains(output, "no tools for it") {
		t.Errorf("a person's answer was reported as a missing feature: %q", output)
	}
	for _, tool := range agent.beltTools() {
		if googleFamily[tool.Name] {
			t.Fatalf("%s reached the belt", tool.Name)
		}
	}
}

// A key account's one tool is armed while EITHER half of the pair is on, because
// the verb decides which half a call falls on — and taken off only when both are
// off.
func TestTheRawCallIsArmedWhileEitherHalfIsOn(t *testing.T) {
	cases := []struct {
		name  string
		read  connect.CapabilityState
		act   connect.CapabilityState
		armed bool
	}{
		{"both on", connect.StateYes, connect.StateAsk, true},
		{"only reading", connect.StateYes, connect.StateOff, true},
		{"only acting", connect.StateOff, connect.StateYes, true},
		{"both off", connect.StateOff, connect.StateOff, false},
	}
	for _, c := range cases {
		hub := &fakeHub{keyService: true, keyConnected: true}
		hub.says("stripe", connect.CapabilityRead, c.read)
		hub.says("stripe", connect.CapabilityAct, c.act)
		completer := &scriptedCompleter{steps: useServiceTurn("stripe")}
		agent := connectAgent(t, completer, hub, true)

		collected := collect(t, mustSubmit(t, agent, "look at stripe"))
		output := lastToolOutput(t, collected)

		held := false
		for _, tool := range agent.beltTools() {
			if tool.Name == "stripe_request" {
				held = true
			}
		}
		if held != c.armed {
			t.Errorf("%s: armed = %v, want %v (%q)", c.name, held, c.armed, output)
		}
	}
}

// The one tool a key account brings says which of its two halves is on. A
// sentence promising that `get` reads, to a model whose reads will all be
// refused, buys one wasted call and a confused turn.
func TestTheRawCallSaysOnlyWhatIsLeftOn(t *testing.T) {
	cases := []struct {
		name    string
		read    connect.CapabilityState
		act     connect.CapabilityState
		says    []string
		saysNot []string
	}{
		{
			name: "both on", read: connect.StateYes, act: connect.StateAsk,
			says: []string{"get reads", "the person is asked before one goes"},
		},
		{
			name: "acting is off", read: connect.StateYes, act: connect.StateOff,
			says:    []string{"get reads", "turned off changing anything"},
			saysNot: []string{"Reading is turned off"},
		},
		{
			name: "reading is off", read: connect.StateOff, act: connect.StateYes,
			says:    []string{"Reading is turned off"},
			saysNot: []string{"get reads"},
		},
	}
	for _, c := range cases {
		hub := &fakeHub{keyService: true, keyConnected: true}
		hub.says("stripe", connect.CapabilityRead, c.read)
		hub.says("stripe", connect.CapabilityAct, c.act)
		agent := connectAgent(t, &scriptedCompleter{}, hub, true)

		tools := agent.familyTools(keyStatus(true))
		if len(tools) != 1 {
			t.Fatalf("%s: a key account brings %d tools, want one", c.name, len(tools))
		}
		for _, want := range c.says {
			if !strings.Contains(tools[0].Description, want) {
				t.Errorf("%s: the description does not say %q: %q", c.name, want, tools[0].Description)
			}
		}
		for _, unwanted := range c.saysNot {
			if strings.Contains(tools[0].Description, unwanted) {
				t.Errorf("%s: the description still says %q: %q", c.name, unwanted, tools[0].Description)
			}
		}
	}
}

// ── the listing ─────────────────────────────────────────────────────────────

// THE CONNECTED LINE IS THE LIVE ANSWER. A capability that is off is not
// advertised, and the blurb — which promises what the plug can do rather than
// what this person left on — gives way to what is actually left.
func TestTheServicesListingDoesNotAdvertiseWhatIsOff(t *testing.T) {
	hub := &fakeHub{connected: true, account: "you@example.test"}
	agent := connectAgent(t, &scriptedCompleter{}, hub, true)

	// Nothing said: the plug's own line stands, exactly as it did before
	// capabilities existed.
	untouched := renderServices(hub.Services(), "", agent.connectedFor)
	if !strings.Contains(untouched, "search and read your mail") {
		t.Errorf("an untouched account lost its own line: %q", untouched)
	}

	hub.says("google", "mail-send", connect.StateOff)
	narrowed := renderServices(hub.Services(), "", agent.connectedFor)
	if strings.Contains(narrowed, "send mail as you") {
		t.Errorf("the listing advertises a capability that is off: %q", narrowed)
	}
	if strings.Contains(narrowed, "search and read your mail") {
		t.Errorf("the blurb outlived the answer that made it untrue: %q", narrowed)
	}
	for _, want := range []string{"read your mail", "read your calendar"} {
		if !strings.Contains(narrowed, want) {
			t.Errorf("the listing does not say what is left: %q wanted in %q", want, narrowed)
		}
	}

	// And an account with nothing left says so rather than listing an empty
	// nothing after a colon.
	for _, capability := range []string{"mail-read", "calendar-read", "calendar-write"} {
		hub.says("google", capability, connect.StateOff)
	}
	empty := renderServices(hub.Services(), "", agent.connectedFor)
	if !strings.Contains(empty, "turned off everything this account can do") {
		t.Errorf("an account with nothing on: %q", empty)
	}
}

// ── the call ────────────────────────────────────────────────────────────────

// A STATE CHANGED MID-SESSION BITES AT THE NEXT CALL. The tool was armed while
// sending was allowed and cannot be un-armed (the belt only grows), so the call
// itself refuses — and nothing leaves.
func TestACapabilityTurnedOffAfterArmingRefusesTheCall(t *testing.T) {
	service := &stubTransport{answer: `{"id":"sent-1"}`}
	hub := &fakeHub{connected: true, account: "you@example.test", transport: service}
	completer := &scriptedCompleter{steps: sendTurn()}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.connectHub = hub
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.AskConsent = true
	})
	armGoogle(t, agent)

	// The settings sheet, in the same process, between two turns.
	hub.says("google", "mail-send", connect.StateOff)

	collected := drainAnswering(t, mustSubmit(t, agent, "tell alice noon works"), func(event Event) {
		agent.ResolveConsent(event.ID, true)
	})

	if service.calls() != 0 {
		t.Fatal("a message left after the person turned sending off")
	}
	if _, asked := firstOfKind(collected, EventConsentRequest); asked {
		t.Error("the person was asked about a call that was never going to run")
	}
	failed, ok := firstOfKind(collected, EventToolFailed)
	if !ok {
		t.Fatalf("the call did not refuse: %v", kinds(collected))
	}
	if !strings.Contains(failed.Output, "turned off") || !strings.Contains(failed.Output, "send mail as you") {
		t.Errorf("the refusal does not say what happened: %q", failed.Output)
	}
	// IN THE PERSON'S WORDS. A refusal that says "capability" or "policy" is a
	// refusal written for the machine.
	lowered := strings.ToLower(failed.Output)
	for _, machinery := range []string{"capability", "policy", "approval rule", "state", "oauth", "scope"} {
		if strings.Contains(lowered, machinery) {
			t.Errorf("the refusal says %q, which is machinery vocabulary: %q", machinery, failed.Output)
		}
	}
}

// The same law on a read: what is off is off whichever half of the pair it is.
func TestAnOffReadRefusesTheCallToo(t *testing.T) {
	hub := &fakeHub{connected: true, account: "you@example.test", transport: &stubTransport{answer: `{}`}}
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "gmail_search", `{"query":"from:alice"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent := connectAgent(t, completer, hub, true)
	armGoogle(t, agent)
	hub.says("google", "mail-read", connect.StateOff)

	collected := collect(t, mustSubmit(t, agent, "what did alice say"))
	failed, ok := firstOfKind(collected, EventToolFailed)
	if !ok {
		t.Fatalf("the call did not refuse: %v", kinds(collected))
	}
	if !strings.Contains(failed.Output, "read your mail") {
		t.Errorf("the refusal does not name what was turned off: %q", failed.Output)
	}
}

// ── the verb split ──────────────────────────────────────────────────────────

// ONE TOOL, TWO CAPABILITIES. The same tool is judged against `read` when the
// verb is a GET and against `act` for everything else, so a person who left
// reading on and turned acting off gets exactly that.
func TestTheVerbChoosesTheCapabilityForARawCall(t *testing.T) {
	cases := []struct {
		name    string
		read    connect.CapabilityState
		act     connect.CapabilityState
		method  string
		reaches bool
	}{
		{"a read while reading is on", connect.StateYes, connect.StateOff, "get", true},
		{"a write while acting is off", connect.StateYes, connect.StateOff, "post", false},
		{"a read while reading is off", connect.StateOff, connect.StateYes, "get", false},
		{"a write while acting is on", connect.StateOff, connect.StateYes, "delete", true},
		{"no verb at all is a read", connect.StateOff, connect.StateYes, "", false},
	}
	for _, c := range cases {
		hub := &fakeHub{keyService: true, keyConnected: true}
		hub.says("stripe", connect.CapabilityRead, c.read)
		hub.says("stripe", connect.CapabilityAct, c.act)
		arguments := `{"path":"/v1/customers"}`
		if c.method != "" {
			arguments = `{"method":"` + c.method + `","path":"/v1/customers"}`
		}
		completer := &scriptedCompleter{steps: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-1", "stripe_request", arguments), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("done"), nil
			},
		}}
		agent, _ := newTestAgent(t, completer, func(config *Config) {
			config.connectHub = hub
			config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
			config.AskConsent = true
		})
		armKeyed(t, agent)

		collected := drainAnswering(t, mustSubmit(t, agent, "look at stripe"), func(event Event) {
			agent.ResolveConsent(event.ID, true)
		})

		reached := len(hub.rawCalls()) > 0
		if reached != c.reaches {
			t.Errorf("%s: the call reached the service = %v, want %v (%v)",
				c.name, reached, c.reaches, kinds(collected))
		}
	}
}

// ── the judging seam ────────────────────────────────────────────────────────

// YES IS THE NAMED ALLOW. It is worth what `gmail_send:allow` is worth, which
// means it lifts the floor internal/approval holds under a blanket allow for a
// call that acts in the person's name — and nobody is asked.
func TestYesOnACapabilityIsTheNamedAllow(t *testing.T) {
	service := &stubTransport{answer: `{"id":"sent-1"}`}
	hub := &fakeHub{connected: true, account: "you@example.test", transport: service}
	hub.says("google", "mail-send", connect.StateYes)
	completer := &scriptedCompleter{steps: sendTurn()}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.connectHub = hub
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.AskConsent = true
	})
	armGoogle(t, agent)

	collected := collect(t, mustSubmit(t, agent, "tell alice noon works"))

	if _, asked := firstOfKind(collected, EventConsentRequest); asked {
		t.Error("somebody who already said yes was asked again")
	}
	if service.calls() != 1 {
		t.Fatalf("the message was sent %d times, want once", service.calls())
	}
}

// ASK IS A FLOOR OF ITS OWN, and it holds under a capability that does not act:
// a person who sets "read your mail" to ask first is asking to be asked, and a
// policy that allows everything must not quietly ignore the only control they
// were given.
func TestAskOnACapabilityAsksEvenWhereTheFloorWouldNot(t *testing.T) {
	service := &stubTransport{answer: `{"messages":[]}`}
	hub := &fakeHub{connected: true, account: "you@example.test", transport: service}
	hub.says("google", "mail-read", connect.StateAsk)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "gmail_search", `{"query":"from:alice"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.connectHub = hub
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.AskConsent = true
	})
	armGoogle(t, agent)

	collected := drainAnswering(t, mustSubmit(t, agent, "what did alice say"), func(event Event) {
		agent.ResolveConsent(event.ID, true)
	})

	request, asked := firstOfKind(collected, EventConsentRequest)
	if !asked {
		t.Fatalf("nobody was asked: %v", kinds(collected))
	}
	if !strings.Contains(request.Rule, "read your mail") {
		t.Errorf("the question does not say why it is being asked: %q", request.Rule)
	}
}

// A DENY IS A DENY. A word on a settings row is not a licence to overrule a rule
// that refuses outright.
func TestYesDoesNotOverruleADenyRule(t *testing.T) {
	service := &stubTransport{answer: `{"id":"sent-1"}`}
	hub := &fakeHub{connected: true, account: "you@example.test", transport: service}
	hub.says("google", "mail-send", connect.StateYes)
	completer := &scriptedCompleter{steps: sendTurn()}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.connectHub = hub
		config.ApprovalPolicy = &approval.Policy{
			Default: approval.ActionAllow,
			Tools:   map[string]approval.Action{"gmail_send": approval.ActionDeny},
		}
		config.AskConsent = true
	})
	armGoogle(t, agent)

	collected := collect(t, mustSubmit(t, agent, "tell alice noon works"))

	if service.calls() != 0 {
		t.Fatal("a denied call ran because a settings row said yes")
	}
	if _, ok := firstOfKind(collected, EventToolFailed); !ok {
		t.Fatalf("the call did not refuse: %v", kinds(collected))
	}
}

// ── the mid-chat "always" ───────────────────────────────────────────────────

// ONE VOCABULARY, ONE STORE. "Always" on a question about somebody's account is
// the same sentence the settings sheet writes, and it lands in the same place —
// not in a second remembered-allow list of its own.
func TestAlwaysOnAConnectorPromptWritesTheSetting(t *testing.T) {
	service := &stubTransport{answer: `{"id":"sent-1"}`}
	hub := &fakeHub{connected: true, account: "you@example.test", transport: service}
	completer := &scriptedCompleter{steps: sendTurn()}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.connectHub = hub
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.AskConsent = true
	})
	armGoogle(t, agent)

	drainAnswering(t, mustSubmit(t, agent, "tell alice noon works"), func(event Event) {
		agent.ResolveConsentRemember(event.ID, true, ConsentToolSession)
	})

	if got := hub.CapabilityState("google", "mail-send"); got != connect.StateYes {
		t.Errorf("the answer did not reach the setting: got %q, want %q", got, connect.StateYes)
	}
	if _, remembered := agent.rememberedConsent("gmail_send"); remembered {
		t.Error("the answer was written twice — the session memo and the setting will drift")
	}
	if service.calls() != 1 {
		t.Fatalf("the message was sent %d times, want once", service.calls())
	}
}

// An ordinary tool keeps the memo it always had: the capability store has
// nothing to say about a call that belongs to no account.
func TestAlwaysOnAnOrdinaryToolStillUsesTheMemo(t *testing.T) {
	hub := &fakeHub{connected: true}
	agent := connectAgent(t, &scriptedCompleter{}, hub, true)

	for _, tool := range []string{"bash", "read", "services", ""} {
		if agent.rememberCapability(tool, nil, true) {
			t.Errorf("%q was treated as a call against an account", tool)
		}
	}
	// And a no, even on a connector tool, is not an off: taking a capability
	// away for good is a decision with its own control.
	if agent.rememberCapability("gmail_send", nil, false) {
		t.Error("a refusal was written to the settings as an answer that lasts")
	}
	if got := hub.CapabilityState("google", "mail-send"); got != connect.StateAsk {
		t.Errorf("a refusal changed the setting: got %q", got)
	}
}

// ── the two tables agree ────────────────────────────────────────────────────

// THE FLOOR AND THE CAPABILITIES DRAW THE SAME LINE. internal/approval holds a
// floor under the calls that act in the person's name outside this machine, and
// internal/connect marks the same fact on a capability. They are written in
// different packages for different readers, and a build where they disagree is a
// build where a settings row says one thing and the gate does another.
//
// It is checked against the REAL registry rather than the fake, because agreeing
// with a copy of itself proves nothing.
func TestTheActsFloorAndTheCapabilitiesAgree(t *testing.T) {
	manager, err := connect.NewManager(t.TempDir(), map[string]connect.ClientCredential{
		"google": {ID: "client-id", Secret: "client-secret"},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	acts := func(service, capability string) (bool, bool) {
		for _, declared := range manager.Capabilities(service) {
			if declared.ID == capability {
				return declared.Acts, true
			}
		}
		return false, false
	}

	for tool := range googleFamily {
		capability := manager.ToolCapability("google", tool)
		if capability == "" {
			t.Errorf("%s is armed for Google and no capability owns it", tool)
			continue
		}
		declared, found := acts("google", capability)
		if !found {
			t.Errorf("%s points at %q, which Google does not declare", tool, capability)
			continue
		}
		if want := approval.ActsInThePersonsName(tool, nil); declared != want {
			t.Errorf("%s: the capability says acts = %v, the consent floor says %v", tool, declared, want)
		}
	}

	// The raw call is the same agreement said by verb: the half the registry
	// points at acts, and the floor reads a GET as a read.
	if got := manager.ToolCapability("stripe", "stripe_request"); got != connect.CapabilityAct {
		t.Errorf("ToolCapability(stripe, stripe_request): got %q, want %q", got, connect.CapabilityAct)
	}
	if declared, found := acts("stripe", connect.CapabilityAct); !found || !declared {
		t.Errorf("the act half of the pair must act in the person's name")
	}
	if declared, found := acts("stripe", connect.CapabilityRead); !found || declared {
		t.Errorf("the read half of the pair must not")
	}
	if approval.ActsInThePersonsName("stripe_request", []byte(`{"method":"get"}`)) {
		t.Error("a GET was read as a call that acts")
	}
	if !approval.ActsInThePersonsName("stripe_request", []byte(`{"method":"post"}`)) {
		t.Error("a POST was read as a call that only reads")
	}
}

// toolService is the inverse of [Agent.familyTools], and this is what keeps the
// two from drifting: every tool the Google family arms answers "google", and
// nothing else does.
func TestToolServiceIsTheInverseOfTheFamily(t *testing.T) {
	hub := &fakeHub{connected: true, keyService: true, keyConnected: true}
	agent := connectAgent(t, &scriptedCompleter{}, hub, true)

	armed := map[string]bool{}
	for _, tool := range agent.familyTools(googleStatus()) {
		armed[tool.Name] = true
		if got := toolService(tool.Name); got != "google" {
			t.Errorf("toolService(%s): got %q, want google", tool.Name, got)
		}
	}
	if len(armed) != len(googleFamily) {
		t.Errorf("the family arms %d tools, the inverse knows %d", len(armed), len(googleFamily))
	}
	for tool := range googleFamily {
		if !armed[tool] {
			t.Errorf("%s is in the inverse and the family does not arm it", tool)
		}
	}

	for _, tool := range agent.familyTools(keyStatus(true)) {
		if got := toolService(tool.Name); got != "stripe" {
			t.Errorf("toolService(%s): got %q, want stripe", tool.Name, got)
		}
	}
	for _, absent := range []string{"bash", "services", "use_service", "_request", ""} {
		if got := toolService(absent); got != "" {
			t.Errorf("toolService(%q): got %q, want empty", absent, got)
		}
	}
}
