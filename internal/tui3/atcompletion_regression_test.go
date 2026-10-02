package tui3

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// Tracked paths make accidental English matches realistic without depending on
// the review's scratch tests or walking a developer's home.
func completionRepoPaths(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", "../..", "ls-files").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(out))
}

func completionBoxEditor(b atBox, a *app) *editor {
	if b.name == "home" {
		return &a.home.box
	}
	return &a.input
}

func assertCompletionSent(t *testing.T, b atBox, a *app, want string) {
	t.Helper()
	drive(t, a, key("enter"))
	if b.name == "home" && a.at(pageHome) {
		t.Fatalf("enter did not start home's conversation: %q", b.box(a))
	}
	for _, e := range a.entries {
		if e.kind == entryUser && strings.Contains(e.text, want) {
			return
		}
	}
	t.Fatalf("enter did not send the whole sentence %q; box=%q", want, b.box(a))
}

func TestBareAtProseSendsWholeOnEveryBox(t *testing.T) {
	paths := completionRepoPaths(t)
	for _, b := range atBoxes {
		for _, text := range []string{"cc @ara on this", "ask @ben to fix", "ask @ben to fix it", "meet @ to fix", "ask @who is this", "first line\n@ben to fix", "`x @ben to fix"} {
			t.Run(b.name+"/"+text, func(t *testing.T) {
				a := b.make(t)
				c := b.comp(a)
				c.all, c.loaded = append([]string(nil), paths...), true
				if b.name == "home" {
					a.home.walked = a.targetWhere()
				}
				for _, r := range text {
					if r == '\n' {
						drive(t, a, key("shift+enter"))
					} else {
						drive(t, a, key(string(r)))
					}
				}
				if c.open {
					t.Errorf("ordinary prose left the @ list open: %q", b.box(a))
				}
				assertCompletionSent(t, b, a, text)
			})
		}
		t.Run(b.name+"/bracketed paste", func(t *testing.T) {
			a := b.make(t)
			c := b.comp(a)
			c.all, c.loaded = append([]string(nil), paths...), true
			if b.name == "home" {
				a.home.walked = a.targetWhere()
			}
			drive(t, a, tea.PasteStartMsg{}, tea.PasteMsg{Content: "ask @ben to fix"}, tea.PasteEndMsg{})
			if c.open {
				t.Error("bracketed prose paste left the @ list open")
			}
			assertCompletionSent(t, b, a, "ask @ben to fix")
		})
		for _, tc := range []struct{ query, want string }{
			{"@chat:who is", "@who-is-kim-jong-il"},
			{"@team:har bor", "●harbor"},
			{"@file:tui3 app", "@internal/tui3/app.go"},
		} {
			t.Run(b.name+"/prefixed/"+tc.query, func(t *testing.T) {
				a := b.make(t)
				typeInto(t, a, tc.query)
				if c := b.comp(a); !c.open || !c.anyHits() {
					t.Fatalf("prefixed search is not offering its match: %q", tc.query)
				}
				drive(t, a, key("enter"))
				if got := b.box(a); got != tc.want {
					t.Fatalf("prefixed search inserted %q, want %q", got, tc.want)
				}
			})
		}
	}
}

// A target change must replace catalogs and rows together, before the next
// frame and before an asynchronous walk is allowed to return.
func TestHomeAtTargetChangeNeverLeavesStaleFileRows(t *testing.T) {
	lab := newHomeLab(t)
	now := lab.pin(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	here, other := lab.workspace("here"), lab.workspace("other")
	mine := lab.session("here", "aaaa000000000001", "here conversation", here, now)
	theirs := lab.session("other", "aaaa000000000002", "other conversation", other, now)
	if err := os.WriteFile(filepath.Join(other, "notes.md"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	a := lab.app(mine)
	a.showPage(pageHome)
	a.target.where = other
	drive(t, a, key("@"))
	drive(t, a, key("backspace"))
	a.target.where = ""
	a.home.point(theirs)
	if a.targetWhere() != other {
		t.Fatal("fixture did not select the other project's folder")
	}
	_, cmd := a.Update(key("@"))
	c := &a.home.comp
	if a.home.walked != other {
		t.Errorf("opening the list changed its root from the selected project to %q", a.home.walked)
	}
	for _, line := range c.lines {
		if line.file >= len(c.all) {
			t.Errorf("home retained file row %d after clearing its %d paths", line.file, len(c.all))
		}
	}
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Errorf("home frame panicked after changing targets: %v", p)
			}
		}()
		_ = homeText(a)
	}()
	spend(t, a, cmd)
}

