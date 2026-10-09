package session

// A PLACE'S MODEL AND PERMISSIONS HAVE TO REACH THE REQUEST, AND ONLY WHERE THE
// RULES SAY.
//
// Every model assertion reads the model a REQUEST was sent on, and every
// posture assertion reads the gate the session actually holds, never the
// record: a default written down and never applied would pass every check on
// the record and change nothing the person pays for.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/placegraph"
)

func (f placeFixture) policyPlace(t *testing.T, name string, pol placegraph.Policy, parents ...string) placegraph.Place {
	t.Helper()
	p, _, err := f.store.CreatePlace(placegraph.NewPlace{Name: name, Parents: parents, Policy: pol})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// policyAgent is a desktop conversation with a folder of its own (so its
// meta.json is real), a gate whose rows stand at standing, and f's graph.
func policyAgent(t *testing.T, f placeFixture, completer Completer, standing string) (*Agent, *fakeApprovalGate, string) {
	t.Helper()
	dir := t.TempDir()
	gate := &fakeApprovalGate{standing: standing}
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.SessionFile = filepath.Join(dir, "session.jsonl")
		c.Place = Place{Dir: dir}
		c.ApprovalGate = gate
		c.ApprovalPolicy = &approval.Policy{Default: approval.ActionPrompt}
		c.PlaceGraph = &PlaceGraphDoor{Path: f.path, ChoicesPath: f.choices, Sources: placegraph.SourcePolicy{Deny: []string{}}}
	})
	return agent, gate, dir
}

func lastModel(t *testing.T, c *scriptedCompleter) string {
	t.Helper()
	return c.model(c.requests() - 1)
}

func placeDefaultsOnDisk(t *testing.T, agent *Agent, dir string) PlaceDefaults {
	t.Helper()
	agent.SettleWrites()
	meta, err := LoadMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.PlaceDefaults == nil {
		return PlaceDefaults{}
	}
	return *meta.PlaceDefaults
}

func TestANewChatInAPlaceTalksOnThePlacesModelFromItsFirstRequest(t *testing.T) {
	f := newPlaceFixture(t)
	release := f.policyPlace(t, "Release", placegraph.Policy{Model: "place/flash"})
	completer := &scriptedCompleter{}
	agent, _, dir := policyAgent(t, f, completer, PostureAsk)
	f.file(t, agent.id, release)

	turn(t, agent, completer, "draft the notes")
	if got := lastModel(t, completer); got != "place/flash" {
		t.Fatalf("the first request went to %q, want the place's model", got)
	}
	if got := agent.Model(); got != "place/flash" {
		t.Fatalf("the conversation reports %q", got)
	}
	rec := placeDefaultsOnDisk(t, agent, dir)
	if rec.Model != "place/flash" || rec.ModelBy != release.ID || !rec.ModelFollows || rec.ModelYours {
		t.Fatalf("record: %+v", rec)
	}
	// The person and the model are both told why the model moved.
	if sys := strings.Join(messageTexts(completer.request(0)), "\n"); !strings.Contains(sys, "Release set this conversation's model to place/flash") {
		t.Fatalf("no aside about the model:\n%s", sys)
	}
}

func TestThePersonsOwnModelPickBeatsThePlaceAndStaysBeaten(t *testing.T) {
	f := newPlaceFixture(t)
	release := f.policyPlace(t, "Release", placegraph.Policy{Model: "place/flash"})
	completer := &scriptedCompleter{}
	agent, _, dir := policyAgent(t, f, completer, PostureAsk)
	f.file(t, agent.id, release)

	agent.SetModel("mine/pro")
	turn(t, agent, completer, "hello")
	if got := lastModel(t, completer); got != "mine/pro" {
		t.Fatalf("the place overrode the person's pick: %q", got)
	}
	// And a later change to the place does not move it either.
	if _, err := f.store.SetPolicy(release.ID, placegraph.Policy{Model: "place/other"}); err != nil {
		t.Fatal(err)
	}
	turn(t, agent, completer, "again")
	if got := lastModel(t, completer); got != "mine/pro" {
		t.Fatalf("a place change moved a person's pick: %q", got)
	}
	if rec := placeDefaultsOnDisk(t, agent, dir); !rec.ModelYours {
		t.Fatalf("the person's pick was not written down: %+v", rec)
	}
}

