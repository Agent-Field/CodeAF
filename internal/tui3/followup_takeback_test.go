package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/history"
	"github.com/Agent-Field/codeaf/internal/manual"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/charmbracelet/x/ansi"
	"path/filepath"
)

// queuedConversation gives the surface a live turn and queued messages without a model clock.
func queuedConversation(t *testing.T, words ...string) (*wiredAgent, *app) {
	t.Helper()
	agent, a := wired(nil)
	a.state, a.stream, a.keysDisambiguated = stateWorking, make(chan session.Event), true
	for _, word := range words {
		a.input.setText(word)
		drive(t, a, key("ctrl+enter"))
	}
	return agent, a
}

func TestTakeBackPreservesCurrentDraft(t *testing.T) {
	_, a := queuedConversation(t, "queued words")
	a.input.setText("new draft " + pasteToken(2, 2))
	a.pastes = []pasteChip{{n: 2, text: "new\ndocument"}}
	drive(t, a, press(2, queuedRowY(t, a, 0)))
	if !strings.Contains(a.input.String(), "new draft") || !strings.Contains(a.input.String(), "queued words") || len(a.pastes) != 1 {
		t.Fatalf("take-back lost a draft or its paste: draft=%q pastes=%+v caret=%d", a.input.String(), a.pastes, a.input.cursor)
	}
}

func TestSuccessfulTakeBackSurvivesParentCloseBeforeFold(t *testing.T) {
	agent, a := queuedConversation(t, "unsent follow-up")
	cmd, took := a.recallQueuedAt(0)
	if !took {
		t.Fatal("click was not claimed")
	}
	answer := cmd()
	if len(agent.followStreams) != 0 {
		t.Fatal("engine did not remove the message")
	}
	// The parent close can reach Update before the unqueue result, even when the engine removed it first.
	_, _ = a.route(streamClosedMsg{gen: a.gen})
	_, _ = a.route(answer)
	if a.input.String() != "unsent follow-up" {
		t.Fatalf("successful unqueue never restored its words: draft=%q waiting=%d entries=%+v", a.input.String(), a.followWaiting(), a.entries)
	}
}

func TestTwoClicksBeforeFoldsPreserveBothMessages(t *testing.T) {
	agent, a := queuedConversation(t, "first follow-up", "second follow-up")
	first, ok := a.recallQueuedAt(0)
	if !ok {
		t.Fatal("first click was not claimed")
	}
	second, ok := a.recallQueuedAt(1)
	if !ok {
		t.Fatal("second click was not claimed")
	}
	answer1, answer2 := first(), second()
	_, _ = a.route(answer1)
	_, _ = a.route(answer2)
	if len(agent.followStreams) != 0 {
		t.Fatal("engine retained an unqueued message")
	}
	if !strings.Contains(a.input.String(), "first follow-up") || !strings.Contains(a.input.String(), "second follow-up") {
		t.Fatalf("two successful take-backs retained only %q", a.input.String())
	}
}

func TestQueuedRowsFitTheirHitTestWidth(t *testing.T) {
	_, a := wired(nil)
	for _, width := range []int{8, 20, 68} {
		a.follows = []queued{{text: strings.Repeat("w", width-2) + "\n" + strings.Repeat("x", width-2)}}
		for i, row := range a.followRows(width) {
			if got := ansi.StringWidth(row); got > width {
				t.Errorf("width=%d row=%d draws %d cells: %q", width, i, got, plain(row))
			}
		}
	}
}

func TestModifyOtherKeysCtrlEnterQueuesWithoutKittyReply(t *testing.T) {
	agent, a := wired(nil)
	a.state, a.stream = stateWorking, make(chan session.Event)
	a.input.setText("follow-up on xterm")
	drive(t, a, wirePress(t, "\x1b[27;5;13~", "ctrl+enter"))
	if len(agent.asked) != 1 {
		t.Fatalf("decoded ctrl+enter was discarded without kitty reply: draft=%q", a.input.String())
	}
}

