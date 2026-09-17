package effort

import "testing"

// ── the ladder ──────────────────────────────────────────────────────────────

func TestParseTakesTheFiveRungsAndAutoAndNothingElse(t *testing.T) {
	for _, ok := range []struct {
		in   string
		want Rung
	}{
		{"auto", None}, {" AUTO ", None}, {"", None}, {"off", None}, {"OFF", None}, {" high ", High},
		{"low", Low}, {"medium", Medium}, {"XHIGH", XHigh}, {"max", Max},
	} {
		got, valid := Parse(ok.in)
		if !valid || got != ok.want {
			t.Fatalf("Parse(%q) = %q, %v; want %q, true", ok.in, got, valid, ok.want)
		}
	}
	// A near-miss is refused rather than quietly downgraded. Every one of these
	// is a word somebody would reasonably type for a rung that exists, which is
	// exactly why a silent fallback would hide the typo forever.
	for _, bad := range []string{"none", "highest", "xtra", "maximum", "x-high", "1", "minimal"} {
		if _, valid := Parse(bad); valid {
			t.Fatalf("Parse(%q) was accepted; a word that is not a rung must be refused", bad)
		}
	}
}

// THE LADDER IS FIVE RUNGS AND ABSENCE. This pins the shape the whole feature
// is built on: a sixth rung added without displacing one moves every surface
// that draws the ladder and every setting that stores it.
func TestTheLadderIsFiveRungsAndAbsenceIsNotOneOfThem(t *testing.T) {
	if len(Rungs) != 5 {
		t.Fatalf("the ladder has %d rungs: %v — five is the whole design", len(Rungs), Rungs)
	}
	if None.Valid() {
		t.Fatal("None reports itself as a rung; it is absence, and a surface asking must be told no")
	}
	for _, rung := range Rungs {
		if !rung.Valid() {
			t.Fatalf("%q is in Rungs and reports itself invalid", rung)
		}
	}
	if Ship != None {
		t.Fatalf("the shipped default is %q; the default must leave reasoning to the provider", Ship)
	}
}

// ── the resolver ────────────────────────────────────────────────────────────

// EVERY SCOPE IN ORDER, EACH ONE PROVED BY BEING THE ONLY ONE THAT SPOKE.
//
// The table walks the precedence down a rung at a time: the same call, with one
// more scope silent each row, and a different answer every time. A resolver that
// read them in the wrong order passes no row of this but the first.
func TestThePrecedenceRunsTurnConversationTaskRoleDefault(t *testing.T) {
	for _, want := range []struct {
		what  string
		scope Scope
		rung  Rung
	}{
		{
			what: "the turn beats everything under it",
			scope: Scope{Turn: Low, Conversation: Medium, Task: High,
				Role: RoleChat, Default: Max},
			rung: Low,
		},
		{
			what: "the conversation beats the work and the default",
			scope: Scope{Conversation: Medium, Task: High,
				Role: RoleChat, Default: Max},
			rung: Medium,
		},
		{
			what:  "the work beats the role and the default",
			scope: Scope{Task: High, Role: RoleErrand, Default: Max},
			rung:  High,
		},
		{
			what:  "the role decides when nothing more specific spoke",
			scope: Scope{Role: RoleErrand, Default: Max},
			rung:  None,
		},
		{
			what:  "and the install's default is the answer to and otherwise",
			scope: Scope{Role: RoleChat, Default: Max},
			rung:  Max,
		},
	} {
		t.Run(want.what, func(t *testing.T) {
			if got := Resolve(want.scope); got != want.rung {
				t.Fatalf("Resolve(%+v) = %q, want %q — %s", want.scope, got, want.rung, want.what)
			}
		})
	}
}

