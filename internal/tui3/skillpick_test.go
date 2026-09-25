package tui3

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/skills"
	store "github.com/Agent-Field/codeaf/internal/store"
)

// The /skill picker, the toggles it answers, and the tray chip that carries
// the attachment.
//
// Everything here drives the real surface against real skill folders in
// temporary directories, on harnesspick_test.go's terms: the list's whole job
// is to say what is on disk and what the session holds, and a test that
// stubbed either would be a test of the stub.

// skillAgent is a scripted session that can also carry attached skills — the
// optional half of the seam ([skillAttacher]), mirroring the four doors on
// internal/session's skillattach.go.
type skillAgent struct {
	*fakeAgent
	held []string
	// shelf is the session's own shelf, read through the agent the way the
	// live session answers it (internal/session's Agent.SkillFacts). Nil is a
	// conversation with no shelf store at all.
	shelf *skillMemory
}

func (s *skillAgent) SkillFacts(status string, limit int) ([]store.Fact, error) {
	if s.shelf == nil {
		return nil, errors.New("this conversation has no skill shelf")
	}
	return s.shelf.SkillFacts(status, limit)
}

func (s *skillAgent) AttachSkills(names ...string) []string {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || attachedHas(s.held, name) {
			continue
		}
		s.held = append(s.held, name)
	}
	return append([]string(nil), s.held...)
}

func (s *skillAgent) DetachSkill(name string) bool {
	for i, held := range s.held {
		if strings.EqualFold(held, name) {
			s.held = append(s.held[:i], s.held[i+1:]...)
			return true
		}
	}
	return false
}

func (s *skillAgent) AttachedSkills() []string { return append([]string(nil), s.held...) }

func (s *skillAgent) ClearAttachedSkills() int {
	n := len(s.held)
	s.held = nil
	return n
}

// skillMemory is the shelf half: the memory place's own seam plus the store's
// skill reading, so a shelf fact reaches the picker the way the real store's
// does.
type skillMemory struct {
	facts []store.Fact
}

func (m *skillMemory) SkillFacts(status string, limit int) ([]store.Fact, error) {
	out := make([]store.Fact, 0, len(m.facts))
	for _, fact := range m.facts {
		if status == "" || fact.Status == status {
			out = append(out, fact)
		}
	}
	return out, nil
}

func (m *skillMemory) Snapshot(int) (store.MemoryShelves, error) { return store.MemoryShelves{}, nil }
func (m *skillMemory) ChangedSince(time.Time) (int, int, error)  { return 0, 0, nil }
func (m *skillMemory) ListMemories(string, int) ([]store.Memory, error) {
	return nil, nil
}
func (m *skillMemory) UpdateMemory(string, string, string, []string) error { return nil }
func (m *skillMemory) ForgetMemory(string) error                           { return nil }
func (m *skillMemory) RestoreMemory(string) error                          { return nil }
func (m *skillMemory) MemoryProvenance(string) (string, string, time.Time, error) {
	return "", "", time.Time{}, nil
}

// seedSkill writes one skill folder under a root directory the way the
// foreign harnesses keep them.
func seedSkill(t *testing.T, root, name, desc string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// skillApp is a surface with a project and a home to discover in and an agent
// that can carry attachments.
func skillApp(t *testing.T) (*app, *skillAgent, string, string) {
	t.Helper()
	project := t.TempDir()
	homeDir := t.TempDir()
	// Both doors to the home point at one directory: HOME for the surface's
	// own `~`, and CODEAF_HOME for the login home discovery reads, which is
	// the one the launch's import pass reads too (internal/home's Login).
	t.Setenv("HOME", homeDir)
	t.Setenv(home.EnvVar, homeDir)
	agent := &skillAgent{fakeAgent: &fakeAgent{}, shelf: &skillMemory{}}
	a := newTestApp(agent)
	a.workspace = project
	a.width = 100
	return a, agent, project, homeDir
}

// ── the list ────────────────────────────────────────────────────────────────

// THE SPACE IS THE DOOR, and what opens is the whole shelf: the attached ones
// at the top, then project scope before user scope.
func TestTheSkillPickerOpensOnTheWholeShelf(t *testing.T) {
	a, agent, project, home := skillApp(t)
	agent.AttachSkills("kept-skill")
	seedSkill(t, filepath.Join(project, ".claude", "skills"), "alpha-flake", "chase a flaky test")
	seedSkill(t, filepath.Join(home, ".codeaf", "skills"), "beta-diff", "read a diff")

	typeInto(t, a, "/skill ")
	if !a.skillPick.open {
		t.Fatal("/skill with a space after it opened nothing")
	}
	if a.menu.open {
		t.Fatal("the command list stayed up under the picker")
	}
	if got := len(a.skillPick.rows); got != 3 {
		t.Fatalf("the list holds %d rows, want the attached one and the two discovered", got)
	}
	if got := a.skillPick.rows[0].name; got != "kept-skill" {
		t.Fatalf("the list opens on %q rather than on the attached skill", got)
	}
	if a.skillPick.rows[1].from != skillFromProject || a.skillPick.rows[2].from != skillFromUser {
		t.Fatalf("project scope did not come before user scope: %+v", a.skillPick.rows)
	}
	screen := strings.Join(plainOverlay(a), "\n")
	for _, want := range []string{"kept-skill", "alpha-flake", "chase a flaky test", "beta-diff", "read a diff"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the list does not say %q:\n%s", want, screen)
		}
	}
}