func TestTakeBackStaysWithItsComposerRecipient(t *testing.T) {
	agent, a := queuedConversation(t, "words for the main conversation")
	cmd, ok := a.recallQueuedAt(0)
	if !ok {
		t.Fatal("click was not claimed")
	}
	answer := cmd()
	// Opening an ordinary task room performs this same recipient change.
	a.room = a.newRoom(7, "task seven")
	a.retargetComposer(taskRecipient(7))
	a.input.setText("existing task draft")
	_, _ = a.route(answer)
	if len(agent.followStreams) != 0 {
		t.Fatal("engine did not remove the message")
	}
	main := a.mainComposer()
	if a.input.String() != "existing task draft" || !strings.Contains(main.box.String(), "words for the main conversation") {
		t.Fatalf("take-back crossed recipients: task=%q main=%q", a.input.String(), main.box.String())
	}
}

func TestTakeBackPreservesDemotedSlashTag(t *testing.T) {
	_, a := wired(nil)
	a.state, a.stream, a.keysDisambiguated = stateWorking, make(chan session.Event), true
	a.input.setText("say /task")
	if !a.demoteTagBehindCaret() || len(a.liveTags()) != 0 {
		t.Fatal("could not demote the task tag")
	}
	drive(t, a, key("ctrl+enter"))
	drive(t, a, press(2, queuedRowY(t, a, 0)))
	if len(a.liveTags()) != 0 {
		t.Fatalf("take-back made a deliberately plain slash word executable: text=%q tags=%+v", a.input.String(), a.liveTags())
	}
}

func TestCtrlEnterRunsCommandsAndIdleSendsThroughEnter(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(fmtRunning(running), func(t *testing.T) {
			agent, a := wired(nil)
			a.keysDisambiguated = true
			if running {
				a.state, a.stream = stateWorking, make(chan session.Event)
			}
			a.input.setText("/cost")
			drive(t, a, key("ctrl+enter"))
			if len(agent.asked) != 0 || a.input.String() != "" || len(agent.sent) != 0 {
				t.Fatalf("ctrl+enter did not run /cost as enter: queued=%v sent=%v draft=%q", agent.asked, agent.sent, a.input.String())
			}
		})
	}
	agent, a := wired([]session.Event{})
	a.keysDisambiguated = true
	a.input.setText("an idle send")
	drive(t, a, key("ctrl+enter"))
	if len(agent.asked) != 0 || len(agent.sent) != 1 || agent.sent[0] != "an idle send" {
		t.Fatalf("idle ctrl+enter bypassed the ordinary send: queued=%v sent=%v", agent.asked, agent.sent)
	}
}

func fmtRunning(running bool) string {
	if running {
		return "running"
	}
	return "idle"
}

func TestCtrlEnterStartsTheChatOnTheStartPage(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.keysDisambiguated = true
	openStart(t, a)
	a.input.setText("words for the new conversation")
	drive(t, a, key("ctrl+enter"))
	if lab.made != 1 || a.startingChat() || len(lab.next.sent) != 1 || len(lab.agent.queue) != 0 {
		t.Fatalf("ctrl+enter sent under the start page: made=%d starting=%v new=%v old-queue=%d", lab.made, a.startingChat(), lab.next.sent, len(lab.agent.queue))
	}
}

// Repeating the start page's send chord while its door is held must reuse the
// first request, just as plain enter does. Channels pin the ordering without sleeps.
func TestRepeatedCtrlEnterWaitsForTheOpeningConversation(t *testing.T) {
	a := newStartLab(t).app()
	openStart(t, a)
	a.input.setText("the first message")
	entered, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	next := &fakeAgent{model: "m"}
	a.start = func(string) (Conversation, error) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		return Conversation{Agent: next, SessionFile: "/next/transcript.jsonl"}, nil
	}
	_, cmd := a.Update(key("ctrl+enter"))
	<-entered
	request, front := a.conversationRequest, a.frontGen
	_, _ = a.Update(key("ctrl+enter"))
	if !a.conversationOpening || a.conversationRequest != request || a.frontGen != front {
		t.Fatalf("repeated ctrl+enter replaced the pending request: opening=%v request=%d want=%d front=%d want=%d", a.conversationOpening, a.conversationRequest, request, a.frontGen, front)
	}
	close(release)
	spend(t, a, cmd)
	if a.file != "/next/transcript.jsonl" || len(next.sent) != 1 || next.sent[0] != "the first message" || a.at(pageHome) || a.startingChat() {
		t.Fatalf("the original opening did not send exactly once: file=%q sent=%v", a.file, next.sent)
	}
}

