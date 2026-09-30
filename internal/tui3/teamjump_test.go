package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// fillEntries puts n settled notes on the conversation, so a message among
// them is off screen until something scrolls to it.
func fillEntries(a *app, n int, word string) {
	for i := 0; i < n; i++ {
		a.entries = append(a.entries, entry{kind: entryNote, text: word + " " + itoa(i), settled: true})
	}
	a.touch()
}

// landedRow is the drawn row of the body holding want, and whether it wears
// the lift.
func landedRow(a *app, want string) (bool, bool) {
	body, _ := a.bodyRows(a.bodyWidth(), a.viewHeight())
	plainRows, _ := a.window(a.bodyWidth(), a.viewHeight())
	for i, r := range body {
		if strings.Contains(ansi.Strip(r.text), want) {
			return true, i < len(plainRows) && r.text != plainRows[i].text
		}
	}
	return false, false
}

// A HANDLE ON A THREAD'S HEADER OPENS ITS MEMBER AT THE DIRECTIVE, scrolled into
// view and lifted, with the member's conversation in front; a handle on an
// answer is a door onto the member's own post.
func TestTrafficJumpOpensTheMemberAtTheMessage(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 160, 30
	price, priceKey := trafficHandle(t, a, harbor, "openrouter")
	rail, _ := trafficHandle(t, a, harbor, "Refactor")
	q := threadScenario(t, a, harbor, price, rail)
	// The column's handles carry where they land: the work row's at the
	// directive, and a reply's, laid open, at the member's own post.
	a.sideToggleThread(sideThreadKey(harbor, q))
	_ = railLines(t, a)
	var header, answer string
	for _, row := range a.side.last {
		for _, d := range row.doors {
			if d.act.kind != sideActJump || d.act.key != priceKey {
				continue
			}
			if row.key == "thread/"+q {
				header = d.act.entry
			} else if strings.HasPrefix(row.key, "reply/") {
				answer = d.act.entry
			}
		}
	}
	if header != q || answer == "" || answer == q {
		t.Fatalf("the handles do not carry their message: header %q answer %q", header, answer)
	}
	// In the member's conversation the directive sits above a long tail.
	spend(t, a, a.trafficGo(priceKey))
	if a.frontTabKey() != priceKey {
		t.Fatal("the member is not in front")
	}
	fillEntries(a, 5, "before")
	note := "Team traffic in \"harbor\" for you (@" + price + "). These are the team's messages, not the person's words:\n◆ directive from manager " + teamstore.ThreadNumber(q) + ": Please provide a brief status update on your part\n(rule)"
	a.entries = append(a.entries, entry{kind: entryTeam, text: note, settled: true,
		team: []session.TeamLine{{Team: "harbor", From: teamstore.FromManager, Kind: teamstore.KindDirective, Text: "Please provide a brief status update on your part", Thread: q}}})
	fillEntries(a, 60, "after")
	a.offset, a.stick = 0, true
	if shown, _ := landedRow(a, "brief status update"); shown {
		t.Fatal("the directive is on screen before the jump")
	}
	spend(t, a, a.trafficJump(priceKey, q))
	shown, lifted := landedRow(a, "brief status update")
	if !shown || !lifted || a.frontTabKey() != priceKey {
		t.Fatalf("the jump did not bring the directive into view, lifted (shown %v lifted %v)", shown, lifted)
	}
	// AND AN ANSWER'S HANDLE LANDS AT THE MEMBER'S OWN POST.
	fillEntries(a, 3, "gap")
	a.entries = append(a.entries, entry{kind: entryTool, tool: "team_post", status: toolOK, settled: true, text: "team_post",
		detail: toolDetail{Args: `{"to":"manager","text":"my own answer"}`, Output: "Posted to the manager in \"harbor\" as " + teamstore.ThreadNumber(answer) + ", answering " + teamstore.ThreadNumber(q) + ". It arrives at the start of their next step."}})
	fillEntries(a, 60, "tail")
	a.offset, a.stick = 0, true
	if at := a.teamEntryAt(answer); at != len(a.entries)-61 {
		t.Fatalf("the member's own post is not found: %d", at)
	}
	spend(t, a, a.trafficJump(priceKey, answer))
	if a.traffic.landing.entry != len(a.entries)-61 {
		t.Fatalf("the jump landed on entry %d", a.traffic.landing.entry)
	}
	// AND A JUMP WAITS FOR ITS CONVERSATION: kept while another is in front.
	a.traffic.jump = trafficJumpTo{key: "somebody-else", id: q, until: a.now().Add(trafficJumpWait)}
	if a.trafficLand(); a.traffic.jump.key == "" {
		t.Fatal("a jump for a conversation not in front was dropped")
	}
}

