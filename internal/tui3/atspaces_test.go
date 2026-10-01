package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── THE WORDS AFTER `@` MAY HAVE SPACES, ON EVERY SECTION AND EVERY BOX ──────

// The token walks back over spaces to the nearest `@` that begins a word, and
// no further than three of them or a newline.
func TestAtTokenHoldsSpaces(t *testing.T) {
	for _, tc := range []struct {
		draft string
		at    int
		query string
		ok    bool
	}{
		{"@chat:who is", 0, "chat:who is", true},
		{"see @who is kim jong", 4, "who is kim jong", true},
		{"see @one two three four five", 0, "", false},
		{"mail foo@bar.com now", 0, "", false},
		{"@parser\nand then", 0, "", false},
		{"@", 0, "", true},
		{"plain words", 0, "", false},
	} {
		value := []rune(tc.draft)
		at, query, ok := atToken(value, len(value))
		if ok != tc.ok || (ok && (at != tc.at || query != tc.query)) {
			t.Errorf("atToken(%q) = %d %q %v, want %d %q %v", tc.draft, at, query, ok, tc.at, tc.query, tc.ok)
		}
	}
}

// Every word must match, in any order, and one that does not fails the whole
// needle.
func TestEveryWordOfTheNeedleMustMatch(t *testing.T) {
	for _, tc := range []struct {
		hay, needle string
		ok          bool
	}{
		{"who is kim jong il", "who", true},
		{"who is kim jong il", "who is", true},
		{"who is kim jong il", "jong who", true},
		{"who is kim jong il", "who is kim jong il", true},
		{"who is kim jong il", "who was", false},
		{"internal/tui3/app.go", "tui3 app", true},
		{"internal/tui3/app.go", "app tui3", true},
		{"internal/tui3/app.go", "tui3 main", false},
		{"harbor harbor", "har bor", true},
		{"the parser parser the-parser", "parser about", false},
		{"anything", "   ", true},
		// Only the word still being typed may match by its letters in order.
		{"side chat side-chat", "sd chat", false},
		{"side chat side-chat", "side cht", true},
		{"side chat side-chat", "is a chat", false},
	} {
		if _, ok := pathScore(tc.hay, strings.ToLower(tc.needle)); ok != tc.ok {
			t.Errorf("pathScore(%q, %q) = %v, want %v", tc.hay, tc.needle, ok, tc.ok)
		}
	}
}

// A sentence after a chosen mention does not keep the list up: several words
// that match nothing close it. One word that matches nothing still says so.
func TestTheListClosesOverASentenceAfterAMention(t *testing.T) {
	a := mentionApp(t)
	typeInto(t, a, "@chat:side ch")
	if !a.comp.open || len(a.comp.chatHits) != 1 {
		t.Fatalf("@chat:side ch kept the list open=%v with %d conversations", a.comp.open, len(a.comp.chatHits))
	}
	drive(t, a, key("enter"))
	if got := a.input.String(); got != "@side-chat" {
		t.Fatalf("enter typed %q", got)
	}
	typeInto(t, a, " and then")
	if a.comp.open {
		t.Fatalf("the list is up over %q", a.input.String())
	}
	a = mentionApp(t)
	typeInto(t, a, "@chat:zzz")
	if !a.comp.open {
		t.Fatal("one word that matches nothing closed the list instead of saying so")
	}

	// AND A TITLE THAT SHARES THE LETTERS. `is`, `a` and `chat` are all in
	// `side chat` in order, which is what reopened the list and let enter put
	// the mention back over the sentence.
	a = mentionApp(t)
	typeInto(t, a, "@chat:side")
	drive(t, a, key("enter"))
	typeInto(t, a, " is a chat")
	if a.comp.open {
		t.Fatalf("the list is up over %q", a.input.String())
	}
	drive(t, a, key("enter"))
	if got := a.input.String(); got == "@side-chat" {
		t.Fatal("enter put the mention back in place of the sentence")
	}
	sent := false
	for _, e := range a.entries {
		if e.kind == entryUser && strings.Contains(e.text, "@side-chat is a chat") {
			sent = true
		}
	}
	if !sent {
		t.Fatalf("enter did not send the whole line; box=%q", a.input.String())
	}
}

// atBoxes is the one fixture every box is tested against: a team, an open
// conversation beside the one on screen, a recent one, and a file under the
// folder the list walks.
type atBox struct {
	name string
	// make raises the surface with the fixture in reach.
	make func(t *testing.T) *app
	// text is what the person sees of the list.
	text func(a *app) string
	// box is the words in the box the list is bound to.
	box func(a *app) string
}

