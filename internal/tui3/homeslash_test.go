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

// TestHomeSlashUnknownAnswer: an unrecognized slash line gets the same answer
// chat gives — the same words, landing in the conversation this window holds
// behind the screen, which is where a refusal that opened nothing belongs
// (home_test.go's [homeNotes]).
func TestHomeSlashUnknownAnswer(t *testing.T) {
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
	if a.at(pageSettings) || a.at(pageNone) {
		t.Fatalf("an unknown command moved the surface to %v", a.page)
	}
	notes := homeNotes(a)
	if !strings.Contains(notes, "unknown command: /nonsense · try /help") {
		t.Fatalf("the unknown-command answer chat gives did not land:\n%s", notes)
	}
	if len(next.sent) != 0 {
		t.Fatalf("an unknown command started a conversation: %q", next.sent)
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
