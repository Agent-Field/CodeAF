package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// bodyRows is the conversation's rows as the frame lays them out, with the
// screen row each lands on.
func bodyRows(a *app) ([]row, int) {
	a.touch()
	body, _ := a.window(a.bodyWidth(), a.viewHeight())
	return body, a.bodyTop()
}

// bodyText is those rows, plain.
func bodyText(a *app) string {
	body, _ := bodyRows(a)
	var out []string
	for _, r := range body {
		out = append(out, ansi.Strip(r.text))
	}
	return strings.Join(out, "\n")
}

// sendRow puts a manager's finished team_send call on the conversation.
func sendRow(a *app, args, output string) {
	a.entries = append(a.entries, entry{kind: entryTool, tool: "team_send", status: toolOK, settled: true,
		text: "team_send", detail: toolDetail{Args: args, Output: output}})
	a.touch()
}

// THE MANAGER'S QUESTION IS A THREAD CARD THAT GROWS. The team_send row says
// who it went to and that it was a directive, quotes the words under it, and
// each member's answer is attached under it, muted, as it is read; the
// finishing folds into the answer's line.
func TestThreadCardGrowsAsRepliesLand(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 160, 40
	a.traffic.hidden = true
	price, _ := trafficHandle(t, a, harbor, "openrouter")
	rail, _ := trafficHandle(t, a, harbor, "Refactor")
	q, _ := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager,
		To: teamstore.ToSeveral, Handles: []string{price, rail}, Text: "status please"})
	trafficReadNow(t, a)
	sendRow(a, `{"to":"@`+price+` @`+rail+`","text":"status please","kind":"directive"}`, "Sent a directive to @"+price+", @"+rail+" ("+teamstore.ThreadNumber(q)+").")
	text := bodyText(a)
	if !strings.Contains(text, teamManagerGlyph+" to @"+price+" @"+rail+" · do") || !strings.Contains(text, "│ status please") {
		t.Fatalf("the call is not a thread card:\n%s", text)
	}
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindNote, From: price, To: teamstore.ToManager, Text: "prices are cached", Answers: q})
	trafficReadNow(t, a)
	if text := bodyText(a); !strings.Contains(text, "└ @"+price+"  prices are cached") {
		t.Fatalf("the first answer is not under the card:\n%s", text)
	}
	trafficAppend(t, a, harbor,
		teamstore.Entry{Kind: teamstore.KindEvent, From: price, To: teamstore.ToManager, State: teamstore.StateFinished, Text: "finished", Answers: q},
		teamstore.Entry{Kind: teamstore.KindNote, From: rail, To: teamstore.ToManager, Text: "scope model is done", Answers: q})
	trafficReadNow(t, a)
	text = bodyText(a)
	if !strings.Contains(text, "├ @"+price+"  ✓ prices are cached") || !strings.Contains(text, "└ @"+rail+"  scope model is done") {
		t.Fatalf("the card did not grow with the second answer and the finishing:\n%s", text)
	}
	// Muted: the answer's words are not in the ink the question is in.
	body, _ := bodyRows(a)
	for _, r := range body {
		if strings.Contains(ansi.Strip(r.text), "scope model is done") && strings.Contains(r.text, a.pal.ink("scope model is done")) {
			t.Fatalf("an answer is drawn in full ink: %q", r.text)
		}
	}
}

// A PRESS ON AN ANSWER'S WORDS LAYS THEM OUT, AND A SECOND FOLDS THEM; the
// hint line says them whole; a handle on the card is a link.
func TestThreadCardAnswerExpandsOnPress(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 160, 40
	a.traffic.hidden = true
	price, priceKey := trafficHandle(t, a, harbor, "openrouter")
	q, _ := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: price, Text: "status?"})
	long := strings.Repeat("every price is checked twice and cached for an hour ", 6) + "END"
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindNote, From: price, To: teamstore.ToManager, Text: long, Answers: q})
	trafficReadNow(t, a)
	sendRow(a, `{"to":"`+price+`","text":"status?","kind":"directive"}`, "Sent a directive to @"+price+" ("+teamstore.ThreadNumber(q)+").")
	if strings.Contains(bodyText(a), "END") {
		t.Fatal("the answer is drawn whole before a press")
	}
	body, top := bodyRows(a)
	y, x := -1, 0
	for i, r := range body {
		if r.hit == hitThread && strings.Contains(ansi.Strip(r.text), "every price") {
			y = top + i
			x = strings.Index(ansi.Strip(r.text), "every price") + 2
			for _, l := range r.links {
				if l.member != priceKey {
					t.Fatalf("the card's handle opens %q", l.member)
				}
			}
			if len(r.links) != 1 {
				t.Fatalf("the answer's handle is not a link: %+v", r.links)
			}
		}
	}
	if y < 0 {
		t.Fatalf("no answer row answers a press:\n%s", bodyText(a))
	}
	drive(t, a, motionTo(x, y))
	if words := a.dockHoverWords(); !strings.Contains(words, "END") {
		t.Fatalf("the hint line over the answer says %q", words)
	}
	front := a.frontTabKey()
	spend(t, a, a.press(x, y))
	if !strings.Contains(bodyText(a), "END") || a.frontTabKey() != front {
		t.Fatalf("the press did not lay the answer out in place:\n%s", bodyText(a))
	}
	spend(t, a, a.press(x, y))
	if strings.Contains(bodyText(a), "END") {
		t.Fatalf("the second press did not fold it:\n%s", bodyText(a))
	}
}

