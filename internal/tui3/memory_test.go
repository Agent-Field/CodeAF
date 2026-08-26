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
	learned   int
	letGo     int
	snapshots int
}

// Snapshot is the fake's half of the seam the place actually draws from: the
// rows shelved by scope and counted by status, exactly as internal/store does
// it in two statements. It is here rather than in a helper because the shape IS
// the contract — a page that could not tell "held" from "let go" would draw
// forgotten lines as though nothing had happened to them.
func (s *panelMemoryStore) Snapshot(limit int) (store.MemoryShelves, error) {
	s.snapshots++
	shelves := map[string]*store.MemoryShelf{}
	var out store.MemoryShelves
	for _, row := range s.rows {
		shelf := shelves[row.Scope]
		if shelf == nil {
			shelf = &store.MemoryShelf{Scope: row.Scope, Label: store.MemoryShelfWord(row.Scope), ByType: map[string]int{}}
			shelves[row.Scope] = shelf
		}
		shelf.Memories = append(shelf.Memories, row)
		shelf.ByType[row.Type]++
		switch row.Status {
		case store.MemoryForgotten:
			shelf.LetGo, out.LetGo = shelf.LetGo+1, out.LetGo+1
		case store.MemorySuperseded:
			shelf.Superseded, out.Superseded = shelf.Superseded+1, out.Superseded+1
		default:
			shelf.Held, out.Held = shelf.Held+1, out.Held+1
		}
		out.Total, out.Shown = out.Total+1, out.Shown+1
	}
	for _, scope := range []string{store.MemoryScopeUser, store.MemoryScopeProject, store.MemoryScopeEnv} {
		if shelf := shelves[scope]; shelf != nil {
			out.Shelves = append(out.Shelves, *shelf)
		}
	}
	return out, nil
}

func (s *panelMemoryStore) ChangedSince(time.Time) (int, int, error) { return s.learned, s.letGo, nil }

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

func memoryPlaceApp(t *testing.T, rows []store.Memory) (*app, *panelMemoryStore) {
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
	a, _ := memoryPlaceApp(t, []store.Memory{{ID: "m1", Title: "uses neovim", Text: "uses neovim daily", Type: store.MemoryPreference, Scope: store.MemoryScopeUser}})
	a.slash("/memory")
	if !a.at(pageMemory) {
		t.Fatal("bare /memory did not open the panel")
	}
	if got := plain(frame(a)); !strings.Contains(got, "uses neovim") {
		t.Fatalf("panel did not list memory:\n%s", got)
	}
	a.leavePlace()
	a.slash("/memory vim")
	if a.at(pageMemory) {
		t.Fatal("/memory <query> opened the panel")
	}
	if got := lastNote(t, a); !strings.Contains(got, "uses neovim") {
		t.Fatalf("print posture said %q", got)
	}
	a.slash("/memories vim")
	if got := lastNote(t, a); !strings.Contains(got, "uses neovim") {
		t.Fatalf("alias said %q", got)
	}
	// AND BARE /memories OPENS THE PLACE NOW, on a surface that has one. It kept
	// an older print posture while memory was a twelve-row overlay; the moment it
	// became a place a person can walk into, filter and act on, sending them to
	// the transcript instead was sending them to the worse half of the feature.
	// With a query BOTH spellings still print, because a query is a question
	// rather than a door.
	a.slash("/memories")
	if !a.at(pageMemory) {
		t.Fatal("bare /memories did not open the memory place")
	}
}

