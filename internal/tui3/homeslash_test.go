package tui3

import (
	"strings"
	"testing"
	"time"
)

// HOME'S COMPOSER ANSWERS A "/" THE WAY CHAT'S DOES: a ranked menu while typing
// and enter running the row through the one dispatcher. These tests drive the
// app through real key messages, the way home's own suite does (home_test.go's
// [TestHomeSearch] is the template), because the contract is what a person's
// fingers meet — not the functions underneath them.

// homeKindAt is the kind of the line the cursor rests on, for asserting where
// the arrows landed without trusting the order the drop-up was built in.
func homeKindAt(a *app) homeRowKind {
	return a.home.lines[a.home.cursor].kind
}

// TestHomeSlashOffersCommandRows: typing a slash word offers the matching
// command rows in the drop-up, ranked by the chat composer's own ranker — an
// alias surfaces the canonical row, exactly as chat's list does.
func TestHomeSlashOffersCommandRows(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	typeHome(a, "/set")
	text := homeText(a)
	if !strings.Contains(text, "/settings") {
		t.Fatalf("typing /set did not offer the settings command row:\n%s", text)
	}
	if !strings.Contains(text, "a command") {
		t.Fatalf("a command row does not say what it is:\n%s", text)
	}

	// "/clea" reaches /new through its clear alias, and the row that appears
	// must be the canonical one — the word this surface runs — with the alias
	// printed beside it, the same bargain chat's list makes.
	a.homeKey(key("esc"))
	typeHome(a, "/clea")
	if text := homeText(a); !strings.Contains(text, "/new") {
		t.Fatalf("typing clea did not offer the canonical /new row:\n%s", text)
	}

	// "/mo" offers /model — the acceptance's own word.
	a.homeKey(key("esc"))
	typeHome(a, "/mo")
	if text := homeText(a); !strings.Contains(text, "/model") {
		t.Fatalf("typing /mo did not offer the model command row:\n%s", text)
	}

	// "/conf" reaches /settings through its config alias.
	a.homeKey(key("esc"))
	typeHome(a, "/conf")
	if text := homeText(a); !strings.Contains(text, "/settings") {
		t.Fatalf("typing /conf did not offer the canonical settings row:\n%s", text)
	}
}

// TestHomeSlashEnterOnOfferedRow: the cursor rests on the action row while
// typing; one ↑ is the errand row, and the second ↑ is the best command row —
// the drop-up's law, best match nearest the box. Enter there runs the row the
// way chat's list does: the box is rewritten with the chosen word and the
// command runs.
func TestHomeSlashEnterOnOfferedRow(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	typeHome(a, "/set")
	if k := homeKindAt(a); k != homeAction {
		t.Fatalf("cursor rested on %v while typing, want homeAction", k)
	}
	a.homeKey(key("up"))
	if k := homeKindAt(a); k != homeAskHere {
		t.Fatalf("first ↑ landed on %v, want homeAskHere", k)
	}
	a.homeKey(key("up"))
	if k := homeKindAt(a); k != homeCommand {
		t.Fatalf("second ↑ landed on %v, want a command row", k)
	}
	if a.home.lines[a.home.cursor].cmd.name != "settings" {
		t.Fatalf("second ↑ reached %q, want the best match /settings", a.home.lines[a.home.cursor].cmd.name)
	}
	runCmd(a.homeEnter())
	if !a.at(pageSettings) {
		t.Fatalf("enter on the offered /settings row did not open the settings place")
	}
	if text := strings.TrimSpace(a.home.box.String()); text != "" {
		t.Fatalf("the box still holds %q after the row ran it", text)
	}
}