func atFixture(t *testing.T, a *app, walk string) {
	t.Helper()
	emptyMachine(a)
	a.wall.loaded = true
	a.wall.teams = []team{{
		ID: "t1", Name: "harbor", Hue: 210,
		Members: []teamMember{{Key: "/s/parser.jsonl", File: "/s/parser.jsonl", Handle: "parser", Word: "the parser"}},
	}}
	a.comp.recentsHeld = true
	a.comp.recents = []mentionChat{{key: "/s/side.jsonl", file: "/s/side.jsonl", title: "side chat", slug: "side-chat", note: "side chat"}}
	a.stow(Conversation{
		Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/lab/kim.jsonl", Workspace: "/tmp/lab", Place: "lab",
	}, &aside{since: a.now().Add(-12 * time.Minute), title: "who is kim jong il"})
	path := filepath.Join(walk, "internal", "tui3", "app.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

var atBoxes = []atBox{
	{
		name: "conversation",
		make: func(t *testing.T) *app {
			a := completionApp(t)
			a.file, a.openingPrompt = "/tmp/lab/this-one.jsonl", "the current conversation"
			atFixture(t, a, a.workspace)
			a.width, a.height = 160, 40
			_ = a.tabsRow(a.width)
			return a
		},
		text: func(a *app) string { return plain(strings.Join(a.overlayRows(a.width, a.overlayHeight()), "\n")) },
		box:  func(a *app) string { return a.input.String() },
	},
	{
		name: "home",
		make: func(t *testing.T) *app {
			a, root := atHome(t)
			atFixture(t, a, root)
			return a
		},
		text: homeText,
		box:  func(a *app) string { return a.home.box.String() },
	},
	{
		name: "new chat page",
		make: func(t *testing.T) *app {
			lab := newStartLab(t)
			a := lab.app()
			a.workspace = t.TempDir()
			atFixture(t, a, a.workspace)
			openStart(t, a)
			if !a.startingChat() {
				t.Fatal("+ did not open the start page")
			}
			return a
		},
		text: func(a *app) string { return plain(strings.Join(a.overlayRows(a.width, a.overlayHeight()), "\n")) },
		box:  func(a *app) string { return a.input.String() },
	},
}

// comp is the list the box is bound to.
func (b atBox) comp(a *app) *completion {
	if b.name == "home" {
		return &a.home.comp
	}
	return &a.comp
}

// TestTheAtListIsTheSameOnEveryBox: the same fixture, the same searches with
// spaces in them, the same answers — on a conversation's box, on home's, and
// on the new-chat page's. The owner asked for this to hold always.
func TestTheAtListIsTheSameOnEveryBox(t *testing.T) {
	for _, b := range atBoxes {
		t.Run(b.name, func(t *testing.T) {
			a := b.make(t)
			typeInto(t, a, "@")
			c := b.comp(a)
			if !c.open {
				t.Fatal("the bare @ did not open the list")
			}
			text := b.text(a)
			for _, want := range []string{"team  chat  file", "harbor", "who is kim jong il", "side chat", "app.go"} {
				if !strings.Contains(text, want) {
					t.Fatalf("the bare @ list is missing %q:\n%s", want, text)
				}
			}

			a = b.make(t)
			typeInto(t, a, "@chat:who is")
			c = b.comp(a)
			if !c.open || len(c.chatHits) != 1 || c.chatHits[0].title != "who is kim jong il" {
				t.Fatalf("@chat:who is: open=%v chats=%+v", c.open, c.chatHits)
			}
			if len(c.teamHits) != 0 || len(c.hits) != 0 {
				t.Fatal("@chat: kept another section")
			}
			drive(t, a, key("enter"))
			if got := b.box(a); got != "@who-is-kim-jong-il" {
				t.Fatalf("choosing the conversation typed %q", got)
			}
			// THE SENTENCE AFTER THE MENTION IS NOT A SEARCH, even when every
			// one of its words shares letters with the title: the list stays
			// closed and the words stay in the box.
			typeInto(t, a, " so it is")
			if c.open {
				t.Fatalf("the list reopened over %q", b.box(a))
			}
			if got := b.box(a); got != "@who-is-kim-jong-il so it is" {
				t.Fatalf("the sentence after the mention became %q", got)
			}

			a = b.make(t)
			typeInto(t, a, "@team:har bor")
			c = b.comp(a)
			if !c.open || len(c.teamHits) != 1 || len(c.chatHits) != 0 || len(c.hits) != 0 {
				t.Fatalf("@team:har bor: open=%v teams=%d chats=%d files=%d", c.open, len(c.teamHits), len(c.chatHits), len(c.hits))
			}
			drive(t, a, key("enter"))
			if got := b.box(a); got != "●harbor" {
				t.Fatalf("choosing the team typed %q", got)
			}

			a = b.make(t)
			typeInto(t, a, "@file:tui3 app")
			c = b.comp(a)
			if !c.open || len(c.hits) != 1 || !strings.HasSuffix(c.all[c.hits[0]], "app.go") || len(c.chatHits) != 0 {
				t.Fatalf("@file:tui3 app: open=%v files=%d chats=%d", c.open, len(c.hits), len(c.chatHits))
			}

			a = b.make(t)
			typeInto(t, a, "@chat:who was")
			c = b.comp(a)
			if c.open {
				t.Fatal("several words that match nothing left the list up")
			}
			a = b.make(t)
			typeInto(t, a, "@chat:zzz")
			if text := b.text(a); !strings.Contains(text, "no conversation matches") {
				t.Fatalf("one word that matches nothing does not say so:\n%s", text)
			}
		})
	}
}
