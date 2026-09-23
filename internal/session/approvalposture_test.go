package session

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/approval"
)

// ── the conversation's posture on the gate ──────────────────────────────────
//
// The defect this scope answers is the thinking rung's, one dial over: the gate
// had an install scope and a launch scope and nothing a person standing in a
// conversation could move. So the door has to move the gate NOW, write the word
// down, and give the word back on resume — and refuse cleanly where there is no
// door at all.

// fakeApprovalGate is the settings rows as a test holds them: it records every
// posture it was asked to build and answers a policy whose default says which.
type fakeApprovalGate struct {
	built    []string
	standing string
	fail     error
}

func (g *fakeApprovalGate) Build(posture string) (*approval.Policy, bool, error) {
	g.built = append(g.built, posture)
	if g.fail != nil {
		return nil, false, g.fail
	}
	mode := approval.ActionPrompt
	switch posture {
	case PostureAllow:
		mode = approval.ActionAllow
	case PostureDeny:
		mode = approval.ActionDeny
	}
	return &approval.Policy{Default: mode}, posture == PostureGuardian, nil
}

func (g *fakeApprovalGate) Standing() string { return g.standing }

func TestThePostureMovesTheGateNowAndIsWrittenDownAndReadBack(t *testing.T) {
	dir := t.TempDir()
	gate := &fakeApprovalGate{standing: PostureAsk}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "session.jsonl")
		config.Place = Place{Dir: dir}
		config.ApprovalGate = gate
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionPrompt}
	})

	if !agent.ApprovalDial() {
		t.Fatal("a session handed a gate says it has no dial")
	}
	if got := agent.ApprovalPosture(); got != "" {
		t.Fatalf("a fresh conversation reports %q, want nothing at all", got)
	}
	if got := agent.ResolvedApprovalPosture(); got != PostureAsk {
		t.Fatalf("an untouched conversation resolves to %q, want the rows' own %q", got, PostureAsk)
	}

	if err := agent.SetApprovalPosture(PostureAllow); err != nil {
		t.Fatalf("allow was refused: %v", err)
	}
	// THE GATE MOVED ON THE SAME CALL. What was pushed is what the door built,
	// and the tool path reads it through the one seam every read goes through.
	if got := agent.approvalGate().Default; got != approval.ActionAllow {
		t.Fatalf("the gate in force says %q after the wheel said allow", got)
	}
	if got := agent.ResolvedApprovalPosture(); got != PostureAllow {
		t.Fatalf("resolved %q, want allow", got)
	}
	meta, err := LoadMeta(dir)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.Approval != PostureAllow {
		t.Fatalf("meta.json says %q, want allow — the wheel did not reach the folder", meta.Approval)
	}

	// THE GUARDIAN IS PART OF THE WORD. `guardian` stands it up whatever the
	// launch said, and `ask` stands it down again.
	if err := agent.SetApprovalPosture(PostureGuardian); err != nil {
		t.Fatalf("guardian was refused: %v", err)
	}
	if !agent.guardianOn() {
		t.Fatal("the guardian posture left the guardian down")
	}
	if err := agent.SetApprovalPosture(PostureAsk); err != nil {
		t.Fatalf("ask was refused: %v", err)
	}
	if agent.guardianOn() {
		t.Fatal("the ask posture left the guardian standing in")
	}

	// A WORD THAT IS NOT A POSTURE MOVES NOTHING AND NAMES THE FIVE.
	err = agent.SetApprovalPosture("wide open")
	if err == nil {
		t.Fatal("a word that is not a posture was accepted")
	}
	for _, word := range ApprovalPostures {
		if !strings.Contains(err.Error(), word) {
			t.Fatalf("the refusal does not name %q: %v", word, err)
		}
	}
	if got := agent.ApprovalPosture(); got != PostureAsk {
		t.Fatalf("a refused word moved the posture to %q", got)
	}

	// AND THE WAY BACK IS THE WHOLE POINT: a second session on the same folder
	// is handed the word the first one left, which is what the door does at
	// launch (cmd/codeaf's v3SavedApproval).
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	meta, _ = LoadMeta(dir)
	second, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "session.jsonl")
		config.Place = Place{Dir: dir}
		config.ApprovalGate = gate
	})
	if err := second.SetApprovalPosture(meta.Approval); err != nil {
		t.Fatalf("the saved word was refused on resume: %v", err)
	}
	if got := second.ResolvedApprovalPosture(); got != PostureAsk {
		t.Fatalf("the reopened conversation resolves %q, want the ask it was left at", got)
	}
}

