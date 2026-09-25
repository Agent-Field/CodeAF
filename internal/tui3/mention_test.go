package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

func mentionApp(t *testing.T) *app {
	t.Helper()
	a := completionApp(t, "internal/tui3/app.go", "cmd/codeaf/main.go")
	a.pal = newPalette(tokens.ANSI256, false)
	a.wall.loaded = true
	a.wall.teams = []team{{
		ID: "t1", Name: "harbor", Hue: 210,
		Members: []teamMember{
			{Key: "/s/parser.jsonl", File: "/s/parser.jsonl", Handle: "parser", Word: "the parser"},
			{Key: "/s/web.jsonl", File: "/s/web.jsonl", Handle: "web", Word: "web frontend"},
		},
	}}
	a.comp.recentsHeld = true
	a.comp.recents = []mentionChat{{
		key: "/s/side.jsonl", file: "/s/side.jsonl",
		title: "side chat", slug: "side-chat", note: "side chat",
	}}
	return a
}

func TestMentionListSectionsFilterAndPrefixes(t *testing.T) {
	a := mentionApp(t)
	typeInto(t, a, "@")
	if !a.comp.open {
		t.Fatal("the bare @ did not open the list")
	}
	plainRows := plain(strings.Join(a.overlayRows(a.width, a.overlayHeight()), "\n"))
	for _, want := range []string{"team", "chat", "file", "harbor", "side chat", "app.go"} {
		if !strings.Contains(plainRows, want) {
			t.Fatalf("the list is missing %q:\n%s", want, plainRows)
		}
	}
	if _, ok := a.comp.teamChoice(); !ok {
		t.Fatal("the first choice is not the team")
	}

	typeInto(t, a, "har")
	if _, ok := a.comp.teamChoice(); !ok {
		t.Fatal("harbor dropped out of a query it matches")
	}
	if len(a.comp.chatHits) != 0 {
		t.Fatalf("conversations matched %q", a.comp.query)
	}

	a = mentionApp(t)
	typeInto(t, a, "@team:app")
	if len(a.comp.teamHits) != 0 || len(a.comp.chatHits) != 0 || len(a.comp.hits) != 0 {
		t.Fatal("@team: kept a section it does not name")
	}
	if !strings.Contains(plain(strings.Join(a.overlayRows(a.width, a.overlayHeight()), "\n")), "no team matches") {
		t.Fatal("a team prefix with no hit did not say so")
	}

	a = mentionApp(t)
	typeInto(t, a, "@file:app")
	if len(a.comp.teamHits) != 0 || len(a.comp.chatHits) != 0 {
		t.Fatal("@file: kept a team or a conversation")
	}
	if path, ok := a.comp.choice(); !ok || !strings.Contains(path, "app.go") {
		t.Fatalf("the file prefix chose %q", path)
	}

	a = mentionApp(t)
	typeInto(t, a, "@chat:side")
	if len(a.comp.chatHits) != 1 || a.comp.chatHits[0].slug != "side-chat" {
		t.Fatalf("the chat prefix kept %+v", a.comp.chatHits)
	}
	if len(a.comp.teamHits) != 0 || len(a.comp.hits) != 0 {
		t.Fatal("the chat prefix kept another section")
	}
}

func TestMentionPrefixWordIsAPress(t *testing.T) {
	a := mentionApp(t)
	typeInto(t, a, "@har")
	word, ok := mentionHeadAt(2)
	if !ok || word != scopeTeam {
		t.Fatalf("column 2 is %q", word)
	}
	var y int
	found := false
	for at := 0; at < a.height; at++ {
		mark, ok := a.chromeAt(at)
		if ok && mark.kind == chromeOverlay && mark.index == 0 {
			y, found = at, true
			break
		}
	}
	if !found {
		t.Fatal("the prefix row is not on the frame")
	}
	drive(t, a, tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseLeft})
	if got := a.input.String(); got != "@team:har" {
		t.Fatalf("the prefix word typed %q", got)
	}
	if a.hot.kind != hoverOverlay {
		a.hot = hoverAt{kind: hoverOverlay, key: scopeTeam}
	}
	if hint := a.mentionHeadHint(); !strings.Contains(hint, "only teams") || !strings.Contains(hint, "click") {
		t.Fatalf("the prefix hint is %q", hint)
	}
}

func TestMentionInsertsTheTokenAndLinksIt(t *testing.T) {
	a := mentionApp(t)
	typeInto(t, a, "see @")
	drive(t, a, key("enter"))
	if got := a.input.String(); got != "see ●harbor" {
		t.Fatalf("choosing the team inserted %q", got)
	}
	painted := a.paintDraftMentions([]string{a.input.String()})
	if ansi.Strip(painted[0]) != "see ●harbor" {
		t.Fatalf("the draft's runes moved: %q", ansi.Strip(painted[0]))
	}
	if painted[0] == "see ●harbor" {
		t.Fatal("the team mark was not drawn in the team's colour")
	}

	a = mentionApp(t)
	typeInto(t, a, "@chat:side")
	drive(t, a, key("enter"))
	if got := a.input.String(); got != "@side-chat" {
		t.Fatalf("choosing the conversation inserted %q", got)
	}

	a = mentionApp(t)
	a.entries = []entry{{kind: entryUser, text: "see ●harbor and @parser"}}
	rows := []row{{entry: 0, text: "see ●harbor and @parser"}}
	a.mentionLinkPass(rows, a.entries)
	if len(rows[0].links) != 2 {
		t.Fatalf("the sent line has %d links", len(rows[0].links))
	}
	if rows[0].links[0].team != "t1" {
		t.Fatalf("the team link is %+v", rows[0].links[0])
	}
	if rows[0].links[1].member != "/s/parser.jsonl" {
		t.Fatalf("the chat link is %+v", rows[0].links[1])
	}
	if !strings.Contains(ansi.Strip(rows[0].text), "●harbor") || !strings.Contains(ansi.Strip(rows[0].text), "@parser") {
		t.Fatalf("the linked line is %q", ansi.Strip(rows[0].text))
	}
	if hint := a.mentionChatHint("/s/parser.jsonl"); !strings.Contains(hint, "the parser") {
		t.Fatalf("the chat hint is %q", hint)
	}
}

func TestMentionFrameDoesNotReadRecents(t *testing.T) {
	a := mentionApp(t)
	framing := false
	a.recentSessions = func() []Session {
		if framing {
			t.Fatal("a frame read the recent conversations")
		}
		return nil
	}
	typeInto(t, a, "@harbor")
	framing = true
	_ = a.View()
	a.mentionLinkPass([]row{{entry: 0, text: "●harbor"}}, []entry{{kind: entryUser}})
}
