package subharness

import (
	"strings"
	"testing"
)

// sample is a harness with one of nearly everything in it, used wherever a test
// needs a program that is valid rather than one that is interesting.
func sample() Harness {
	return Harness{
		Name:        "triage-flake",
		Description: "chase a flaky test to a fix",
		Author:      "test",
		Tools:       []string{"read", "grep", "bash"},
		Verify:      RungLoop,
		Dynamism:    DynWidth,
		Cap:         4,
		Program: []Node{
			{ID: "start", Kind: KindTrigger, On: TriggerWatch, Spec: "bench/nightly.log"},
			{ID: "name-it", Kind: KindAgentLoop, Prompt: "name the test that failed", Tools: []string{"read", "grep"}},
			{ID: "pick", Kind: KindBranch, Cases: []Case{{
				When:  "contains flaky",
				Steps: []Node{{ID: "rerun", Kind: KindToolCall, Tool: "bash", Args: map[string]any{"command": "go test -count 20"}}},
			}}, Else: []Node{{ID: "explain", Kind: KindAgentLoop, Prompt: "say why it is not flaky"}}},
			{ID: "fix", Kind: KindLoopUntil, Until: "ok", Max: 3, Steps: []Node{
				{ID: "patch", Kind: KindAgentLoop, Prompt: "fix the test", Tools: []string{"read", "bash"}},
				{ID: "check", Kind: KindVerify, Rung: RungLoop, Check: "go test ./..."},
			}},
			{ID: "land", Kind: KindHumanGate, Prompt: "land the fix?", Escalate: true},
		},
	}
}

func TestValidateAcceptsAWholeProgram(t *testing.T) {
	harness := sample()
	Clamp(&harness)
	if err := Validate(&harness); err != nil {
		t.Fatalf("a valid harness was refused: %v", err)
	}
}

func TestValidateRefusals(t *testing.T) {
	for _, one := range []struct {
		name  string
		build func(*Harness)
		want  string
	}{
		{"an unknown kind", func(h *Harness) { h.Program[1].Kind = "agent.think" }, "not a node kind"},
		{"a kind above the dynamism rung", func(h *Harness) {
			h.Dynamism = DynFixed
		}, "needs dynamism branch"},
		{"a tool off the whitelist", func(h *Harness) {
			h.Program[1].Tools = []string{"curl"}
		}, "not in this harness's tools"},
		{"a verify above the harness's ladder", func(h *Harness) {
			h.Verify = RungSchema
		}, "above this harness's ladder"},
		{"two steps with one id", func(h *Harness) { h.Program[1].ID = "land" }, "both called"},
		{"a step with no id", func(h *Harness) { h.Program[1].ID = "" }, "has no id"},
		{"a trigger below the top", func(h *Harness) {
			h.Program = append(h.Program, Node{ID: "late", Kind: KindTrigger, On: TriggerHosted})
		}, "only be the first step"},
		{"a condition nobody can evaluate", func(h *Harness) {
			h.Program[2].Cases[0].When = "smells like flake"
		}, "is not a condition"},
		{"a regular expression that does not compile", func(h *Harness) {
			h.Program[2].Cases[0].When = "matches ([a-"
		}, "not a regular expression"},
		{"a self-call", func(h *Harness) {
			h.Dynamism = DynRecursive
			h.Program = append(h.Program, Node{ID: "again", Kind: KindSubharnessCall, Call: "triage-flake"})
		}, "calls itself"},
		{"nested steps on a kind that cannot run them", func(h *Harness) {
			h.Program[1].Steps = []Node{{ID: "orphan", Kind: KindAgentLoop, Prompt: "hi"}}
		}, "carries nested steps"},
		{"a lane count over the harness's own cap", func(h *Harness) {
			h.Cap = 2
			h.Program = append(h.Program, Node{ID: "wide", Kind: KindParallelSplit, Lanes: []Lane{
				{Name: "a", Steps: []Node{{ID: "l1", Kind: KindAgentLoop, Prompt: "x"}}},
				{Name: "b", Steps: []Node{{ID: "l2", Kind: KindAgentLoop, Prompt: "x"}}},
				{Name: "c", Steps: []Node{{ID: "l3", Kind: KindAgentLoop, Prompt: "x"}}},
			}})
		}, "over this harness's cap"},
		{"an empty program", func(h *Harness) { h.Program = nil }, "at least one step"},
		{"a name that climbs out of the directory", func(h *Harness) { h.Name = "../etc/passwd" }, "contains .."},
		{"a name a filename could not hold", func(h *Harness) { h.Name = "Triage Flake" }, "may only hold"},
	} {
		t.Run(one.name, func(t *testing.T) {
			harness := sample()
			one.build(&harness)
			Clamp(&harness)
			err := Validate(&harness)
			if err == nil {
				t.Fatalf("%s was accepted", one.name)
			}
			if !strings.Contains(err.Error(), one.want) {
				t.Fatalf("the refusal does not say why: %v (want %q)", err, one.want)
			}
		})
	}
}