// THE EMPTY PLACE TEACHES, AND SO DOES THE ONE WITH NO STORE BEHIND IT.
//
// A machine that has remembered nothing meets the three sentences that say what
// this place is for. Memory switched off in the settings meets exactly the same
// three, with the one fact they cannot carry said once on the note line under
// them — this build is not remembering anything, and here is the row that
// changes that ([memoryOffNote]).
//
// THE SECOND HALF USED TO BE A REFUSAL. The door would not open the place at all
// and wrote its sentence into the transcript, so `alt+4` on a build without a
// store was a key that did nothing; SCREEN 1f's preamble says an almost-empty
// place is the best teacher on the machine, and a person who has never seen this
// page is exactly who is standing there.
func TestMemoryPlaceEmptyOffAndFilter(t *testing.T) {
	a, _ := memoryPlaceApp(t, nil)
	a.slash("/memory")
	got := plain(frame(a))
	if !strings.Contains(got, memoryTeaching[0]) {
		t.Fatalf("the empty place did not teach:\n%s", got)
	}
	if strings.Contains(got, "shelves · biggest first") {
		t.Fatalf("a heading was drawn over no shelves:\n%s", got)
	}
	drive(t, a, key("esc"))
	if a.at(pageMemory) {
		t.Fatal("esc did not close the memory place")
	}

	off := newTestApp(&rememberingAgent{off: true})
	off.width, off.height = 100, 30
	off.slash("/memory")
	if !off.at(pageMemory) {
		t.Fatal("a build with no store behind it opened no memory place")
	}
	offScreen := plain(frame(off))
	if !strings.Contains(offScreen, memoryTeaching[0]) {
		t.Fatalf("the place with no store did not teach:\n%s", offScreen)
	}
	if !strings.Contains(offScreen, memoryOffNote) {
		t.Fatalf("the place with no store does not say so:\n%s", offScreen)
	}

	// TYPING FILTERS THE HELD SNAPSHOT AND READS NOTHING. The store is asked
	// once, when the place opens; every letter after that narrows shelves and
	// lines already in memory.
	rows := []store.Memory{
		{ID: "m1", Title: "terminal editor", Text: "uses neovim", Scope: store.MemoryScopeUser},
		{ID: "m2", Title: "deploys", Text: "deploys Fridays", Scope: store.MemoryScopeProject},
	}
	a, brain := memoryPlaceApp(t, rows)
	a.slash("/memory")
	asked := brain.snapshots
	typeInto(t, a, "nvm")
	body := plain(frame(a))
	if !strings.Contains(body, "terminal editor") || strings.Contains(body, "deploys") {
		t.Fatalf("the filter did not narrow the shelves:\n%s", body)
	}
	if brain.snapshots != asked {
		t.Fatalf("typing read the store %d more times", brain.snapshots-asked)
	}
}

