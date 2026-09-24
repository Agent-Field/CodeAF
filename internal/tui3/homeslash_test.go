package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
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

// The best name match is selected while typing, in alphabetical list order.
// Enter runs that row through the same dispatch as a conversation.
func TestHomeSlashEnterOnOfferedRow(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	typeHome(a, "/set")
	if k := homeKindAt(a); k != homeCommand {
		t.Fatalf("cursor rested on %v while typing, want a command", k)
	}
	if a.home.lines[a.home.cursor].cmd.name != "settings" {
		t.Fatalf("filter selected %q, want the best match /settings", a.home.lines[a.home.cursor].cmd.name)
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

func TestHomeSkillPathOpensAConversationWithThePathStillInThePicker(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "reading the skill shelf", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	runCmd(a.openHome())
	a.start = func(string) (Conversation, error) {
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/alpha/next/transcript.jsonl"}, nil
	}
	a.homeSlash("/skills ./tools/reviewer")
	if a.at(pageHome) {
		t.Fatal("the skill command stayed on home")
	}
	if got := a.input.String(); got != "/skill ./tools/reviewer" {
		t.Fatalf("the new conversation's skill picker lost its path: %q", got)
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
	// THE FOOT NAMES THE KEYS THE ROW UNDER THE CURSOR ANSWERS TO, which on a
	// model row is the fold and the rung as well as the walk and the two ways
	// out ([app.targetPickFoot]). It used to be one fixed sentence while the
	// keys lived in the filter box's placeholder — where two of them named
	// gestures this door did not answer at all.
	if text := homeText(a); !strings.Contains(text, a.targetPickFoot()) {
		t.Fatalf("the foot does not name the list's own keys:\n%s", text)
	}
	for _, want := range []string{targetPickWalkWord, "→ providers", sortKeyWord, "enter choose", effortKeyWord, targetPickLeaveWord} {
		if !strings.Contains(a.targetPickFoot(), want) {
			t.Fatalf("the foot on a model row does not name %q: %q", want, a.targetPickFoot())
		}
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

// targetPathWord is the full project path before the seam truncates it.
func targetPathWord(a *app) string {
	return a.targetProject()
}

// TestHomesRuleSaysWhereTheNextConversationGoes: the rule above the box is a
// legend, and its left is the target — the folder and the model — with the two
// chords that change them on its right.
func TestHomesRuleSaysWhereTheNextConversationGoes(t *testing.T) {
	lab := newHomeLab(t)
	where := lab.workspace("parser")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", where, time.Now())
	a := lab.app(mine)
	a.width = 240
	a.workspace = where
	openHomeOn(a, mine)
	runCmd(a.openHome())

	text := homeText(a)
	if !strings.Contains(text, targetProjectLead+targetPathWord(a)) {
		t.Fatalf("the rule does not say where the next conversation opens:\n%s", text)
	}
	if strings.Contains(text, "alt+o model") || strings.Contains(text, "opt+o model") {
		t.Fatalf("home still names the retired model shortcut:\n%s", text)
	}
	// AND THE CHIP IS OFF THE BOX ROW. It said the same fact one row down, in
	// competition with the draft, and it was the reading `enter` did not honour.
	if strings.Contains(text, "here "+where) {
		t.Fatalf("home still draws the scope chip on its box row:\n%s", text)
	}
}

// A long project path yields its right end before displacing the model.
func TestHomesRuleShortensTheProjectAfterItsRoot(t *testing.T) {
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

	wide, _ := a.targetLegend(200, a.pal)
	if !strings.Contains(ansi.Strip(wide), modelBase(a.model)) {
		t.Fatalf("a wide rule dropped the model:\n%s", ansi.Strip(wide))
	}
	if strings.Contains(ansi.Strip(wide), targetFolderKeyWord) {
		t.Fatalf("a wide rule still carries the folder chord:\n%s", ansi.Strip(wide))
	}
	if !strings.Contains(a.homeHint(), targetFolderKeyWord) {
		t.Fatalf("the foot does not carry the folder chord:\n%s", a.homeHint())
	}
	// A long path keeps its root on the keys row and yields its tail to the
	// keys; the rule carries the model and never the path (hometip.go).
	a.target.where = "/tmp/" + strings.Repeat("nested/", 20)
	narrow, drew := a.targetLegend(80, a.pal)
	stripped := ansi.Strip(narrow)
	if !drew || !strings.HasPrefix(stripped, "─ "+a.modelIdentity(a.model)) || strings.Contains(stripped, targetProjectLead) {
		t.Fatalf("the model was lost or the project is still on the rule: %q", stripped)
	}
	foot := ansi.Strip(a.homeFootLine(80, a.pal))
	if !strings.Contains(foot, "project: /tmp/") || !strings.HasSuffix(foot, "…") {
		t.Fatalf("the keys row lost the project root or its ellipsis: %q", foot)
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
	// to carry a sentence somewhere else pins it, which is what `alt+p` is for.
	runCmd(a.key(key("alt+p")))
	for i := 0; i < len(a.composerDestinations()) && a.targetWhere() != theirs; i++ {
		runCmd(a.key(key("alt+p")))
	}
	if got := a.targetWhere(); got != theirs {
		t.Fatalf("alt+p never reached %q; it stopped on %q", theirs, got)
	}

	typeHome(a, "why is the lexer allocating")
	runCmd(a.homeEnter())
	if opened != theirs {
		t.Fatalf("enter opened a conversation in %q, want the row's own folder %q", opened, theirs)
	}
	if len(next.sent) != 1 || next.sent[0] != "why is the lexer allocating" {
		t.Fatalf("the sentence did not reach the conversation that opened: %q", next.sent)
	}
	// The explicit project choice survives both starting and returning home.
	runCmd(a.openHome())
	a.home.point(one)
	if a.targetWhere() != theirs {
		t.Fatalf("returning home forgot the selected project: %q", a.targetWhere())
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
	// AND IT SAYS NOTHING ON THE LINE UNDER THE BOX. That line used to read `model
	// · glm-5.3 · for the next conversation you start here`, and the question it
	// was answering is answered by the RULE below instead — which carries the
	// pinned model for as long as the pin lasts rather than until the next note
	// replaces it (the owner's ruling).
	if a.home.msg != "" {
		t.Fatalf("home's line reads %q, want nothing — the rule says it instead", a.home.msg)
	}
	// AND THE RULE WEARS IT, which is the whole of how the pin announces itself.
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

	runCmd(a.key(key("alt+o")))
	if a.target.pick.open || a.pick.open || !a.home.box.empty() {
		t.Fatal("the retired alt+o shortcut changed home")
	}
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
	if text := ansi.Strip(mustFrame(a)); strings.Contains(text, pickerHintAt(80, false)) {
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
	paths := []string{lab.workspace("alpha"), lab.workspace("beta"), lab.workspace("gamma"), lab.workspace("delta")}
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", paths[0], now)
	lab.session("-tmp-beta", "bbbb000000000001", "the cafe pricing page", paths[1], now.Add(-time.Hour))
	lab.session("-tmp-gamma", "cccc000000000001", "the parser", paths[2], now.Add(-2*time.Hour))
	a := lab.app(mine)
	a.width, a.height = 240, 60
	a.workspace = paths[0]
	openHomeOn(a, mine)
	runCmd(a.openHome())
	// Standing-only projects are visible too, even without a conversation.
	a.home.bare = append(a.home.bare, homeBare{project: session.Project{Path: paths[3], Dir: "/buckets/delta"}})
	in := a.home.gridInput()
	rows := (projectsPanel{}).rows(&in).lines
	if len(rows) != len(paths) {
		t.Fatalf("projects panel has %d rows, want %d", len(rows), len(paths))
	}
	for i, row := range rows {
		if row.proj.Path != paths[i] {
			t.Fatalf("panel project %d = %q, want %q", i, row.proj.Path, paths[i])
		}
	}
	// Mix the two real controls for two full laps. The old two-project test
	// could not see a cycle rebuilding its order around the current pin.
	for i := 1; i <= 2*len(paths); i++ {
		if i%2 == 1 {
			runCmd(a.key(key("alt+p")))
		} else {
			homeText(a)
			if _, took := a.placeTargetPress(a.targetFolderSpan.from, a.footRow); !took {
				t.Fatal("the seam project did not accept the click")
			}
		}
		if got, want := a.targetWhere(), paths[i%len(paths)]; got != want {
			t.Fatalf("step %d selected %q, want %q", i, got, want)
		}
		if a.home.msg != "" {
			t.Fatalf("project selection added a footer message: %q", a.home.msg)
		}
		if text := homeText(a); !strings.Contains(text, targetProjectLead+targetPathWord(a)) || strings.Contains(text, "next conversation opens in ") {
			t.Fatalf("the seam and footer disagree with the selection:\n%s", text)
		}
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

	// /project is the other half of this test's original claim and it moved
	// twice: home's answer to "which folder" used to be one line — `alt+p moves
	// the next conversation · or type a path` — which named two gestures and
	// drew neither; then it was a bare /folder, which meant one thing here and
	// another in a conversation. It is /project since 2026-09-22
	// (projectcmd.go), it opens the browser aimed at the target, and the
	// browser takes the frame.
	typeHome(a, "/project")
	runCmd(a.key(key("enter")))
	if !a.folder.open {
		t.Fatal("/project at home did not open the folder browser")
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
	if k := homeKindAt(a); k != homeCommand {
		t.Fatalf("the cursor left the action row onto %v", k)
	}
	text := homeText(a)
	if !strings.Contains(text, "/settings") {
		t.Fatalf("the action row does not say it will run the command:\n%s", text)
	}
	if strings.Contains(text, homeStartWord+`: "/settings"`) {
		t.Fatalf("the action row still offers to start a conversation with the command:\n%s", text)
	}
	if hint := a.homeHintWords(); !strings.Contains(hint, "enter") {
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
		a.homeKey(key("down"))
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
		a.homeKey(key("down"))
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

// A folder pasted into an empty home box offers a one-use project start,
// while recognized slash commands retain their usual meaning.
func TestHomeSlashDoesNotSwallowAPastedPath(t *testing.T) {
	lab := newHomeLab(t)
	dir := t.TempDir()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	runCmd(a.openHome())

	pasteText(t, a, dir)
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
