package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

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
