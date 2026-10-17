package factory

import (
	"testing"
	"time"
)

var tokenNow = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func tokenItem() Item {
	return Item{
		Repo: "acme/api", Num: 12, Title: "Fix the webhook retry", Author: "@Dana",
		Assignee: "Erin", State: StateRunning, Created: tokenNow.Add(-30 * time.Hour),
		Labels: []string{"Bug", "area:billing", "good first issue"},
		Triage: Triage{Type: "bug", Size: "M", SizeFrom: SizeFromRead, TriagedAt: tokenNow.Add(-time.Hour), Area: "Webhooks"},
		Stream: &Stream{Cur: 1, Phases: []Phase{{Name: "plan", State: PhaseDone}, {Name: "Code", State: PhaseRunning}, {Name: "test", State: PhasePending}}},
	}
}

func TestTypedTokens(t *testing.T) {
	for _, tc := range []struct {
		q    string
		want bool
	}{
		{"label:bug", true}, {"label:BUG", true}, {"label:bu", false}, {"label:area:billing", true},
		{`label:"good first issue"`, true}, {`label:"good first"`, false}, {"label:bug label:area:billing", true},
		{"label:bug label:nope", false}, {"-label:nope", true}, {"-label:bug", false}, {`-label:"good first issue"`, false},
		{"assignee:@erin", true}, {"assignee:erin", true}, {"assignee:@er", false}, {"assignee:none", false},
		{"assignee:me", false}, {"area:webhooks", true}, {"area:webhook", false},
		{"step:code", true}, {"step:plan", false}, {"step:cod", false},
		{"age:>1d", true}, {"age:>1w", false}, {"age:<72h", true}, {"age:<24h", false},
		{"repo:acme/api", true}, {"repo:api", true}, {"repo:ap", false}, {"repo:web repo:api", true},
		{"repo:web repo:api bugs", true}, {"repo:web repo:db", false}, {"repo:api label:nope", false},
		{"author:dana", true}, {"author:@DANA", true}, {"author:dan", false},
		{"type:bug", true}, {"type:bu", false}, {"size:m", true}, {"size:S", false},
		{"#12", true}, {"#1", false}, {"#123", false}, {"api#12", true}, {"acme/api#12", true}, {"web#12", false},
		{"bugs mine", false}, {"webhook", true}, {"retry", true}, {"would:start", false},
	} {
		if got := MatchAt(tc.q, tokenNow)(tokenItem()); got != tc.want {
			t.Errorf("%q = %v, want %v", tc.q, got, tc.want)
		}
	}
}

func TestAssigneeViewerAndNone(t *testing.T) {
	it := tokenItem()
	if !MatchAt("assignee:@me", tokenNow, " @ERIN ")(it) || !MatchAt("assignee:me", tokenNow, "erin")(it) {
		t.Fatal("@me and me must be the viewer")
	}
	if MatchAt("assignee:me", tokenNow, "dana")(it) || MatchAt("assignee:me", tokenNow)(it) {
		t.Fatal("another or unknown viewer must not match")
	}
	free := Item{Author: "dana"}
	if !MatchAt("assignee:none", tokenNow)(free) || MatchAt("assignee:me", tokenNow)(free) {
		t.Fatal("none is nobody; an unknown viewer matches nothing")
	}
	if !MatchAt("assignee:me", tokenNow, "kim")(Item{Taker: "kim"}) {
		t.Fatal("Person() falls through to the taker")
	}
}

func TestMalformedTokensNeverFallBackToTheTitle(t *testing.T) {
	it := tokenItem()
	it.Title = "label: age:>x repo: assignee: step: size: area: type: author: label:"
	for _, q := range []string{
		"label:", "-label:", `label:""`, `label:"bug`, "assignee:", "assignee:@", "area:", "step:", "repo:",
		"author:", "type:", "size:", "age:", "age:1d", "age:>d", "age:>1m", "age:>x", "age:>99999d", "age:<-1h",
	} {
		if MatchAt(q, tokenNow)(it) {
			t.Errorf("malformed %q matched", q)
		}
	}
	if MatchAt("repo: repo:api", tokenNow)(it) {
		t.Error("a malformed repo term must not be rescued by a good one")
	}
}