// TestHomeSlashTypedLineDispatches: a slash line typed in full and entered from
// the resting row is DISPATCHED and never sent — the bug this change exists
// for. /settings opens the settings place and starts no conversation.
func TestHomeSlashTypedLineDispatches(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	next := &fakeAgent{model: "m"}
	a.start = func(string) (Conversation, error) {
		return Conversation{Agent: next, SessionFile: "/tmp/alpha/next/transcript.jsonl"}, nil
	}
	typeHome(a, "/settings")
	runCmd(a.homeEnter())
	if !a.at(pageSettings) {
		t.Fatal("typing /settings and pressing enter did not open the settings place")
	}
	if len(next.sent) != 0 {
		t.Fatalf("a slash line started a conversation: %q", next.sent)
	}
}

// TestHomeUnknownSlashStarts: an unrecognized slash line is an arbitrary first
// message, exactly as it is in chat.
func TestHomeUnknownSlashStarts(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	next := &fakeAgent{model: "m"}
	a.start = func(string) (Conversation, error) {
		return Conversation{Agent: next, SessionFile: "/tmp/alpha/next/transcript.jsonl"}, nil
	}
	typeHome(a, "/nonsense")
	runCmd(a.homeEnter())
	if a.at(pageHome) {
		t.Fatal("slash prose left home open")
	}
	if len(next.sent) != 1 || next.sent[0] != "/nonsense" {
		t.Fatalf("the first message was %q", next.sent)
	}
}

// TestHomePlainSentenceStillStarts: the regression this change must not break —
// a sentence with no slash in it still starts a conversation from the box.
func TestHomePlainSentenceStillStarts(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	next := &fakeAgent{model: "m"}
	a.start = func(string) (Conversation, error) {
		return Conversation{Agent: next, SessionFile: "/tmp/alpha/next/transcript.jsonl"}, nil
	}
	typeHome(a, "hello there")
	runCmd(a.homeEnter())
	if a.at(pageHome) {
		t.Fatal("a plain sentence no longer starts a conversation")
	}
	if len(next.sent) != 1 || next.sent[0] != "hello there" {
		t.Fatalf("the sentence did not reach the agent: %q", next.sent)
	}
}

// TestHomeSlashSmokeWalks: a handful of commands walked through home's
// dispatcher, each doing what it does in chat. /home re-opens the screen;
// /model opens the picker, which is modal over every surface; /help writes the
// key sheet into the conversation behind the screen.
func TestHomeSlashSmokeWalks(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	// /model opens the picker, and the picker takes the keys from any surface.
	typeHome(a, "/model")
	runCmd(a.homeEnter())
	if !a.pick.open {
		t.Fatal("typing /model on home did not open the model picker")
	}
	// The picker is modal over every surface (input.go), so esc belongs to it
	// while it is up — the router would hand it there, and the test must too.
	a.pickerKey(key("esc"))

	// /help answers in the conversation behind the screen, as chat's does.
	typeHome(a, "/help")
	runCmd(a.homeEnter())
	if notes := homeNotes(a); !strings.Contains(notes, "/help") && !strings.Contains(notes, "chord") {
		t.Fatalf("the key sheet did not land behind home:\n%s", notes)
	}

	// /home stays home, with the composer emptied by the dispatch.
	typeHome(a, "/home")
	runCmd(a.homeEnter())
	if !a.at(pageHome) {
		t.Fatal("typing /home on home left the screen")
	}
	if text := strings.TrimSpace(a.home.box.String()); text != "" {
		t.Fatalf("the box still holds %q after /home ran", text)
	}
}

// TestHomeSlashSelectionSurvivesTheSlowTick: the drop-up's selection is a thing
// a person DID, and nothing but another key of theirs may move it.
//
// THE BUG THIS PINS. Home's slow tick rebuilds the whole column every three
// seconds ([app.refreshHome]), and the rebuild used to keep the cursor only
// when it stood on a conversation — every other row the drop-up offers was
// forgotten, and the cursor was put back on `start a new conversation`, which
// is the LAST row of a drop-up. So somebody who had walked up onto `/settings`
// and then sat still watched the selection fall back down to the bottom on its
// own, with nothing typed and nothing touched.
func TestHomeSlashSelectionSurvivesTheSlowTick(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	typeHome(a, "/set")
	a.homeKey(key("up"))
	a.homeKey(key("up"))
	if k := homeKindAt(a); k != homeCommand {
		t.Fatalf("two ↑ landed on %v, want a command row", k)
	}
	chosen := a.home.lines[a.home.cursor].cmd

	// Three beats of the tick and three paints, and not one keystroke between
	// them — which is a person reading the row they just walked onto.
	for i := 0; i < 3; i++ {
		a.refreshHome()
		_ = homeText(a)
	}
	if k := homeKindAt(a); k != homeCommand {
		t.Fatalf("the selection walked off onto %v with nothing typed:\n%s", k, homeText(a))
	}
	if got := a.home.lines[a.home.cursor].cmd; got != chosen {
		t.Fatalf("the selection moved to /%s, want the /%s it was left on", got.name, chosen.name)
	}
}

