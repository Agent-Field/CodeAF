package tui3

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE ALWAYS THAT OUTLIVES THE SESSION.
//
// Pressing [a] has always stopped the asking for the rest of the agent's life
// (internal/session's memo). These tests are about the half that is new: where
// the door wired a write seam, the same press also writes the answer into the
// person's own settings — a tool's allow, or one whole shell command — says so
// in a receipt, and never writes a no.

// remembering wires both save seams onto a surface and records what they were
// handed. It stands in for cmd/aforge's closures over the profile directory.
type remembered struct {
	tools    []string
	commands []string
	err      error
}

func rememberingApp(t *testing.T, events []session.Event) (*wiredAgent, *app, *remembered) {
	t.Helper()
	agent, a := wired(events)
	saved := &remembered{}
	a.saveApproval = func(tool string) error {
		saved.tools = append(saved.tools, tool)
		return saved.err
	}
	a.saveBashApproval = func(command string) error {
		saved.commands = append(saved.commands, command)
		return saved.err
	}
	return agent, a, saved
}

// bashBegin is a bash call with its PAYLOAD, which is the shape a remembered
// command has to be read out of: the row's own text is a gloss, the arguments
// are what arrived.
func bashBegin(command string) session.Event {
	return session.Event{
		Kind: session.EventToolBegin, Tool: "bash", Hint: "bash " + command,
		Args: `{"command":` + strconv.Quote(command) + `}`,
	}
}

// A plain tool: the press answers the session AND writes the tool's allow.
func TestAlwaysOnAPlainToolWritesTheToolsAllow(t *testing.T) {
	agent, a, saved := rememberingApp(t, []session.Event{
		toolBegin("read", "read internal/session/consent.go"),
		consentEvent(7, "read", "read internal/session/consent.go", `tool "read"`),
	})
	typeLine(t, a, "look at the gate")
	settleAsk(a)
	drive(t, a, key("2"))

	if len(agent.answers) != 1 || agent.answers[0] != (answered{id: 7, allow: true, scope: session.ConsentToolSession}) {
		t.Fatalf("the session was answered %+v", agent.answers)
	}
	if len(saved.tools) != 1 || saved.tools[0] != "read" {
		t.Fatalf("the write seam was handed %v, want one `read`", saved.tools)
	}
	if len(saved.commands) != 0 {
		t.Fatalf("a plain tool wrote a shell rule: %v", saved.commands)
	}
}

// Bash: the press opens the second beat, and the LINE is the last thing on it —
// read from the call's arguments and not from the line on screen.
func TestAlwaysOnBashWritesTheWholeCommandLine(t *testing.T) {
	command := `git commit -m "wave 9, the card"`
	_, a, saved := rememberingApp(t, []session.Event{
		bashBegin(command),
		consentEvent(7, "bash", "bash "+command, `tool "bash"`),
	})
	typeLine(t, a, "commit it")
	settleAsk(a)
	drive(t, a, key("2"))
	if len(saved.commands) != 0 {
		t.Fatalf("the always wrote before the shape was chosen: %v", saved.commands)
	}
	drive(t, a, key("3"))

	if len(saved.commands) != 1 || saved.commands[0] != command {
		t.Fatalf("the write seam was handed %q, want the command whole", saved.commands)
	}
	if len(saved.tools) != 0 {
		t.Fatalf("bash was remembered by name: %v", saved.tools)
	}
}

// THE RECEIPT. A press that changed a setting says what happened and where to
// undo it, in the row's own dim slot.
func TestASavedAlwaysLeavesAReceiptOnTheRow(t *testing.T) {
	_, a, _ := rememberingApp(t, []session.Event{
		toolBegin("read", "read consent.go"),
		consentEvent(7, "read", "read consent.go", `tool "read"`),
	})
	typeLine(t, a, "look")
	settleAsk(a)
	drive(t, a, key("2"))

	if got := plain(frame(a)); !strings.Contains(got, "always · saved — /permissions to change") {
		t.Fatalf("the row does not say what was saved or where to change it:\n%s", got)
	}
}

// A WRITE THAT FAILED IS DROPPED, and the receipt goes with it: the answer
// stands, the session stops asking, and the row says the plain truth instead of
// claiming a file that was never written.
func TestAFailedWriteIsDroppedAndClaimsNothing(t *testing.T) {
	agent, a, saved := rememberingApp(t, []session.Event{
		toolBegin("read", "read consent.go"),
		consentEvent(7, "read", "read consent.go", `tool "read"`),
	})
	saved.err = errors.New("the profile directory is read-only")
	typeLine(t, a, "look")
	settleAsk(a)
	drive(t, a, key("2"))

	if len(agent.answers) != 1 || !agent.answers[0].allow {
		t.Fatalf("the answer did not stand: %+v", agent.answers)
	}
	got := plain(frame(a))
	if strings.Contains(got, "saved") {
		t.Fatalf("a failed write printed a receipt:\n%s", got)
	}
	if strings.Contains(got, "read-only") {
		t.Fatalf("a config error reached a line in the middle of the work:\n%s", got)
	}
	if !strings.Contains(got, "allowed") {
		t.Fatalf("the row does not say the call was allowed:\n%s", got)
	}
}

