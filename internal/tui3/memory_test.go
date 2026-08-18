package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// rememberingAgent is a fakeAgent that also has a brain — the optional
// interface memory.go asserts. It is a separate type rather than three more
// fields on fakeAgent for exactly the reason the interface is separate: a
// session without memory is the ordinary case, and every other test in this
// package has to keep meaning what it means.
type rememberingAgent struct {
	fakeAgent

	kept    []session.MemoryLine
	failing error
	off     bool

	remembered, forgotten, listed []string
}

func (r *rememberingAgent) Remembers() bool { return !r.off }

func (r *rememberingAgent) Remember(text string) (string, error) {
	r.remembered = append(r.remembered, text)
	if r.failing != nil {
		return "", r.failing
	}
	line := session.MemoryLine{ID: "mem_1", Title: "prefers tabs", Text: text}
	r.kept = append(r.kept, line)
	return line.Title, nil
}

func (r *rememberingAgent) Forget(query string) (string, error) {
	r.forgotten = append(r.forgotten, query)
	if r.failing != nil {
		return "", r.failing
	}
	for index, line := range r.kept {
		if strings.Contains(strings.ToLower(line.Text), strings.ToLower(query)) {
			r.kept = append(r.kept[:index], r.kept[index+1:]...)
			return line.Title, nil
		}
	}
	return "", nil
}

func (r *rememberingAgent) Memories(query string) ([]session.MemoryLine, error) {
	r.listed = append(r.listed, query)
	if r.failing != nil {
		return nil, r.failing
	}
	if strings.TrimSpace(query) == "" {
		return r.kept, nil
	}
	var found []session.MemoryLine
	for _, line := range r.kept {
		if strings.Contains(strings.ToLower(line.Text), strings.ToLower(query)) {
			found = append(found, line)
		}
	}
	return found, nil
}

type panelMemoryStore struct {
	rows      []store.Memory
	origins   map[string]memoryOrigin
	updated   []string
	forgotten []string
	restored  []string
}