func TestTheSkillPickerShowsShelfWhileDiskScanIsHeld(t *testing.T) {
	a, agent, project, _ := skillApp(t)
	agent.AttachSkills("held-skill")
	seedSkill(t, filepath.Join(project, ".agents", "skills"), "disk-skill", "from disk")
	started, release := make(chan struct{}), make(chan struct{})
	old := skillDiscover
	skillDiscover = func(opts skills.Options) ([]skills.Skill, error) {
		close(started)
		<-release
		return old(opts)
	}
	t.Cleanup(func() { skillDiscover = old })
	typeInto(t, a, "/skill")
	settled := make(chan struct{})
	go func() { drive(t, a, key(" ")); close(settled) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("disk scan did not start")
	}
	if !a.skillPick.open || len(a.skillPick.rows) != 1 || a.skillPick.rows[0].name != "held-skill" {
		t.Fatalf("picker did not open with its shelf row while disk was held: %+v", a.skillPick.rows)
	}
	close(release)
	select {
	case <-settled:
	case <-time.After(2 * time.Second):
		t.Fatal("picker did not fold the disk scan")
	}
	if len(a.skillPick.rows) != 2 || a.skillPick.rows[1].name != "disk-skill" {
		t.Fatalf("disk row did not arrive after release: %+v", a.skillPick.rows)
	}
}

func TestSkillDiskScanDoesNotDelayALaterOrderedToggle(t *testing.T) {
	a, agent, _, _ := skillApp(t)
	a.skillPick.start([]skillPickRow{{name: "shelf-skill"}}, "")
	started, release := make(chan struct{}), make(chan struct{})
	old := skillDiscover
	skillDiscover = func(opts skills.Options) ([]skills.Skill, error) {
		close(started)
		<-release
		return nil, nil
	}
	t.Cleanup(func() { skillDiscover = old })
	scan := a.readSkillDisk()
	scanDone := make(chan tea.Msg, 1)
	go func() { scanDone <- scan() }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("disk scan did not begin")
	}
	toggle := a.skillToggled()
	toggled := make(chan tea.Msg, 1)
	go func() { toggled <- toggle() }()
	select {
	case msg := <-toggled:
		a.doorSaid(msg.(doorMsg))
		if len(agent.held) != 1 || agent.held[0] != "shelf-skill" {
			t.Fatalf("ordered toggle attached %v", agent.held)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("disk scan held the later ordered attachment")
	}
	close(release)
	a.doorSaid((<-scanDone).(doorMsg))
}

// THE FOLDER ROW RUNS NO DISK SCAN. Its attach is a gesture and keeps its place
// in the ordered line, so the name is read on the loop — which is only safe
// while that read is the folder's one SKILL.md, never a walk of every skill
// root.
func TestTheFolderRowAttachesWithoutADiskScan(t *testing.T) {
	a, agent, _, homeDir := skillApp(t)
	folder := seedSkill(t, homeDir, "loose-skill", "from a folder")
	old := skillDiscover
	skillDiscover = func(skills.Options) ([]skills.Skill, error) {
		t.Error("enter on the folder row ran a disk scan")
		return nil, nil
	}
	t.Cleanup(func() { skillDiscover = old })
	a.skillPick.start([]skillPickRow{{name: "shelf-skill"}}, folder)
	a.skillPick.cursor = len(a.skillPick.hits)
	spend(t, a, a.skillToggled())
	if !attachedHas(agent.held, "loose-skill") {
		t.Fatalf("the folder row attached %v, want loose-skill", agent.held)
	}
}