func TestFirstSpacedPasteStartsTheCatalogReads(t *testing.T) {
	for _, b := range atBoxes[:2] {
		for _, query := range []string{"@chat:who is", "@file:internal tui3"} {
			t.Run(b.name+"/"+query, func(t *testing.T) {
				a := b.make(t)
				c := b.comp(a)
				c.all, c.loaded, c.loading = nil, false, false
				a.comp.recentsHeld, a.comp.recents = false, nil
				a.behind, a.prev, a.chatTabs = nil, nil, nil
				reads := 0
				a.recentSessions = func() []Session { reads++; return []Session{{File: "/s/kim.jsonl", Title: "who is kim jong il"}} }
				_, cmd := a.Update(tea.PasteMsg{Content: query})
				if !c.open {
					t.Error("first spaced paste closed before its catalogs were read")
				}
				if !c.loading {
					t.Error("first spaced paste did not start the file walk")
				}
				if !strings.Contains(b.text(a), "looking…") {
					t.Error("unread search did not say looking…")
				}
				if !a.comp.recentsHeld {
					t.Error("first spaced paste did not schedule recents")
				}
				spend(t, a, cmd)
				if reads != 1 || !c.loaded {
					t.Fatalf("paste completed reads=%d filesLoaded=%v", reads, c.loaded)
				}
				if !c.open || !c.anyHits() {
					t.Fatalf("loaded search has no open matching list: %q", query)
				}
			})
		}
	}
}

func TestHomePasteOffersCatalogsAndRefreshesRecents(t *testing.T) {
	for _, query := range []string{"@", "@chat:", "@team:h"} {
		t.Run(query, func(t *testing.T) {
			a := atHomeWithMentions(t)
			a.comp.recentsHeld, a.comp.recents = false, nil
			reads := 0
			a.recentSessions = func() []Session { reads++; return []Session{{File: "/s/side.jsonl", Title: "side chat"}} }
			drive(t, a, tea.PasteStartMsg{}, tea.PasteMsg{Content: query}, tea.PasteEndMsg{})
			c := &a.home.comp
			if reads != 1 || !c.loaded {
				t.Errorf("home paste read recents=%d filesLoaded=%v", reads, c.loaded)
			}
			if query != "@chat:" && len(c.teamHits) != 1 {
				t.Errorf("home paste offered %d teams", len(c.teamHits))
			}
			if query != "@team:h" && len(c.chatHits) != 1 {
				t.Errorf("home paste offered %d conversations", len(c.chatHits))
			}
		})
	}
}

func TestRecentArrivalKeepsTheChosenConversation(t *testing.T) {
	for _, b := range atBoxes[:2] {
		for _, larger := range []bool{false, true} {
			name := b.name + "/unchanged"
			if larger {
				name = b.name + "/larger"
			}
			t.Run(name, func(t *testing.T) {
				a := b.make(t)
				typeInto(t, a, "@")
				c := b.comp(a)
				for i := 0; i < len(c.sel); i++ {
					if b.name == "home" {
						line, _ := a.home.focusedLine()
						if line.comp >= 0 && c.lines[line.comp].chat >= 0 && c.chatHits[c.lines[line.comp].chat].slug == "side-chat" {
							break
						}
					} else if chat, ok := c.chatChoice(); ok && chat.slug == "side-chat" {
						break
					}
					drive(t, a, key("down"))
				}
				if b.name == "home" {
					line, _ := a.home.focusedLine()
					if line.comp != c.selLine() {
						t.Errorf("home arrow left cursor=%d and completion=%d disagreeing", line.comp, c.selLine())
					}
				}
				rows := []Session{{File: "/s/side.jsonl", Title: "side chat"}}
				if larger {
					rows = append([]Session{{File: "/s/new.jsonl", Title: "new conversation"}}, rows...)
				}
				drive(t, a, mentionRecentsMsg{rows: rows})
				drive(t, a, key("enter"))
				if got := b.box(a); got != "@side-chat" {
					t.Fatalf("recent arrival changed the chosen conversation to %q", got)
				}
			})
		}
	}
}