func TestASecondClickWhileTakeBackWaitsAsksNothing(t *testing.T) {
	_, a := queuedConversation(t, "queued words")
	first, ok := a.recallQueuedAt(0)
	if !ok || first == nil {
		t.Fatal("first click was not claimed")
	}
	second, _ := a.recallQueuedAt(0)
	if second != nil {
		t.Fatal("a second click asked for the same message again")
	}
	if a.queuedTakesBack() {
		t.Fatal("an in-flight take-back still offered its hover")
	}
	_, _ = a.route(first())
}

func TestRefusedTakeBackAdoptsItsTurnAfterTheParentCloses(t *testing.T) {
	agent, a := queuedConversation(t, "already running")
	ch := a.follows[0].ch
	agent.followStreams = nil
	cmd, ok := a.recallQueuedAt(0)
	if !ok {
		t.Fatal("click was not claimed")
	}
	answer := cmd()
	_, _ = a.route(streamClosedMsg{gen: a.gen})
	if a.stream != nil || len(a.entries) != 0 {
		t.Fatal("surface adopted a turn before its take-back settled")
	}
	_, _ = a.route(answer)
	if a.stream != ch || len(a.entries) != 1 || a.entries[0].text != "already running" || a.input.String() != "" {
		t.Fatalf("refusal failed to adopt exactly once: stream=%v entries=%+v draft=%q", a.stream, a.entries, a.input.String())
	}
}

func TestTakeBackMergesPasteNumbersPlainTagsAndTheExistingTray(t *testing.T) {
	_, a := queuedConversation(t)
	a.input.setText(pasteToken(1, 3) + " say /task")
	a.pastes = []pasteChip{{n: 1, text: "old\ndocument\nbody"}}
	if !a.demoteTagBehindCaret() {
		t.Fatal("could not demote queued tag")
	}
	drive(t, a, key("ctrl+enter"))
	a.input.setText("keep /task " + pasteToken(9, 3))
	a.input.demotedTags = []segment{{from: 5, to: 10}}
	a.pastes = []pasteChip{{n: 9, text: "new\ndocument\nbody"}}
	a.chips = []chip{{path: "/tmp/current.png"}}
	a.harnChip = "picked"
	a.input.cursor = 2
	drive(t, a, press(2, queuedRowY(t, a, 0)))
	want := "keep /task " + pasteToken(9, 3) + "\n" + pasteToken(10, 3) + " say /task"
	if a.input.String() != want || len(a.pastes) != 2 || a.pastes[1].n != 10 || len(a.liveTags()) != 0 || len(a.input.demotedTags) != 2 || len(a.chips) != 1 || a.harnChip != "picked" || a.input.cursor != len([]rune(want)) {
		t.Fatalf("merge damaged the composers: text=%q pastes=%+v plain=%+v tray=%+v harness=%q caret=%d", a.input.String(), a.pastes, a.input.demotedTags, a.chips, a.harnChip, a.input.cursor)
	}
}

func TestSuccessfulTakeBackRestoresInClickOrderWhenAnswersFoldBackwards(t *testing.T) {
	_, a := queuedConversation(t, "first in queue", "second in queue")
	first, _ := a.recallQueuedAt(1)
	second, _ := a.recallQueuedAt(0)
	answer1, answer2 := first(), second()
	_, _ = a.route(answer2)
	_, _ = a.route(answer1)
	if a.input.String() != "second in queue\nfirst in queue" {
		t.Fatalf("restoration did not keep click order: %q", a.input.String())
	}
}

func TestTakenBackWordsDoNotCrossConversations(t *testing.T) {
	_, a := queuedConversation(t)
	store := history.New(filepath.Join(t.TempDir(), "history.jsonl"))
	t.Cleanup(func() { _ = store.Close() })
	a.history = store
	a.input.setText("words from the old conversation")
	drive(t, a, key("ctrl+enter"))
	cmd, _ := a.recallQueuedAt(0)
	answer := cmd()
	a.frontGen++
	a.input.setText("new conversation draft")
	_, _ = a.route(answer)
	if a.input.String() != "new conversation draft" {
		t.Fatalf("take-back crossed conversations: %q", a.input.String())
	}
	found := false
	lines, _ := a.recallList()
	for _, line := range lines {
		found = found || line == "words from the old conversation"
	}
	if !found {
		t.Fatal("queued words were not remembered for history")
	}
}