// THE MANAGER IS NOT SHOWN AN ANSWER TWICE. A delivery note whose every line
// is an answer already under its card is one dim line naming who answered; a
// line that answers nothing is still drawn.
func TestThreadCardFoldsTheDeliveredAnswers(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 160, 40
	a.traffic.hidden = true
	price, _ := trafficHandle(t, a, harbor, "openrouter")
	q, _ := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: price, Text: "status?"})
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindNote, From: price, To: teamstore.ToManager, Text: "prices are cached", Answers: q})
	trafficReadNow(t, a)
	sendRow(a, `{"to":"`+price+`","text":"status?","kind":"directive"}`, "Sent ("+teamstore.ThreadNumber(q)+").")
	note := "Your team's replies started this turn; the person did not speak.\n\nTeam traffic in \"harbor\", which you manage. These are your members' messages, not the person's words:\nfrom @" + price + ": prices are cached\n(rule)"
	a.entries = append(a.entries, entry{kind: entryTeam, text: note, settled: true,
		team: []session.TeamLine{{Team: "harbor", From: price, Kind: teamstore.KindNote, Text: "prices are cached"}}})
	text := bodyText(a)
	if strings.Count(text, "prices are cached") != 1 || !strings.Contains(text, "@"+price+" answered · in the thread above") {
		t.Fatalf("the delivered answer is drawn twice, or its line is missing:\n%s", text)
	}
	a.entries = append(a.entries, entry{kind: entryTeam, text: note, settled: true,
		team: []session.TeamLine{{Team: "harbor", From: price, Kind: teamstore.KindNote, Text: "an aside nobody asked for"}}})
	if text := bodyText(a); !strings.Contains(text, "an aside nobody asked for") {
		t.Fatalf("a line that answers nothing was folded:\n%s", text)
	}
}

// THE MEMBER'S CHAT MIRRORS IT: the manager's line as the quoted card, and the
// member's own answer to it under it, muted.
func TestThreadCardMemberMirror(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 160, 40
	price, priceKey := trafficHandle(t, a, harbor, "openrouter")
	rail, _ := trafficHandle(t, a, harbor, "Refactor")
	q, _ := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager,
		To: teamstore.ToSeveral, Handles: []string{price, rail}, Text: "status please"})
	trafficAppend(t, a, harbor,
		teamstore.Entry{Kind: teamstore.KindNote, From: rail, To: teamstore.ToManager, Text: "not price's answer", Answers: q},
		teamstore.Entry{Kind: teamstore.KindNote, From: price, To: teamstore.ToManager, Text: "prices are cached", Answers: q})
	trafficReadNow(t, a)
	spend(t, a, a.trafficGo(priceKey))
	if a.frontTabKey() != priceKey {
		t.Fatalf("the member is not in front")
	}
	trafficReadNow(t, a)
	note := "Team traffic in \"harbor\" for you (@" + price + "). These are the team's messages, not the person's words:\n◆ directive from manager " + teamstore.ThreadNumber(q) + ": status please\n(rule)"
	a.entries = append(a.entries, entry{kind: entryTeam, text: note, settled: true,
		team: []session.TeamLine{{Team: "harbor", From: teamstore.FromManager, Kind: teamstore.KindDirective, Text: "status please", Thread: q}}})
	text := bodyText(a)
	card := strings.Index(text, "│ status please")
	mine := strings.Index(text, "└ @"+price+"  prices are cached")
	if card < 0 || mine < card || strings.Contains(text, "not price's answer") {
		t.Fatalf("the member's chat does not mirror its answer under the manager's line:\n%s", text)
	}
}

// A MANAGER'S STEPS ARE SAID AS TEAM WORK. Its fold read `▾ team_send 1 call
// … 1 call`: the tool's own name where the work goes, and the count twice.
func TestTeamToolCaptionsSayTheWork(t *testing.T) {
	send := func(to string) entry {
		return entry{kind: entryTool, tool: "team_send", status: toolOK, detail: toolDetail{Args: `{"to":"` + to + `","text":"status?"}`}}
	}
	if got := composeCaption([]entry{send("@scrape model")}, 0, 1); got != "messaging @scrape @model" {
		t.Fatalf("one team_send is captioned %q", got)
	}
	if got := captionPast(composeCaption([]entry{send("everyone")}, 0, 1)); got != "messaged everyone" {
		t.Fatalf("a finished broadcast is captioned %q", got)
	}
	if got := composeCaption([]entry{send("scrape"), send("model")}, 0, 2); got != "sending 2 messages" {
		t.Fatalf("two sends are captioned %q", got)
	}
	start := entry{kind: entryTool, tool: "team_start", status: toolOK, detail: toolDetail{Args: `{"handle":"lexer","brief":"x"}`}}
	if got := composeCaption([]entry{start}, 0, 1); got != "starting @lexer" {
		t.Fatalf("a start is captioned %q", got)
	}
	for _, e := range []entry{send("a"), start} {
		if got := composeCaption([]entry{e}, 0, 1); strings.Contains(got, "team_") {
			t.Fatalf("a caption names the tool: %q", got)
		}
	}
}