func TestAFollowingChatTakesAChangedDefaultAtTheNextTurnNeverMidTurn(t *testing.T) {
	f := newPlaceFixture(t)
	release := f.policyPlace(t, "Release", placegraph.Policy{Model: "place/flash"})
	var completer *scriptedCompleter
	completer = &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("first"), nil },
		// The second turn's first request: the place changes WHILE the turn
		// runs, and the turn asks a tool so it makes a second request.
		func(context.Context, []ai.Message) (*ai.Response, error) {
			if _, err := f.store.SetPolicy(release.ID, placegraph.Policy{Model: "place/next"}); err != nil {
				t.Error(err)
			}
			return toolResponse("call-1", "no_such_tool", `{}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("second"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("third"), nil },
	}}
	agent, _, _ := policyAgent(t, f, completer, PostureAsk)
	f.file(t, agent.id, release)

	turn(t, agent, completer, "one")
	at := turn(t, agent, completer, "two")
	for i := at; i < completer.requests(); i++ {
		if got := completer.model(i); got != "place/flash" {
			t.Fatalf("request %d of the running turn moved to %q mid-turn", i, got)
		}
	}
	turn(t, agent, completer, "three")
	if got := lastModel(t, completer); got != "place/next" {
		t.Fatalf("the next turn is on %q, want the place's new model", got)
	}
}

func TestAChatAlreadyRunningKeepsItsModelWhenFiled(t *testing.T) {
	f := newPlaceFixture(t)
	release := f.policyPlace(t, "Release", placegraph.Policy{Model: "place/flash"})
	completer := &scriptedCompleter{}
	agent, _, _ := policyAgent(t, f, completer, PostureAsk)
	turn(t, agent, completer, "started before the place")
	f.file(t, agent.id, release)
	turn(t, agent, completer, "now filed")
	if got := lastModel(t, completer); got != "test/model" {
		t.Fatalf("filing a running chat switched its model to %q", got)
	}
	agent.mu.Lock()
	facts := agent.placeFactsLocked(false)
	bundle := agent.placeGraphBundle
	agent.mu.Unlock()
	if s := DecidePlaceSettings(bundle, facts); len(s) != 1 || s[0].State != PlaceSettingNotNew {
		t.Fatalf("settings: %+v", s)
	}
}

func TestDisagreeingPlacesApplyNothingUntilThePickThenThePick(t *testing.T) {
	f := newPlaceFixture(t)
	a := f.policyPlace(t, "Marketing", placegraph.Policy{Model: "place/a", Permissions: PostureAsk})
	b := f.policyPlace(t, "Software", placegraph.Policy{Model: "place/b", Permissions: PostureDeny})
	completer := &scriptedCompleter{}
	agent, gate, _ := policyAgent(t, f, completer, PostureGuardian)
	f.file(t, agent.id, a)
	f.file(t, agent.id, b)

	turn(t, agent, completer, "hello")
	if got := lastModel(t, completer); got != "test/model" {
		t.Fatalf("a conflict applied %q", got)
	}
	if len(gate.built) != 0 || agent.ApprovalPosture() != "" {
		t.Fatalf("a conflict moved the gate: built %v posture %q", gate.built, agent.ApprovalPosture())
	}
	book, err := placegraph.OpenChoices(f.choices)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := book.Set(agent.id, placegraph.PolicyModel, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := book.Set(agent.id, placegraph.PolicyPermissions, b.ID); err != nil {
		t.Fatal(err)
	}
	turn(t, agent, completer, "picked")
	if got := lastModel(t, completer); got != "place/b" {
		t.Fatalf("the remembered pick was not applied: %q", got)
	}
	if got := agent.ApprovalPosture(); got != PostureDeny {
		t.Fatalf("the picked (narrower) posture was not applied: %q", got)
	}
}

func TestAPlaceNeverWidensTheGateByItself(t *testing.T) {
	f := newPlaceFixture(t)
	open := f.policyPlace(t, "Sandbox", placegraph.Policy{Permissions: PostureAllow})
	completer := &scriptedCompleter{}
	agent, gate, dir := policyAgent(t, f, completer, PostureAsk)
	f.file(t, agent.id, open)

	turn(t, agent, completer, "hello")
	if len(gate.built) != 0 || agent.ResolvedApprovalPosture() != PostureAsk || agent.approvalGate().Default != approval.ActionPrompt {
		t.Fatalf("a place opened the gate: built %v resolved %q", gate.built, agent.ResolvedApprovalPosture())
	}
	agent.mu.Lock()
	settings := DecidePlaceSettings(agent.placeGraphBundle, agent.placeFactsLocked(true))
	agent.mu.Unlock()
	if len(settings) != 1 || settings[0].State != PlaceSettingNeedsYou {
		t.Fatalf("settings: %+v", settings)
	}
	// The person's own act opens it, and then it is theirs.
	if err := agent.SetApprovalPosture(PostureAllow); err != nil {
		t.Fatal(err)
	}
	if rec := placeDefaultsOnDisk(t, agent, dir); !rec.PermissionsYours {
		t.Fatalf("record: %+v", rec)
	}
}

func TestANarrowerPlacePostureIsAppliedThroughTheGate(t *testing.T) {
	f := newPlaceFixture(t)
	careful := f.policyPlace(t, "Production", placegraph.Policy{Permissions: PostureAsk})
	completer := &scriptedCompleter{}
	agent, gate, dir := policyAgent(t, f, completer, PostureGuardian)
	f.file(t, agent.id, careful)

	turn(t, agent, completer, "hello")
	if len(gate.built) != 1 || gate.built[0] != PostureAsk || agent.ApprovalPosture() != PostureAsk || agent.guardianOn() {
		t.Fatalf("built %v posture %q guardian %v", gate.built, agent.ApprovalPosture(), agent.guardianOn())
	}
	if rec := placeDefaultsOnDisk(t, agent, dir); rec.Permissions != PostureAsk || rec.PermissionsYours {
		t.Fatalf("record: %+v", rec)
	}
}

func TestAPostureWordCodeafDoesNotHaveIsNeverApplied(t *testing.T) {
	f := newPlaceFixture(t)
	odd := f.policyPlace(t, "Odd", placegraph.Policy{Permissions: "sudo"})
	completer := &scriptedCompleter{}
	agent, gate, _ := policyAgent(t, f, completer, PostureAsk)
	f.file(t, agent.id, odd)
	turn(t, agent, completer, "hello")
	if len(gate.built) != 0 || agent.ApprovalPosture() != "" {
		t.Fatalf("an unknown posture was applied: %v", gate.built)
	}
	agent.mu.Lock()
	settings := DecidePlaceSettings(agent.placeGraphBundle, agent.placeFactsLocked(true))
	agent.mu.Unlock()
	if len(settings) != 1 || settings[0].State != PlaceSettingUnavailable {
		t.Fatalf("settings: %+v", settings)
	}
}

func TestATerminalConversationKeepsNoPlaceRecord(t *testing.T) {
	dir := t.TempDir()
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.SessionFile = filepath.Join(dir, "session.jsonl")
		c.Place = Place{Dir: dir}
	})
	agent.SetModel("mine/pro")
	turn(t, agent, completer, "hello")
	agent.SettleWrites()
	meta, _ := LoadMeta(dir)
	if meta.PlaceDefaults != nil {
		t.Fatalf("a conversation with no places wrote a place record: %+v", meta.PlaceDefaults)
	}
}

func TestDecidePlaceSettingsTable(t *testing.T) {
	bundle := func(d ...placegraph.PolicyDecision) *placegraph.Bundle { return &placegraph.Bundle{Policy: d} }
	model := func(v string, o placegraph.PolicyOutcome) placegraph.PolicyDecision {
		return placegraph.PolicyDecision{Field: placegraph.PolicyModel, Value: v, Outcome: o, DecidedBy: "pl_a"}
	}
	perm := func(v string) placegraph.PolicyDecision {
		return placegraph.PolicyDecision{Field: placegraph.PolicyPermissions, Value: v, Outcome: placegraph.PolicyAgreed, DecidedBy: "pl_a"}
	}
	base := PlaceSettingFacts{Door: true, Fresh: true, Model: "m/0", Resolved: PostureAsk, Standing: PostureAsk, Dial: true}
	for _, tc := range []struct {
		name  string
		b     *placegraph.Bundle
		f     func(PlaceSettingFacts) PlaceSettingFacts
		state PlaceSettingState
	}{
		{"fresh model pending", bundle(model("m/1", placegraph.PolicyAgreed)), nil, PlaceSettingPending},
		{"already on it", bundle(model("m/0", placegraph.PolicyDecided)), nil, PlaceSettingApplied},
		{"needs pick", bundle(model("", placegraph.PolicyNeedsPick)), nil, PlaceSettingNeedsPick},
		{"no door", bundle(model("m/1", placegraph.PolicyAgreed)), func(f PlaceSettingFacts) PlaceSettingFacts { f.Door = false; return f }, PlaceSettingUnavailable},
		{"yours", bundle(model("m/1", placegraph.PolicyAgreed)), func(f PlaceSettingFacts) PlaceSettingFacts { f.Record.ModelYours = true; return f }, PlaceSettingYours},
		{"not new", bundle(model("m/1", placegraph.PolicyAgreed)), func(f PlaceSettingFacts) PlaceSettingFacts { f.Fresh = false; return f }, PlaceSettingNotNew},
		{"following, moved elsewhere", bundle(model("m/1", placegraph.PolicyAgreed)), func(f PlaceSettingFacts) PlaceSettingFacts {
			f.Fresh, f.Record.ModelFollows, f.Record.Model = false, true, "m/9"
			return f
		}, PlaceSettingYours},
		{"following, place changed", bundle(model("m/1", placegraph.PolicyAgreed)), func(f PlaceSettingFacts) PlaceSettingFacts {
			f.Fresh, f.Record.ModelFollows, f.Record.Model = false, true, "m/0"
			return f
		}, PlaceSettingPending},
		{"narrower posture", bundle(perm(PostureDeny)), nil, PlaceSettingPending},
		{"same posture", bundle(perm(PostureAsk)), nil, PlaceSettingPending},
		{"wider posture", bundle(perm(PostureGuardian)), nil, PlaceSettingNeedsYou},
		{"auto wider than now", bundle(perm(PostureAuto)), func(f PlaceSettingFacts) PlaceSettingFacts { f.Standing = PostureAllow; return f }, PlaceSettingNeedsYou},
		{"auto unknown standing", bundle(perm(PostureAuto)), func(f PlaceSettingFacts) PlaceSettingFacts { f.Standing = ""; return f }, PlaceSettingNeedsYou},
		{"unknown word", bundle(perm("root")), nil, PlaceSettingUnavailable},
		{"no dial", bundle(perm(PostureDeny)), func(f PlaceSettingFacts) PlaceSettingFacts { f.Dial = false; return f }, PlaceSettingUnavailable},
		{"posture applied", bundle(perm(PostureDeny)), func(f PlaceSettingFacts) PlaceSettingFacts { f.Posture = PostureDeny; return f }, PlaceSettingApplied},
	} {
		facts := base
		if tc.f != nil {
			facts = tc.f(facts)
		}
		got := DecidePlaceSettings(tc.b, facts)
		if len(got) != 1 || got[0].State != tc.state {
			t.Errorf("%s: %+v, want %s", tc.name, got, tc.state)
		}
		if tc.state != PlaceSettingApplied && tc.state != PlaceSettingPending && got[0].Reason == "" {
			t.Errorf("%s: a state that applies nothing must say why", tc.name)
		}
	}
	if got := DecidePlaceSettings(nil, base); got == nil || len(got) != 0 {
		t.Fatalf("nil bundle: %#v", got)
	}
}

func TestPlaceGraphDoorForIsOneAbsolutePathAndItsPicksBesideIt(t *testing.T) {
	if _, err := PlaceGraphDoorFor("relative/places.json"); err == nil {
		t.Fatal("a relative graph path was accepted")
	}
	dir := t.TempDir()
	door, err := PlaceGraphDoorFor(filepath.Join(dir, "x", "..", "places.json"))
	if err != nil {
		t.Fatal(err)
	}
	if door.Path != filepath.Join(dir, "places.json") || door.ChoicesPath != filepath.Join(dir, PlaceChoicesFile) || len(door.Sources.Deny) == 0 {
		t.Fatalf("%+v", door)
	}
}