// The older guard test changes the counter directly. This one commits home's
// asynchronous open, so it pins when that counter and the door's "here" change.
func TestTakeBackFollowsHomesAsynchronousConversationReplacement(t *testing.T) {
	for _, beforeCommit := range []bool{true, false} {
		name := "after completion"
		if beforeCommit {
			name = "before completion"
		}
		t.Run(name, func(t *testing.T) {
			agent, a := queuedConversation(t, "old conversation's queued words")
			dir := t.TempDir()
			a.file, a.workspace = filepath.Join(dir, "old", "transcript.jsonl"), dir
			a.turn = 1
			t.Cleanup(a.doorLine.close)
			recall, took := a.recallQueuedAt(0)
			if !took || recall == nil {
				t.Fatal("the old conversation did not offer take-back")
			}
			answer := recall()
			if len(agent.followStreams) != 0 {
				t.Fatal("the session did not remove the queued message")
			}
			a.openHome()
			next := &fakeAgent{model: "m"}
			line := homeLine{row: session.SessionRow{ProjectDir: dir, Transcript: filepath.Join(dir, "new", "transcript.jsonl")}}
			a.open = func(workspace, file string) (Conversation, error) {
				return Conversation{Agent: next, Workspace: workspace, SessionFile: file}, nil
			}
			front := a.frontGen
			opening := a.homeOpenDoor(line)
			if !a.conversationOpening || a.frontGen != front {
				t.Fatal("preparing the conversation changed the front before its completion")
			}
			if beforeCommit {
				_, _ = a.Update(answer)
				if a.input.String() != "old conversation's queued words" {
					t.Fatalf("the same conversation lost its take-back while opening: %q", a.input.String())
				}
			}
			spend(t, a, opening)
			if a.file != line.row.Transcript || a.agent != next || a.frontGen != front+1 {
				t.Fatalf("home did not replace the conversation through takeUp: file=%q front=%d want=%d", a.file, a.frontGen, front+1)
			}
			a.input.setText("new conversation draft")
			if !beforeCommit {
				_, _ = a.Update(answer)
			}
			if a.input.String() != "new conversation draft" || len(a.followRecalls) != 0 {
				t.Fatalf("the old take-back crossed conversations or remained pending: draft=%q pending=%d", a.input.String(), len(a.followRecalls))
			}
		})
	}
}

func TestOnlyThisWindowsQueuedRowsOfferTakeBack(t *testing.T) {
	_, a := queuedConversation(t, "local receipt")
	other := make(chan session.Event)
	_ = a.admitFollowing(Following{Said: "another window", Events: other})
	for _, index := range []int{1, 2} {
		if index == 2 {
			_ = a.steerFell(steerFellMsg{words: "steer fell through", ch: make(chan session.Event), gen: a.convGen})
		}
		y := queuedRowY(t, a, index)
		drive(t, a, motionTo(2, y))
		if a.hot.kind == hoverQueued || strings.Contains(strings.Join(a.followRows(a.width), "\n"), "\x1b[48;") {
			t.Fatalf("row %d without a local receipt offered hover", index)
		}
		if cmd, took := a.followPress(y); cmd != nil || took {
			t.Fatalf("row %d without a local receipt took the press", index)
		}
	}
}

func TestPendingTakeBackKeepsParkedWordsBehindTheSessionQueue(t *testing.T) {
	_, a := queuedConversation(t, "take this back", "then run this")
	a.parks = []parked{{text: "parked words"}}
	cmd, _ := a.recallQueuedAt(0)
	answer := cmd()
	_, _ = a.route(streamClosedMsg{gen: a.gen})
	if len(a.parks) != 1 || len(a.entries) != 0 {
		t.Fatalf("parked words jumped an unsettled take-back: parks=%+v entries=%+v", a.parks, a.entries)
	}
	_, _ = a.route(answer)
	if a.input.String() != "take this back" || a.stream == nil || len(a.entries) != 1 || a.entries[0].text != "then run this" || len(a.parks) != 1 {
		t.Fatalf("take-back did not adopt the next session turn first: draft=%q entries=%+v parks=%+v", a.input.String(), a.entries, a.parks)
	}
}

