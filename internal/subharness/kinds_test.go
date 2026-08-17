package subharness

import (
	"strconv"
	"strings"
	"testing"
)

// The registry is a library-in-binary: ten kinds, each one a name for
// machinery this binary already has. A kind that quietly disappears or arrives
// changes what every saved harness means, so the list itself is asserted.
func TestTheRegistryHoldsExactlyTheDeclaredKinds(t *testing.T) {
	want := []string{
		KindAgentLoop, KindBranch, KindHumanGate, KindLoopUntil,
		KindParallelJoin, KindParallelSplit, KindSubharnessCall,
		KindToolCall, KindTrigger, KindVerify,
	}
	got := kindNames()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("registry is %v, want %v", got, want)
	}
	for _, kind := range Kinds() {
		if kind.Desc == "" {
			t.Fatalf("kind %q has no description", kind.Name)
		}
		if DynRung(kind.MinDyn) < 0 {
			t.Fatalf("kind %q wants a rung that is not on the ladder: %q", kind.Name, kind.MinDyn)
		}
		if kind.Valid == nil {
			t.Fatalf("kind %q has no field law", kind.Name)
		}
	}
}

// Every kind's field law, one table. Each case is the fields of one node and
// what the kind should say about them.
func TestEachKindHoldsItsOwnFields(t *testing.T) {
	cases := []struct {
		kind   string
		fields Fields
		want   string // "" means the fields are legal
	}{
		// agent.loop: a brief is the only thing it cannot run without.
		{KindAgentLoop, Fields{"brief": "do the thing"}, ""},
		{KindAgentLoop, Fields{"brief": "b", "model": "work", "tools": "read", "max_turns": "64"}, ""},
		{KindAgentLoop, Fields{"model": "work"}, `field "brief" is required`},
		{KindAgentLoop, Fields{"brief": "b", "max_turns": strconv.Itoa(MaxTurns + 1)}, "past the cap of 64"},
		{KindAgentLoop, Fields{"brief": "b", "max_turns": "0"}, "below 1"},
		{KindAgentLoop, Fields{"brief": "b", "max_turns": "many"}, `"many" is not an integer`},
		{KindAgentLoop, Fields{"brief": "b", "turns": "3"}, `unknown field "turns"`},

		// tool.call
		{KindToolCall, Fields{"tool": "read", "args": "README.md"}, ""},
		{KindToolCall, Fields{"args": "README.md"}, `field "tool" is required`},

		// verify: the ladder word is a closed set.
		{KindVerify, Fields{}, ""},
		{KindVerify, Fields{"ladder": VerifyRederive, "check": "derive it twice"}, ""},
		{KindVerify, Fields{"ladder": "vibes"}, `"vibes" is not one of`},

		// human.gate
		{KindHumanGate, Fields{"ask": "ship it?"}, ""},
		{KindHumanGate, Fields{}, `field "ask" is required`},

		// trigger: source is a closed set, and one of its words carries a
		// payload the others do not.
		{KindTrigger, Fields{"source": TriggerHosted}, ""},
		{KindTrigger, Fields{"source": TriggerWatch}, ""},
		{KindTrigger, Fields{"source": TriggerCommand, "command": "git status --short"}, ""},
		{KindTrigger, Fields{"source": TriggerCommand}, `required when source is source.command`},
		{KindTrigger, Fields{"source": "cron"}, `"cron" is not one of`},
		{KindTrigger, Fields{}, `field "source" is required`},

		// branch
		{KindBranch, Fields{"when": "the test still fails"}, ""},
		{KindBranch, Fields{}, `field "when" is required`},

		// loop.until
		{KindLoopUntil, Fields{"until": "it passes"}, ""},
		{KindLoopUntil, Fields{"until": "it passes", "max_rounds": strconv.Itoa(MaxRounds)}, ""},
		{KindLoopUntil, Fields{"until": "it passes", "max_rounds": strconv.Itoa(MaxRounds + 1)}, "past the cap of 8"},
		{KindLoopUntil, Fields{"max_rounds": "2"}, `field "until" is required`},

		// parallel.split / join
		{KindParallelSplit, Fields{"width": "4", "over": "the failing tests"}, ""},
		{KindParallelSplit, Fields{"over": "the failing tests"}, `field "width" is required`},
		{KindParallelSplit, Fields{"width": strconv.Itoa(MaxWidth + 1)}, "past the cap of 16"},
		{KindParallelJoin, Fields{}, ""},
		{KindParallelJoin, Fields{"mode": JoinAny}, ""},
		{KindParallelJoin, Fields{"mode": "some"}, `"some" is not one of`},

		// subharness.call
		{KindSubharnessCall, Fields{"name": "reviewer"}, ""},
		{KindSubharnessCall, Fields{"name": "reviewer", "version": "3"}, ""},
		{KindSubharnessCall, Fields{"version": "3"}, `field "name" is required`},
		{KindSubharnessCall, Fields{"name": "reviewer", "version": strconv.Itoa(MaxVersion + 1)}, "past the cap"},
	}
	for _, c := range cases {
		kind, found := Lookup(c.kind)
		if !found {
			t.Fatalf("kind %q is not registered", c.kind)
		}
		err := kind.Valid(c.fields)
		switch {
		case c.want == "" && err != nil:
			t.Fatalf("%s %v: refused legal fields: %v", c.kind, c.fields, err)
		case c.want != "" && err == nil:
			t.Fatalf("%s %v: accepted fields that should say %q", c.kind, c.fields, c.want)
		case c.want != "" && !strings.Contains(err.Error(), c.want):
			t.Fatalf("%s %v: refused with %q, want %q", c.kind, c.fields, err, c.want)
		}
	}
}

