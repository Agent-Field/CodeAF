package tui3

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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
	if a.home.walked != here || a.targetWhere() != here {
		t.Errorf("opening walks %q while its sentence opens in %q, want %q", a.home.walked, a.targetWhere(), here)
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
		a.comp.teams = []mentionTeam{{id: "old", name: "old team", slug: "old-team"}}
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

// The moved conversation remains a reference, while a shell named only by
// its draft remains a tab without being offered as a conversation.
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
	var recent mentionChat
	for _, chat := range a.home.comp.chatHits {
		if chat.title == "see node_modules/@types/node is old" {
			t.Errorf("unsent shell is offered as a conversation: %+v", chat)
		}
		if chat.title == "user asks who kim jong il is" {
			recent = chat
		}
	}
	if recent.key == "" || recent.file != mine {
		t.Fatalf("the moved conversation is missing: %+v", a.home.comp.chatHits)
	}
	if name := a.conversationName(); name != "see node_modules/@types/node is old" {
		t.Fatalf("the draft no longer names its tab: %q", name)
	}
	for _, e := range a.entries {
		if e.kind == entryUser {
			t.Fatal("the shell already has a sent message")
		}
	}
	t.Logf("moved key=%q file=%q; unsent shell is absent from mentions", recent.key, recent.file)
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

// A local list and its foot must describe the same folder all the way through send.
func TestHomeAtFilesFollowTheSentencesFolder(t *testing.T) {
	lab := newHomeLab(t)
	now := lab.pin(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	here, other := lab.workspace("here"), lab.workspace("other")
	mine := lab.session("here", "aaaa000000000001", "here conversation", here, now)
	theirs := lab.session("other", "aaaa000000000002", "other conversation", other, now)
	for root, name := range map[string]string{here: "here.md", other: "other.md"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	a := lab.app(mine)
	a.width = 260
	a.showPage(pageHome)
	a.home.point(theirs)
	if a.targetWhere() != other {
		t.Fatal("fixture did not select the other project")
	}
	typeInto(t, a, "@file:")
	if a.home.walked != here || a.targetWhere() != here {
		t.Errorf("list walks %q but sentence opens in %q, want %q", a.home.walked, a.targetWhere(), here)
	}
	if text := homeText(a); !strings.Contains(text, "here.md") || strings.Contains(text, "other.md") || !strings.Contains(text, targetProjectLead+a.targetProject()) {
		t.Errorf("files and project foot disagree:\n%s", text)
	}
	drive(t, a, key("enter"))
	typeInto(t, a, " say only ok")
	assertCompletionSent(t, atBoxes[1], a, "@here.md say only ok")
	if a.workspace != here {
		t.Fatalf("started workspace=%q, want %q", a.workspace, here)
	}
}

// A project chord must replace the paths and the rows before any frame draws.
func TestHomeAtProjectChordRebuildsBeforeTheNextFrame(t *testing.T) {
	lab := newHomeLab(t)
	now := lab.pin(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	one, two := lab.workspace("one"), lab.workspace("two")
	mine := lab.session("one", "aaaa000000000001", "first chat", one, now)
	lab.session("two", "aaaa000000000002", "second chat", two, now)
	for root, name := range map[string]string{one: "one.md", two: "two.md"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	a := lab.app(mine)
	a.showPage(pageHome)
	a.target.where = one
	_, first := a.Update(key("@"))
	a.homeFilesLoaded([]string{"one.md"}, one)
	_, next := a.Update(key("alt+p"))
	if a.target.where != two {
		t.Fatal("project chord did not pin the other folder")
	}
	if a.home.walked != two || a.home.comp.loaded || len(a.home.comp.all) != 0 {
		t.Errorf("project chord retained the old catalog: walked=%q paths=%v loaded=%v", a.home.walked, a.home.comp.all, a.home.comp.loaded)
	}
	for _, line := range a.home.comp.lines {
		if line.file >= len(a.home.comp.all) {
			t.Errorf("stale file index %d", line.file)
		}
	}
	_ = homeText(a)
	spend(t, a, first)
	if a.home.walked != two || a.home.comp.loaded || len(a.home.comp.all) != 0 {
		t.Error("the old walk replaced the current target")
	}
	spend(t, a, next)
	if text := homeText(a); !strings.Contains(text, "two.md") || strings.Contains(text, "one.md") {
		t.Errorf("new target's files did not land:\n%s", text)
	}
}

// Closed-list navigation must preserve a completed walk for the next opening.
func TestHomeAtClosedArrowsReuseTheCompletedWalk(t *testing.T) {
	lab := newHomeLab(t)
	now := lab.pin(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	one, two := lab.workspace("one"), lab.workspace("two")
	mine := lab.session("one", "aaaa000000000001", "first chat", one, now)
	other := lab.session("two", "aaaa000000000002", "second chat", two, now)
	a := lab.app(mine)
	a.width, a.height = 180, 40
	a.showPage(pageHome)
	a.home.point(mine)
	a.home.walked = a.targetWhere()
	a.home.comp.all, a.home.comp.loaded = []string{"notes.md"}, true
	root := a.home.walked
	a.home.point(other)
	a.Update(key("down"))
	a.home.point(mine)
	a.Update(key("down"))
	a.home.point(mine)
	_, cmd := a.Update(key("@"))
	walks := 0
	for _, msg := range runCmd(cmd) {
		if files, ok := msg.(filesLoadedMsg); ok && files.home {
			walks++
		}
	}
	if a.home.walked != root || !a.home.comp.loaded || walks != 0 {
		t.Fatalf("cached root=%q reopened=%q loaded=%v new walks=%d, want zero", root, a.home.walked, a.home.comp.loaded, walks)
	}
}

// A warm catalog must wait for the opening's fresh read, including on a paste.
func TestSpacedOpeningRefreshesTheRecentListOnEveryBox(t *testing.T) {
	for _, b := range atBoxes {
		for _, pasted := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/paste=%v", b.name, pasted), func(t *testing.T) {
				a := b.make(t)
				a.comp.recentsHeld, a.comp.recentsLoaded, a.comp.recents = false, true, nil
				reads := 0
				a.recentSessions = func() []Session { reads++; return []Session{{File: "/s/brand.jsonl", Title: "brand new conversation"}} }
				var cmd tea.Cmd
				if pasted {
					_, cmd = a.Update(tea.PasteMsg{Content: "@chat:brand new"})
				} else {
					_, cmd = a.Update(key("@"))
					for _, r := range "chat:brand new" {
						a.Update(key(string(r)))
					}
				}
				c := b.comp(a)
				if !c.open || !strings.Contains(b.text(a), "looking…") {
					t.Error("warm opening closed before the fresh recent read answered")
				}
				if reads != 0 {
					t.Fatal("the recent reader ran on the update loop")
				}
				spend(t, a, cmd)
				if reads != 1 || !c.open || len(c.chatHits) != 1 {
					t.Fatalf("fresh opening reads=%d open=%v hits=%d, want one read and one row", reads, c.open, len(c.chatHits))
				}
				drive(t, a, key("x"))
				if reads != 1 {
					t.Fatalf("typing in the open list started %d reads", reads)
				}
				if c.open {
					t.Error("a completed unmatched multi-word search stayed open")
				}
			})
		}
	}
}

// Plain draft rows must not pay for a team catalog they cannot use.
func TestPlainDraftTeamPaintingAllocatesNothing(t *testing.T) {
	a := atHomeWithMentions(t)
	block := []string{"ordinary draft"}
	for _, stale := range []bool{false, true} {
		if stale {
			a.comp.teams = []mentionTeam{{slug: "old-team"}}
		}
		if got := testing.AllocsPerRun(20, func() { a.paintDraftMentions(block) }); got != 0 {
			t.Errorf("plain row stale=%v allocated %.0f times, want exactly zero", stale, got)
		}
	}
}

// Unsent drafts may name tabs, but only sent or named conversations are references.
// The window can judge only its own front's unsent state, not another tab's.
func TestAtListOmitsUnsentShellsOnEveryBox(t *testing.T) {
	for _, b := range atBoxes {
		for _, kind := range []string{"draft", "sent", "named", "held-draft"} {
			t.Run(b.name+"/"+kind, func(t *testing.T) {
				a := b.make(t)
				a.title, a.openingPrompt, a.entries = "", "", nil
				a.file = filepath.Join(t.TempDir(), "shell.jsonl")
				if err := os.WriteFile(a.file, []byte(`{"type":"session","version":1,"id":"shell"}`+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				a.input.setText("unsent shell words")
				if kind == "sent" {
					a.entries = []entry{{kind: entryUser, text: "sent shell words"}}
				}
				if kind == "named" {
					a.title = "named shell"
				}
				file := a.file
				if kind == "held-draft" {
					conv := a.front()
					side := a.detachConversation()
					side.draft = "unsent shell words"
					a.stow(conv, side)
					a.file = filepath.Join(t.TempDir(), "new-front.jsonl")
					a.input.reset()
				}
				if b.name != "home" {
					a.input.reset()
				}
				typeInto(t, a, "@chat:")
				found := false
				for _, chat := range b.comp(a).chatHits {
					if chat.file == file {
						found = true
					}
				}
				want := kind == "held-draft" || b.name != "conversation" && kind != "draft"
				if found != want {
					t.Fatalf("%s front offered=%v, want %v; rows=%+v", kind, found, want, b.comp(a).chatHits)
				}
			})
		}
	}
}

// Retrieval must tell a person why a draft-only tab is missing and what home walks.
func TestMentionManualExplainsSentenceFoldersAndUnsentShells(t *testing.T) {
	for name, words := range map[string][]string{
		"home.md":                    {"sentence opens", "project: ", "nothing sent"},
		"conversations-and-teams.md": {"nothing sent", "fresh read", "looking…"},
	} {
		raw, err := os.ReadFile(filepath.Join("..", "manual", "chat", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, word := range words {
			if !strings.Contains(string(raw), word) {
				t.Errorf("%s does not explain %q", name, word)
			}
		}
	}
}

// A hosted list offers this machine's files, while its sentence opens on the far one.
func TestHomeAtHostedFilesUseLocalRootAndSentenceUsesFarWorkspace(t *testing.T) {
	a, root := atHome(t)
	far := "/far/project"
	a.target.where = ""
	a.host, a.workspace, a.localRoot = "far", far, root
	a.width = 260
	drive(t, a, key("@"))
	if a.home.walked != root || a.home.walked != a.pathRoot() {
		t.Fatalf("hosted list walks %q, want local root %q", a.home.walked, root)
	}
	walk := a.loadFiles()
	if walk == nil {
		t.Fatal("the conversation's file walk did not start")
	}
	conversation := walk().(filesLoadedMsg)
	if got, want := strings.Join(a.home.comp.all, "\n"), strings.Join(conversation.paths, "\n"); got != want {
		t.Fatalf("hosted home files=%q, conversation files=%q", got, want)
	}
	if a.targetWhere() != far {
		t.Errorf("hosted sentence targets %q, want far workspace %q", a.targetWhere(), far)
	}
	if text := homeText(a); !strings.Contains(text, "notes.md") || !strings.Contains(text, targetProjectLead+"far:"+far) {
		t.Errorf("hosted list must show local files and the foot must name the far workspace:\n%s", text)
	}
	drive(t, a, key("n"), key("enter"))
	typeInto(t, a, " say only ok")
	assertCompletionSent(t, atBoxes[1], a, "@notes.md say only ok")
	if a.workspace != far {
		t.Fatalf("hosted sentence opened in %q, want far workspace %q", a.workspace, far)
	}
}

// A ready walk from another target cannot dismiss a paste before the new walk.
func TestHomeAtSpacedFilePasteWaitsForItsNewFolder(t *testing.T) {
	a, old := atHome(t)
	a.home.walked = old
	a.home.comp.all, a.home.comp.loaded = []string{"old.md"}, true
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "new note.md"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	a.target.where = root
	_, cmd := a.Update(tea.PasteMsg{Content: "@file:new note"})
	if !a.home.comp.open || !a.home.comp.loading || a.home.walked != root {
		t.Fatalf("new-folder paste open=%v loading=%v walked=%q", a.home.comp.open, a.home.comp.loading, a.home.walked)
	}
	spend(t, a, cmd)
	drive(t, a, key("enter"))
	if got := a.home.box.String(); got != "@new note.md" {
		t.Fatalf("new-folder paste chose %q", got)
	}
}

// A paste recognizes an opening even when its ready section closes at once.
func TestUnmatchedPrefixedPasteStillReadsRecentsOnEveryBox(t *testing.T) {
	for _, b := range atBoxes {
		for _, query := range []string{"@team:no such", "@file:no such"} {
			t.Run(b.name+"/"+query, func(t *testing.T) {
				a := b.make(t)
				c := b.comp(a)
				c.all, c.loaded = []string{"unrelated.md"}, true
				if b.name == "home" {
					a.home.walked = a.homeCompletionRoot()
				}
				a.comp.recentsHeld, a.comp.recentsLoaded = false, true
				reads := 0
				a.recentSessions = func() []Session { reads++; return nil }
				_, cmd := a.Update(tea.PasteMsg{Content: query})
				if c.open {
					t.Error("a ready unmatched multi-word section stayed open")
				}
				if reads != 0 {
					t.Fatal("the reader ran on the update loop")
				}
				spend(t, a, cmd)
				if reads != 1 {
					t.Fatalf("pasted opening read recents %d times, want once", reads)
				}
			})
		}
	}
}

// Removing a newline can recognize the same opening as a printable key or paste.
func TestBackspaceOpeningRefreshesRecentsOnEveryBox(t *testing.T) {
	for _, b := range atBoxes {
		t.Run(b.name, func(t *testing.T) {
			a := b.make(t)
			c := b.comp(a)
			c.recentsLoaded = true
			a.comp.recentsHeld, a.comp.recentsLoaded, a.comp.recents = false, true, nil
			completionBoxEditor(b, a).setText("@chat:brand new\n")
			reads := 0
			a.recentSessions = func() []Session { reads++; return []Session{{File: "/s/brand.jsonl", Title: "brand new conversation"}} }
			_, cmd := a.Update(key("backspace"))
			if !c.open || !strings.Contains(b.text(a), "looking…") {
				t.Error("backspace opening did not wait for its fresh recent read")
			}
			spend(t, a, cmd)
			if reads != 1 || !c.open || len(c.chatHits) != 1 {
				t.Fatalf("backspace reads=%d open=%v hits=%d", reads, c.open, len(c.chatHits))
			}
		})
	}
}

// A chosen mention in a closed box does not make arrows catalog edits.
func TestClosedHomeArrowsLeaveMentionCatalogsAlone(t *testing.T) {
	a, root := atHome(t)
	a.home.walked = root
	a.home.comp.all, a.home.comp.loaded = []string{"notes.md"}, true
	a.home.box.setText("@notes.md")
	a.home.comp.done = "notes.md"
	a.home.comp.teams = []mentionTeam{{id: "kept", name: "kept", slug: "kept"}}
	a.Update(key("down"))
	if got := a.home.comp.teams; len(got) != 1 || got[0].id != "kept" {
		t.Fatalf("closed arrow copied a catalog: %+v", got)
	}
}

// An opening belongs to the token, even after its unmatched rows disappear.
func TestOneRecentReadAndNoticePerAtTokenOnEveryBox(t *testing.T) {
	for _, b := range atBoxes {
		for _, scenario := range []string{"@chat:no such words", "@chat:unique words", "@no such words", "paste and caret", "remove and replace", "cut and replace", "send and replace", "different at"} {
			t.Run(b.name+"/"+scenario, func(t *testing.T) {
				a := b.make(t)
				a.comp.recentsHeld, a.comp.recentsLoaded, a.comp.recents = false, true, nil
				reads, notices := 0, 0
				a.recentSessions = func() []Session {
					reads++
					return []Session{{File: filepath.Join(a.workspace, "unique.jsonl"), Title: "unique words"}}
				}
				edit := func(msg tea.Msg) {
					a.notices.seen[eventAtOpened] = false
					drive(t, a, msg)
					if a.notices.seen[eventAtOpened] {
						notices++
					}
				}
				typeWords := func(words string) {
					for _, r := range words {
						edit(key(string(r)))
					}
				}
				want := 1
				switch scenario {
				case "paste and caret":
					edit(tea.PasteMsg{Content: "@chat:no such words"})
					edit(key("left"))
					edit(key("right"))
				case "remove and replace":
					typeWords("@chat:x")
					for b.box(a) != "" {
						edit(key("backspace"))
					}
					typeWords("@chat:y")
					want = 2
				case "different at":
					typeWords("@chat:x @chat:y")
					want = 2
				case "cut and replace":
					typeWords("@chat:x")
					edit(key("ctrl+u"))
					typeWords("@chat:y")
					want = 2
				case "send and replace":
					typeWords("@chat:no such words")
					edit(key("enter"))
					if b.box(a) != "" {
						t.Fatalf("send kept its token in the box: %q", b.box(a))
					}
					if b.name == "home" {
						a.showPage(pageHome)
					}
					typeWords("@chat:y")
					want = 2
				default:
					typeWords(scenario)
				}
				t.Logf("reads=%d opening notices=%d, want %d of each", reads, notices, want)
				if reads != want || notices != want {
					t.Errorf("one opening per token: reads=%d notices=%d, want %d", reads, notices, want)
				}
			})
		}
	}
}

// Escape ends an opening without deleting its token; the next letter reads anew.
func TestEscEndsAtOpeningAndNextLetterReadsFreshOnEveryBox(t *testing.T) {
	for _, b := range atBoxes {
		for _, query := range []string{"@", "@chat:"} {
			t.Run(b.name+"/"+query, func(t *testing.T) {
				a := b.make(t)
				a.comp.recentsHeld, a.comp.recentsLoaded, a.comp.recents = false, true, nil
				reads, notices := 0, 0
				rows := []Session{{File: filepath.Join(a.workspace, "old.jsonl"), Title: "old conversation"}}
				a.recentSessions = func() []Session { reads++; return rows }
				edit := func(msg tea.Msg) {
					a.notices.seen[eventAtOpened] = false
					drive(t, a, msg)
					if a.notices.seen[eventAtOpened] {
						notices++
					}
				}
				for _, r := range query {
					edit(key(string(r)))
				}
				c := b.comp(a)
				if !c.open || reads != 1 || notices != 1 {
					t.Fatalf("initial opening: open=%v reads=%d notices=%d", c.open, reads, notices)
				}
				edit(key("esc"))
				if c.open || b.box(a) != query || reads != 1 || notices != 1 {
					t.Fatalf("escape: open=%v box=%q reads=%d notices=%d", c.open, b.box(a), reads, notices)
				}
				rows = []Session{{File: filepath.Join(a.workspace, "cloudflare.jsonl"), Title: "Cloudflare worker deploy"}}
				edit(key("c"))
				if !c.open || reads != 2 || notices != 2 {
					t.Fatalf("letter after escape: open=%v reads=%d notices=%d, want 2 of each", c.open, reads, notices)
				}
				edit(key("l"))
				if !c.open || reads != 2 || notices != 2 || len(c.chatHits) != 1 || c.chatHits[0].title != rows[0].Title {
					t.Fatalf("next letter: open=%v reads=%d notices=%d chats=%+v", c.open, reads, notices, c.chatHits)
				}
				edit(key("esc"))
				edit(key("o"))
				if !c.open || reads != 3 || notices != 3 {
					t.Fatalf("another dismissal: open=%v reads=%d notices=%d, want 3 of each", c.open, reads, notices)
				}
				edit(key("esc"))
				edit(key(" "))
				if c.open || reads != 3 || notices != 3 {
					t.Fatalf("separator after escape: open=%v reads=%d notices=%d", c.open, reads, notices)
				}
				for _, r := range "@chat:cl" {
					edit(key(string(r)))
				}
				if !c.open || reads != 4 || notices != 4 {
					t.Fatalf("new token after escape and space: open=%v reads=%d notices=%d, want 4 of each", c.open, reads, notices)
				}
				t.Logf("four openings, reads=%d opening notices=%d", reads, notices)
			})
		}
	}
}

// A new token shares the pending walk's follow-up, whose newer catalog must
// survive an older answer arriving again after the fresh rows have landed.
func TestANewAtTokenWaitsForTheWalkInFlightAndIsSettledByItsFollowUp(t *testing.T) {
	for _, b := range atBoxes {
		t.Run(b.name, func(t *testing.T) {
			a := b.make(t)
			a.comp.recentsHeld, a.comp.recentsLoaded, a.comp.recents = false, true, nil
			var reads, active, maximum atomic.Int32
			started := make(chan int32, 1)
			release := make(chan struct{})
			answers := make(chan tea.Msg, 3)
			var workers sync.WaitGroup
			defer func() { close(release); workers.Wait() }()
			a.recentSessions = func() []Session {
				read := reads.Add(1)
				n := active.Add(1)
				for old := maximum.Load(); n > old; old = maximum.Load() {
					if maximum.CompareAndSwap(old, n) {
						break
					}
				}
				started <- read
				<-release
				active.Add(-1)
				if read == 1 {
					return []Session{{File: filepath.Join(a.workspace, "old.jsonl"), Title: "old words"}}
				}
				return []Session{{File: filepath.Join(a.workspace, "unique.jsonl"), Title: "unique words"}}
			}
			scheduled := 0
			start := func(cmd tea.Cmd) {
				for _, read := range completionRecentCommands(cmd) {
					scheduled++
					workers.Add(1)
					go func() { defer workers.Done(); answers <- read() }()
					select {
					case <-started:
					case <-time.After(5 * time.Second):
						t.Fatal("scheduled recent reader did not start")
					}
				}
			}
			land := func() (tea.Msg, tea.Cmd) {
				release <- struct{}{}
				select {
				case msg := <-answers:
					_, cmd := a.Update(msg)
					return msg, cmd
				case <-time.After(5 * time.Second):
					t.Fatal("released reader did not answer")
					return nil, nil
				}
			}
			_, first := a.Update(tea.PasteMsg{Content: "@chat:old words"})
			start(first)
			if scheduled != 1 || reads.Load() != 1 || active.Load() != 1 {
				t.Fatalf("first token scheduled=%d reads=%d active=%d, want 1 of each", scheduled, reads.Load(), active.Load())
			}
			for b.box(a) != "" {
				_, cmd := a.Update(key("backspace"))
				start(cmd)
			}
			_, second := a.Update(tea.PasteMsg{Content: "@chat:unique words"})
			start(second)
			c := b.comp(a)
			if scheduled != 1 || reads.Load() != 1 || active.Load() != 1 || maximum.Load() != 1 {
				t.Fatalf("new token overlapped the pending walk: scheduled=%d reads=%d active=%d peak=%d", scheduled, reads.Load(), active.Load(), maximum.Load())
			}
			if !a.comp.recentsPending || !a.comp.recentsAgain || c.recentsLoaded || !c.open || !strings.Contains(b.text(a), "looking…") {
				t.Fatal("new token did not wait for the pending walk's follow-up")
			}
			old, follow := land()
			if !a.comp.recentsPending || a.comp.recentsLoaded || c.recentsLoaded || len(a.comp.recents) != 0 || len(c.chatHits) != 0 || !c.open || !strings.Contains(b.text(a), "looking…") {
				t.Fatal("answer begun before the newest opening settled its search as fresh")
			}
			start(follow)
			if scheduled != 2 || reads.Load() != 2 || active.Load() != 1 || maximum.Load() != 1 {
				t.Fatalf("first answer did not start exactly one follow-up: scheduled=%d reads=%d active=%d peak=%d", scheduled, reads.Load(), active.Load(), maximum.Load())
			}
			_, extra := land()
			if len(completionRecentCommands(extra)) != 0 || a.comp.recentsPending || !a.comp.recentsLoaded || !c.recentsLoaded || !c.open || len(c.chatHits) != 1 || c.chatHits[0].title != "unique words" {
				t.Fatalf("follow-up did not settle the new token: pending=%v open=%v rows=%+v", a.comp.recentsPending, c.open, c.chatHits)
			}
			_, stale := a.Update(old)
			if len(completionRecentCommands(stale)) != 0 || a.comp.recentsPending || !c.recentsLoaded || !c.open || len(a.comp.recents) != 1 || a.comp.recents[0].title != "unique words" || len(c.chatHits) != 1 || c.chatHits[0].title != "unique words" {
				t.Fatalf("old answer displaced the newer catalog: pending=%v open=%v rows=%+v", a.comp.recentsPending, c.open, c.chatHits)
			}
			if reads.Load() != 2 || active.Load() != 0 || maximum.Load() != 1 {
				t.Fatalf("settled reads=%d active=%d peak=%d, want 2, 0, 1", reads.Load(), active.Load(), maximum.Load())
			}
			t.Logf("settled: reads=%d peak=%d catalog=%q", reads.Load(), maximum.Load(), c.chatHits[0].title)
		})
	}
}

// Emptying home's box must end its token before another window changes recents.
func TestHomeEmptyBoxEndsAtTokenAndNextOpeningReadsFreshRows(t *testing.T) {
	a := atBoxes[1].make(t)
	a.comp.recentsHeld, a.comp.recents = false, nil
	reads := 0
	rows := []Session{{File: filepath.Join(a.workspace, "old.jsonl"), Title: "old conversation"}}
	a.recentSessions = func() []Session { reads++; return rows }
	typeInto(t, a, "@chat:")
	for a.home.box.String() != "" {
		drive(t, a, key("backspace"))
	}
	if a.home.comp.open {
		t.Error("home kept the list open after backspace emptied its box")
	}
	rows = []Session{{File: filepath.Join(a.workspace, "puff.jsonl"), Title: "puff conversation"}}
	typeInto(t, a, "@chat:puff")
	if reads != 2 || len(a.home.comp.chatHits) != 1 || a.home.comp.chatHits[0].title != "puff conversation" {
		t.Fatalf("second opening reads=%d rows=%+v, want a fresh puff conversation", reads, a.home.comp.chatHits)
	}
	drive(t, a, key("enter"))
	if a.home.box.String() != "@puff-conversation" {
		t.Fatalf("enter inserted %q", a.home.box.String())
	}
}

// A default cursor is not a person's choice; fresh data should improve its match.
func TestRecentArrivalSelectsBestMatchUntilPersonChooses(t *testing.T) {
	for _, b := range atBoxes[:2] {
		for _, arrowed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/arrowed=%v", b.name, arrowed), func(t *testing.T) {
				a := b.make(t)
				loose := Session{File: filepath.Join(a.workspace, "swan.jsonl"), Title: "what color is a swan heron"}
				best := Session{File: filepath.Join(a.workspace, "heron.jsonl"), Title: "heron conversation"}
				a.mentionRecentsLoaded([]Session{loose})
				a.recentSessions = func() []Session { return []Session{best, loose} }
				_, cmd := a.Update(tea.PasteMsg{Content: "@chat:heron"})
				c := b.comp(a)
				if chosen, ok := c.chatChoice(); !ok || chosen.file != loose.File {
					t.Fatalf("stale catalog did not initially offer the loose match: %+v", c.chatHits)
				}
				if arrowed {
					drive(t, a, key("down"))
				}
				spend(t, a, cmd)
				want := "@heron-conversation"
				if arrowed {
					want = "@what-color-is-a-swan-heron"
				}
				if c.chatHits[0].file != best.File {
					t.Fatal("fresh data did not rank the best match first")
				}
				drive(t, a, key("enter"))
				if got := b.box(a); got != want {
					t.Fatalf("arrival inserted %q, want %q", got, want)
				}
			})
		}
	}
}

// Changing a query gives the best match the cursor again, including on file arrivals.
func TestQueryChangeEndsTheChosenRowOnEveryBox(t *testing.T) {
	for _, b := range atBoxes[:2] {
		t.Run(b.name, func(t *testing.T) {
			a := b.make(t)
			c := b.comp(a)
			c.all, c.loaded = []string{"heron-a.md", "heron-b.md"}, true
			if b.name == "home" {
				a.home.walked = a.homeCompletionRoot()
			}
			typeInto(t, a, "@file:heron")
			drive(t, a, key("down"))
			drive(t, a, key("-"))
			drive(t, a, filesLoadedMsg{paths: []string{"heron-.md", "heron-a.md", "heron-b.md"}, home: b.name == "home"})
			drive(t, a, key("enter"))
			if got := b.box(a); got != "@heron-.md" {
				t.Fatalf("query change retained the old choice: %q", got)
			}
		})
	}
}

// A queued first user message remains mentionable when its untitled tab is held.
func TestFirstQueuedFollowUpRemainsMentionableWhenHeld(t *testing.T) {
	_, a := queuedConversation(t)
	file := filepath.Join(t.TempDir(), "first-follow.jsonl")
	a.file, a.title, a.openingPrompt, a.entries = file, "", "", nil
	a.input.setText("first sent follow-up")
	drive(t, a, key("ctrl+enter"), streamClosedMsg{gen: a.gen})
	sent := false
	for _, e := range a.entries {
		if e.kind == entryUser && e.text == "first sent follow-up" {
			sent = true
		}
	}
	if !sent {
		t.Fatal("the queued turn did not record its sent user message")
	}
	a.input.setText("a later unsent draft")
	conv := a.front()
	side := a.detachConversation()
	a.stow(conv, side)
	a.file = filepath.Join(t.TempDir(), "new-front.jsonl")
	a.input.reset()
	for _, chat := range a.mentionChatsExcept("") {
		if chat.file == file {
			return
		}
	}
	t.Fatalf("held queued conversation was omitted: title=%q opening=%q draft=%q", side.title, side.openingPrompt, side.draft)
}

// Only the in-memory front is subject to the unsent-shell rule.
func TestOtherTabsRemainMentionableWithoutTitlesOrOpenings(t *testing.T) {
	a := mentionApp(t)
	file := filepath.Join(t.TempDir(), "held.jsonl")
	a.stow(Conversation{Agent: &fakeAgent{}, SessionFile: file}, &aside{draft: "held draft"})
	for _, chat := range a.mentionChatsExcept("") {
		if chat.file == file {
			return
		}
	}
	t.Fatal("an unlabelled held tab was filtered by the front's unsent-shell rule")
}

// The manual must explain token openings, deliberate choices and the front-only omission.
func TestMentionManualExplainsTokenOpeningsAndDeliberateChoices(t *testing.T) {
	for _, name := range []string{"conversations-and-teams.md", "home.md", "keys.md", "attaching-files.md"} {
		raw, err := os.ReadFile(filepath.Join("..", "manual", "chat", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, words := range []string{"once per", "token", "best match", "chose", "window's own"} {
			if !strings.Contains(string(raw), words) {
				t.Errorf("%s does not explain %q", name, words)
			}
		}
	}
}

// Only recent-reader commands are driven here, so unrelated clocks and file walks
// cannot obscure the exact number of reads admitted by the real edit path.
func completionRecentCommands(cmd tea.Cmd) []tea.Cmd {
	if cmd == nil {
		return nil
	}
	name := runtime.FuncForPC(reflect.ValueOf(cmd).Pointer()).Name()
	if strings.Contains(name, ".loadMentionRecents.") {
		return []tea.Cmd{cmd}
	}
	// Bubble Tea may return its generic compactCmds closure for Batch.
	// Unwrap both forms so the test drives the actual recent reader.
	if !strings.Contains(name, ".Batch.") && !strings.Contains(name, ".compactCmds[") {
		return nil
	}
	var out []tea.Cmd
	for _, child := range cmd().(tea.BatchMsg) {
		out = append(out, completionRecentCommands(child)...)
	}
	return out
}

// Blocking the reader makes overlap and freshness counts independent of disk
// speed. A burst owes one follow-up, begun after its last opening, not one per edit.
func TestRecentWalksNeverOverlapAndCoalesceOpeningBursts(t *testing.T) {
	for _, b := range atBoxes {
		for _, scenario := range []string{"three tokens", "sixty-four tokens", "escape and eight letters", "one pasted token", "replace pasted token"} {
			t.Run(b.name+"/"+scenario, func(t *testing.T) {
				a := b.make(t)
				a.comp.recentsHeld, a.comp.recentsLoaded, a.comp.recents = false, true, nil
				var openings, active, maximum, reads atomic.Int32
				started := make(chan int32, 128)
				release := make(chan struct{})
				answers := make(chan tea.Msg, 128)
				var workers sync.WaitGroup
				defer func() { close(release); workers.Wait() }()
				a.recentSessions = func() []Session {
					reads.Add(1)
					n := active.Add(1)
					for old := maximum.Load(); n > old; old = maximum.Load() {
						if maximum.CompareAndSwap(old, n) {
							break
						}
					}
					opening := openings.Load()
					started <- opening
					<-release
					active.Add(-1)
					return []Session{{File: filepath.Join(a.workspace, "latest.jsonl"), Title: fmt.Sprintf("latest words at opening %d", opening)}}
				}
				scheduled := 0
				start := func(cmd tea.Cmd) {
					for _, read := range completionRecentCommands(cmd) {
						scheduled++
						workers.Add(1)
						go func() { defer workers.Done(); answers <- read() }()
						select {
						case opening := <-started:
							if opening != openings.Load() {
								t.Fatalf("read began at opening %d, newest is %d", opening, openings.Load())
							}
						case <-time.After(5 * time.Second):
							t.Fatal("scheduled recent reader did not start")
						}
					}
				}
				edit := func(msg tea.Msg) {
					a.notices.seen[eventAtOpened] = false
					_, cmd := a.Update(msg)
					if a.notices.seen[eventAtOpened] {
						openings.Add(1)
					}
					start(cmd)
				}
				wantOpenings := int32(1)
				switch scenario {
				case "three tokens", "sixty-four tokens":
					wantOpenings = 3
					if scenario == "sixty-four tokens" {
						wantOpenings = 64
					}
					for i := int32(0); i < wantOpenings; i++ {
						if i > 0 {
							edit(key("backspace"))
						}
						edit(key("@"))
					}
				case "escape and eight letters":
					edit(key("@"))
					for i := 0; i < 8; i++ {
						edit(key("esc"))
						edit(key("c"))
					}
					wantOpenings = 9
				case "one pasted token", "replace pasted token":
					edit(tea.PasteMsg{Content: "@chat:latest words"})
					if scenario == "replace pasted token" {
						for b.box(a) != "" {
							edit(key("backspace"))
						}
						edit(tea.PasteMsg{Content: "@chat:latest words"})
						wantOpenings = 2
					}
				}
				t.Logf("before answers: reads=%d active=%d peak=%d openings=%d", reads.Load(), active.Load(), maximum.Load(), openings.Load())
				if scheduled != 1 || maximum.Load() != 1 || openings.Load() != wantOpenings {
					t.Fatalf("burst scheduled=%d peak=%d openings=%d, want 1, 1, %d", scheduled, maximum.Load(), openings.Load(), wantOpenings)
				}
				if !a.comp.recentsPending || b.comp(a).recentsLoaded {
					t.Fatal("blocked read was treated as settled")
				}
				if strings.Contains(b.box(a), " ") && (!b.comp(a).open || !strings.Contains(b.text(a), "looking…")) {
					t.Fatal("spaced search did not wait for its fresh catalog")
				}
				land := func() (tea.Msg, tea.Cmd) {
					release <- struct{}{}
					select {
					case msg := <-answers:
						_, cmd := a.Update(msg)
						return msg, cmd
					case <-time.After(5 * time.Second):
						t.Fatal("released reader did not answer")
						return nil, nil
					}
				}
				old, follow := land()
				wantReads := int32(1)
				if wantOpenings > 1 {
					wantReads = 2
					if !a.comp.recentsPending || a.comp.recentsLoaded || b.comp(a).recentsLoaded || len(a.comp.recents) != 0 {
						t.Fatal("read begun before the newest opening settled its search")
					}
					start(follow)
					if scheduled != 2 {
						t.Fatalf("burst scheduled %d reads after first answer, want 2", scheduled)
					}
					if _, cmd := a.Update(old); len(completionRecentCommands(cmd)) != 0 || !a.comp.recentsPending {
						t.Fatal("duplicate old answer disturbed the follow-up")
					}
					_, extra := land()
					if len(completionRecentCommands(extra)) != 0 {
						t.Fatal("settled burst scheduled a third read")
					}
				} else if len(completionRecentCommands(follow)) != 0 {
					t.Fatal("one opening scheduled a follow-up")
				}
				a.Update(old)
				wantTitle := fmt.Sprintf("latest words at opening %d", wantOpenings)
				if a.comp.recentsPending || !a.comp.recentsLoaded || len(a.comp.recents) != 1 || a.comp.recents[0].title != wantTitle {
					t.Fatalf("final catalog is not fresh: pending=%v rows=%+v", a.comp.recentsPending, a.comp.recents)
				}
				if reads.Load() != wantReads || maximum.Load() != 1 || active.Load() != 0 {
					t.Fatalf("settled reads=%d peak=%d active=%d, want %d, 1, 0", reads.Load(), maximum.Load(), active.Load(), wantReads)
				}
				t.Logf("settled: reads=%d peak=%d final catalog=%q", reads.Load(), maximum.Load(), wantTitle)
			})
		}
	}
}

// A supplied catalog with its door held shut remains authoritative across openings.
func TestHeldRecentCatalogDoesNotScheduleWalksForOpeningBursts(t *testing.T) {
	for _, b := range atBoxes {
		t.Run(b.name, func(t *testing.T) {
			a := b.make(t)
			reads := 0
			a.recentSessions = func() []Session { reads++; return nil }
			for i := 0; i < 3; i++ {
				drive(t, a, key("@"), key("backspace"))
			}
			if reads != 0 || a.comp.recentsRead != 0 || a.comp.recentsPending {
				t.Fatalf("held catalog read=%d generation=%d pending=%v", reads, a.comp.recentsRead, a.comp.recentsPending)
			}
		})
	}
}

// Moving between boxes must not create a second owner of the pending catalog.
func TestAtBoxesShareOnePendingRecentCatalog(t *testing.T) {
	a := atBoxes[2].make(t)
	a.cancelChatStart()
	a.comp.recentsHeld, a.comp.recents = false, nil
	reads := 0
	a.recentSessions = func() []Session {
		reads++
		return []Session{{File: filepath.Join(a.workspace, "latest.jsonl"), Title: fmt.Sprintf("latest words %d", reads)}}
	}
	_, first := a.Update(tea.PasteMsg{Content: "@chat:latest words"})
	commands := completionRecentCommands(first)
	if len(commands) != 1 {
		t.Fatalf("conversation scheduled %d readers, want 1", len(commands))
	}
	a.showPage(pageHome)
	_, home := a.Update(tea.PasteMsg{Content: "@chat:latest words"})
	if len(completionRecentCommands(home)) != 0 {
		t.Fatal("home started a second reader while the conversation's was pending")
	}
	a.showPage(pageNone)
	a.openChatStart()
	if !a.startingChat() {
		t.Fatal("fixture did not open the start page")
	}
	_, start := a.Update(tea.PasteMsg{Content: "@chat:latest words"})
	if len(completionRecentCommands(start)) != 0 {
		t.Fatal("start started a second reader while the conversation's was pending")
	}
	_, next := a.Update(commands[0]())
	commands = completionRecentCommands(next)
	if len(commands) != 1 || !a.comp.recentsPending || a.comp.recentsLoaded || a.home.comp.recentsLoaded {
		t.Fatal("three boxes did not share one pending follow-up")
	}
	_, extra := a.Update(commands[0]())
	if len(completionRecentCommands(extra)) != 0 || reads != 2 || a.comp.recentsPending || !a.comp.recentsLoaded {
		t.Fatalf("shared catalog did not settle after two reads: reads=%d pending=%v", reads, a.comp.recentsPending)
	}
	if len(a.comp.chatHits) != 1 || a.comp.chatHits[0].title != "latest words 2" {
		t.Fatalf("start did not receive the shared fresh catalog: %+v", a.comp.chatHits)
	}
	// Leaving home closes its list, so it copies the shared rows when needed again.
	a.fillHomeMentions()
	for _, chat := range a.home.comp.chats {
		if chat.title == "latest words 2" && a.home.comp.recentsLoaded {
			return
		}
	}
	t.Fatalf("home did not copy the shared fresh catalog: %+v", a.home.comp.chats)
}

// Escape seals even the empty query over a separator; letters still begin one
// fresh opening, and another letter in that opening starts no additional read.
func TestEscThenSeparatorKeepsBareAndNamedAtListsClosed(t *testing.T) {
	for _, b := range atBoxes {
		for _, query := range []string{"@", "@chat:ki"} {
			for _, after := range []string{" ", ",", ".", ";", ":", "!", "?", "(", ")", "'", "c", "é"} {
				t.Run(b.name+"/"+query+"/"+after, func(t *testing.T) {
					a := b.make(t)
					a.comp.recentsHeld = false
					reads, notices := 0, 0
					a.recentSessions = func() []Session { reads++; return nil }
					edit := func(msg tea.Msg) {
						a.notices.seen[eventAtOpened] = false
						drive(t, a, msg)
						if a.notices.seen[eventAtOpened] {
							notices++
						}
					}
					for _, r := range query {
						edit(key(string(r)))
					}
					edit(key("esc"))
					if b.comp(a).open || b.box(a) != query {
						t.Fatal("escape changed the token or left the list open")
					}
					edit(key(after))
					wantOpen := after == "c" || after == "é"
					wantReads := 1
					if wantOpen {
						wantReads++
					}
					if b.comp(a).open != wantOpen || reads != wantReads || notices != wantReads {
						t.Fatalf("after %q: open=%v reads=%d notices=%d, want open=%v reads/notices=%d", after, b.comp(a).open, reads, notices, wantOpen, wantReads)
					}
					edit(key("c"))
					if b.comp(a).open != wantOpen || reads != wantReads || notices != wantReads {
						t.Fatal("following letter changed the separator or read again within an opening")
					}
				})
			}
		}
	}
}