func TestClampSettlesTheSilentNumbers(t *testing.T) {
	harness := Harness{
		Name: "quiet", Tools: []string{" read ", ""},
		Program: []Node{
			{ID: "work", Kind: KindAgentLoop, Prompt: "do it", Tools: []string{"read"}},
		},
	}
	Clamp(&harness)
	if err := Validate(&harness); err != nil {
		t.Fatalf("the clamped harness is invalid: %v", err)
	}
	if harness.Verify != RungAccept || harness.Dynamism != DynFixed {
		t.Fatalf("the silent ladders did not settle: %s / %s", harness.Verify, harness.Dynamism)
	}
	if harness.Program[0].MaxTurns != DefaultTurns {
		t.Fatalf("turns = %d, want the default %d", harness.Program[0].MaxTurns, DefaultTurns)
	}
	if len(harness.Tools) != 1 || harness.Tools[0] != "read" {
		t.Fatalf("the whitelist was not tidied: %q", harness.Tools)
	}
}

func TestClampHoldsTheCeilings(t *testing.T) {
	harness := sample()
	harness.Program[1].MaxTurns = 1000
	harness.Program[3].Max = 1000
	Clamp(&harness)
	if got := harness.Program[1].MaxTurns; got != MaxTurns {
		t.Fatalf("turns = %d, want the ceiling %d", got, MaxTurns)
	}
	if got := harness.Program[3].Max; got != MaxRounds {
		t.Fatalf("rounds = %d, want the ceiling %d", got, MaxRounds)
	}
}

func TestAnEmptyWhitelistIsNoTools(t *testing.T) {
	harness := Harness{
		Name:    "bare",
		Program: []Node{{ID: "work", Kind: KindToolCall, Tool: "bash"}},
	}
	Clamp(&harness)
	err := Validate(&harness)
	if err == nil || !strings.Contains(err.Error(), "no tool whitelist") {
		t.Fatalf("a harness with no whitelist was handed a tool: %v", err)
	}
}

func TestDecodeReadsCommentsAndTrailingCommas(t *testing.T) {
	file := []byte(`{
  # what this is for
  "name": "annotated",
  "version": 2,
  "tools": ["read",],  // the hash in "read #1" is not a comment
  "program": [
    {"id": "work", "kind": "agent.loop", "prompt": "read #1 and say what it does", "tools": ["read"]},
  ],
}`)
	harness, err := Decode(file)
	if err != nil {
		t.Fatalf("an annotated file did not load: %v", err)
	}
	if harness.Name != "annotated" || harness.Version != 2 {
		t.Fatalf("identity did not survive: %+v", harness)
	}
	if got := harness.Program[0].Prompt; got != "read #1 and say what it does" {
		t.Fatalf("a # inside a string was eaten: %q", got)
	}
}

func TestEncodeDecodeRoundTrips(t *testing.T) {
	harness := sample()
	Clamp(&harness)
	data, err := Encode(harness)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	back, err := Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := Validate(&back); err != nil {
		t.Fatalf("the round trip broke the program: %v", err)
	}
	if len(back.Program) != len(harness.Program) || back.Program[3].Steps[1].Check != "go test ./..." {
		t.Fatalf("the round trip lost the nesting: %+v", back.Program)
	}
	// Two encodes of one harness must be identical bytes, or a version bump is
	// unreadable in a diff.
	again, _ := Encode(back)
	if string(again) != string(data) {
		t.Fatalf("encoding is not stable")
	}
}

func TestConditions(t *testing.T) {
	state := State{Last: "the test is Flaky", OK: false}
	for _, one := range []struct {
		condition string
		want      bool
	}{
		{"always", true},
		{"never", false},
		{"ok", false},
		{"failed", true},
		{"empty", false},
		{"nonempty", true},
		{"contains flaky", true},
		{"contains passing", false},
		{"matches [Ff]laky$", true},
		{"equals the test is flaky", true},
	} {
		got, err := Match(one.condition, state)
		if err != nil {
			t.Fatalf("%q: %v", one.condition, err)
		}
		if got != one.want {
			t.Fatalf("%q = %v, want %v", one.condition, got, one.want)
		}
	}
	if _, err := Match("looks fine", state); err == nil {
		t.Fatalf("a condition nobody defined evaluated anyway")
	}
}