func TestFileArrivalKeepsTheChosenPath(t *testing.T) {
	for _, b := range atBoxes[:2] {
		t.Run(b.name, func(t *testing.T) {
			a := b.make(t)
			c := b.comp(a)
			c.all, c.loaded = []string{"first.md", "chosen.md"}, true
			if b.name == "home" {
				a.home.walked = a.targetWhere()
			}
			typeInto(t, a, "@file:")
			if path, _ := c.choice(); path != "first.md" {
				t.Fatalf("fixture chose %q", path)
			}
			drive(t, a, key("down"))
			drive(t, a, filesLoadedMsg{paths: []string{"new.md", "first.md", "chosen.md"}, home: b.name == "home"})
			drive(t, a, key("enter"))
			if got := b.box(a); got != "@chosen.md" {
				t.Fatalf("file arrival changed the chosen path to %q", got)
			}
		})
	}
}

func TestRecentKeysTravelOffLoopAndStillDedupeSymlinks(t *testing.T) {
	root := t.TempDir()
	real, alias := filepath.Join(root, "real"), filepath.Join(root, "alias")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"front.jsonl", "side.jsonl", "other.jsonl"} {
		if err := os.WriteFile(filepath.Join(real, n), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	a := mentionApp(t)
	emptyMachine(a)
	a.file, a.title = filepath.Join(real, "front.jsonl"), "front"
	a.stow(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: filepath.Join(real, "side.jsonl"), Workspace: real}, &aside{title: "side"})
	_ = a.tabsRow(a.width)
	a.comp.recentsHeld = false
	a.recentSessions = func() []Session {
		return []Session{{File: filepath.Join(alias, "front.jsonl"), Title: "front"}, {File: filepath.Join(alias, "side.jsonl"), Title: "side"}, {File: filepath.Join(alias, "other.jsonl"), Title: "other"}, {File: filepath.Join(real, "other.jsonl"), Title: "other duplicate"}}
	}
	prior := resolveTranscript
	walks := 0
	resolveTranscript = func(p string) (string, error) { walks++; return prior(p) }
	defer func() { resolveTranscript = prior }()
	msg := a.loadMentionRecents()()
	t.Logf("off-loop canonical walks=%d", walks)
	walks = 0
	a.Update(msg)
	if walks != 0 {
		t.Errorf("recent message handling performed %d disk walks, want zero", walks)
	}
	if got := len(a.mentionChats()); got != 2 {
		t.Fatalf("canonical conversation catalog has %d rows, want two", got)
	}
	a.fillHomeMentions()
	if got := len(a.home.comp.chats); got != 3 {
		t.Fatalf("canonical home catalog has %d rows, want three", got)
	}
}

func TestChosenMentionPunctuationKeepsTheListClosed(t *testing.T) {
	for _, b := range atBoxes {
		for _, query := range []string{"@chat:side", "@file:app.go"} {
			for _, tail := range []string{" ", ",", ".", ";", ":", "!", "?", ")"} {
				t.Run(b.name+"/"+query+"/"+tail, func(t *testing.T) {
					a := b.make(t)
					typeInto(t, a, query)
					drive(t, a, key("enter"))
					before := b.box(a)
					if b.comp(a).open {
						t.Fatal("mention was not chosen")
					}
					typeInto(t, a, tail)
					if b.comp(a).open {
						t.Fatalf("punctuation reopened the @ list over %q", b.box(a))
					}
					if b.box(a) != before+tail {
						t.Fatal("punctuation changed the chosen mention")
					}
				})
			}
		}
	}
}

func TestHomeChosenTeamUsesItsColourAtWideAndPhoneWidths(t *testing.T) {
	for _, width := range []int{120, 38} {
		a := atHomeWithMentions(t)
		a.pal = newPalette(tokens.ANSI256, false)
		a.width, a.height = width, 30
		typeInto(t, a, "@team:h")
		drive(t, a, key("enter"))
		lines, _, _, _ := a.homeFrame(width, 30)
		pen := a.pal.onPlaces().teamInk(a.wall.teams[0].HueSpec())
		painted := pen(a.home.box.String())
		if painted == a.home.box.String() {
			t.Fatal("fixture has no coloured ink")
		}
		if !strings.Contains(strings.Join(lines, "\n"), painted) {
			t.Errorf("home width %d draws the chosen team without its colour", width)
		}
	}
}

