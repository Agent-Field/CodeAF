package session

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/roles"
)

// THE BLOCK'S SHAPE IS OURS AND NOT THE MODEL'S. Whatever came back, what the
// surface draws — and what a person's enter appends to their own message — opens
// with [SpellOutOpening] and carries clauses marked "- ", because this text lands
// in somebody's draft and a person who has read one of these has to be able to
// read the next one at a glance.
func TestTheBlockIsRepairedIntoItsOneShape(t *testing.T) {
	for _, tc := range []struct {
		name, answer, want string
	}{
		{
			name:   "the shape it was asked for",
			answer: SpellOutOpening + "\n- email and password\n- an error state",
			want:   SpellOutOpening + "\n- email and password\n- an error state",
		},
		{
			name:   "another mark, and the opening said in the model's own words",
			answer: "Here is what I take it to mean:\n\n* email and password\n• an error state\n",
			want:   SpellOutOpening + "\n- email and password\n- an error state",
		},
		{
			name:   "a fence and a closing sentence around it",
			answer: "```\n" + SpellOutOpening + "\n- email and password\n```\n\nLet me know!",
			want:   SpellOutOpening + "\n- email and password",
		},
		{
			name:   "the clauses are held to the prompt's own limit",
			answer: "- one\n- two\n- three\n- four\n- five\n- six\n- seven\n- eight",
			want:   SpellOutOpening + "\n- one\n- two\n- three\n- four\n- five\n- six",
		},
	} {
		if got := cleanSpellOut(tc.answer); got != tc.want {
			t.Fatalf("%s:\ngot\n%q\nwanted\n%q", tc.name, got, tc.want)
		}
	}
}

// AN ANSWER WITH NO CLAUSES IN IT IS NO ANSWER. A model that replied with a
// paragraph, or that went and did the request instead of spelling it out, has
// handed back exactly the thing the call was made to avoid — and every failure
// of this call is spelled the same way, as nothing at all.
func TestAnAnswerWithNoClausesIsNoAnswer(t *testing.T) {
	for _, answer := range []string{
		"",
		"   \n\n  ",
		"Sure! I'll build you a login page. First I'll set up the routes.",
		SpellOutOpening,
		"```go\nfunc main() {}\n```",
	} {
		if got := cleanSpellOut(answer); got != "" {
			t.Fatalf("cleanSpellOut(%q) = %q, wanted nothing", answer, got)
		}
	}
}

// One long bullet is held to the cap rather than pasted whole into a draft.
func TestALongClauseIsHeldToItsCap(t *testing.T) {
	long := strings.Repeat("x", spellOutClauseLimit*2)
	got := cleanSpellOut("- " + long)
	if len(got) > len(SpellOutOpening)+spellOutClauseLimit+8 {
		t.Fatalf("a long clause was not capped: %d bytes", len(got))
	}
}

// THE EXPANSION IS A CHEAP CALL, and it resolves the way every other auxiliary
// call in this build resolves. A role that stopped being registered would fall
// back to the conversation's own model, which is the expensive one.
func TestTheExpansionIsRegisteredOnTheLowTier(t *testing.T) {
	tier, ok := roles.TierOf(spellOutRole)
	if !ok {
		t.Fatal("the expansion is not a registered role")
	}
	if tier != roles.TierLow {
		t.Fatalf("the expansion resolves on the %s tier", tier)
	}
}

// THE DISCIPLINE IS IN THE PROMPT, and it is the whole reason this is a model
// call rather than a template. Each of the three kinds of thing has its own
// handling and they may not blur: a person's own requirement quietly reworded,
// or a question asked about something nobody cares about, are the two failures
// this call exists to avoid.
func TestThePromptCarriesTheThreeKindsAndTheirDiscipline(t *testing.T) {
	for _, want := range []string{
		"SAID", "IMPLIED", "OPEN",
		"never repeat it, never improve it",
		"I'll pick X unless you say",
		"LOAD-BEARING",
		SpellOutOpening,
		"3 to 6 clauses",
	} {
		if !strings.Contains(spellOutPrompt, want) {
			t.Fatalf("the prompt no longer says %q", want)
		}
	}
}