func TestLastTakeBackLetsTheParkedMessageRunAfterParentClose(t *testing.T) {
	_, a := queuedConversation(t, "take this back")
	a.parks = []parked{{text: "parked words"}}
	cmd, _ := a.recallQueuedAt(0)
	answer := cmd()
	_, _ = a.route(streamClosedMsg{gen: a.gen})
	if len(a.parks) != 1 {
		t.Fatal("parked message ran while a take-back was pending")
	}
	_, _ = a.route(answer)
	if a.input.String() != "take this back" || len(a.parks) != 0 || len(a.entries) != 1 || a.entries[0].text != "parked words" {
		t.Fatalf("last answer stranded or duplicated parked words: draft=%q parks=%+v entries=%+v", a.input.String(), a.parks, a.entries)
	}
}

func TestCtrlEnterCommitsTheSelectedSlashCommand(t *testing.T) {
	agent, a := wired(nil)
	store := history.New(filepath.Join(t.TempDir(), "history.jsonl"))
	t.Cleanup(func() { _ = store.Close() })
	a.history = store
	typeInto(t, a, "/cos")
	chosen := false
	for i, hit := range a.menu.hits {
		if commands[hit].name == "cost" {
			a.menu.cursor, chosen = i, true
		}
	}
	if !chosen {
		t.Fatal("/cost is missing from the typed command list")
	}
	drive(t, a, key("ctrl+enter"))
	lines, _ := a.recallList()
	if len(agent.asked) != 0 || a.input.String() != "" || len(lines) != 1 || lines[0] != "/cost" {
		t.Fatalf("ctrl+enter bypassed the selected command: queued=%v draft=%q history=%v", agent.asked, a.input.String(), lines)
	}
}

func TestCtrlEnterKeepsTheTaskRoomsEnterMeaning(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRail(t, a, 0)
	a.input.setText("words for this task")
	cmd, took := a.roomKey(key("ctrl+enter"))
	if !took || cmd == nil {
		t.Fatal("ctrl+enter bypassed the task room's enter door")
	}
	drive(t, a, runCmd(cmd)...)
	if a.followWaiting() != 0 {
		t.Fatal("task words reached the conversation queue")
	}
}

func TestCtrlEnterKeepsTheHeldRostersEnterMeaning(t *testing.T) {
	a, _, _ := roomApp(t)
	a.railTake(true)
	drive(t, a, key("ctrl+enter"))
	if !a.roomOpen() || a.room.id != 7 {
		t.Fatal("ctrl+enter did not open the roster's selected task")
	}
}

func TestCtrlEnterKeepsRewindsEnterMeaning(t *testing.T) {
	a, agent := newRewindApp(t, rewindPast())
	drive(t, a, runCmd(a.enterRewind())...)
	if !a.rew.on {
		t.Fatal("rewind did not open")
	}
	drive(t, a, key("ctrl+enter"))
	if a.rew.on || len(agent.cuts) != 1 {
		t.Fatal("ctrl+enter did not commit the selected rewind")
	}
}

func TestCtrlEnterLeavesTheModelPickersChordAlone(t *testing.T) {
	_, a := wired(nil)
	a.openPicker()
	a.input.setText("untouched draft")
	drive(t, a, key("ctrl+enter"))
	if !a.pick.open || a.input.String() != "untouched draft" {
		t.Fatal("ctrl+enter was converted above the model picker")
	}
}

// The corpus is the model's only account of these doors. A behavior test alone
// cannot catch a page that still tells it to deny a key or invent a refusal.
func TestQueueManualDescribesMergeAndDraftInput(t *testing.T) {
	keys, ok := manual.Chat().Page("keys")
	if !ok {
		t.Fatal("keys page is missing")
	}
	if !strings.Contains(keys, "appended on a new line") || !strings.Contains(keys, "modifyOtherKeys") {
		t.Error("queue manual omits draft merging or decoded modifyOtherKeys input")
	}
	if strings.Contains(keys, "`ctrl+enter` needs the\nsame support") {
		t.Error("queue manual denies input from a terminal without a kitty reply")
	}
}

