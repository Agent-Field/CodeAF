package tui3

import (
	"strings"
	"testing"
	"time"

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

// TestMentionRecentsAreReadAgainOnEachOpening is the list against a
// conversation that did not exist, or had no name, the first time "@" opened
// in this window. A snapshot taken once per process kept it off the list for
// as long as the window lived, with its title in plain sight on a tab strip.
func TestMentionRecentsAreReadAgainOnEachOpening(t *testing.T) {
	a := mentionApp(t)
	a.comp.recentsHeld, a.comp.recents = false, nil
	reads := 0
	rows := []Session{{Title: "openrouter price scrape", File: "/s/price.jsonl"}}
	a.recentSessions = func() []Session {
		reads++
		return append([]Session(nil), rows...)
	}
	typeInto(t, a, "@chat:")
	if reads != 1 {
		t.Fatalf("the first opening read the recent list %d times", reads)
	}
	if len(a.comp.chatHits) != 1 || a.comp.chatHits[0].title != "openrouter price scrape" {
		t.Fatalf("the first opening lists %+v", a.comp.chatHits)
	}
	drive(t, a, key("esc"))
	if a.comp.open {
		t.Fatal("esc did not close the list")
	}
	rows = append(rows, Session{Title: "Cloudflare worker deploy", File: "/s/cloudflare.jsonl"})
	typeInto(t, a, " @chat:cloudfl")
	if reads != 2 {
		t.Fatalf("the second opening left the recent list at %d reads", reads)
	}
	if len(a.comp.chatHits) != 1 || a.comp.chatHits[0].title != "Cloudflare worker deploy" {
		t.Fatalf("a conversation started after the first opening is not on the list: %+v", a.comp.chatHits)
	}
	// And the letters after the "@" do not walk the disk again.
	typeInto(t, a, "are")
	if reads != 2 {
		t.Fatalf("typing into an open list read the recent list again: %d reads", reads)
	}
}

// TestChatPrefixKeepsEveryConversation: the mixed list keeps a screenful of
// conversations beside the teams, tasks and files; "@chat:" is the person
// asking for conversations and nothing else, and it keeps the whole catalog
// rather than the first eight.
func TestChatPrefixKeepsEveryConversation(t *testing.T) {
	a := mentionApp(t)
	a.comp.recents = a.comp.recents[:0]
	for i := 0; i < mentionRows+4; i++ {
		a.comp.recents = append(a.comp.recents, mentionChat{
			key: "/s/chat-" + itoa(i) + ".jsonl", file: "/s/chat-" + itoa(i) + ".jsonl",
			title: "chat " + itoa(i), slug: "chat-" + itoa(i), note: "chat " + itoa(i),
		})
	}
	typeInto(t, a, "@")
	if len(a.comp.chatHits) != mentionRows {
		t.Fatalf("the mixed list keeps %d conversations, and a screenful is %d", len(a.comp.chatHits), mentionRows)
	}
	a = mentionApp(t)
	a.comp.recents = a.comp.recents[:0]
	for i := 0; i < mentionRows+4; i++ {
		a.comp.recents = append(a.comp.recents, mentionChat{
			key: "/s/chat-" + itoa(i) + ".jsonl", file: "/s/chat-" + itoa(i) + ".jsonl",
			title: "chat " + itoa(i), slug: "chat-" + itoa(i), note: "chat " + itoa(i),
		})
	}
	typeInto(t, a, "@chat:")
	if len(a.comp.chatHits) != mentionRows+4 {
		t.Fatalf("the chat prefix keeps %d conversations of %d", len(a.comp.chatHits), mentionRows+4)
	}
	// The last one is reachable: the cursor walks past the first screenful.
	for i := 0; i < mentionRows+3; i++ {
		drive(t, a, key("down"))
	}
	if chat, ok := a.comp.chatChoice(); !ok || chat.slug != "chat-"+itoa(mentionRows+3) {
		t.Fatalf("the cursor stopped on %+v", chat)
	}
}

// TestMentionRecentsAreKeyedLikeTabs: a recent row is the same conversation as
// a tab when their canonical files agree, whatever spelling the walk used. A
// home reached through a symlink used to list the conversation in front, and
// every open tab a second time, as recent rows.
func TestMentionRecentsAreKeyedLikeTabs(t *testing.T) {
	a := mentionApp(t)
	emptyMachine(a)
	a.file = "/tmp/lab/this-one.jsonl"
	a.openingPrompt = "the current conversation"
	a.stow(Conversation{
		Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/lab/cf.jsonl",
		Workspace: "/tmp/lab", Place: "lab",
	}, &aside{since: a.now().Add(-12 * time.Minute), title: "Cloudflare worker deploy"})
	_ = a.tabsRow(a.width)
	a.comp.recentsHeld, a.comp.recents = false, nil
	a.recentSessions = func() []Session {
		return []Session{
			{Title: "the current conversation", File: "/tmp/lab/x/../this-one.jsonl"},
			{Title: "Cloudflare worker deploy", File: "/tmp/lab/x/../cf.jsonl"},
			{Title: "openrouter price scrape", File: "/tmp/lab/x/../price.jsonl"},
		}
	}
	typeInto(t, a, "@chat:")
	var got []string
	for _, chat := range a.comp.chatHits {
		got = append(got, chat.title+"/"+chat.key)
	}
	if len(got) != 2 || got[0] != "Cloudflare worker deploy//tmp/lab/cf.jsonl" || got[1] != "openrouter price scrape//tmp/lab/price.jsonl" {
		t.Fatalf("the list is %q", got)
	}
}

// TestTheStartPageLeavesNoConversationOffTheList: `+` opens no file, so the
// window still carries the conversation it came from as the one in front. The
// sentence typed on the page opens a NEW conversation, so that one is on the
// list — it was left off as though the person were typing inside it, and
// `@chat:kim` beside a lit `tell me about kim jung il` said nothing matched.
func TestTheStartPageLeavesNoConversationOffTheList(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.title = "tell me about kim jung il"
	a.comp.recentsHeld = true
	openStart(t, a)
	if !a.startingChat() {
		t.Fatal("+ did not open the start page")
	}
	typeInto(t, a, "@chat:kim")
	if !a.comp.open {
		t.Fatal("@chat:kim did not open the list on the start page")
	}
	if len(a.comp.chatHits) != 1 || a.comp.chatHits[0].title != "tell me about kim jung il" {
		t.Fatalf("the start page's list holds %+v, and the conversation it came from is open", a.comp.chatHits)
	}
	drive(t, a, key("enter"))
	if got := a.input.String(); got != "@tell-me-about-kim-jung-il" {
		t.Fatalf("choosing it typed %q", got)
	}
}
