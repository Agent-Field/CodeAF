package tui3

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
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
	// AND THE ROW SAYS WHAT ENTER WOULD DO WITH IT HERE, which is the half a
	// person standing on home cannot work out for themselves ([homeFate]).
	if !strings.Contains(text, fatePlace) {
		t.Fatalf("a command row does not say its fate at home:\n%s", text)
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
	if !strings.Contains(notes, unknownCommandWord("nonsense")) {
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
// dispatcher, each doing what it does HERE — which is not always what it does
// in chat, and the difference is the gate (homeslash.go).
//
// EVERY KEY GOES THROUGH THE REAL ROUTER. This test used to assert that /model
// set `a.pick.open` and then drive the list with a direct call to
// [app.pickerKey] — so the gate was green on a thing a person could not do: the
// picker was open behind a place that cannot draw it, and every key while a
// place is showing goes to [app.placeKeyPress], which never asks it. The router
// is [app.key], and it is what a person's fingers reach.
func TestHomeSlashSmokeWalks(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	// /model opens the list OVER THE TARGET, drawn in home's own body, and the
	// router drives it: the walk, the filter and the way out are all keys.
	typeHome(a, "/model")
	runCmd(a.key(key("enter")))
	if a.pick.open {
		t.Fatal("/model at home opened the conversation's picker, which a place cannot draw")
	}
	if !a.target.pick.open {
		t.Fatal("typing /model on home did not open the model list over the target")
	}
	if text := homeText(a); !strings.Contains(text, targetPickWord) {
		t.Fatalf("the foot does not name the list's own keys:\n%s", text)
	}
	runCmd(a.key(key("esc")))
	if a.target.pick.open {
		t.Fatal("esc through the router did not close the model list")
	}

	// /help answers with a note, and the note is READ where it was typed.
	typeHome(a, "/help")
	runCmd(a.key(key("enter")))
	if notes := homeNotes(a); !strings.Contains(notes, "/help") && !strings.Contains(notes, "chord") {
		t.Fatalf("the key sheet did not land behind home:\n%s", notes)
	}
	if a.home.msg == "" {
		t.Fatal("/help answered on home and said nothing on home's own line")
	}

	// /home stays home, with the composer emptied by the dispatch.
	typeHome(a, "/home")
	runCmd(a.key(key("enter")))
	if !a.at(pageHome) {
		t.Fatal("typing /home on home left the screen")
	}
	if text := strings.TrimSpace(a.home.box.String()); text != "" {
		t.Fatalf("the box still holds %q after /home ran", text)
	}
}

// targetPathWord is the folder home's rule actually draws — the same short form
// every other path on this surface wears (render.go's [shortPath]) — so an
// assertion is about which folder the rule named rather than about how it was
// abbreviated.
func targetPathWord(a *app) string {
	return a.hostedPath(a.placeWord(shortPath(a.targetWhere(), a.tilde, 0)))
}

// TestHomesRuleSaysWhereTheNextConversationGoes: the rule above the box is a
// legend, and its left is the target — the folder and the model — with the two
// chords that change them on its right.
func TestHomesRuleSaysWhereTheNextConversationGoes(t *testing.T) {
	lab := newHomeLab(t)
	where := lab.workspace("parser")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", where, time.Now())
	a := lab.app(mine)
	a.workspace = where
	openHomeOn(a, mine)
	runCmd(a.openHome())

	text := homeText(a)
	if !strings.Contains(text, targetLeadWord+targetPathWord(a)) {
		t.Fatalf("the rule does not say where the next conversation opens:\n%s", text)
	}
	if !strings.Contains(text, targetModelKeyWord) {
		t.Fatalf("the rule does not name the model chord:\n%s", text)
	}
	// AND THE CHIP IS OFF THE BOX ROW. It said the same fact one row down, in
	// competition with the draft, and it was the reading `enter` did not honour.
	if strings.Contains(text, placeScopeWord+" "+where) {
		t.Fatalf("home still draws the scope chip on its box row:\n%s", text)
	}
}

// TestHomesRuleGivesUpTheModelBeforeTheFolder is the ladder. The folder is the
// fact `enter` acts on, so it is the last thing standing — and the keys are
// never dropped before the label is shortened.
func TestHomesRuleGivesUpTheModelBeforeTheFolder(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.workspace("parser")
	other := lab.workspace("cafe")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", where, now)
	lab.session("-tmp-beta", "bbbb000000000001", "the cafe pricing page", other, now.Add(-time.Hour))
	a := lab.app(mine)
	a.workspace = where
	a.model = "zhipu/glm-5.3-flash"
	openHomeOn(a, mine)
	runCmd(a.openHome())
	short := targetPathWord(a)

	wide, _ := a.targetLegend(200, a.pal)
	if !strings.Contains(ansi.Strip(wide), modelBase(a.model)) {
		t.Fatalf("a wide rule dropped the model:\n%s", ansi.Strip(wide))
	}
	if !strings.Contains(ansi.Strip(wide), targetFolderKeyWord) {
		t.Fatalf("a wide rule dropped the folder chord:\n%s", ansi.Strip(wide))
	}
	// Narrow enough that the model cannot fit beside the folder, wide enough
	// that the folder can. The keys survive: they are the cheapest true thing on
	// the line and the label is what has too much to say.
	room := ansi.StringWidth(targetLeadWord+short) + ansi.StringWidth(targetModelKeyWord) + 12
	narrow, drew := a.targetLegend(room, a.pal)
	if !drew {
		t.Fatalf("a %d-column rule drew nothing at all", room)
	}
	stripped := ansi.Strip(narrow)
	if strings.Contains(stripped, modelBase(a.model)) {
		t.Fatalf("the model outlived the room for it:\n%s", stripped)
	}
	if !strings.Contains(stripped, filepath.Base(where)) {
		t.Fatalf("the folder went before the model did:\n%s", stripped)
	}
	if !strings.Contains(stripped, targetModelKeyWord) {
		t.Fatalf("the keys were dropped before the label was shortened:\n%s", stripped)
	}
}

// TestEnterAtHomeOpensTheConversationInTheRowsFolder: the disagreement this
// wave exists to end. The cursor rests on another project's row, the rule says
// so, and `enter` honours it — where it used to open a conversation in this
// window's own workspace and ignore the row entirely.
func TestEnterAtHomeOpensTheConversationInTheRowsFolder(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.workspace("alpha")
	theirs := lab.workspace("beta")
	one := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", mine, now)
	two := lab.session("-tmp-beta", "bbbb000000000001", "the cafe pricing page", theirs, now.Add(-time.Hour))
	_ = two
	a := lab.app(one)
	a.workspace = mine

	var opened string
	next := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a.start = func(workspace string) (Conversation, error) {
		opened = workspace
		return Conversation{Agent: next, SessionFile: workspace + "/next/transcript.jsonl", Workspace: workspace}, nil
	}
	openHomeOn(a, two)
	runCmd(a.openHome())
	a.home.point(two)
	// THE CURSOR'S ROW IS THE TARGET WITH NOTHING TYPED, which is the reading the
	// scope chip used to draw and `enter` used to ignore.
	if got := a.targetWhere(); got != theirs {
		t.Fatalf("the target reads %q with the cursor on the other project's row, want %q", got, theirs)
	}
	// AND A PIN IS THAT READING HELD. The action row takes the cursor the moment
	// a letter lands — it is the last line of the drop-up — so a person who wants
	// to carry a sentence somewhere else pins it, which is what `alt+w` is for.
	runCmd(a.key(key("alt+w")))
	for i := 0; i < len(a.composerDestinations()) && a.targetWhere() != theirs; i++ {
		runCmd(a.key(key("alt+w")))
	}
	if got := a.targetWhere(); got != theirs {
		t.Fatalf("alt+w never reached %q; it stopped on %q", theirs, got)
	}

	typeHome(a, "why is the lexer allocating")
	runCmd(a.homeEnter())
	if opened != theirs {
		t.Fatalf("enter opened a conversation in %q, want the row's own folder %q", opened, theirs)
	}
	if len(next.sent) != 1 || next.sent[0] != "why is the lexer allocating" {
		t.Fatalf("the sentence did not reach the conversation that opened: %q", next.sent)
	}
	// AND THE FOLDER PIN IS SPENT. It was used; the honest reading from here on
	// is the cursor's own row again (homedraft.go's owner ruling).
	if a.target.where != "" {
		t.Fatalf("the folder pin survived the conversation that spent it: %q", a.target.where)
	}
}

// TestModelAtHomePinsTheDraftAndSaysSo: /model at home is about the DRAFT. It
// used to switch the model of the conversation behind the screen and write the
// answer into it, where it could not be read.
func TestModelAtHomePinsTheDraftAndSaysSo(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.model = "zhipu/glm-5.3-flash"
	a.openHome()
	runCmd(a.openHome())

	typeHome(a, "/model zhipu/glm-5.3")
	runCmd(a.key(key("enter")))
	if a.target.model != "zhipu/glm-5.3" {
		t.Fatalf("the draft's model is %q, want the slug that was typed", a.target.model)
	}
	if a.model != "zhipu/glm-5.3-flash" {
		t.Fatalf("/model at home re-modelled the conversation behind the screen: %q", a.model)
	}
	want := "model · " + modelBase("zhipu/glm-5.3") + targetPinnedModelWord
	if a.home.msg != want {
		t.Fatalf("home's line reads %q, want %q", a.home.msg, want)
	}
	// AND THE RULE WEARS IT, so the pin is visible after the line has gone.
	if text := homeText(a); !strings.Contains(text, modelBase("zhipu/glm-5.3")) {
		t.Fatalf("the rule does not say the pinned model:\n%s", text)
	}
	// AND IT SURVIVES LEAVING HOME AND COMING BACK (the owner's ruling).
	a.closeHome()
	runCmd(a.showPage(pageHome))
	if a.target.model != "zhipu/glm-5.3" {
		t.Fatalf("the model pin did not survive a reopen of home: %q", a.target.model)
	}
}

// TestAnUnknownCommandRefusesWhereItWasTyped: the one refusal a person most
// needs to see was the one they could not see.
func TestAnUnknownCommandRefusesWhereItWasTyped(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	typeHome(a, "/nonsense")
	runCmd(a.key(key("enter")))
	if got := a.home.msg; got != unknownCommandWord("nonsense") {
		t.Fatalf("home's line reads %q, want %q", got, unknownCommandWord("nonsense"))
	}
	if text := homeText(a); !strings.Contains(text, unknownCommandWord("nonsense")) {
		t.Fatalf("the refusal is not on the frame:\n%s", text)
	}
}

// TestModelThenEscLeavesNoPickerOverTheConversation is "it goes to an old
// chat", pinned. /model opened a bottom-anchored overlay a place cannot draw,
// nothing closed it, and one `esc` handed the frame to the conversation
// underneath — with the picker on it.
func TestModelThenEscLeavesNoPickerOverTheConversation(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	typeHome(a, "/model")
	runCmd(a.key(key("enter")))
	if !a.target.pick.open {
		t.Fatal("/model did not open the list over the target")
	}
	runCmd(a.key(key("esc")))
	runCmd(a.key(key("esc")))
	if a.at(pageHome) {
		t.Fatal("two escapes did not leave home")
	}
	if a.pick.open || a.target.pick.open {
		t.Fatal("a model list is standing over the conversation home was in front of")
	}
	if text := ansi.Strip(mustFrame(a)); strings.Contains(text, pickerHintAt(80)) {
		t.Fatalf("the picker is drawn over the conversation:\n%s", text)
	}
}

// TestShowPageClosesEveryModalOnTheWayIn is the other half of the same repair,
// said about all ten rather than about the one a person met first.
func TestShowPageClosesEveryModalOnTheWayIn(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)

	a.pick.open, a.roster.open, a.shelf.open = true, true, true
	a.crewPick.open, a.effPick.open = true, true
	a.connPanel.open, a.harnPanel.open, a.permPanel.open, a.subPage.open = true, true, true, true
	runCmd(a.showPage(pageHome))
	for name, open := range map[string]bool{
		"a.pick": a.pick.open, "a.roster": a.roster.open, "a.shelf": a.shelf.open,
		"a.crewPick": a.crewPick.open, "a.effPick": a.effPick.open,
		"a.connPanel": a.connPanel.open, "a.harnPanel": a.harnPanel.open,
		"a.permPanel": a.permPanel.open, "a.subPage": a.subPage.open,
	} {
		if open {
			t.Errorf("%s survived a place standing up; it will appear over whatever esc lands on", name)
		}
	}
}

// TestAltWCyclesWhereTheNextConversationOpens: the chord the router swallows on
// every other place, claimed by the one place it means something on.
func TestAltWCyclesWhereTheNextConversationOpens(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	one := lab.workspace("alpha")
	two := lab.workspace("beta")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", one, now)
	lab.session("-tmp-beta", "bbbb000000000001", "the cafe pricing page", two, now.Add(-time.Hour))
	a := lab.app(mine)
	a.workspace = one
	openHomeOn(a, mine)
	runCmd(a.openHome())

	places := a.composerDestinations()
	if len(places) < 2 {
		t.Skipf("this lab offered one destination (%v); the chord is correctly absent", places)
	}
	was := a.targetWhere()
	runCmd(a.key(key("alt+w")))
	if got := a.targetWhere(); got == was {
		t.Fatalf("alt+w did not move the target off %q", was)
	}
	if !strings.HasPrefix(a.home.msg, targetMovedWord) {
		t.Fatalf("alt+w said %q, want it naming where the next conversation opens", a.home.msg)
	}
	if text := homeText(a); !strings.Contains(text, targetLeadWord+targetPathWord(a)) {
		t.Fatalf("the rule did not follow the chord:\n%s", text)
	}
	// Round the cycle and back: a walk with a fixed order, never a lottery. One
	// press has already been spent, so the round trip is one short of the list.
	for i := 0; i < len(places)-1; i++ {
		runCmd(a.key(key("alt+w")))
	}
	if got := a.targetWhere(); got != was {
		t.Fatalf("the cycle came back to %q, want %q", got, was)
	}
}

// TestResumeAnswersOnHomesOwnLineAndFolderOpensTheBrowser: two commands home
// used to answer in one line each. /resume still does — home already IS that
// list — and /folder does not, because the thing it names is a real surface a
// person can walk and one line naming a chord was not it.
func TestResumeAnswersOnHomesOwnLineAndFolderOpensTheBrowser(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	typeHome(a, "/resume")
	runCmd(a.key(key("enter")))
	if a.roster.open {
		t.Fatal("/resume at home opened the roster over a screen that cannot draw it")
	}
	if a.home.msg != homeIsTheResumeWord {
		t.Fatalf("/resume said %q, want %q", a.home.msg, homeIsTheResumeWord)
	}

	// /folder is the other half of this test's original claim and it moved: it
	// used to answer in one line — `alt+w moves the next conversation · or type
	// a path` — which named two gestures and drew neither. It opens the browser
	// now, aimed at the target (folderplace.go), and the browser takes the frame.
	typeHome(a, "/folder")
	runCmd(a.key(key("enter")))
	if !a.folder.open {
		t.Fatal("/folder at home did not open the folder browser")
	}
	if !a.folder.forTarget {
		t.Fatal("the browser home opened is not aimed at the target")
	}
	if a.at(pageHome) {
		t.Fatal("home is still drawn under a sheet that takes the frame")
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

	// And a slash line that is NOT a folder is still a command.
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