// THE SHELF FACTS RIDE THE SAME LIST, deduplicated by name against what
// discovery found in place.
func TestTheSkillPickerMergesTheShelfWithDiscovery(t *testing.T) {
	a, agent, project, _ := skillApp(t)
	seedSkill(t, filepath.Join(project, ".claude", "skills"), "alpha-flake", "chase a flaky test")
	agent.shelf = &skillMemory{facts: []store.Fact{
		{Kind: store.FactSkill, Status: store.FactActive, Artifact: filepath.Join(project, "shelf", "alpha-flake"), Body: "the shelf's own line"},
		{Kind: store.FactSkill, Status: store.FactActive, Artifact: filepath.Join(project, "shelf", "nightly-notes"), Body: "write the notes"},
	}}

	typeInto(t, a, "/skill ")
	if got := len(a.skillPick.rows); got != 2 {
		t.Fatalf("the list holds %d rows, want the two names and not the duplicate", got)
	}
	screen := strings.Join(plainOverlay(a), "\n")
	for _, want := range []string{"alpha-flake", "the shelf's own line", "nightly-notes", "write the notes", skillFromShelf} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the list does not say %q:\n%s", want, screen)
		}
	}
}

// A SKILL THAT CARRIES A WARNING SHOWS IT dim rather than being hidden.
func TestTheSkillPickerShowsAWarningOnItsRow(t *testing.T) {
	a, _, project, _ := skillApp(t)
	dir := filepath.Join(project, ".claude", "skills", "odd-one")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: other-name\ndescription: one line\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	typeInto(t, a, "/skill ")
	screen := strings.Join(plainOverlay(a), "\n")
	if !strings.Contains(screen, "name does not match folder") {
		t.Fatalf("the row hid what is wrong with the skill:\n%s", screen)
	}
}

// THE QUERY NARROWS THE LIST on the shared fuzzy scoring.
func TestTheSkillPickerFiltersByQuery(t *testing.T) {
	a, _, project, home := skillApp(t)
	seedSkill(t, filepath.Join(project, ".claude", "skills"), "alpha-flake", "chase a flaky test")
	seedSkill(t, filepath.Join(home, ".codeaf", "skills"), "beta-diff", "read a diff")

	typeInto(t, a, "/skill flake")
	if len(a.skillPick.hits) != 1 {
		t.Fatalf("the query kept %d rows, not the one that carries the word", len(a.skillPick.hits))
	}
	if got := a.skillPick.rows[a.skillPick.hits[0]].name; got != "alpha-flake" {
		t.Fatalf("the query surfaced %q", got)
	}
}

// ENTER TOGGLES A ROW AND LEAVES THE PICKER OPEN, which is the one difference
// from the harness picker: the choice is a set.
func TestEnterOnASkillRowTogglesItAndLeavesThePickerOpen(t *testing.T) {
	a, agent, project, _ := skillApp(t)
	seedSkill(t, filepath.Join(project, ".claude", "skills"), "alpha-flake", "chase a flaky test")
	seedSkill(t, filepath.Join(project, ".claude", "skills"), "beta-diff", "read a diff")

	typeInto(t, a, "/skill ")
	drive(t, a, key("enter"))
	if !a.skillPick.open {
		t.Fatal("enter closed the picker")
	}
	if len(agent.held) != 1 || agent.held[0] != "alpha-flake" {
		t.Fatalf("enter attached %v", agent.held)
	}
	if !a.skillPick.rows[0].on {
		t.Fatal("the row does not carry its on mark")
	}
	screen := strings.Join(plainOverlay(a), "\n")
	if !strings.Contains(screen, "alpha-flake") {
		t.Fatalf("the row left the list:\n%s", screen)
	}
	// A SECOND ENTER ON THE SAME ROW TAKES IT BACK OFF, and the cursor is
	// still where it was.
	drive(t, a, key("enter"))
	if len(agent.held) != 0 {
		t.Fatalf("the second enter left %v attached", agent.held)
	}
	// AND A SECOND SKILL IS A SECOND KEYSTROKE, not a second command.
	drive(t, a, key("down"))
	drive(t, a, key("enter"))
	if len(agent.held) != 1 || agent.held[0] != "beta-diff" {
		t.Fatalf("the second enter attached %v", agent.held)
	}
}

