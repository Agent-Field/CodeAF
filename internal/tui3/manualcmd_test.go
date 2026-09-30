package tui3

import (
	"strings"
	"testing"
)

// manualSent runs one /manual form through the dispatch, the way the loop
// would, and hands back what the model was given and what the transcript says
// the person typed — the two halves [app.submitShown] keeps apart.
func manualSent(t *testing.T, a *app, fake *fakeAgent, line string) (sent, shown string) {
	t.Helper()
	before := len(fake.sent)
	typeLine(t, a, line)
	if len(fake.sent) != before+1 {
		t.Fatalf("%s sent %d messages, want one", line, len(fake.sent)-before)
	}
	for i := len(a.entries) - 1; i >= 0; i-- {
		if a.entries[i].kind == entryUser {
			return fake.sent[before], a.entries[i].text
		}
	}
	t.Fatalf("%s left no line of the person's in the transcript", line)
	return "", ""
}

// A QUESTION IS A TURN, since 2026-09-22: the model is handed the question with
// the manual named as where to answer from, and the transcript keeps what the
// person actually typed.
func TestManualCommandPutsTheQuestionToTheModelWithTheManualOpen(t *testing.T) {
	fake := &fakeAgent{model: "m"}
	a := newTestApp(fake)
	sent, shown := manualSent(t, a, fake, "/manual how do I change the effort level")
	if sent != manualQuestionLead+"how do I change the effort level" {
		t.Fatalf("the model was handed %q", sent)
	}
	if !strings.Contains(sent, "manual tool") || !strings.Contains(sent, "which page") {
		t.Fatalf("the question does not name the manual and ask for the page: %q", sent)
	}
	if shown != "/manual how do I change the effort level" {
		t.Fatalf("the transcript says %q, not what was typed", shown)
	}
	if !a.notices.retired("manual-answers") {
		t.Fatal("asking did not retire the tip that teaches the command")
	}
}

// A BARE /manual IS THE TOUR — what codeaf can do, from its own account —
// rather than a listing nobody asked the model for.
func TestABareManualAsksTheModelForTheTour(t *testing.T) {
	fake := &fakeAgent{model: "m"}
	a := newTestApp(fake)
	sent, shown := manualSent(t, a, fake, "/manual")
	if sent != manualTourAsk {
		t.Fatalf("the model was handed %q", sent)
	}
	if shown != "/manual" {
		t.Fatalf("the transcript says %q, not what was typed", shown)
	}
	if last := a.entries[len(a.entries)-1]; last.kind == entryNote {
		t.Fatalf("a bare /manual still prints a note: %q", last.text)
	}
}

// ON HOME THE QUESTION OPENS A CONVERSATION FIRST AND IS ASKED THERE. It used to
// be an answer echoed to home's line, and the answer — the pages, printed —
// went into the conversation behind home, where the person who typed it could
// see nothing happen at all (the owner met it, 2026-09-22).
func TestManualOnHomeOpensAConversationAndAsksThere(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.door("")
	goHome(t, a)
	if got := homeFate("manual", "how do I change the effort level"); got != fateNeedsChat {
		t.Fatalf("the drop-up says /manual %q on home", got)
	}
	typeLine(t, a, "/manual how do I change the effort level")
	if a.at(pageHome) {
		t.Fatal("the question did not open a conversation")
	}
	var fake *fakeAgent
	switch agent := a.agent.(type) {
	case *switchAgent:
		fake = agent.fakeAgent
	case *fakeAgent:
		fake = agent
	default:
		t.Fatalf("the conversation that opened runs on a %T", a.agent)
	}
	if len(fake.sent) == 0 || fake.sent[len(fake.sent)-1] != manualQuestionLead+"how do I change the effort level" {
		t.Fatalf("the new conversation was handed %q", fake.sent)
	}
	said := ""
	for i := len(a.entries) - 1; i >= 0; i-- {
		if a.entries[i].kind == entryUser {
			said = a.entries[i].text
			break
		}
	}
	if said != "/manual how do I change the effort level" {
		t.Fatalf("the transcript says %q, not what was typed on home", said)
	}
}

// THE /crew ROWS NAME THE THREE SEATS A PERSON CAN PIN, by the words the
// router uses for them, and every shortcut the command takes has a row.
func TestTheCrewRowsNameTheSeatsAndTheShortcuts(t *testing.T) {
	var said []string
	for _, row := range commands {
		if row.name == "crew" {
			said = append(said, row.args+" "+row.desc)
		}
	}
	text := strings.Join(said, "\n")
	for _, want := range []string{"pin", "unpin", "models", "cap", "worker", "planner", "checker"} {
		if !strings.Contains(text, want) {
			t.Errorf("the /crew rows never say %q:\n%s", want, text)
		}
	}
	for _, retired := range []string{"frugal", "balanced", "preset", "mastermind"} {
		if strings.Contains(text, retired) {
			t.Errorf("the /crew rows still say the retired %q:\n%s", retired, text)
		}
	}
}
