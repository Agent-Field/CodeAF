package main

// The demo home exists to be looked at, which is exactly the kind of thing that
// rots without anybody noticing: a field renamed in internal/session, a schema
// number moved, a reader that stopped believing a shape — and the next person to
// run `make demo-home` sees the empty screens the owner was complaining about in
// the first place, with nothing on the surface saying why.
//
// So this seeds into a temporary directory and reads every place back through
// the SAME readers the surface uses, and insists that each of them has rows. It
// asserts counts and joins rather than words: what the pages say is their own
// tests' business, and a fixture test that pinned prose would fail every time
// somebody improved a sentence.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestTheDemoHomeFillsEveryPlace(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	built, err := seedDemoHome(dir, now)
	if err != nil {
		t.Fatalf("seed the demo home: %v", err)
	}

	// ── home: the projects, the conversations, and the two live rows ──────────
	world := session.ReadWorld(filepath.Join(dir, ".aforge", "v3", "projects"))
	if len(world.Projects) != built.Projects {
		t.Fatalf("home reads %d projects out of a home built with %d: %+v",
			len(world.Projects), built.Projects, world.Projects)
	}
	rows := world.Sessions()
	if len(rows) != built.Conversations {
		t.Fatalf("home reads %d conversations out of a home built with %d", len(rows), built.Conversations)
	}
	wanting, moving, archived, named := 0, 0, 0, 0
	for _, row := range rows {
		if row.NeedsPerson() {
			wanting++
		}
		if row.Tasks.Running > 0 {
			moving++
		}
		if row.Title != "" {
			named++
		}
		// A bucket that could not say its project's real path would draw the
		// encoded directory name as a project name — which is what a drift in
		// [encodeWorkspace] against the launch door's own encoder looks like from
		// the outside.
		if !strings.HasPrefix(row.Workspace, dir) {
			t.Fatalf("the conversation %q records a workspace outside the demo home: %q", row.Title, row.Workspace)
		}
	}
	if named != len(rows) {
		t.Fatalf("%d of %d conversations came back without a name", len(rows)-named, len(rows))
	}
	if wanting == 0 {
		t.Fatal("nothing on this home is waiting on the person, so the `want you` clause draws nothing")
	}
	if moving == 0 {
		t.Fatal("nothing on this home is running, so the `moving` clause draws nothing")
	}
	for _, project := range world.Projects {
		if project.Name == "" || project.Path == "" {
			t.Fatalf("a project came back with no name or no path: %+v", project)
		}
		for _, meta := range project.Sessions {
			if got, err := session.LoadMeta(meta.Dir); err == nil && got.Archived {
				archived++
			}
		}
	}
	if archived == 0 {
		t.Fatal("no conversation is archived, so home's folded archive line draws nothing")
	}

	// Every journal the surface will open has to be readable BY THE ENGINE'S OWN
	// READER, which is what keeps [writeTranscript]'s one hand-spelled shape
	// honest.
	for _, row := range rows {
		summary, spoken := session.Peek(row.Transcript)
		if !spoken {
			t.Fatalf("session.Peek could not read %s as a conversation", row.Transcript)
		}
		if summary.Title == "" || summary.Opening == "" || summary.Asked == 0 {
			t.Fatalf("session.Peek read %s as %+v — a row with nothing on it", row.Transcript, summary)
		}
	}

	// ── the tasks place: one running row, and landed work behind it ───────────
	index, running, landed := 0, 0, 0
	for _, project := range world.Projects {
		for _, entry := range session.ReadTaskIndex(filepath.Join(project.Dir, "tasks.jsonl")) {
			index++
			if entry.Live() {
				running++
			} else {
				landed++
			}
			if entry.SessionID == "" {
				t.Fatalf("the work %q names no conversation", entry.Title)
			}
			// A row with no slug draws a bare id in the spend page's `what it was
			// for` column and resolves no "@" mention.
			if entry.Name == "" || entry.Label == "" {
				t.Fatalf("the work %q came back without a slug or a row label: %+v", entry.Title, entry)
			}
			if len(entry.Label) > 56 {
				t.Fatalf("the work %q has a label longer than the engine's own cap: %q", entry.Title, entry.Label)
			}
		}
	}
	if index != built.Tasks {
		t.Fatalf("the task index holds %d rows out of %d written", index, built.Tasks)
	}
	if running == 0 || landed == 0 {
		t.Fatalf("the record holds %d running and %d landed rows; the page wants both sections", running, landed)
	}

	// ── standing: the four states the page draws ──────────────────────────────
	orders, err := standing.Open(filepath.Join(dir, ".aforge", "v3", "standing"))
	if err != nil {
		t.Fatalf("open the standing store: %v", err)
	}
	items, err := orders.List()
	if err != nil {
		t.Fatalf("list the standing orders: %v", err)
	}
	if len(items) != built.Standing {
		t.Fatalf("the standing store holds %d items out of %d written", len(items), built.Standing)
	}
	asking, fired, paused, held := 0, 0, 0, 0
	for _, item := range items {
		switch {
		case item.NeedsPerson != "":
			asking++
		case item.Status == standing.StatusPaused:
			paused++
		case item.When.Kind == standing.WhenHold:
			held++
		}
		if item.CleanRuns > 0 && item.LastCheckLine != "" {
			fired++
		}
	}
	if asking == 0 || fired == 0 || paused == 0 || held == 0 {
		t.Fatalf("the standing page wants one of each and has asking=%d fired=%d paused=%d rule=%d",
			asking, fired, paused, held)
	}
	spend, err := orders.Today("", now)
	if err != nil {
		t.Fatalf("read today's standing ledger: %v", err)
	}
	if spend.USD <= 0 {
		t.Fatal("nothing standing has spent anything today, so cost per firing draws nothing")
	}

	// ── memory: three shelves, both counters, and one let go ──────────────────
	brain, err := store.Open(filepath.Join(dir, ".aforge", "graph.db"))
	if err != nil {
		t.Fatalf("open the memory store: %v", err)
	}
	defer brain.Close()
	shelves, err := brain.MemorySnapshot(0)
	if err != nil {
		t.Fatalf("read the memory snapshot: %v", err)
	}
	if shelves.Total != built.Memories {
		t.Fatalf("the store holds %d memories out of %d written", shelves.Total, built.Memories)
	}
	if len(shelves.Shelves) != 3 {
		t.Fatalf("the page draws %d shelves; all three scopes should have something on them", len(shelves.Shelves))
	}
	if shelves.Held == 0 || shelves.LetGo == 0 {
		t.Fatalf("the page wants held and let-go rows and has held=%d letGo=%d", shelves.Held, shelves.LetGo)
	}
	used, missed := 0, 0
	for _, shelf := range shelves.Shelves {
		if len(shelf.Memories) == 0 {
			t.Fatalf("the %q shelf came back with no rows on it", shelf.Scope)
		}
		for _, memory := range shelf.Memories {
			if memory.UseCount > 0 {
				used++
			}
			if memory.MissCount > 0 {
				missed++
			}
		}
	}
	if used == 0 || missed == 0 {
		t.Fatalf("the ranking columns want both counters and have used=%d missed=%d", used, missed)
	}

	// ── spend: fourteen days, three models, and every subject bound ───────────
	lines, err := session.ReadUsage(filepath.Join(dir, ".aforge", "v3", session.UsageLedgerName), time.Time{})
	if err != nil {
		t.Fatalf("read the usage ledger: %v", err)
	}
	if len(lines) != built.UsageLines {
		t.Fatalf("the ledger holds %d lines out of %d written", len(lines), built.UsageLines)
	}
	models, days := map[string]bool{}, map[string]bool{}
	conversations, work, standingSpend := 0, 0, 0
	for _, line := range lines {
		models[line.Model] = true
		days[line.Day] = true
		switch {
		case line.Standing != "":
			standingSpend++
		case line.Task != "":
			work++
		case line.Session != "":
			conversations++
		}
		if line.USD <= 0 {
			t.Fatalf("a ledger line spent nothing, which the ledger's own law forbids: %+v", line)
		}
	}
	if len(models) < 3 {
		t.Fatalf("the spend page's model column has %d models to draw", len(models))
	}
	if len(days) < usageDays-1 {
		t.Fatalf("the spend page's day axis has %d days on it, want about %d", len(days), usageDays)
	}
	if conversations == 0 || work == 0 || standingSpend == 0 {
		t.Fatalf("the `what it was for` column wants all three subjects and has conversations=%d work=%d standing=%d",
			conversations, work, standingSpend)
	}

	// ── search: a query a person would actually type finds a conversation ─────
	for _, ask := range []string{"enterprise ladder pricing", "why the frame jumps", "backup window"} {
		hits, err := brain.SearchConversations(ask, 10)
		if err != nil {
			t.Fatalf("search %q: %v", ask, err)
		}
		if len(hits) == 0 {
			t.Fatalf("search for %q found nothing, so the search place is empty", ask)
		}
		if hits[0].SessionID == "" || hits[0].Title == "" {
			t.Fatalf("a hit for %q could not say which conversation it came from: %+v", ask, hits[0])
		}
	}

	// ── made for you: the deliverables index, and the files behind it ─────────
	deliverables := 0
	for _, row := range session.ReadArtifacts(filepath.Join(dir, ".aforge", "v3", session.ArtifactsIndexName)) {
		if _, err := os.Stat(row.Path); err != nil {
			t.Fatalf("the made thing %q is not on the disk: %v", row.Title, err)
		}
		deliverables++
	}
	if deliverables != built.Artifacts {
		t.Fatalf("the deliverables index holds %d rows out of %d written", deliverables, built.Artifacts)
	}
}

