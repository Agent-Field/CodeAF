package tui3

import (
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE SURFACE OF MEMORY: the commands say exactly what the conversation wrote,
// a command typed on home answers on home, the memory place shelves what is
// kept by where it holds and says who kept each line, its foot names only keys
// that work, and every write leaves the update loop.

// rememberingAgent is a fakeAgent with a memory: the optional interfaces
// memory.go asserts. Its receipts are the engine's own spellings, because the
// surface prints what the conversation wrote rather than composing its own.
type rememberingAgent struct {
	fakeAgent

	kept    []session.MemoryLine
	failing error
	off     bool
	project string

	remembered, listed, moved []string
	everywhere                []bool
}

func (r *rememberingAgent) Remembers() bool { return !r.off }

func (r *rememberingAgent) Remember(text string) (string, error) {
	r.remembered = append(r.remembered, text)
	if r.failing != nil {
		return "", r.failing
	}
	r.kept = append(r.kept, session.MemoryLine{ID: "mem_" + itoa(len(r.kept)+1), Title: text, Text: text, Where: "for this project", Origin: store.OriginPerson})
	return "remembered · " + text + " · for this project", nil
}

func (r *rememberingAgent) RememberAlways(text string, everywhere bool) (string, error) {
	r.everywhere = append(r.everywhere, everywhere)
	r.remembered = append(r.remembered, text)
	if r.failing != nil {
		return "", r.failing
	}
	where := "for this project"
	if everywhere {
		where = "for you everywhere"
	}
	r.kept = append(r.kept, session.MemoryLine{ID: "mem_" + itoa(len(r.kept)+1), Title: text, Text: text, Where: where, Origin: store.OriginPerson})
	return "remembered · " + text + " · " + where, nil
}

func (r *rememberingAgent) AlwaysMemories(everywhere bool) ([]session.MemoryLine, error) {
	return r.Memories("")
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

func (r *rememberingAgent) MoveMemory(id, scope string) (string, error) {
	r.moved = append(r.moved, id+"→"+scope)
	return "now for you everywhere", nil
}

func (r *rememberingAgent) MemoryProject() string { return r.project }

// panelMemoryStore is the store as the memory place reads it, shelving rows by
// owner exactly as internal/store does.
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

func (s *panelMemoryStore) Snapshot(limit int) (store.MemoryShelves, error) {
	s.snapshots++
	byOwner := map[string]*store.MemoryShelf{}
	var out store.MemoryShelves
	for _, row := range s.rows {
		if row.Owner == "" {
			row.Owner = store.OwnerUser
		}
		if row.Status != store.MemoryActive && row.Status != "" {
			out.LetGo = append(out.LetGo, row)
			out.LetGoCount++
			continue
		}
		shelf := byOwner[row.Owner]
		if shelf == nil {
			shelf = &store.MemoryShelf{Owner: row.Owner}
			byOwner[row.Owner] = shelf
		}
		row.Status = store.MemoryActive
		shelf.Memories = append(shelf.Memories, row)
		shelf.Held++
		if row.ProjectName != "" {
			shelf.ProjectName = row.ProjectName
		}
		out.Held++
	}
	rank := func(owner string) int {
		switch owner {
		case store.OwnerUser:
			return 0
		case store.OwnerMachine:
			return 1
		case store.OwnerLegacyProject:
			return 3
		}
		return 2
	}
	for _, shelf := range byOwner {
		out.Shelves = append(out.Shelves, *shelf)
	}
	sort.SliceStable(out.Shelves, func(i, j int) bool {
		if rank(out.Shelves[i].Owner) != rank(out.Shelves[j].Owner) {
			return rank(out.Shelves[i].Owner) < rank(out.Shelves[j].Owner)
		}
		return out.Shelves[i].Owner < out.Shelves[j].Owner
	})
	return out, nil
}

func (s *panelMemoryStore) ChangedSince(time.Time) (int, int, error) { return s.learned, s.letGo, nil }

func (s *panelMemoryStore) UpdateMemory(id, text string) error {
	s.updated = append(s.updated, text)
	for i := range s.rows {
		if s.rows[i].ID == id {
			s.rows[i].Text, s.rows[i].Title = text, text
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
	return origin.session, origin.title, origin.at, nil
}

func memoryPlaceApp(t *testing.T, rows []store.Memory) (*app, *panelMemoryStore) {
	t.Helper()
	agent := &rememberingAgent{project: store.OwnerProject("here")}
	for _, row := range rows {
		agent.kept = append(agent.kept, session.MemoryLine{ID: row.ID, Title: row.Title, Text: row.Text})
	}
	memory := &panelMemoryStore{rows: rows, origins: map[string]memoryOrigin{}}
	a := newApp(t.Context(), Options{Agent: agent, Workspace: "/tmp/lab", Memory: memory, ProfileDir: t.TempDir()})
	return a, memory
}

// openMemoryPlace walks into the place and lands the first snapshot.
func openMemoryPlace(t *testing.T, a *app) {
	t.Helper()
	a.width, a.height = 120, 40
	a.slash("/memory")
	if !a.at(pageMemory) {
		t.Fatal("/memory did not open the memory place")
	}
}

// memoryCursorTo walks the cursor to the first row whose drawn text contains want.
func memoryCursorTo(t *testing.T, a *app, want string) {
	t.Helper()
	rows := a.mem.reading.rows(200, newPalette(tokens.NoColor, false))
	for i, row := range rows {
		if _, stop := a.mem.reading.at(i); stop && strings.Contains(row, want) {
			a.mem.cursor = i
			return
		}
	}
	t.Fatalf("no row says %q:\n%s", want, strings.Join(rows, "\n"))
}

func memoryPlaceRowsText(a *app) string {
	return strings.Join(a.mem.reading.rows(200, newPalette(tokens.NoColor, false)), "\n")
}

// ── the commands ────────────────────────────────────────────────────────────

func TestTheMemoryCommandsAreOnTheList(t *testing.T) {
	want := map[string]bool{"memory": false, "memories": false, "remember": false, "always": false}
	for _, command := range commands {
		if _, ok := want[command.name]; ok {
			want[command.name] = true
		}
		// THERE IS NO /forget: a line is forgotten on the Memories tab.
		if command.name == "forget" {
			t.Errorf("/forget is on the command list; forgetting is the Memories tab's")
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("/%s is not on the command list", name)
		}
	}
}

func TestRememberPrintsTheConversationsOwnReceipt(t *testing.T) {
	agent := &rememberingAgent{}
	a := newTestApp(agent)
	answerMemory(a, a.runRemember("prefers tabs"))
	if got := plain(lastNote(t, a)); got != "remembered · prefers tabs · for this project" {
		t.Fatalf("receipt = %q", got)
	}
}

func TestMemoriesPrintsWhereALineHoldsAndWhoKeptIt(t *testing.T) {
	lines := []session.MemoryLine{{Text: "prefers tabs", Where: "for you everywhere", Origin: store.OriginNoticed}}
	if got := memoriesText("", lines); got != "· prefers tabs  · for you everywhere · noticed" {
		t.Fatalf("listing = %q", got)
	}
	if got := memoriesText("", nil); !strings.Contains(got, "/remember <text>") {
		t.Fatalf("the empty listing does not say how to keep a line: %q", got)
	}
	if got := memoriesText("zebra", nil); got != "nothing remembered matches zebra" {
		t.Fatalf("an empty match = %q", got)
	}
}

func TestWithoutAStoreEveryCommandSaysWhy(t *testing.T) {
	a := newTestApp(&rememberingAgent{off: true})
	for _, run := range []func(){
		func() { a.runRemember("x") },
		func() { a.runMemories("x") },
	} {
		run()
		if got := plain(lastNote(t, a)); got != memoryUnavailableNote {
			t.Fatalf("with no store a command said %q", got)
		}
	}
	if strings.Contains(memoryUnavailableNote, "/settings") {
		t.Fatal("the unavailable note sends the person to a setting that does not turn memory on")
	}
}

func TestRememberTypedOnHomeIsKeptEverywhereAndAnsweredOnHome(t *testing.T) {
	agent := &rememberingAgent{}
	a := newTestApp(agent)
	a.width, a.height = 120, 40
	a.page = pageHome
	answerMemory(a, a.runRemember("no emoji anywhere"))
	if len(agent.everywhere) != 1 || !agent.everywhere[0] {
		t.Fatalf("typed on home it was not kept everywhere: %v", agent.everywhere)
	}
	if got := plain(a.home.msg); !strings.Contains(got, "remembered · no emoji anywhere · for you everywhere") {
		t.Fatalf("home's message line = %q, want the receipt where it was typed", got)
	}
}

// answerMemory runs one memory command off the loop, as Bubble Tea would, and
// routes the reply it answered back into the surface.
func answerMemory(a *app, cmd tea.Cmd) {
	for _, msg := range runCmd(cmd) {
		if reply, ok := msg.(memoryReplyMsg); ok {
			a.adoptMemoryReply(reply)
		}
	}
}

// ── the place ───────────────────────────────────────────────────────────────

func TestTheEmptyPlaceIsItsWhisperAndTheWhisperNamesTheDoor(t *testing.T) {
	a, _ := memoryPlaceApp(t, nil)
	openMemoryPlace(t, a)
	if foot := (placeMemory{}).hint(a); foot != memoryBareHint {
		t.Fatalf("the empty page's foot = %q", foot)
	}
	shown := plain(frame(a))
	if !strings.Contains(shown, placeWhisper[pageMemory].whisper) || !strings.Contains(placeWhisper[pageMemory].whisper, "/remember") {
		t.Fatalf("the empty page lacks a whisper that names /remember:\n%s", shown)
	}
	if strings.Contains(shown, "0 kept") {
		t.Fatal("the empty page drew a zero count")
	}
}

func TestTheShelvesSayWhereEachLineHoldsAndWhoKeptIt(t *testing.T) {
	a, _ := memoryPlaceApp(t, []store.Memory{
		{ID: "u", Owner: store.OwnerUser, Text: "prefers tabs", Origin: store.OriginPerson},
		{ID: "m", Owner: store.OwnerMachine, Text: "rg is ripgrep here", Origin: store.OriginModel},
		{ID: "p", Owner: store.OwnerProject("here"), Text: "deploys on Tuesdays", Origin: store.OriginNoticed, ProjectName: "codeaf"},
		{ID: "o", Owner: store.OwnerProject("other"), Text: "their rule", ProjectName: "infra"},
		{ID: "q", Owner: store.OwnerLegacyProject, Text: "came in blind"},
		{ID: "g", Owner: store.OwnerUser, Text: "ships on Fridays", Status: store.MemoryForgotten},
	})
	openMemoryPlace(t, a)
	shown := memoryPlaceRowsText(a)
	for _, want := range []string{
		memoryShelfYou + " · 1", memoryShelfMachine + " · 1", memoryShelfHere + " · codeaf · 1",
		"infra · 1", memoryShelfLost + " · 1", memoryShelfLetGo + " · 1",
	} {
		if !strings.Contains(shown, want) {
			t.Fatalf("the place does not say %q:\n%s", want, shown)
		}
	}
	// EVERY LINE HANGS UNDER ITS GROUP on the tree's connector and says who
	// kept it in its own column; a group's row has no connector, so the two
	// cannot be mistaken for each other.
	for text, who := range map[string]string{"prefers tabs": "you", "rg is ripgrep here": "the model", "deploys on Tuesdays": "noticed"} {
		row := memoryRowSaying(shown, text)
		if !strings.Contains(row, "─ "+text) || !strings.Contains(row, who) {
			t.Fatalf("the line %q does not hang from a connector beside %q:\n%s", text, who, shown)
		}
	}
	if row := memoryRowSaying(shown, memoryShelfYou); strings.Contains(row, "─") {
		t.Fatalf("a group's row wears a connector like a line: %q", row)
	}
	// THE SHELVES A CONVERSATION HERE IS NOT TOLD ARE ROLLED UP.
	for _, hidden := range []string{"their rule", "came in blind", "ships on Fridays"} {
		if strings.Contains(shown, hidden) {
			t.Fatalf("a shelf a conversation here is not told opened by itself: %q\n%s", hidden, shown)
		}
	}
	for _, gone := range []string{"helped", "bore on", "always"} {
		if strings.Contains(shown, gone) {
			t.Fatalf("the place still says %q", gone)
		}
	}
}

// memoryRowSaying is the one drawn row that carries some words.
func memoryRowSaying(shown, words string) string {
	for _, row := range strings.Split(shown, "\n") {
		if strings.Contains(row, words) {
			return row
		}
	}
	return ""
}

// A GROUP OPENS ON `→` AND FOLDS ON `←`, and `enter` does neither: the
// Activity tab's bargain with the same keys. The foot over a group names the
// arrow that would act, and only that one.
func TestAGroupOpensOnTheRightArrowAndFoldsOnTheLeft(t *testing.T) {
	a, _ := memoryPlaceApp(t, []store.Memory{
		{ID: "u", Owner: store.OwnerUser, Text: "prefers tabs"},
		{ID: "g", Owner: store.OwnerUser, Text: "ships on Fridays", Status: store.MemoryForgotten},
	})
	openMemoryPlace(t, a)
	memoryCursorTo(t, a, memoryShelfYou)
	if foot := (placeMemory{}).hint(a); foot != memoryShelfShutHint {
		t.Fatalf("the foot over an open group = %q", foot)
	}
	drive(t, a, key("enter"))
	if !strings.Contains(memoryPlaceRowsText(a), "prefers tabs") {
		t.Fatal("enter folded a group; groups fold on ←")
	}
	drive(t, a, key("left"))
	if strings.Contains(memoryPlaceRowsText(a), "prefers tabs") {
		t.Fatalf("← did not fold the group:\n%s", memoryPlaceRowsText(a))
	}
	if foot := (placeMemory{}).hint(a); foot != memoryShelfOpenHint {
		t.Fatalf("the foot over a folded group = %q", foot)
	}
	drive(t, a, key("right"))
	if !strings.Contains(memoryPlaceRowsText(a), "prefers tabs") {
		t.Fatal("→ did not open the group again")
	}
	// AND THE CURSOR STAYED ON THE GROUP through both, rather than sliding to
	// whatever row took its place.
	if stop, ok := a.mem.reading.at(a.mem.cursor); !ok || stop.shelf != store.OwnerUser || stop.line != nil {
		t.Fatalf("the cursor left the group it folded: %+v", stop)
	}
}

// WHAT IS BEING WRITTEN IS ON THE SCREEN, in the row it belongs to, with the
// caret after it. The add row's editor used to take the keys and draw nothing.
func TestTheWordsBeingWrittenAreDrawnInTheirRow(t *testing.T) {
	a, _ := memoryPlaceApp(t, []store.Memory{{ID: "u", Owner: store.OwnerUser, Text: "prefers tabs"}})
	openMemoryPlace(t, a)
	memoryCursorTo(t, a, memoryAddWord)
	drive(t, a, key("enter"))
	typeInto(t, a, "uses pnpm")
	if !strings.Contains(plain(frame(a)), "+ uses pnpm") {
		t.Fatalf("the new line is not drawn while it is written:\n%s", plain(frame(a)))
	}
	drive(t, a, key("esc"))
	memoryCursorTo(t, a, "prefers tabs")
	for _, v := range (placeMemory{}).verbs(a) {
		if v.key == 'e' {
			runCmd(v.do())
		}
	}
	typeInto(t, a, " always")
	if !strings.Contains(plain(frame(a)), "prefers tabs always") {
		t.Fatalf("a line being reworded is not drawn in its row:\n%s", plain(frame(a)))
	}
}

// A WIDE FRAME READS THE CURSOR ROW OUT BESIDE THE LIST, at the width the
// Activity tab splits at, and a narrow one has no pane at all.
func TestAWideFrameReadsTheLineOutBesideTheList(t *testing.T) {
	a, memory := memoryPlaceApp(t, []store.Memory{{ID: "u", Owner: store.OwnerUser, Text: "prefers tabs", Origin: store.OriginNoticed}})
	memory.origins["u"] = memoryOrigin{session: "s1", title: "Editor setup", at: time.Now().Add(-2 * time.Hour)}
	openMemoryPlace(t, a)
	memoryCursorTo(t, a, "prefers tabs")
	// The read the walk-in asked for went with the command this harness did
	// not run; the cursor's own follow asks again.
	a.mem.asking = nil
	for _, msg := range runCmd(a.memoryFollow()) {
		if origin, ok := msg.(memoryOriginMsg); ok {
			a.adoptMemoryOrigin(origin)
		}
	}
	a.width, a.height = taskPaneFloor+20, 30
	shown := plain(frame(a))
	for _, want := range []string{"for you everywhere · kept by noticed", "in 'Editor setup'", "→ edit, forget, move"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("the pane does not say %q:\n%s", want, shown)
		}
	}
	a.width = taskPaneFloor - 10
	if strings.Contains(plain(frame(a)), "in 'Editor setup'") {
		t.Fatal("a frame under the split width drew a pane")
	}
}

func TestTheFootNamesOnlyKeysThatWork(t *testing.T) {
	a, _ := memoryPlaceApp(t, []store.Memory{{ID: "u", Owner: store.OwnerUser, Text: "prefers tabs"}})
	openMemoryPlace(t, a)
	memoryCursorTo(t, a, "prefers tabs")
	foot := (placeMemory{}).hint(a)
	if foot != memoryLineHint || strings.Contains(foot, "e fix") || strings.Contains(foot, "f forget") {
		t.Fatalf("the foot over a line = %q", foot)
	}
	// AND A LETTER TYPED IS A LETTER: it narrows the page, it does not act.
	typeInto(t, a, "e")
	if a.mem.edit != nil || a.mem.filter.String() != "e" {
		t.Fatalf("typing `e` acted instead of filtering: edit=%v filter=%q", a.mem.edit != nil, a.mem.filter.String())
	}
}

func TestEnterOpensTheCardAndTheCardsFootIsItsOwn(t *testing.T) {
	a, memory := memoryPlaceApp(t, []store.Memory{{ID: "u", Owner: store.OwnerUser, Text: "prefers tabs", Origin: store.OriginNoticed}})
	memory.origins["u"] = memoryOrigin{session: "s1", title: "Editor setup", at: time.Now().Add(-2 * time.Hour)}
	openMemoryPlace(t, a)
	memoryCursorTo(t, a, "prefers tabs")
	drive(t, a, key("enter"))
	if a.mem.expanded != "u" {
		t.Fatal("enter on a line did not open its card")
	}
	if foot := (placeMemory{}).hint(a); foot != memoryCardHint {
		t.Fatalf("the card's foot = %q", foot)
	}
	shown := plain(frame(a))
	for _, want := range []string{"prefers tabs", "for you everywhere", "kept by noticed", "in 'Editor setup'"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("the card does not say %q:\n%s", want, shown)
		}
	}
	drive(t, a, key("esc"))
	if a.mem.expanded != "" || !a.at(pageMemory) {
		t.Fatal("esc did not take the card back to the list")
	}
}

func TestTheVerbsAreTheLinesAndALetGoLineCanBePutBack(t *testing.T) {
	a, memory := memoryPlaceApp(t, []store.Memory{
		{ID: "u", Owner: store.OwnerUser, Text: "prefers tabs"},
		{ID: "g", Owner: store.OwnerUser, Text: "ships on Fridays", Status: store.MemoryForgotten},
	})
	openMemoryPlace(t, a)
	memoryCursorTo(t, a, "prefers tabs")
	var keys []rune
	for _, v := range (placeMemory{}).verbs(a) {
		keys = append(keys, v.key)
	}
	if string(keys) != "efpma" {
		t.Fatalf("a line's verbs = %q, want edit, forget, the two moves it is not in, and ask", string(keys))
	}
	memoryCursorTo(t, a, memoryShelfLetGo)
	drive(t, a, key("right"))
	memoryCursorTo(t, a, "ships on Fridays")
	verbs := (placeMemory{}).verbs(a)
	if len(verbs) != 1 || verbs[0].key != 'u' {
		t.Fatalf("a let-go line's verbs = %+v, want only put it back", verbs)
	}
	runCmd(verbs[0].do())
	drainPlaceWrites(t, a)
	if len(memory.restored) != 1 || memory.restored[0] != "g" {
		t.Fatalf("put back restored %v", memory.restored)
	}
}

func TestForgettingFromThePlaceLeavesTheLoopAndSaysWhereItWent(t *testing.T) {
	a, memory := memoryPlaceApp(t, []store.Memory{{ID: "u", Owner: store.OwnerUser, Text: "prefers tabs"}})
	openMemoryPlace(t, a)
	memoryCursorTo(t, a, "prefers tabs")
	cmd := a.memoryKey(key("delete"))
	if len(memory.forgotten) != 0 {
		t.Fatal("the forget ran on the update loop instead of beside it")
	}
	for _, msg := range runCmd(cmd) {
		if done, ok := msg.(memoryPlaceDoneMsg); ok {
			a.adoptMemoryPlaceDone(done)
		}
	}
	if len(memory.forgotten) != 1 || !strings.Contains(a.mem.footer, "under let go") {
		t.Fatalf("forgot %v with footer %q", memory.forgotten, a.mem.footer)
	}
}

func TestTheAddRowKeepsALineThroughTheConversation(t *testing.T) {
	a, _ := memoryPlaceApp(t, []store.Memory{{ID: "u", Owner: store.OwnerUser, Text: "prefers tabs"}})
	agent := a.agent.(*rememberingAgent)
	openMemoryPlace(t, a)
	memoryCursorTo(t, a, memoryAddWord)
	drive(t, a, key("enter"))
	if a.mem.edit == nil || a.mem.editID != memoryAddID {
		t.Fatal("enter on the add row did not open an empty editor")
	}
	typeInto(t, a, "uses pnpm")
	cmd := a.memoryKey(key("enter"))
	for _, msg := range runCmd(cmd) {
		if done, ok := msg.(memoryPlaceDoneMsg); ok {
			a.adoptMemoryPlaceDone(done)
		}
	}
	if len(agent.remembered) != 1 || agent.remembered[0] != "uses pnpm" {
		t.Fatalf("the add row kept %v", agent.remembered)
	}
	if a.mem.footer != "remembered · uses pnpm · for this project" {
		t.Fatalf("the add row's receipt = %q", a.mem.footer)
	}
}

func TestTheHeadRowSaysWhenLinesDoNotFit(t *testing.T) {
	var rows []store.Memory
	for i := 0; i < 40; i++ {
		rows = append(rows, store.Memory{ID: "m" + itoa(i), Owner: store.OwnerUser, Text: strings.Repeat("long words ", 20) + itoa(i)})
	}
	a, _ := memoryPlaceApp(t, rows)
	openMemoryPlace(t, a)
	if shown := memoryPlaceRowsText(a); !strings.Contains(shown, "don't fit in front of the model") {
		t.Fatalf("an overflowing store does not say lines are left out:\n%.400s", shown)
	}
}

// drainPlaceWrites lands every place write a test's command answered. The
// place's writes come back as memoryPlaceDoneMsg; runCmd already ran them.
func drainPlaceWrites(t *testing.T, a *app) {
	t.Helper()
	a.refreshMemory()
}

// settleMemorySlash runs one slash command through the update loop, with the
// reply its memory call answers routed back as Bubble Tea would.
func settleMemorySlash(t *testing.T, a *app, line string) {
	t.Helper()
	if cmd := a.slash(line); cmd != nil {
		drive(t, a, cmd())
	}
}