func TestTakeBackKeepsPlainTagsWhenQueueingTrimsTheDraft(t *testing.T) {
	_, a := queuedConversation(t)
	a.input.setText(" \tsay /task \n")
	a.input.cursor = len([]rune(" \tsay /task"))
	if !a.demoteTagBehindCaret() {
		t.Fatal("could not demote the tag before the trailing whitespace")
	}
	drive(t, a, key("ctrl+enter"))
	drive(t, a, press(2, queuedRowY(t, a, 0)))
	if a.input.String() != "say /task" || len(a.liveTags()) != 0 || len(a.input.demotedTags) != 1 || a.input.demotedTags[0] != (segment{from: 4, to: 9}) {
		t.Fatalf("queue trimming lost the plain tag's position: draft=%q plain=%+v", a.input.String(), a.input.demotedTags)
	}
}

// A QUEUED DEMOTION MUST REACH THE TRANSCRIPT. The door-only fallback cannot
// recover a demoted recognition chip from the words alone.
func TestFollowUpKeepsQueuedDemotionsInTheTranscript(t *testing.T) {
	agent, a := queuedConversation(t)
	typeInto(t, a, "say /model now")
	// Backspace currently demotes send-door tags alone. Seed the non-door
	// snapshot here to prove this queue boundary preserves it independently.
	want := segment{from: 4, to: 10}
	a.input.demotedTags = []segment{want}
	drive(t, a, key("ctrl+enter"))
	if len(agent.asked) != 1 || agent.asked[0] != "say /model now" || len(a.follows) != 1 || !containsSegment(a.follows[0].demoted, want) {
		t.Fatalf("the queue did not keep the demotion: asked=%v follows=%+v", agent.asked, a.follows)
	}
	_, _ = a.route(streamClosedMsg{gen: a.gen})
	e := lastUserEntry(t, a)
	if e.text != "say /model now" || !containsSegment(e.plainTags, want) {
		t.Fatalf("the started follow-up lost its plain /model: text=%q plain=%+v", e.text, e.plainTags)
	}
	if got := transcriptCommandSpans([]rune(e.text), e.plainTags, 0); len(got) != 0 {
		t.Fatalf("the demoted follow-up would draw chips: %+v", got)
	}
}

// The transcript keeps both fallback door ranges and the queue's own ranges.
// Duplicate or stale offsets must not become additional plain annotations.
func TestFollowUpUnionsOnlyValidQueuedDemotions(t *testing.T) {
	_, a := wired(nil)
	words := "say /model now /task"
	model := segment{from: 4, to: 10}
	door := segment{from: 15, to: 20}
	a.follows = []queued{{
		text: words, ch: make(chan session.Event),
		demoted: []segment{model, model, door, {from: -1, to: 4}, {from: 4, to: 99}, {from: 0, to: 3}},
	}}
	a.startFollow()
	e := lastUserEntry(t, a)
	if len(e.plainTags) != 2 || !containsSegment(e.plainTags, model) || !containsSegment(e.plainTags, door) {
		t.Fatalf("the follow-up did not union valid plain ranges once: %+v", e.plainTags)
	}
}

// Backspace's real send-door demotion still queues as words, and the started
// transcript must retain the same plain range after the composer resets.
func TestFollowUpKeepsABackspacedTaskTagPlain(t *testing.T) {
	_, a := queuedConversation(t)
	typeInto(t, a, "say /task")
	drive(t, a, key("backspace"))
	want := segment{from: 4, to: 9}
	if a.input.String() != "say /task" || !containsSegment(a.input.demotedTags, want) || len(a.liveTags()) != 0 {
		t.Fatal("backspace did not demote the task tag")
	}
	drive(t, a, key("ctrl+enter"))
	if len(a.follows) != 1 || !containsSegment(a.follows[0].demoted, want) {
		t.Fatalf("the demoted task tag did not queue as words: %+v", a.follows)
	}
	_, _ = a.route(streamClosedMsg{gen: a.gen})
	e := lastUserEntry(t, a)
	if e.text != "say /task" || len(e.plainTags) != 1 || e.plainTags[0] != want {
		t.Fatalf("the backspaced tag did not stay plain: text=%q plain=%+v", e.text, e.plainTags)
	}
}