// ESC CLOSES THE LIST AND THE ATTACHMENT STAYS, which is what the chip is
// for: the session holds the names, not the draft.
func TestEscClosesTheSkillPickerAndKeepsTheAttachment(t *testing.T) {
	a, agent, project, _ := skillApp(t)
	seedSkill(t, filepath.Join(project, ".claude", "skills"), "alpha-flake", "chase a flaky test")

	typeInto(t, a, "/skill ")
	drive(t, a, key("enter"))
	drive(t, a, key("esc"))
	if a.skillPick.open {
		t.Fatal("esc left the picker open")
	}
	if got := a.input.String(); got != "/skill " {
		t.Fatalf("esc rewrote the draft to %q", got)
	}
	if len(agent.held) != 1 {
		t.Fatalf("esc took the attachment off: %v", agent.held)
	}
	if chip := strings.Join(a.skillTrayCells(), " "); !strings.Contains(chip, "alpha-flake") {
		t.Fatalf("the tray lost the chip: %q", chip)
	}
}

// ── the folder row ──────────────────────────────────────────────────────────

// A QUERY THAT LOOKS LIKE A PATH OFFERS ONE EXTRA ROW, and enter on it
// attaches the skill in that folder by the name its own SKILL.md carries.
// Nothing is copied anywhere.
func TestAPathQueryOffersTheSkillInTheFolder(t *testing.T) {
	a, agent, project, home := skillApp(t)
	seedSkill(t, filepath.Join(project, ".claude", "skills"), "alpha-flake", "chase a flaky test")
	folder := seedSkill(t, home, "loose-skill", "lives anywhere")

	typeInto(t, a, "/skill "+folder)
	if a.skillPick.folder == "" {
		t.Fatal("a path query offered no folder row")
	}
	if got := len(a.skillPick.hits); got != 0 {
		t.Fatalf("the path matched %d shelf rows, want none", got)
	}
	screen := strings.Join(plainOverlay(a), "\n")
	if !strings.Contains(screen, skillFolderWord) {
		t.Fatalf("the folder row is not on the list:\n%s", screen)
	}
	if got := a.skillPick.note(len(a.skillPick.hits)); got != folder {
		t.Fatalf("the folder row names %q, want the folder", got)
		t.Fatalf("the folder row is not on the list:\n%s", screen)
	}
	drive(t, a, key("enter"))
	if len(agent.held) != 1 || agent.held[0] != "loose-skill" {
		t.Fatalf("enter on the folder row attached %v", agent.held)
	}
	// The folder was read where it lives and copied nowhere.
	if _, err := os.Stat(filepath.Join(folder, "SKILL.md")); err != nil {
		t.Fatalf("the folder was disturbed: %v", err)
	}
}

// A FOLDER WITH NO SKILL.MD IS REFUSED IN ONE PLAIN LINE.
func TestAFolderWithNoSkillMDIsRefusedInOneLine(t *testing.T) {
	a, agent, _, home := skillApp(t)
	empty := filepath.Join(home, "not-a-skill")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}

	typeInto(t, a, "/skill "+empty)
	drive(t, a, key("enter"))
	if len(agent.held) != 0 {
		t.Fatalf("a folder with no SKILL.md attached %v", agent.held)
	}
	said := strings.Join(plainRows(a), "\n")
	if !strings.Contains(said, "no SKILL.md in") {
		t.Fatalf("the refusal does not say what was missing:\n%s", said)
	}
}

// ── the chip ────────────────────────────────────────────────────────────────