// TestHomeOfferedPlaceSelectionSurvivesTheSlowTick is the same law over the
// OTHER kind of row the typed drop-up offers — a place (homeplaces.go). It is a
// separate test because the defect was never about commands: the rebuild asked
// whether the cursor stood on a conversation, so a place row snapped back to
// the action row too, and had done since before the composer answered a slash.
func TestHomeOfferedPlaceSelectionSurvivesTheSlowTick(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	typeHome(a, "sett")
	for i := 0; i < len(a.home.lines) && homeKindAt(a) != homePlace; i++ {
		a.homeKey(key("up"))
	}
	if k := homeKindAt(a); k != homePlace {
		t.Fatalf("↑ never reached the offered place; it rests on %v:\n%s", k, homeText(a))
	}
	word := a.home.lines[a.home.cursor].project

	for i := 0; i < 3; i++ {
		a.refreshHome()
		_ = homeText(a)
	}
	if k := homeKindAt(a); k != homePlace {
		t.Fatalf("the selection walked off the place onto %v with nothing typed:\n%s", k, homeText(a))
	}
	if got := a.home.lines[a.home.cursor].project; got != word {
		t.Fatalf("the selection moved to the %q place, want the %q it was left on", got, word)
	}
}

// TestHomeSlashActionRowSaysItWillRun: the resting row and the foot under it
// both name what enter will actually do with a slash line.
//
// THE ROW MAY NOT PROMISE A CONVERSATION IT WILL NOT START. Enter on the action
// row dispatches a "/" line ([app.homeEnter]), and the row went on reading
// `+ start a new conversation: "/settings"` while it did — which is the one row
// on this screen whose whole job is to say what the key means.
func TestHomeSlashActionRowSaysItWillRun(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	typeHome(a, "/settings")
	if k := homeKindAt(a); k != homeAction {
		t.Fatalf("the cursor left the action row onto %v", k)
	}
	text := homeText(a)
	if !strings.Contains(text, homeStartGlyph+" run /settings") {
		t.Fatalf("the action row does not say it will run the command:\n%s", text)
	}
	if strings.Contains(text, homeStartWord+`: "/settings"`) {
		t.Fatalf("the action row still offers to start a conversation with the command:\n%s", text)
	}
	if hint := a.homeHintWords(); !strings.Contains(hint, "enter runs this command") {
		t.Fatalf("the foot reads %q, want it naming the run", hint)
	}

	// An ordinary sentence is untouched: the row and the foot say what they have
	// always said, quoted words and all.
	a.homeKey(key("esc"))
	typeHome(a, "pricing")
	if text := homeText(a); !strings.Contains(text, homeStartWord+`: "pricing"`) {
		t.Fatalf("a sentence lost the row it has always had:\n%s", text)
	}
	if hint := a.homeHintWords(); !strings.Contains(hint, "enter starts a new conversation and sends this") {
		t.Fatalf("a sentence's foot reads %q", hint)
	}
}