// NO IS NEVER WRITTEN DOWN. Neither the deny key nor the clock's expiry may
// reach a write seam: a standing never is a settings edit somebody makes on
// purpose.
func TestADenyIsNeverPersisted(t *testing.T) {
	_, a, saved := rememberingApp(t, []session.Event{
		toolBegin("bash", "bash rm -rf build"),
		consentEvent(7, "bash", "bash rm -rf build", `bash pattern "rm -rf *"`),
	})
	typeLine(t, a, "clean it")
	settleAsk(a)
	drive(t, a, key("3"))
	if len(saved.tools)+len(saved.commands) != 0 {
		t.Fatalf("a no was written down: %v %v", saved.tools, saved.commands)
	}
}

// A SURFACE THAT WIRED NOTHING BEHAVES EXACTLY AS IT DID BEFORE: the always is
// still answered and still session-scoped, the offer still says so out loud,
// and the row still says "allowed".
func TestWithNoWriteSeamTheCardIsTheOldCard(t *testing.T) {
	agent, a := wired([]session.Event{
		toolBegin("read", "read consent.go"),
		consentEvent(7, "read", "read consent.go", `tool "read"`),
	})
	a.width = 120
	typeLine(t, a, "look")
	settleAsk(a)
	if got := plain(frame(a)); !strings.Contains(got, "[2] always, this tool (session)") {
		t.Fatalf("the unwired offer changed its words:\n%s", got)
	}
	drive(t, a, key("2"))
	if len(agent.answers) != 1 || agent.answers[0].scope != session.ConsentToolSession {
		t.Fatalf("the session was answered %+v", agent.answers)
	}
	if got := plain(frame(a)); strings.Contains(got, "saved") {
		t.Fatalf("a surface with nowhere to write claimed a save:\n%s", got)
	}
}

// THE OFFER SAYS WHAT IT WILL DO. With a seam behind it the parenthetical goes
// — the answer is not session-scoped any more — and bash, the one tool answered
// by its argument, names the command instead of the tool.
func TestTheOfferNamesWhatTheAlwaysReaches(t *testing.T) {
	_, a, _ := rememberingApp(t, []session.Event{
		toolBegin("read", "read consent.go"),
		consentEvent(7, "read", "read consent.go", `tool "read"`),
	})
	typeLine(t, a, "look")
	settleAsk(a)
	a.width = 120
	got := plain(frame(a))
	if !strings.Contains(got, "[2] always, this tool") || strings.Contains(got, "(session)") {
		t.Fatalf("the offer still promises a session-scoped always:\n%s", got)
	}

	_, b, _ := rememberingApp(t, []session.Event{
		bashBegin("git status"),
		consentEvent(7, "bash", "bash git status", `tool "bash"`),
	})
	typeLine(t, b, "check the tree")
	settleAsk(b)
	b.width = 120
	if got := plain(frame(b)); !strings.Contains(got, "[2] always, this command") {
		t.Fatalf("the bash offer does not say it is about the command:\n%s", got)
	}
}

// And the phone sheet's band says the same thing, in the same words.
func TestThePhoneBandNamesTheCommandToo(t *testing.T) {
	_, a, _ := rememberingApp(t, []session.Event{
		bashBegin("git status"),
		consentEvent(7, "bash", "bash git status", `tool "bash"`),
	})
	a.width, a.height = 44, 30
	typeLine(t, a, "check the tree")
	settleAsk(a)
	rows := strings.Join(askRows(a), "\n")
	if !strings.Contains(rows, "[2] always, this command") {
		t.Fatalf("the band does not name the command:\n%s", rows)
	}
}

// A bash call whose arguments never arrived whole writes NOTHING. The rule would
// have been written from a gloss, and a standing approval for a command nobody
// ran is the one outcome this seam must not have.
func TestABashCallWithNoReadableCommandWritesNothing(t *testing.T) {
	agent, a, saved := rememberingApp(t, []session.Event{
		toolBegin("bash", "bash git status"), // a hint and no payload
		consentEvent(7, "bash", "bash git status", `tool "bash"`),
	})
	typeLine(t, a, "check the tree")
	settleAsk(a)
	drive(t, a, key("2"))

	if len(saved.commands) != 0 {
		t.Fatalf("a rule was written from a gloss: %v", saved.commands)
	}
	if len(agent.answers) != 1 || agent.answers[0].scope != session.ConsentToolSession {
		t.Fatalf("the session answer changed: %+v", agent.answers)
	}
	if got := plain(frame(a)); strings.Contains(got, "saved") {
		t.Fatalf("nothing was written and the row said otherwise:\n%s", got)
	}
}