// A MESSAGE OLDER THAN THE CONVERSATION'S HISTORY opens at the bottom and the
// hint line says so.
func TestTrafficJumpToAnOlderMessage(t *testing.T) {
	a, _, _, _ := trafficApp(t)
	a.width, a.height = 160, 30
	fillEntries(a, 60, "line")
	a.offset, a.stick = 0, false
	spend(t, a, a.trafficJump("", "000000000999"))
	if !a.stick || a.dockHoverWords() != trafficOlderWords {
		t.Fatalf("an older message left stick %v and the hint %q", a.stick, a.dockHoverWords())
	}
}

// A hosted switch puts the member in front before its atomic transcript
// arrives. The reply's receipt must still be allowed to land after replay.
func TestTrafficReplyJumpWaitsForHostedReplay(t *testing.T) {
	const id = "000000000002"
	a, _, _, _ := trafficApp(t)
	agent := replayTestAgent(nil)
	agent.entries = []session.DisplayEntry{{Role: "tool", Tool: "team_post", Hint: "team_post", Output: "Posted to the manager in \"traffic\" as #2, answering #1"}}
	a.agent = agent
	replay := a.refreshHostedReplay()
	_ = a.trafficJump("", id)
	if a.traffic.jump.id != id || a.trafficJumpWords() != "" {
		t.Fatalf("jump settled before replay: jump=%+v hint=%q", a.traffic.jump, a.trafficJumpWords())
	}
	drain(t, a, replay)
	if a.traffic.landing.entry < 0 || a.traffic.landing.older || a.traffic.jump.id != "" {
		t.Fatalf("reply did not land after replay: landing=%+v jump=%+v", a.traffic.landing, a.traffic.jump)
	}

	b, _, _, _ := trafficApp(t)
	b.hostReplayLoading = true
	_ = b.trafficJump("", id)
	b.traffic.jump.until = b.now().Add(-time.Second)
	b.trafficLand()
	if b.trafficJumpWords() != trafficOlderWords {
		t.Fatalf("expired jump has hint %q", b.trafficJumpWords())
	}
}

// IN THE MANAGER'S CONVERSATION a press on a row of work brings the thread
// card into view, lifted, and the conversation in front stays in front; a
// handle on the card's answer opens the member at its own post.
func TestTrafficJumpBringsTheCardIntoView(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 160, 30
	price, _ := trafficHandle(t, a, harbor, "openrouter")
	q, _ := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: price, Text: "status?"})
	reply, _ := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindNote, From: price, To: teamstore.ToManager, Text: "prices are cached", Answers: q})
	trafficReadNow(t, a)
	fillEntries(a, 5, "before")
	sendRow(a, `{"to":"`+price+`","text":"status?","kind":"directive"}`, "Sent a directive to @"+price+" ("+teamstore.ThreadNumber(q)+").")
	fillEntries(a, 60, "after")
	if shown, _ := landedRow(a, "│ status?"); shown {
		t.Fatal("the card is on screen before the press")
	}
	front := a.frontTabKey()
	_ = railLines(t, a)
	x, y := sideRowOn(t, a, "thread/"+q)
	sideClick(t, a, x, y)
	if shown, lifted := landedRow(a, "│ status?"); !shown || !lifted || a.frontTabKey() != front {
		t.Fatalf("the press did not bring the card into view, lifted, here (shown %v lifted %v)", shown, lifted)
	}
	// The card's answer row names the answer for its handle.
	body, _ := bodyRows(a)
	found := false
	for _, r := range body {
		if strings.Contains(ansi.Strip(r.text), "prices are cached") && r.open != "" {
			found = a.threadRowLand(r) == reply
		}
	}
	if !found {
		t.Fatal("the card's answer does not open its member at the answer")
	}
}