// The rungs a kind needs are the enforcement point for the whole autonomy
// spectrum, so the mapping is asserted rather than left to the kind files.
func TestEachKindNeedsTheRungItsShapeImplies(t *testing.T) {
	want := map[string]string{
		KindAgentLoop:      DynFixed,
		KindToolCall:       DynFixed,
		KindVerify:         DynFixed,
		KindHumanGate:      DynFixed,
		KindTrigger:        DynFixed,
		KindBranch:         DynBranch,
		KindLoopUntil:      DynBranch,
		KindParallelSplit:  DynWidth,
		KindParallelJoin:   DynWidth,
		KindSubharnessCall: DynRecursive,
	}
	for name, rung := range want {
		kind, found := Lookup(name)
		if !found {
			t.Fatalf("kind %q is not registered", name)
		}
		if kind.MinDyn != rung {
			t.Fatalf("kind %q needs %q, want %q", name, kind.MinDyn, rung)
		}
	}
}

func TestBothLaddersReadInOrder(t *testing.T) {
	verify := VerifyLadder()
	if len(verify) != 8 || verify[0] != VerifyAccept || verify[len(verify)-1] != VerifyHuman {
		t.Fatalf("verification ladder is %v", verify)
	}
	for height := 1; height < len(verify); height++ {
		if VerifyRung(verify[height]) <= VerifyRung(verify[height-1]) {
			t.Fatalf("verification ladder is not ordered at %q", verify[height])
		}
	}
	dyn := DynLadder()
	if len(dyn) != 6 || dyn[0] != DynFixed || dyn[len(dyn)-1] != DynSelfmod {
		t.Fatalf("dynamism ladder is %v", dyn)
	}
	for height := 1; height < len(dyn); height++ {
		if DynRung(dyn[height]) <= DynRung(dyn[height-1]) {
			t.Fatalf("dynamism ladder is not ordered at %q", dyn[height])
		}
	}
	if VerifyRung("rederived") != -1 || DynRung("dynamic") != -1 {
		t.Fatal("a word that is not on a ladder read as a rung")
	}
}

// The ladders are returned as copies. A caller that sorts the slice it was
// handed must not be able to reorder the ladder for everyone else.
func TestALadderCannotBeEditedByItsReader(t *testing.T) {
	got := DynLadder()
	got[0] = "anything"
	if DynLadder()[0] != DynFixed {
		t.Fatal("editing a returned ladder changed the ladder")
	}
}

func TestFieldsReadIntegersWithADefaultAndTrimWhitespace(t *testing.T) {
	f := Fields{"max_rounds": " 3 ", "brief": "  do the thing  ", "bad": "three"}
	if got := f.Int("max_rounds", DefaultRounds); got != 3 {
		t.Fatalf("max_rounds read as %d", got)
	}
	if got := f.Int("absent", DefaultRounds); got != DefaultRounds {
		t.Fatalf("an absent integer read as %d, want the default %d", got, DefaultRounds)
	}
	if got := f.Int("bad", DefaultRounds); got != DefaultRounds {
		t.Fatalf("an unparseable integer read as %d, want the default %d", got, DefaultRounds)
	}
	if got := f.Get("brief"); got != "do the thing" {
		t.Fatalf("Get returned %q", got)
	}
}

func TestRegisteringAKindTwiceIsRefusedLoudly(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("registering a kind twice was allowed")
		}
	}()
	Register(Kind{Name: KindToolCall, MinDyn: DynFixed, Valid: def()})
}