func TestMissingFactsAreNotFabricated(t *testing.T) {
	var empty Item
	for _, q := range []string{"age:>1d", "age:<72h", "step:code", "area:x", "size:s", "type:bug", "label:x", "assignee:@x", "author:x", "repo:x", "#0"} {
		if MatchAt(q, tokenNow, "x")(empty) {
			t.Errorf("empty item matched %q", q)
		}
	}
	if !MatchAt("-label:x assignee:none", tokenNow)(empty) {
		t.Error("an unlabelled, unassigned item satisfies -label and none")
	}
	it := tokenItem()
	it.Stream.Cur = 9
	if MatchAt("step:code", tokenNow)(it) {
		t.Error("an out-of-range current step is no step")
	}
	it.Stream.Cur = -1
	if MatchAt("step:plan", tokenNow)(it) {
		t.Error("a negative current step is no step")
	}
	it.Stream = nil
	if MatchAt("step:plan", tokenNow)(it) {
		t.Error("no stream is no step")
	}
	future := tokenItem()
	future.Created = tokenNow.Add(time.Hour)
	if !MatchAt("age:<1h", tokenNow)(future) || MatchAt("age:>0h", tokenNow)(future) {
		t.Error("a future Created clamps to age zero")
	}
}

func TestMatchAtUsesTheHandedInClockOnly(t *testing.T) {
	it := tokenItem() // created 30h before tokenNow
	if !MatchAt("age:<2d", tokenNow)(it) || MatchAt("age:<2d", tokenNow.Add(48*time.Hour))(it) {
		t.Fatal("age must follow the clock handed in, not the wall clock")
	}
	at := MatchAt("age:>1d", tokenNow)
	if !at(it) || !at(it) {
		t.Fatal("a handed-in clock is stable across calls")
	}
}

func TestLexerQuotesAndPlainWordsPreserved(t *testing.T) {
	for _, tc := range []struct {
		q    string
		want []string
	}{
		{"  mine   cheap ", []string{"mine", "cheap"}},
		{`label:"a b" x`, []string{"label:a b", "x"}},
		{`-label:"a b"c`, []string{"-label:a bc"}},
		{`say "hi there"`, []string{"say", `"hi`, `there"`}},
		{`foo:"a b"`, []string{`foo:"a`, `b"`}},
	} {
		raw := lexQuery(tc.q)
		if len(raw) != len(tc.want) {
			t.Fatalf("%q lexed to %v, want %v", tc.q, raw, tc.want)
		}
		for i := range raw {
			if raw[i].text != tc.want[i] || raw[i].bad {
				t.Errorf("%q term %d = %+v, want %q", tc.q, i, raw[i], tc.want[i])
			}
		}
	}
	if raw := lexQuery(`label:"open`); len(raw) != 1 || !raw[0].bad {
		t.Fatalf("an unclosed quote must mark the term bad: %v", raw)
	}
}

func TestExistingWordsAndSubstringsUnchanged(t *testing.T) {
	it := tokenItem()
	it.Triage.Est = 1
	for q, want := range map[string]bool{
		"bugs": true, "cheap": true, "prs": false, "webhooks": true, "dana": true, "api": true,
		"acme": true, "unknown:thing": false, "label": false, "": true,
	} {
		if got := MatchAt(q, tokenNow)(it); got != want {
			t.Errorf("%q = %v, want %v", q, got, want)
		}
	}
}

func TestAssigneeNoneSentinelIsNotTheHandleNone(t *testing.T) {
	for _, tc := range []struct {
		q    string
		it   Item
		want bool
	}{
		{"assignee:@none", Item{Assignee: "none"}, true},
		{"assignee:@none", Item{}, false},
		{"assignee:@NONE", Item{Assignee: "None"}, true},
		{"assignee:none", Item{}, true},
		{"assignee:none", Item{Assignee: "none"}, false},
		{"assignee:none", Item{Assignee: "erin"}, false},
		{"author:none", Item{Author: "none"}, true},
		{"author:me", Item{Author: "me"}, true},
		{"author:me", Item{Author: "erin"}, false},
	} {
		if got := MatchAt(tc.q, tokenNow, "erin")(tc.it); got != tc.want {
			t.Errorf("%q on %+v = %v, want %v", tc.q, tc.it, got, tc.want)
		}
	}
	if !MatchAt("assignee:@me", tokenNow, "erin")(Item{Assignee: "erin"}) {
		t.Fatal("@me stays the viewer")
	}
}