// A PRESS ON THE ROW, NOT ONLY THE HANDLE, OPENS THE CHAT THE MESSAGE BELONGS
// TO, at that message. The manager's own words stay here and scroll. A reply
// opens the member who wrote it. In the member's chat, the member's own reply
// scrolls in place, and a message the manager wrote opens the manager.
func TestTrafficPressOpensTheConversationTheMessageBelongsTo(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 160, 40
	price, priceKey := trafficHandle(t, a, harbor, "openrouter")
	manager := a.frontTabKey()
	q, _ := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: teamstore.ToEveryone, Text: "all hands on the parser"})
	reply, _ := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindNote, From: price, To: teamstore.ToManager, Text: "parser numbers are in", Answers: q})
	trafficReadNow(t, a)
	fillEntries(a, 8, "before")
	sendRow(a, `{"to":"everyone","text":"all hands on the parser","kind":"directive"}`, "Sent a directive to everyone ("+teamstore.ThreadNumber(q)+").")
	fillEntries(a, 60, "after")
	a.offset, a.stick = 0, true
	_ = railLines(t, a)
	x, y := sideRowOn(t, a, "thread/"+q)
	sideClick(t, a, x, y)
	shown, lifted := landedRow(a, "all hands on the parser")
	if !shown || !lifted || a.frontTabKey() != manager {
		t.Fatalf("a press on the manager's row did not stay and lift it (shown %v lifted %v front %q)", shown, lifted, a.frontTabKey())
	}
	if a.traffic.landing.entry < 0 {
		t.Fatal("the manager's row did not highlight an entry")
	}

	// THE REPLY OPENS THE MEMBER WHO WROTE IT.
	a.sideToggleThread(sideThreadKey(harbor, q))
	_ = railLines(t, a)
	x, y = sideRowOn(t, a, "reply/"+reply)
	sideClick(t, a, x, y)
	if a.frontTabKey() != priceKey {
		t.Fatalf("a press on the reply opened %q, want %q", a.frontTabKey(), priceKey)
	}

	// IN THE MEMBER'S CHAT the member's own line scrolls here, and the
	// manager's line opens the manager.
	note := "Posted to the manager in \"harbor\" as " + teamstore.ThreadNumber(reply) + ", answering " + teamstore.ThreadNumber(q) + "."
	a.entries = nil
	fillEntries(a, 8, "pad")
	a.entries = append(a.entries, entry{kind: entryTool, tool: "team_post", status: toolOK, settled: true, text: "team_post",
		detail: toolDetail{Output: note}})
	fillEntries(a, 60, "tail")
	a.offset, a.stick = 0, true
	a.sideSetView(sideTraffic)
	_ = railLines(t, a)
	x, y = sideRowOn(t, a, railKeyOfReply(t, a, "parser numbers"))
	front := a.frontTabKey()
	sideClick(t, a, x, y)
	if a.frontTabKey() != front || a.traffic.landing.entry < 0 {
		t.Fatalf("a press on the member's own row left %q or highlighted nothing (entry %d)", a.frontTabKey(), a.traffic.landing.entry)
	}
	x, y = sideRowOn(t, a, railKeyOfReply(t, a, "all hands"))
	sideClick(t, a, x, y)
	if a.frontTabKey() != manager {
		t.Fatalf("a press on the manager's message in the member's chat opened %q", a.frontTabKey())
	}
}