// THE CHIP CARRIES THE NAME FOR ONE SKILL AND A COUNT FOR MORE, and one
// gesture — the ✕ — takes every one off.
func TestTheSkillChipCountsAndClearsInOneGesture(t *testing.T) {
	a, agent, project, _ := skillApp(t)
	seedSkill(t, filepath.Join(project, ".claude", "skills"), "alpha-flake", "chase a flaky test")
	seedSkill(t, filepath.Join(project, ".claude", "skills"), "beta-diff", "read a diff")

	typeInto(t, a, "/skill ")
	drive(t, a, key("enter"))
	if chip := strings.Join(a.skillTrayCells(), " "); !strings.Contains(chip, "alpha-flake") {
		t.Fatalf("one attached skill did not name itself on the chip: %q", chip)
	}
	drive(t, a, key("down"))
	drive(t, a, key("enter"))
	if chip := strings.Join(a.skillTrayCells(), " "); !strings.Contains(chip, "2 skills") {
		t.Fatalf("two attached skills did not count themselves: %q", chip)
	}
	drop := a.dropSkillChip()
	if drop == nil {
		t.Fatal("the ✕ changed nothing")
	}
	drain(t, a, drop)
	if len(agent.held) != 0 {
		t.Fatalf("the ✕ left %v attached", agent.held)
	}
	if cells := a.skillTrayCells(); len(cells) != 0 {
		t.Fatalf("the chip survived its own ✕: %v", cells)
	}
}