// TestHomeSlashChosenRowWritesTheNameAndNotThePlaceholder: choosing a command
// that TAKES words leaves "/model " in the box with the caret after it — the
// name and a space, never the "<slug>" the row is drawn with.
//
// This is what running the one [chooseCommand] over both boxes buys: home used
// to write [command.typed] and put a literal "<slug>" in front of the person.
func TestHomeSlashChosenRowWritesTheNameAndNotThePlaceholder(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	typeHome(a, "/model")
	// Up to the "/model <slug>" row — the one that TAKES words. Its neighbour is
	// the argless form, which runs and opens the picker instead, so the walk
	// looks for the row by what it is rather than counting keystrokes.
	for i := 0; i < len(a.home.lines); i++ {
		if line := a.home.lines[a.home.cursor]; line.kind == homeCommand && line.cmd.args != "" {
			break
		}
		a.homeKey(key("up"))
	}
	chosen := a.home.lines[a.home.cursor]
	if chosen.kind != homeCommand || chosen.cmd.args == "" {
		t.Fatalf("↑ never reached a command that takes words:\n%s", homeText(a))
	}
	// The ROW says "/model <slug>", because that is what the command wants said
	// to it. What lands in the box is the other half of the bargain.
	if word := chosen.cmd.typed(); !strings.Contains(word, "<") {
		t.Fatalf("the row reads %q, so this test is no longer about a placeholder", word)
	}
	runCmd(a.homeEnter())
	if got := a.home.box.String(); got != "/model " {
		t.Fatalf("choosing /model <slug> left %q in the box, want %q", got, "/model ")
	}
}

// TestHomeSlashMentionRewritesTheTokenInPlace: a slash word inside a sentence
// is a MENTION — the token is rewritten, the list is sealed, and nothing runs.
// It is the same bargain chat's list makes, and it is the same code making it.
func TestHomeSlashMentionRewritesTheTokenInPlace(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	typeHome(a, "what does /sett")
	for i := 0; i < len(a.home.lines) && homeKindAt(a) != homeCommand; i++ {
		a.homeKey(key("up"))
	}
	if k := homeKindAt(a); k != homeCommand {
		t.Fatalf("↑ never reached a command row; it rests on %v:\n%s", k, homeText(a))
	}
	runCmd(a.homeEnter())
	if got := a.home.box.String(); got != "what does /settings" {
		t.Fatalf("the mention left %q in the box", got)
	}
	if a.at(pageSettings) {
		t.Fatal("a command mentioned inside a sentence ran")
	}
}

// TestHomeSlashDoesNotSwallowATypedPath: an absolute path begins with a slash
// too, and a folder that exists is still a folder.
//
// `/tmp/alpha` has opened a conversation in that directory since long before
// this box could dispatch anything, and the dispatch must not take the gesture
// away. The row says which of the two it means, and enter does that one.
func TestHomeSlashDoesNotSwallowATypedPath(t *testing.T) {
	lab := newHomeLab(t)
	dir := t.TempDir()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	typeHome(a, dir)
	if got := a.home.runLabel(dir); got != "" {
		t.Fatalf("a real folder was read as the command %q", got)
	}
	// The label itself rather than the painted row: a temp directory's path is
	// longer than the column, and this test is about which of the two readings
	// the row took, not about where it was cut.
	if got := a.home.startLabel(); got != homeStartWord+" in "+dir {
		t.Fatalf("the action row says %q, want it offering the folder", got)
	}
	if hint := a.homeHintWords(); strings.Contains(hint, "enter runs this command") {
		t.Fatalf("the foot called a folder a command: %q", hint)
	}

	// A slash line from the command table is still a command.
	a.homeKey(key("esc"))
	typeHome(a, "/settings")
	if got := a.home.runLabel("/settings"); got != "run /settings" {
		t.Fatalf("the command reads %q", got)
	}
	// AND A WORD THE TABLE KNOWS BEATS A FOLDER OF THE SAME NAME. `/home` is a
	// command and a directory a Linux box really has, and the thirty words
	// somebody chose to learn win — which is the order the dispatcher itself
	// takes (app.go's [app.slash] tries the table, then the disk).
	if got := a.home.runLabel("/home"); got != "run /home" {
		t.Fatalf("/home reads %q, want the command", got)
	}
}