// A row press must reveal the actual message, not merely remember its hidden index.
func TestTrafficJumpRevealsACompletedToolInsideClosedWork(t *testing.T) {
	for _, tool := range []string{"team_send", "team_post"} {
		t.Run(tool, func(t *testing.T) {
			a, _, _, _ := trafficApp(t)
			a.width, a.height = 160, 30
			a.workMode = config.WorkFold
			a.entries = foldFixture()
			id := "000000000123"
			output := "Sent to @review (#123)."
			if tool == "team_post" {
				output = "Posted to the manager as #123, answering #122."
			}
			a.entries[3] = entry{kind: entryTool, tool: tool, text: tool, turn: 1, status: toolOK, settled: true,
				detail: toolDetail{Args: `{"to":"review","text":"findings"}`, Output: output}}
			a.touch()
			initiallyVisible := false
			for _, r := range a.visible(a.bodyWidth()) {
				initiallyVisible = initiallyVisible || r.entry == 3
			}
			// Completed sends and posts both stay inside closed work until
			// the person deliberately opens their Traffic row.
			if initiallyVisible {
				t.Fatalf("unexpected initial visibility for %s: %v", tool, initiallyVisible)
			}
			spend(t, a, a.trafficJump("", id))
			if a.traffic.landing.entry != 3 {
				t.Fatalf("target = %d, want 3", a.traffic.landing.entry)
			}
			found := false
			for _, r := range a.visible(a.bodyWidth()) {
				found = found || r.entry == 3
			}
			if !found {
				t.Fatal("jump found the target but left its enclosing disclosure closed")
			}
		})
	}
}

// A new-member root predates numbered tool receipts. Its exact accepted brief
// still identifies the manager's call, and the delivered member card keeps ID.
func TestTrafficStartRootRevealsManagerCallAndMemberBrief(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 160, 40
	handle, member := trafficHandle(t, a, harbor, "openrouter")
	manager := a.frontTabKey()
	brief := "Check the invoice report against the source rows"
	id, err := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindStart, From: teamstore.FromManager, To: handle, Text: brief})
	if err != nil {
		t.Fatal(err)
	}
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindNote, From: handle, To: teamstore.ToManager, Text: "Invoice rows checked", Answers: id})
	trafficReadNow(t, a)
	a.sideSetView(sideTraffic)
	a.workMode = config.WorkFold
	a.entries = foldFixture()
	a.entries[3] = entry{kind: entryTool, tool: "team_start", text: "team_start", turn: 1, status: toolOK, settled: true,
		detail: toolDetail{Args: `{"handle":"` + handle + `","brief":"` + brief + `"}`, Output: "Asked for a new member @" + handle + " in \"harbor\"."}}
	a.touch()
	for _, r := range a.visible(a.bodyWidth()) {
		if r.entry == 3 {
			t.Fatal("start should begin inside closed work")
		}
	}
	_ = railLines(t, a)
	x, y := sideRowOn(t, a, "thread/"+id)
	sideClick(t, a, x, y)
	if a.frontTabKey() != manager || a.traffic.landing.entry != 3 || a.traffic.landing.older {
		t.Fatalf("root did not land on manager start: %+v", a.traffic.landing)
	}
	found := false
	for _, r := range a.visible(a.bodyWidth()) {
		found = found || r.entry == 3
	}
	if !found {
		t.Fatal("root jump left manager start hidden")
	}
	spend(t, a, a.trafficGo(member))
	note := "Team traffic in \"harbor\" for you (@" + handle + "). These are the team's messages, not the person's words:\n◆ brief from manager " + teamstore.ThreadNumber(id) + ": " + brief + "\n(rule)"
	a.entries, _ = a.replayBlocks([]session.DisplayEntry{{Role: "aside", Text: note, Team: []session.TeamLine{{Team: "harbor", From: teamstore.FromManager, Kind: teamstore.KindStart, Text: brief, Thread: id}}}}, replayShape{})
	fillEntries(a, 60, "after")
	a.offset, a.stick = 0, true
	spend(t, a, a.trafficJump(member, id))
	shown, lifted := landedRow(a, brief)
	if !shown || !lifted || a.traffic.landing.older {
		t.Fatalf("member brief not shown/lifted: %v %v %+v", shown, lifted, a.traffic.landing)
	}
}

func TestTrafficStartRootDoesNotGuessAnotherCall(t *testing.T) {
	for _, tc := range []struct {
		name, handle, brief string
		status              toolState
	}{
		{"different handle", "other", "accepted brief", toolOK},
		{"different brief", "review", "other brief", toolOK},
		{"failed start", "review", "accepted brief", toolFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, harbor, _, _ := trafficApp(t)
			a.traffic.rows = map[string][]teamstore.Entry{harbor: {{ID: "000000000123", Kind: teamstore.KindStart, From: teamstore.FromManager, To: "review", Text: "accepted brief"}}}
			a.entries = []entry{{kind: entryTool, tool: "team_start", status: tc.status, settled: true, detail: toolDetail{Args: `{"handle":"` + tc.handle + `","brief":"` + tc.brief + `"}`, Output: "Asked for a new member."}}}
			if at := a.teamEntryAt("000000000123"); at != -1 {
				t.Fatalf("matched unrelated call at %d", at)
			}
		})
	}
}