// AUTO IS A CHOICE AND IS WRITTEN DOWN AS ONE: it resolves through to the rows,
// and it silences a launch flag that would otherwise still speak.
func TestAutoHandsTheConversationBackToTheRowsAndOutranksTheFlag(t *testing.T) {
	dir := t.TempDir()
	gate := &fakeApprovalGate{standing: PostureGuardian}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "session.jsonl")
		config.Place = Place{Dir: dir}
		config.ApprovalGate = gate
		config.ApprovalPosture = PostureAllow // --yolo
	})
	if got := agent.ResolvedApprovalPosture(); got != PostureAllow {
		t.Fatalf("an untouched --yolo conversation resolves %q, want the flag's allow", got)
	}
	if err := agent.SetApprovalPosture(PostureAuto); err != nil {
		t.Fatalf("auto was refused: %v", err)
	}
	if got := gate.built; len(got) != 1 || got[0] != "" {
		t.Fatalf("auto built %q, want the rows exactly as they stand (an empty posture)", got)
	}
	if got := agent.ResolvedApprovalPosture(); got != PostureGuardian {
		t.Fatalf("after auto the conversation resolves %q, want the rows' %q", got, PostureGuardian)
	}
	if meta, _ := LoadMeta(dir); meta.Approval != PostureAuto {
		t.Fatalf("meta.json says %q, want auto written down", meta.Approval)
	}
}

// A REBUILD THAT FAILS MOVES NOTHING: not the gate, not the word, not the file.
func TestARebuildThatFailsLeavesTheGateAndTheWordAlone(t *testing.T) {
	dir := t.TempDir()
	gate := &fakeApprovalGate{standing: PostureAsk, fail: errors.New("the rows do not read back")}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "session.jsonl")
		config.Place = Place{Dir: dir}
		config.ApprovalGate = gate
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionPrompt}
	})
	if err := agent.SetApprovalPosture(PostureAllow); err == nil {
		t.Fatal("a rebuild that failed was reported as a move")
	}
	if got := agent.approvalGate().Default; got != approval.ActionPrompt {
		t.Fatalf("the gate moved to %q on a failed rebuild", got)
	}
	if got := agent.ApprovalPosture(); got != "" {
		t.Fatalf("the word moved to %q on a failed rebuild", got)
	}
}

// A SESSION WITH NO DOOR HAS NO DIAL, and says so rather than storing a word
// that would move nothing.
func TestASessionWithNoGateRefusesThePostureAndAdmitsIt(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if agent.ApprovalDial() {
		t.Fatal("a session with no gate claims a dial")
	}
	if err := agent.SetApprovalPosture(PostureAllow); !errors.Is(err, errNoApprovalDial) {
		t.Fatalf("got %v, want the no-dial refusal", err)
	}
	if got := agent.ResolvedApprovalPosture(); got != "" {
		t.Fatalf("a session with no gate resolves %q, want nothing", got)
	}
}

// THE REBUILD AFTER A BANKED RULE KEEPS THE POSTURE IN FORCE — the
// conversation's word where there is one, the launch's flag where there is not.
func TestRebuildingTheGateKeepsThePostureInForce(t *testing.T) {
	gate := &fakeApprovalGate{standing: PostureAsk}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ApprovalGate = gate
		config.ApprovalPosture = PostureAllow
	})
	if err := agent.RebuildApprovalGate(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if got := gate.built; len(got) != 1 || got[0] != PostureAllow {
		t.Fatalf("an untouched --yolo conversation rebuilt %q, want the flag's allow", got)
	}
	if err := agent.SetApprovalPosture(PostureGuardian); err != nil {
		t.Fatalf("guardian: %v", err)
	}
	if err := agent.RebuildApprovalGate(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if got := gate.built[len(gate.built)-1]; got != PostureGuardian {
		t.Fatalf("a walked conversation rebuilt %q, want its own guardian", got)
	}
	if got := agent.approvalGate().Default; got != approval.ActionPrompt {
		t.Fatalf("the rebuilt gate says %q, want prompt", got)
	}
}

// THE STANDING WORD IS ABOUT THE NEXT CONVERSATION AND NOT THIS ONE. A draft
// on home says what a conversation nobody has touched would open at, so a pin
// made in the conversation behind home must not leak into it — the flag does,
// because the flag is the whole process's.
func TestTheStandingPostureIgnoresThisConversationsOwnPin(t *testing.T) {
	dir := t.TempDir()
	gate := &fakeApprovalGate{standing: PostureGuardian}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "session.jsonl")
		config.Place = Place{Dir: dir}
		config.ApprovalGate = gate
	})
	if got := agent.StandingApprovalPosture(); got != PostureGuardian {
		t.Fatalf("an untouched install stands at %q, want the rows' %q", got, PostureGuardian)
	}
	if err := agent.SetApprovalPosture(PostureAllow); err != nil {
		t.Fatalf("allow was refused: %v", err)
	}
	if got := agent.StandingApprovalPosture(); got != PostureGuardian {
		t.Fatalf("this conversation's own pin leaked into the standing word: %q", got)
	}
	flagged, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
		config.ApprovalGate = gate
		config.ApprovalPosture = PostureAllow // --yolo
	})
	if got := flagged.StandingApprovalPosture(); got != PostureAllow {
		t.Fatalf("under --yolo the next conversation stands at %q, want allow", got)
	}
	bare, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	if got := bare.StandingApprovalPosture(); got != "" {
		t.Fatalf("a session with no door stands at %q, want nothing", got)
	}
}