func (s *panelMemoryStore) ListMemories(scope string, limit int) ([]store.Memory, error) {
	var rows []store.Memory
	for _, row := range s.rows {
		if row.Status == store.MemoryForgotten || scope != "" && row.Scope != scope {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}
func (s *panelMemoryStore) UpdateMemory(id, title, text string, tags []string) error {
	s.updated = append(s.updated, text)
	for i := range s.rows {
		if s.rows[i].ID == id {
			s.rows[i].Text = text
		}
	}
	return nil
}
func (s *panelMemoryStore) ForgetMemory(id string) error {
	s.forgotten = append(s.forgotten, id)
	for i := range s.rows {
		if s.rows[i].ID == id {
			s.rows[i].Status = store.MemoryForgotten
		}
	}
	return nil
}
func (s *panelMemoryStore) RestoreMemory(id string) error {
	s.restored = append(s.restored, id)
	for i := range s.rows {
		if s.rows[i].ID == id {
			s.rows[i].Status = store.MemoryActive
		}
	}
	return nil
}
func (s *panelMemoryStore) MemoryProvenance(id string) (string, string, time.Time, error) {
	origin := s.origins[id]
	return "session", origin.title, origin.at, nil
}

func memoryPanelApp(t *testing.T, rows []store.Memory) (*app, *panelMemoryStore) {
	t.Helper()
	agent := &rememberingAgent{}
	for _, row := range rows {
		agent.kept = append(agent.kept, session.MemoryLine{ID: row.ID, Title: row.Title, Text: row.Text})
	}
	memory := &panelMemoryStore{rows: rows, origins: map[string]memoryOrigin{}}
	a := newApp(t.Context(), Options{Agent: agent, Workspace: "/tmp/lab", Memory: memory})
	return a, memory
}

// ── the table ───────────────────────────────────────────────────────────────

func TestTheThreeMemoryCommandsAreOnTheList(t *testing.T) {
	help := helpText("")
	for _, name := range []string{"memory", "remember", "forget"} {
		var found bool
		for _, c := range commands {
			found = found || c.name == name
		}
		if !found {
			t.Fatalf("/%s is not on the command list", name)
		}
		if !strings.Contains(help, "/"+name) {
			t.Fatalf("/%s is not in /help", name)
		}
	}
	if got := canonicalCommand("memories"); got != "memories" {
		t.Fatalf("/memories ran as /%s", got)
	}
	if err := checkCommands(commands); err != nil {
		t.Fatalf("the table stopped being a table: %v", err)
	}
}

// ── the happy paths ─────────────────────────────────────────────────────────

func TestRememberKeepsOneThingAndSaysWhatItKept(t *testing.T) {
	agent := &rememberingAgent{}
	a := newTestApp(agent)

	a.slash("/remember I prefer tabs over spaces in Go")

	if len(agent.remembered) != 1 || agent.remembered[0] != "I prefer tabs over spaces in Go" {
		t.Fatalf("the agent was asked to remember %v", agent.remembered)
	}
	if text := lastNote(t, a); !strings.Contains(text, "remembered") || !strings.Contains(text, "prefers tabs") {
		t.Fatalf("the answer was %q", text)
	}
}

func TestMemoriesListsOnePerLineWithTheIdThatNamesIt(t *testing.T) {
	agent := &rememberingAgent{kept: []session.MemoryLine{
		{ID: "mem_1", Title: "prefers tabs", Text: "prefers tabs over spaces in Go"},
		{ID: "mem_2", Title: "standup time", Text: "standup is at 9:15"},
	}}
	a := newTestApp(agent)

	a.slash("/memories")

	text := lastNote(t, a)
	lines := strings.Split(text, "\n")
	if len(lines) != 2 {
		t.Fatalf("the list is %d lines:\n%s", len(lines), text)
	}
	if !strings.Contains(lines[0], "prefers tabs — prefers tabs over spaces in Go") || !strings.Contains(lines[0], "(mem_1)") {
		t.Fatalf("a row reads %q", lines[0])
	}
}

func TestMemoriesWithAQueryNarrowsTheList(t *testing.T) {
	agent := &rememberingAgent{kept: []session.MemoryLine{
		{ID: "mem_1", Title: "prefers tabs", Text: "prefers tabs over spaces in Go"},
		{ID: "mem_2", Title: "standup time", Text: "standup is at 9:15"},
	}}
	a := newTestApp(agent)

	a.slash("/memories standup")

	if len(agent.listed) != 1 || agent.listed[0] != "standup" {
		t.Fatalf("the agent was asked for %v", agent.listed)
	}
	text := lastNote(t, a)
	if strings.Contains(text, "tabs") || !strings.Contains(text, "standup") {
		t.Fatalf("the narrowed list is:\n%s", text)
	}
}

func TestForgetDropsTheMatchAndNamesIt(t *testing.T) {
	agent := &rememberingAgent{kept: []session.MemoryLine{
		{ID: "mem_2", Title: "standup time", Text: "standup is at 9:15"},
	}}
	a := newTestApp(agent)

	a.slash("/forget standup")

	if text := lastNote(t, a); !strings.Contains(text, "forgot") || !strings.Contains(text, "standup time") {
		t.Fatalf("the answer was %q", text)
	}
	if len(agent.kept) != 0 {
		t.Fatalf("the memory is still kept: %v", agent.kept)
	}
}

// ── the empty and the missing ───────────────────────────────────────────────

// A COMMAND TYPED ON PURPOSE ALWAYS ANSWERS. An empty store, an empty search
// and a no-match forget are three different facts and each gets its own line —
// silence would read as a command that broke.
func TestTheEmptyStatesEachSayWhichEmptinessItIs(t *testing.T) {
	agent := &rememberingAgent{}
	a := newTestApp(agent)

	a.slash("/memories")
	if text := lastNote(t, a); text != "nothing is remembered yet" {
		t.Fatalf("an empty store answered %q", text)
	}

	agent.kept = []session.MemoryLine{{ID: "mem_1", Title: "prefers tabs", Text: "prefers tabs over spaces in Go"}}
	a.slash("/memories pineapples")
	if text := lastNote(t, a); !strings.Contains(text, "nothing remembered matches pineapples") {
		t.Fatalf("an empty search answered %q", text)
	}

	a.slash("/forget pineapples")
	if text := lastNote(t, a); !strings.Contains(text, "nothing matched pineapples") {
		t.Fatalf("a no-match forget answered %q", text)
	}
}

func TestTheTwoArgumentCommandsAskForTheirArgument(t *testing.T) {
	a := newTestApp(&rememberingAgent{})

	a.slash("/remember")
	if text := lastNote(t, a); !strings.Contains(text, "what should be kept") {
		t.Fatalf("/remember with nothing answered %q", text)
	}
	a.slash("/forget   ")
	if text := lastNote(t, a); !strings.Contains(text, "what should be dropped") {
		t.Fatalf("/forget with nothing answered %q", text)
	}
}

// A SESSION WITH NO BRAIN SAYS SO AND NAMES THE ROW. "no" without "and here is
// how to change that" is the half of an answer that sends somebody to the
// manual.
func TestWithoutABrainAllThreeSayMemoryIsOff(t *testing.T) {
	// Two shapes of "no brain", and they must read the same: an agent that has
	// never heard of memory (every other test's fake, and every surface built
	// before this feature), and a real session whose door opened no store.
	for _, agent := range []Agent{&fakeAgent{model: "m"}, &rememberingAgent{off: true}} {
		a := newTestApp(agent)
		for _, line := range []string{"/memories", "/remember something", "/forget something"} {
			a.slash(line)
			if text := lastNote(t, a); !strings.Contains(text, "memory is off") || !strings.Contains(text, "/settings") {
				t.Fatalf("%s answered %q", line, text)
			}
		}
	}
}

func TestBareMemoryOpensPanelAndQueryPrints(t *testing.T) {
	a, _ := memoryPanelApp(t, []store.Memory{{ID: "m1", Title: "uses neovim", Text: "uses neovim daily", Type: store.MemoryPreference, Scope: store.MemoryScopeUser}})
	a.slash("/memory")
	if !a.memPanel.open {
		t.Fatal("bare /memory did not open the panel")
	}
	if got := plain(frame(a)); !strings.Contains(got, "uses neovim") {
		t.Fatalf("panel did not list memory:\n%s", got)
	}
	a.memPanel.close()
	a.slash("/memory vim")
	if a.memPanel.open {
		t.Fatal("/memory <query> opened the panel")
	}
	if got := lastNote(t, a); !strings.Contains(got, "uses neovim") {
		t.Fatalf("print posture said %q", got)
	}
	a.slash("/memories vim")
	if got := lastNote(t, a); !strings.Contains(got, "uses neovim") {
		t.Fatalf("alias said %q", got)
	}
	a.slash("/memories")
	if a.memPanel.open || !strings.Contains(lastNote(t, a), "uses neovim") {
		t.Fatal("bare /memories stopped using the print posture")
	}
}

func TestMemoryPanelEmptyOffFilterAndEscape(t *testing.T) {
	a, _ := memoryPanelApp(t, nil)
	a.slash("/memory")
	if got := plain(frame(a)); !strings.Contains(got, "nothing is remembered here") {
		t.Fatalf("empty panel:\n%s", got)
	}
	drive(t, a, key("esc"))
	if a.memPanel.open {
		t.Fatal("esc did not close the memory panel")
	}

	off := newTestApp(&rememberingAgent{off: true})
	off.slash("/memory")
	if got := lastNote(t, off); got != "memory is off · turn it on under /settings" {
		t.Fatalf("off note was %q", got)
	}

	rows := []store.Memory{
		{ID: "m1", Title: "terminal editor", Text: "uses neovim", Scope: store.MemoryScopeUser},
		{ID: "m2", Title: "deploys", Text: "deploys Fridays", Scope: store.MemoryScopeProject},
	}
	a, _ = memoryPanelApp(t, rows)
	a.slash("/memory")
	typeInto(t, a, "nvm")
	if memory, ok := a.memPanel.choice(); !ok || memory.ID != "m1" {
		t.Fatalf("fuzzy filter chose %#v, %v", memory, ok)
	}
}

func TestMemoryExpandProvenanceEditAndCancel(t *testing.T) {
	a, memory := memoryPanelApp(t, []store.Memory{{ID: "m1", Title: "uses neovim", Text: "uses neovim daily", Tags: []string{"editor"}, UseCount: 7, Scope: store.MemoryScopeUser}})
	memory.origins["m1"] = memoryOrigin{title: "Editor setup", at: time.Now().Add(-2 * time.Hour)}
	a.slash("/memory")
	drive(t, a, key("enter"))
	if got := plain(frame(a)); !strings.Contains(got, "in 'Editor setup'") || !strings.Contains(got, "tags · editor") {
		t.Fatalf("expanded row:\n%s", got)
	}
	drive(t, a, key("enter"))
	if a.memPanel.edit == nil || a.memPanel.edit.String() != "uses neovim daily" {
		t.Fatal("edit was not preloaded")
	}
	drive(t, a, key("ctrl+u"))
	typeInto(t, a, "uses helix")
	drive(t, a, key("esc"))
	if len(memory.updated) != 0 {
		t.Fatal("esc wrote the edit")
	}
	drive(t, a, key("enter"))
	drive(t, a, key("ctrl+u"))
	typeInto(t, a, "uses helix")
	drive(t, a, key("enter"))
	if len(memory.updated) != 1 || memory.updated[0] != "uses helix" {
		t.Fatalf("updates were %v", memory.updated)
	}

	a.memPanel.expanded = ""
	memory.origins["m1"] = memoryOrigin{at: time.Now().Add(-time.Hour)}
	a.memPanel.origins["m1"] = memory.origins["m1"]
	drive(t, a, key("enter"))
	if got := plain(frame(a)); !strings.Contains(got, "learned") || !strings.Contains(got, "ago") {
		t.Fatalf("unknown provenance:\n%s", got)
	}
}

func TestMemoryForgetUndoIsOneDeepAndScopeCycles(t *testing.T) {
	a, memory := memoryPanelApp(t, []store.Memory{
		{ID: "m1", Title: "uses neovim", Text: "uses neovim", Scope: store.MemoryScopeUser},
		{ID: "m2", Title: "release branch", Text: "release is main", Scope: store.MemoryScopeProject},
	})
	a.slash("/memory")
	drive(t, a, key("delete"))
	if len(memory.forgotten) != 1 || !strings.Contains(a.memPanel.footer, "forgot 'uses neovim' — u to undo") {
		t.Fatalf("forget state: %v %q", memory.forgotten, a.memPanel.footer)
	}
	drive(t, a, key("u"))
	if len(memory.restored) != 1 {
		t.Fatalf("restore calls %v", memory.restored)
	}
	drive(t, a, key("delete"))
	drive(t, a, key("delete"))
	drive(t, a, key("u"))
	if len(memory.restored) != 2 || memory.restored[1] != "m2" {
		t.Fatalf("one-deep restore calls %v", memory.restored)
	}

	// Reload the two rows, then tab narrows all to user and project in order.
	memory.rows[0].Status, memory.rows[1].Status = store.MemoryActive, store.MemoryActive
	a.memPanel.start(memory.rows)
	drive(t, a, key("tab"))
	if got, _ := a.memPanel.choice(); got.Scope != store.MemoryScopeUser {
		t.Fatalf("user scope chose %#v", got)
	}
	drive(t, a, key("tab"))
	if got, _ := a.memPanel.choice(); got.Scope != store.MemoryScopeProject {
		t.Fatalf("project scope chose %#v", got)
	}
}

func TestAFailureIsReportedAndNotSwallowed(t *testing.T) {
	agent := &rememberingAgent{failing: errors.New("the brain is locked")}
	a := newTestApp(agent)

	a.slash("/remember I prefer tabs")
	if text := lastNote(t, a); !strings.Contains(text, "the brain is locked") {
		t.Fatalf("a failed write answered %q", text)
	}
}