func TestMentionManualSectionsAreShortAndExplainPrefixedSpaces(t *testing.T) {
	for _, name := range []string{"conversations-and-teams.md", "keys.md"} {
		raw, err := os.ReadFile(filepath.Join("..", "manual", "chat", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, section := range strings.Split(string(raw), "\n## ")[1:] {
			heading, _, _ := strings.Cut(section, "\n")
			if !strings.Contains(heading, "@") {
				continue
			}
			if len([]rune(section)) > 2100 {
				t.Errorf("%s: @ section %q has %d characters, want about 2000", name, heading, len([]rune(section)))
			}
		}
		if strings.Contains(string(raw), "`@internal tui3`") {
			t.Errorf("%s still promises bare spaced searches", name)
		}
	}
}

// The moved engine conversation and the shell carrying its unsent words have
// different transcripts, even when the shell's tab borrows those words as its title.
func TestMovedDraftTabIsADifferentConversationFromTheRecentRow(t *testing.T) {
	lab := newHomeLab(t)
	now := lab.pin(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	where := lab.workspace("project")
	mine := lab.session("project", "aaaa000000000001", "user asks who kim jong il is", where, now)
	a := lab.app(mine)
	a.workspace = where
	shell := filepath.Join(where, "next", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(shell), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shell, []byte(`{"type":"session","version":1,"id":"next"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// An empty workspace asks the start door for its current project, the same
	// contract the product uses when a moved window needs a fresh shell.
	a.start = func(workspace string) (Conversation, error) {
		if workspace == "" {
			workspace = where
		}
		return Conversation{Agent: &switchAgent{fakeAgent: &fakeAgent{model: "m"}}, SessionFile: shell, Workspace: workspace}, nil
	}
	a.input.setText("see node_modules/@types/node is old")
	drive(t, a, a.movedAway(""))
	if !a.at(pageHome) {
		t.Fatal("moved conversation did not reach home")
	}
	a.comp.recentsHeld = false
	a.recentSessions = func() []Session { return []Session{{File: mine, Title: "user asks who kim jong il is"}} }
	typeInto(t, a, "@chat:")
	var draft, recent mentionChat
	for _, chat := range a.home.comp.chatHits {
		if chat.title == "see node_modules/@types/node is old" {
			draft = chat
		}
		if chat.title == "user asks who kim jong il is" {
			recent = chat
		}
	}
	if draft.key == "" || recent.key == "" {
		t.Fatalf("missing moved fixture rows: %+v", a.home.comp.chatHits)
	}
	if draft.key == recent.key || draft.file == recent.file {
		t.Fatalf("draft and recent are the same transcript: %+v %+v", draft, recent)
	}
	if draft.file != filepath.Join(where, "next", "transcript.jsonl") {
		t.Fatalf("draft tab does not name the new shell: %+v", draft)
	}
	for _, e := range a.entries {
		if e.kind == entryUser {
			t.Fatal("the shell already has a sent message")
		}
	}
	t.Logf("draft key=%q file=%q; moved key=%q file=%q; shell has no sent messages", draft.key, draft.file, recent.key, recent.file)
}

// The command keeps the hosted rule it was issued under, even if the surface
// switches back to a local conversation before that command executes.
func TestRecentReadCapturesHostedIdentityBeforeItsCommandRuns(t *testing.T) {
	a := mentionApp(t)
	a.host = "far"
	a.comp.recentsHeld = false
	a.recentSessions = func() []Session { return []Session{{File: "/home/far/x/../chat.jsonl", Title: "far chat"}} }
	cmd := a.loadMentionRecents()
	a.host = ""
	prior := resolveTranscript
	walks := 0
	resolveTranscript = func(p string) (string, error) { walks++; return prior(p) }
	defer func() { resolveTranscript = prior }()
	msg := cmd()
	a.Update(msg)
	if walks != 0 {
		t.Fatalf("hosted recent read or arrival walked the local disk %d times", walks)
	}
	if len(a.comp.recents) != 1 || a.comp.recents[0].key != "/home/far/chat.jsonl" {
		t.Fatalf("hosted recent keys = %+v", a.comp.recents)
	}
}

func TestHomeAtIgnoresAFileWalkFromItsPreviousTarget(t *testing.T) {
	a, first := atHome(t)
	_, firstCmd := a.Update(key("@"))
	if firstCmd == nil {
		t.Fatal("first opening started no walk")
	}
	a.target.where = t.TempDir()
	_, nextCmd := a.Update(key("n"))
	next := a.home.walked
	if next == first {
		t.Fatal("pinning the target did not start another walk")
	}
	spend(t, a, firstCmd)
	if a.home.walked != next || a.home.comp.loaded || len(a.home.comp.all) > 0 {
		t.Fatalf("old walk replaced the new target's catalog: %+v", a.home.comp.all)
	}
	spend(t, a, nextCmd)
	if !a.home.comp.loaded {
		t.Fatal("the current target's walk did not land")
	}
}
