package effort

import "testing"

// ── the ladder ──────────────────────────────────────────────────────────────

func TestParseTakesTheFiveRungsAndOffAndNothingElse(t *testing.T) {
	for _, ok := range []struct {
		in   string
		want Rung
	}{
		{"", None}, {"off", None}, {"OFF", None}, {" high ", High},
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
	if Ship != High {
		t.Fatalf("the shipped default is %q; the manual and the settings row both say high", Ship)
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
			scope: Scope{Task: High, Role: RoleSentinel, Default: Max},
			rung:  High,
		},
		{
			what:  "the role decides when nothing more specific spoke",
			scope: Scope{Role: RoleSentinel, Default: Max},
			rung:  Low,
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

// THE THREE ROLES THAT ANSWER FOR THEMSELVES, AND THE TWO THAT DO NOT.
//
// A standing firing and its sentinel run cheap however high the install is
// dialled, because they run unattended and forever. An errand asks for nothing
// at all. A person's turn and the work they hand out get what they configured.
func TestTheRoleFloorsAreTheOnesTheInstallDefaultCannotRaise(t *testing.T) {
	for _, want := range []struct {
		role Role
		rung Rung
	}{
		{RoleChat, Max},
		{RoleWorker, Max},
		{RoleErrand, None},
		{RoleStanding, Low},
		{RoleSentinel, Low},
	} {
		if got := Resolve(Scope{Role: want.role, Default: Max}); got != want.rung {
			t.Fatalf("with the install dialled to max, %q resolves to %q, want %q", want.role, got, want.rung)
		}
	}
	// And a role floor is a FLOOR AND NOT A CEILING: something set closer to the
	// work still wins, which is what makes a deliberately deep standing item
	// possible at all.
	if got := Resolve(Scope{Task: Max, Role: RoleSentinel, Default: Low}); got != Max {
		t.Fatalf("a rung set on the work resolved to %q, want max — the role is a fallback, not a cap", got)
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