func TestStepIsTheLiveCurrentPhaseNeverAFinishedJobsLastOne(t *testing.T) {
	stream := func(state PhaseState) *Stream {
		return &Stream{Cur: 1, Phases: []Phase{{Name: "code", State: PhaseDone}, {Name: "review", State: state}}}
	}
	for _, tc := range []struct {
		name  string
		state State
		ph    PhaseState
		want  bool
	}{
		{"landed parked on a done last phase", StateLanded, PhaseDone, false},
		{"shipped parked on a done last phase", StateShipped, PhaseDone, false},
		{"landed even if the phase still reads running", StateLanded, PhaseRunning, false},
		{"running on a running phase", StateRunning, PhaseRunning, true},
		{"live waiting proof", StateNeedsYou, PhaseWaiting, true},
		{"failed current phase", StateNeedsYou, PhaseFailed, true},
		{"running job whose phase is done", StateRunning, PhaseDone, false},
		{"running job whose phase is pending", StateRunning, PhasePending, false},
		{"queued job", StateQueued, PhaseRunning, false},
		{"dismissed job", StateDismissed, PhaseWaiting, false},
	} {
		it := Item{State: tc.state, Stream: stream(tc.ph)}
		if got := MatchAt("step:review", tokenNow)(it); got != tc.want {
			t.Errorf("%s: step:review = %v, want %v", tc.name, got, tc.want)
		}
		if MatchAt("step:code", tokenNow)(it) {
			t.Errorf("%s: a done earlier phase is not current", tc.name)
		}
	}
}

func TestRepoOrMalformedAfterValidAndUnclosedNegatives(t *testing.T) {
	it := tokenItem()
	for _, q := range []string{"repo:api repo:", "repo:api repo:web repo:", `-label:"x`, `-label:"bug`, `label:"bug mine`, `repo:api label:"bug`} {
		if MatchAt(q, tokenNow)(it) {
			t.Errorf("%q must match nothing", q)
		}
	}
}

func TestUnicodeCaseAndRefIDFallback(t *testing.T) {
	it := Item{ID: 12, Repo: "Acme/Api", Labels: []string{"ÉTÉ"}, State: StateRunning,
		Stream: &Stream{Cur: 0, Phases: []Phase{{Name: "Code", State: PhaseRunning}}}}
	for q, want := range map[string]bool{
		"label:été": true, "label:ÉTÉ": true, "-label:été": false, "step:CODE": true, "repo:ACME/API": true,
		"#12": true, "api#12": true, "other#12": false, "#13": false,
	} {
		if got := MatchAt(q, tokenNow)(it); got != want {
			t.Errorf("%q = %v, want %v", q, got, want)
		}
	}
	if !MatchAt("api#12", tokenNow)(Item{Num: 12, Repo: "other/api"}) {
		t.Error("a ref's repo is compared by short name, so other/api#12 is api#12")
	}
	if MatchAt("#5", tokenNow)(Item{Title: "learn c#5"}) {
		t.Error("#5 is a number, not a title substring")
	}
}

func TestHasWord(t *testing.T) {
	for _, tc := range []struct {
		q    string
		want bool
	}{
		{"is:dismissed", true}, {"IS:Dismissed bugs", true}, {"bugs is:dismissed", true},
		{`label:"x is:dismissed"`, false}, {`label:"x is:dismissed`, false}, {"is:dismissedx", false}, {"", false},
	} {
		if got := HasWord(tc.q, "is:dismissed"); got != tc.want {
			t.Errorf("HasWord(%q) = %v, want %v", tc.q, got, tc.want)
		}
	}
}