// THE TWO ROLES THAT ANSWER FOR THEMSELVES, AND THE FOUR THAT DO NOT.
//
// An errand asks for nothing at all: the person's dial is not spent on naming
// their own conversation. A work seat — a task worker on the bash belt — asks
// for low: the belt's one action per response spends a deep answer again on
// every round, so the seat holds a cheap answer of its own. Everybody else — a
// turn, the work handed out, a standing firing, the sentinel in front of it —
// gets what was configured, and on an install that configured NOTHING that is
// nothing.
//
// The two rows that moved here are standing and sentinel. They used to hold a
// floor of `low`, which was a rung this harness chose for a model it knew
// nothing about; what keeps a firing from inheriting a conversation's depth is
// now that the conversation's dial does not reach it at all (internal/session's
// standing_run.go sets DefaultEffort to None).
func TestOnlyAnErrandAnswersForItselfAndTheRestTakeWhatWasConfigured(t *testing.T) {
	for _, want := range []struct {
		role Role
		rung Rung
	}{
		{RoleChat, Max},
		{RoleWorker, Max},
		{RoleErrand, None},
		{RoleWork, Low},
		{RoleStanding, Max},
		{RoleSentinel, Max},
	} {
		if got := Resolve(Scope{Role: want.role, Default: Max}); got != want.rung {
			t.Fatalf("with the install dialled to max, %q resolves to %q, want %q", want.role, got, want.rung)
		}
	}
	// And with nothing dialled anywhere, every one of them is absence — no
	// reasoning field on the wire, whoever the call is for. The work seat is
	// not among them: its floor answers low on a silent install too, which is
	// the next test's business.
	for _, role := range []Role{RoleChat, RoleWorker, RoleErrand, RoleStanding, RoleSentinel} {
		if got := Resolve(Scope{Role: role}); got != None {
			t.Fatalf("with nothing configured, %q resolves to %q, want absence", role, got)
		}
	}
	// A rung set closer to the work still wins over the errand's silence, which
	// is what makes a deliberately deep standing item possible at all.
	if got := Resolve(Scope{Task: Max, Role: RoleErrand, Default: Low}); got != Max {
		t.Fatalf("a rung set on the work resolved to %q, want max — the role is a fallback, not a cap", got)
	}
}

// THE WORK SEAT, PROVED FROM BOTH SIDES.
//
// A task worker on the bash belt thinks at low when nothing above the role
// spoke — and nothing above it means nothing: the install's default is the
// answer to "and otherwise?", and the role spoke before the default was asked.
// What does lift the seat is a rung closer to the call, one scope at a time,
// exactly as it does for every other role. The ladder is unchanged; only the
// role's own answer is new.
func TestTheWorkSeatAnswersLowAndARungAboveItStillWins(t *testing.T) {
	if got := Resolve(Scope{Role: RoleWork}); got != Low {
		t.Fatalf("the work seat on a silent install resolves to %q, want low", got)
	}
	if got := Resolve(Scope{Role: RoleWork, Default: Max}); got != Low {
		t.Fatalf("the work seat over an install dialled to max resolves to %q, want low", got)
	}
	for _, said := range []struct {
		what  string
		scope Scope
	}{
		{"the turn", Scope{Turn: Max, Role: RoleWork, Default: Low}},
		{"the conversation", Scope{Conversation: Max, Role: RoleWork, Default: Low}},
		{"the task", Scope{Task: Max, Role: RoleWork, Default: Low}},
	} {
		if got := Resolve(said.scope); got != Max {
			t.Fatalf("with %s set, the work seat resolved to %q, want max", said.what, got)
		}
	}
}

// A scope that said nothing everywhere is absence, not the bottom of the
// ladder: an install nobody has configured must send no reasoning field, which
// is the one shape that leaves a request exactly what it was.
func TestASilentScopeResolvesToAbsence(t *testing.T) {
	if got := Resolve(Scope{}); got != None {
		t.Fatalf("Resolve(Scope{}) = %q, want absence", got)
	}
	// A word nobody in this package would produce is treated as silence rather
	// than obeyed, at every scope. The parse door refuses it; this is the second
	// lock, for a value that arrived from an older file on disk.
	junk := Rung("deepest")
	if got := Resolve(Scope{Turn: junk, Conversation: junk, Task: junk, Default: junk}); got != None {
		t.Fatalf("a value off the ladder resolved to %q, want absence", got)
	}
}