// A CLICK ON THE CHIP IS RESOLVED TO THE SKILL CELL and clears every
// attachment, on the tray's one-function bargain.
func TestTheTrayAnswersTheSkillChipForAPress(t *testing.T) {
	a, agent, project, _ := skillApp(t)
	seedSkill(t, filepath.Join(project, ".claude", "skills"), "alpha-flake", "chase a flaky test")
	agent.AttachSkills("triage-flake")

	cells := a.skillTrayCells()
	if len(cells) == 0 {
		t.Fatal("an attached skill drew no chip")
	}
	// The field test's own shape: nothing else on the tray, the row the frame
	// marked as the input block's first.
	width, height := a.size()
	rows, marks, _, _ := a.chrome(width)
	row := -1
	for i, mark := range marks {
		if mark.kind == chromeDraft && mark.index == 0 {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatal("the frame marked no tray row")
	}
	at, ok := a.chipTrayTarget(len(inputPad), height-len(rows)+row)
	if !ok || at != traySkillChip {
		t.Fatalf("a press on the chip answered %d, want %d", at, traySkillChip)
	}
	// The press hands back the clearing door, asked off the update loop; the
	// program loop's own job is to run it and fold the answer in.
	cmd, took := a.chipPress(len(inputPad), height-len(rows)+row)
	if !took || cmd == nil {
		t.Fatalf("the press did not clear the chip")
	}
	drain(t, a, cmd)
	if len(agent.held) != 0 {
		t.Fatalf("the press left %v attached", agent.held)
	}
}

// ── the command ─────────────────────────────────────────────────────────────

// BARE /SKILL OPENS THE PICKER ON THE WHOLE SHELF, by writing the command and
// its space into the box the query is then typed into.
func TestBareSkillOpensThePickerOnTheWholeShelf(t *testing.T) {
	a, _, project, _ := skillApp(t)
	seedSkill(t, filepath.Join(project, ".claude", "skills"), "alpha-flake", "chase a flaky test")

	cmd := a.slash("/skill")
	if !a.skillPick.open {
		t.Fatal("bare /skill opened no picker")
	}
	if got := a.input.String(); got != "/skill " {
		t.Fatalf("bare /skill left %q in the box", got)
	}
	spend(t, a, cmd)
	screen := strings.Join(plainOverlay(a), "\n")
	if !strings.Contains(screen, "alpha-flake") {
		t.Fatalf("the picker did not open on the shelf:\n%s", screen)
	}
}

// THE COMMAND LIST CARRIES THE ROW, spelled the way the other query-bearing
// commands are.
func TestSkillIsOnTheCommandList(t *testing.T) {
	found, aliased := false, false
	for _, c := range commands {
		if c.name == "skill" && c.args == "" {
			found = true
		}
		if c.name == "skill" && containsString(c.alias, "skills") {
			aliased = true
		}
	}
	if !found || !aliased {
		t.Fatalf("/skill is not on the list with its alias: found=%v aliased=%v", found, aliased)
	}
}

func containsString(hay []string, needle string) bool {
	for _, straw := range hay {
		if straw == needle {
			return true
		}
	}
	return false
}

var _ = tea.Msg(nil)

// memoryOnly is the memory place's seam and nothing more, the way the live
// door wraps its store (cmd/codeaf's v3Brain): it answers the memory place and
// has no reading of the skill shelf at all.
type memoryOnly struct{}

func (memoryOnly) Snapshot(int) (store.MemoryShelves, error)        { return store.MemoryShelves{}, nil }
func (memoryOnly) ChangedSince(time.Time) (int, int, error)         { return 0, 0, nil }
func (memoryOnly) ListMemories(string, int) ([]store.Memory, error) { return nil, nil }
func (memoryOnly) UpdateMemory(string, string, string, []string) error {
	return nil
}
func (memoryOnly) ForgetMemory(string) error  { return nil }
func (memoryOnly) RestoreMemory(string) error { return nil }
func (memoryOnly) MemoryProvenance(string) (string, string, time.Time, error) {
	return "", "", time.Time{}, nil
}

// THE PICKER READS THE SHELF THE SESSION READS, whatever memory is doing. It
// used to look for the shelf through the memory seam, which the live door
// wraps with no reading of skills, so on every machine it dropped the shelf's
// own rows and told a person with memory on that memory was off. Asked of the
// session, the shelf is there with a memory-shaped store beside it and with
// no memory at all.
func TestTheSkillPickerReadsTheShelfTheSessionReads(t *testing.T) {
	for _, memory := range []memoryStore{memoryOnly{}, nil} {
		a, agent, project, _ := skillApp(t)
		seedSkill(t, filepath.Join(project, ".claude", "skills"), "alpha-flake", "chase a flaky test")
		a.memory = memory
		agent.shelf = &skillMemory{facts: []store.Fact{
			{Kind: store.FactSkill, Status: store.FactActive, Artifact: filepath.Join(project, "shelf", "nightly-notes"), Body: "write the notes"},
		}}

		typeInto(t, a, "/skill ")
		screen := strings.Join(plainOverlay(a), "\n")
		for _, want := range []string{"alpha-flake", "nightly-notes", "write the notes"} {
			if !strings.Contains(screen, want) {
				t.Fatalf("memory %T: the list does not say %q:\n%s", memory, want, screen)
			}
		}
		for _, stale := range []string{skillNoShelfWarning, "memory is off"} {
			if strings.Contains(screen, stale) {
				t.Fatalf("memory %T: a conversation with a shelf was told %q:\n%s", memory, stale, screen)
			}
		}
	}
}

// A LIST OF ROWS THAT DO NOTHING WHEN CHOSEN SAYS SO ON THE ROW. A session
// with no shelf store at all still lists the folders on disk, and every one of
// those rows says it cannot be attached, rather than being chosen for nothing.
func TestTheSkillPickerSaysWhyARowCannotBeAttachedWithNoShelf(t *testing.T) {
	a, agent, project, _ := skillApp(t)
	seedSkill(t, filepath.Join(project, ".claude", "skills"), "alpha-flake", "chase a flaky test")
	agent.shelf = nil

	typeInto(t, a, "/skill ")
	screen := strings.Join(plainOverlay(a), "\n")
	if !strings.Contains(screen, "alpha-flake") {
		t.Fatalf("the picker stopped listing the skills on disk:\n%s", screen)
	}
	// The row is clipped at the overlay's width, so the check is on the words
	// that carry the reason rather than on the whole sentence.
	if reason, _, _ := strings.Cut(skillNoShelfWarning, ","); !strings.Contains(screen, reason) {
		t.Fatalf("the row does not say why choosing it does nothing:\n%s", screen)
	}
}

// THE PICKER LISTS THE SAME HOME THE LAUNCH IMPORTS FROM. The import pass
// reads the login home through internal/home, which follows CODEAF_HOME; a
// picker that read the process's HOME instead listed one machine's skills
// while the shelf held another's.
func TestTheSkillPickerReadsTheLoginHomeTheImportReads(t *testing.T) {
	a, _, _, homeDir := skillApp(t)
	moved := t.TempDir()
	t.Setenv(home.EnvVar, moved)
	seedSkill(t, filepath.Join(moved, ".claude", "skills"), "moved-skill", "a skill under the moved home")
	seedSkill(t, filepath.Join(homeDir, ".claude", "skills"), "process-home-skill", "a skill under the process HOME")

	typeInto(t, a, "/skill ")
	screen := strings.Join(plainOverlay(a), "\n")
	if !strings.Contains(screen, "moved-skill") {
		t.Fatalf("the picker did not list the home the import reads:\n%s", screen)
	}
	if strings.Contains(screen, "process-home-skill") {
		t.Fatalf("the picker listed a home the import never reads:\n%s", screen)
	}
}