// `enter` ON A SHELF OPENS AND CLOSES IT, and a line's own card is `→ c` — one
// provenance read, for one line, on the keystroke that asked for it.
//
// THE CARD MOVED AND THE LAW DID NOT. It used to be behind `enter` on a line;
// SCREEN 1f puts `ask me about it` there and the card on the row's strip, and
// what this test is about — that the store is asked about ONE line, when
// somebody asks for that line, and not about every line on the page — is the
// same law either way.
func TestMemoryShelvesOpenAndALineCarriesItsProvenance(t *testing.T) {
	a, memory := memoryPlaceApp(t, []store.Memory{{ID: "m1", Title: "uses neovim", Text: "uses neovim daily", Tags: []string{"editor"}, UseCount: 7, Scope: store.MemoryScopeUser}})
	memory.origins["m1"] = memoryOrigin{title: "Editor setup", at: time.Now().Add(-2 * time.Hour)}
	a.slash("/memory")
	if _, ok := a.mem.shelfUnder(); !ok {
		t.Fatal("the cursor did not open on a shelf heading")
	}
	// The biggest shelf opens itself, so enter here ROLLS IT UP.
	drive(t, a, key("enter"))
	if strings.Contains(plain(frame(a)), "uses neovim") {
		t.Fatalf("enter did not close the shelf:\n%s", plain(frame(a)))
	}
	drive(t, a, key("enter"))
	drive(t, a, key("down"))
	if got, ok := a.mem.choice(); !ok || got.ID != "m1" {
		t.Fatalf("down did not land on the line: %#v %v", got, ok)
	}
	drive(t, a, key("right"))
	drive(t, a, key("c"))
	if got := plain(frame(a)); !strings.Contains(got, "in 'Editor setup'") || !strings.Contains(got, "tags · editor") {
		t.Fatalf("the line's card:\n%s", got)
	}
	drive(t, a, key("enter"))
	if a.mem.edit == nil || a.mem.edit.String() != "uses neovim daily" {
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
	if got := plain(frame(a)); !strings.Contains(got, "uses helix") {
		t.Fatalf("the corrected wording is not on the page:\n%s", got)
	}
}

// THE UNDO IS ONE DEEP, AND IT IS A VERB ON THE ROW'S STRIP RATHER THAN A BARE
// LETTER.
//
// `u` used to be matched ahead of the filter's default arm, which meant the
// letter could not be TYPED — a search for a word with a `u` in it lost it and
// put something back instead. It is on the `→` strip now, offered only while
// there is something to put back, and `tab` — which cycled the shelves — is the
// way to the next place, so the shelf walk moved to `alt+s` (verbstrip.go).
func TestMemoryForgetUndoIsOneDeepAndAltSWalksTheShelves(t *testing.T) {
	a, memory := memoryPlaceApp(t, []store.Memory{
		{ID: "m1", Title: "uses neovim", Text: "uses neovim", Scope: store.MemoryScopeUser},
		{ID: "m2", Title: "release branch", Text: "release is main", Scope: store.MemoryScopeProject},
	})
	a.slash("/memory")
	drive(t, a, key("down"))
	drive(t, a, key("delete"))
	if len(memory.forgotten) != 1 || !strings.Contains(a.mem.footer, "forgot 'uses neovim'") {
		t.Fatalf("forget state: %v %q", memory.forgotten, a.mem.footer)
	}
	drive(t, a, key("right"), key("u"))
	if len(memory.restored) != 1 {
		t.Fatalf("restore calls %v", memory.restored)
	}

	// `alt+s` WALKS THE SHELVES, one open at a time, and ends with them all
	// rolled up — a state `enter` cannot reach in one press.
	a.slash("/memory")
	open := func() []string {
		var found []string
		for scope, on := range a.mem.shelfOpen {
			if on {
				found = append(found, scope)
			}
		}
		return found
	}
	first := open()
	if len(first) != 1 {
		t.Fatalf("the place opened with %v unrolled, want exactly one", first)
	}
	drive(t, a, key("alt+s"))
	second := open()
	if len(second) != 1 || second[0] == first[0] {
		t.Fatalf("alt+s left %v unrolled, want the next shelf after %v", second, first)
	}
	drive(t, a, key("alt+s"))
	if rest := open(); len(rest) != 0 {
		t.Fatalf("alt+s past the last shelf left %v unrolled", rest)
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

// `enter` ON A LINE IS `ask me about it` (SCREEN 1f / FIDELITY item 5).
//
// A line here is something aforge believes about you, and the useful thing to do
// with one is to talk about it. It used to open the card in place — the thing
// the design moved onto the row's `→` strip — and the foot said `enter open a
// shelf` over every row, so on a line it named a key and described something
// else.
func TestEnterOnAMemoryLineOpensAConversationAboutThatLine(t *testing.T) {
	a, _ := memoryPlaceApp(t, []store.Memory{{
		ID: "m1", Title: "uses neovim", Text: "uses neovim daily", Scope: store.MemoryScopeUser,
	}})
	started := &rememberingAgent{}
	a.start = func(workspace string) (Conversation, error) {
		return Conversation{Agent: started, SessionFile: "/tmp/lab/next/transcript.jsonl", Workspace: workspace}, nil
	}
	a.slash("/memory")
	drive(t, a, key("down"))
	line, ok := a.mem.choice()
	if !ok || line.ID != "m1" {
		t.Fatalf("the cursor is not on the line: %#v %v", line, ok)
	}
	// The foot says what THIS row can be asked for, and names the two verbs the
	// row's strip holds.
	if got := (placeMemory{}).hint(a); got != memoryLineHint {
		t.Fatalf("over a line the foot reads %q, want %q", got, memoryLineHint)
	}

	drive(t, a, key("enter"))
	if a.at(pageMemory) {
		t.Fatal("`enter` on a line left the person standing in the memory place")
	}
	if len(started.sent) != 1 {
		t.Fatalf("the fresh conversation was sent %v", started.sent)
	}
	if !strings.Contains(started.sent[0], "uses neovim daily") {
		t.Fatalf("the conversation was not seeded with the line: %q", started.sent[0])
	}
	if !strings.HasPrefix(started.sent[0], memoryAskOpening) {
		t.Fatalf("the opening does not say what the line is: %q", started.sent[0])
	}
}

// AND A SHELF HEADING KEEPS ITS OWN `enter` AND ITS OWN SENTENCE. Two rows, two
// things the key does, two feet — which is pages.go's contract for a hint.
func TestTheMemoryFootSaysWhatTheRowUnderTheCursorCanBeAskedFor(t *testing.T) {
	a, _ := memoryPlaceApp(t, []store.Memory{{
		ID: "m1", Title: "uses neovim", Text: "uses neovim daily", Scope: store.MemoryScopeUser,
	}})
	a.slash("/memory")
	if _, ok := a.mem.shelfUnder(); !ok {
		t.Fatal("the cursor did not open on a shelf heading")
	}
	if got := (placeMemory{}).hint(a); got != memoryShelfHint {
		t.Fatalf("over a shelf the foot reads %q, want %q", got, memoryShelfHint)
	}
	if strings.Contains(memoryShelfHint, memoryAskWord) {
		t.Fatalf("the shelf's foot promises %q, which `enter` does not do there", memoryAskWord)
	}
	drive(t, a, key("down"))
	if got := (placeMemory{}).hint(a); strings.Contains(got, "open a shelf") {
		t.Fatalf("over a line the foot still says `enter open a shelf`: %q", got)
	}
	// AND THE CARD IS ON THE STRIP, offered on a line and on nothing else.
	words := ""
	for _, v := range (placeMemory{}).verbs(a) {
		words += string(v.key) + " " + v.word + " · "
	}
	if !strings.Contains(words, "c "+memoryCardWord) {
		t.Fatalf("the line's strip does not carry the card: %s", words)
	}
}