func TestTrafficStartRootUsesReceiptAndRejectsAmbiguousLegacy(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	root := teamstore.Entry{ID: "000000000123", Kind: teamstore.KindStart, From: teamstore.FromManager, To: "review", Text: "accepted brief"}
	later := root
	later.ID = "000000000124"
	a.traffic.rows = map[string][]teamstore.Entry{harbor: {root, later}}
	call := entry{kind: entryTool, tool: "team_start", status: toolOK, settled: true, detail: toolDetail{Args: `{"handle":"review","brief":"accepted brief"}`, Output: "Asked for a new member."}}
	a.entries = []entry{call, call}
	if at := a.teamEntryAt(root.ID); at != -1 {
		t.Fatalf("ambiguous legacy start guessed entry%d", at)
	}
	a.entries[0].detail.Output = "Asked for a new member @review in \"harbor\" (#123)."
	a.entries[1].detail.Output = "Asked for a new member @review in \"harbor\" (#124)."
	if at := a.teamEntryAt(root.ID); at != 0 {
		t.Fatalf("receipt selected entry%d instead of0", at)
	}
	if at := a.teamEntryAt(later.ID); at != 1 {
		t.Fatalf("receipt selected entry%d instead of1", at)
	}
	a.entries[0].detail.Args = `{"handle":"review","brief":"accepted brief","team":"another-team"}`
	if at := a.teamEntryAt(root.ID); at != -1 {
		t.Fatalf("cross-team receipt selected entry%d", at)
	}
	a.entries[0] = call
	a.entries = a.entries[:1]
	if at := a.teamEntryAt(root.ID); at != -1 {
		t.Fatalf("two roots for one legacy call guessed entry%d", at)
	}
}

// TRAFFIC NUMBERS COUNT PER TEAM. A manager that also sends into another team
// holds a `team_send` receipt carrying the same `(#1)` as this team's start;
// pressing this team's start row must land on its own `team_start`, and a send
// into the other team never answers for a row of this one.
func TestTrafficJumpNeverLandsOnAnotherTeamsSameNumber(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	start := teamstore.Entry{ID: "000000000001", Kind: teamstore.KindStart, From: teamstore.FromManager, To: "review", Text: "same brief"}
	send := teamstore.Entry{ID: "000000000002", Kind: teamstore.KindDirective, From: teamstore.FromManager, To: "review", Text: "and check the totals"}
	a.traffic.rows = map[string][]teamstore.Entry{harbor: {start, send}}
	started := entry{kind: entryTool, tool: "team_start", status: toolOK, settled: true,
		detail: toolDetail{Args: `{"handle":"review","brief":"same brief"}`, Output: "Asked for a new member @review (#1)."}}
	ownSend := entry{kind: entryTool, tool: "team_send", status: toolOK, settled: true,
		detail: toolDetail{Args: `{"to":"review","text":"and check the totals"}`, Output: "Sent to @review (#2)."}}
	otherStart := entry{kind: entryTool, tool: "team_send", status: toolOK, settled: true,
		detail: toolDetail{Args: `{"team":"other","to":"someone","text":"different work"}`, Output: "Sent to @someone (#1)."}}
	otherSend := entry{kind: entryTool, tool: "team_send", status: toolOK, settled: true,
		detail: toolDetail{Args: `{"team":"other","to":"someone","text":"more different work"}`, Output: "Sent to @someone (#2)."}}
	a.entries = []entry{started, ownSend, otherStart, otherSend}
	if got := a.teamEntryAt(start.ID); got != 0 {
		t.Fatalf("this team's start landed on entry %d, want its own team_start at 0", got)
	}
	if got := a.teamEntryAt(send.ID); got != 1 {
		t.Fatalf("this team's send landed on entry %d, want its own team_send at 1", got)
	}
}