// A demo home must be safe to build, which means it writes inside the directory
// it was given and nowhere else — least of all into the state root of the person
// running it, which is the whole reason this program exists.
func TestTheDemoHomeSeedsNothingOutsideTheDirectoryItWasGiven(t *testing.T) {
	outside := t.TempDir()
	t.Setenv("HOME", outside)
	t.Setenv("AFORGE_HOME", filepath.Join(outside, ".aforge"))

	dir := t.TempDir()
	if _, err := seedDemoHome(dir, time.Now()); err != nil {
		t.Fatalf("seed the demo home: %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("seeding wrote %d things into the state root it was not given: %+v", len(entries), entries)
	}
}

// The presence files are what draw `1 want you` and `1 moving`, and they are
// believed for three heartbeats and no longer. The beat is what keeps them true
// while somebody is looking at the demo; without it the two clauses vanish
// fifteen seconds after the seeding.
func TestTheDemoHomeKeepsItsLiveConversationsFresh(t *testing.T) {
	dir := t.TempDir()
	if _, err := seedDemoHome(dir, time.Now()); err != nil {
		t.Fatalf("seed the demo home: %v", err)
	}
	rows := livePresenceRows(dir)
	if len(rows) == 0 {
		t.Fatal("the demo home has no live conversations to keep alive")
	}

	// Age every claim past the window the readers believe, exactly as the clock
	// does while nobody is beating them.
	for _, row := range rows {
		row.row.UpdatedAt = time.Now().Add(-time.Hour)
		if err := writePresence(row.dir, row.row); err != nil {
			t.Fatal(err)
		}
		if _, live := session.ReadSessionPresence(row.dir, time.Now()); live {
			t.Fatalf("an hour-old presence file in %s is still believed", row.dir)
		}
	}

	stop := startPresenceBeat(dir)
	defer stop()
	deadline := time.Now().Add(5 * time.Second)
	for {
		fresh := 0
		for _, row := range rows {
			if _, live := session.ReadSessionPresence(row.dir, time.Now()); live {
				fresh++
			}
		}
		if fresh == len(rows) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the beat made %d of %d conversations live again", fresh, len(rows))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The way this program could stop being safe is somebody pointing --into at a
// directory that is not a demo home at all, so a non-empty directory is refused
// outright and --keep only opens one that already looks like ours.
func TestTheDemoHomeRefusesADirectoryThatIsNotItsOwn(t *testing.T) {
	occupied := t.TempDir()
	if err := os.WriteFile(filepath.Join(occupied, "somebody-elses-work.txt"), []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, err := demoDir(occupied, false); err == nil {
		t.Fatal("a non-empty directory was accepted without --keep")
	}
	if _, _, err := demoDir(occupied, true); err == nil {
		t.Fatal("--keep accepted a directory that holds no demo home")
	}

	// An empty directory, and one this program has already built, are the two it
	// will take.
	empty := t.TempDir()
	dir, fresh, err := demoDir(empty, false)
	if err != nil || !fresh || dir != empty {
		t.Fatalf("an empty directory came back as (%q, fresh=%v, %v)", dir, fresh, err)
	}
	if _, err := seedDemoHome(empty, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, fresh, err := demoDir(empty, true); err != nil || fresh {
		t.Fatalf("--keep on a built demo home came back as (fresh=%v, %v)", fresh, err)
	}
}
