package remote

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// ── THE PLACES OVER THE WIRE ────────────────────────────────────────────────
//
// A place is a listing of one machine's disk, and the surface used to list its
// own: over --host the tasks place walked the LAPTOP's `~/.aforge/v3` and drew
// eight rows and $22.54 of work under a conversation on a server that had run
// none of it. Places.World is the door that ends that, and what these tests hold
// it to is the two properties the surface is built on — the reading arrives
// WHOLE, and an engine that cannot answer refuses rather than answering empty.

// farWorld is the sort of thing the engine's own [session.ReadWorld] hands back:
// one project, one conversation, and one finished piece of work with money on
// it. Every field the surface reads on the far side is filled, because a field
// that survives the round trip in a test and not in the product is a field
// somebody has to debug on a real machine.
func farWorld(now time.Time) session.World {
	return session.World{
		Read: now,
		Projects: []session.Project{{
			Dir: "-srv-code-api", Path: "/srv/code/api", Name: "api",
			Sessions: []session.SessionRow{{
				ID: "bbbb000000000002", Dir: "/srv/.aforge/v3/projects/-srv-code-api/bbbb000000000002",
				Transcript: "/srv/.aforge/v3/projects/-srv-code-api/bbbb000000000002/transcript.jsonl",
				Title:      "rewriting the importer", Project: "api", ProjectDir: "/srv/code/api",
				Workspace: "/srv/code/api", Model: "m", At: now, Created: now,
				Tasks: session.TaskRollup{Rows: []session.TaskIndexEntry{{
					ID: "1", Name: "trimming", Label: "trimming the index",
					Title: "trimming the index", Status: string(session.TaskDone),
					Cost: 22.54, SessionID: "bbbb000000000002", EndedAt: now,
				}}},
			}},
		}},
	}
}

func TestTheLatePlaceDoorsCrossAndArchiveStaysInsidePlaces(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	var archived string
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{model: "m"}, PlacesRoot: "/srv/.aforge/v3/projects",
			Ledger: func(time.Time) LedgerReading {
				return LedgerReading{Lines: []session.UsageLine{{At: now, USD: 1.25}}, Held: true}
			},
			Search: func(q string, limit int) ([]store.ConversationHit, error) {
				return []store.ConversationHit{{Title: q}}, nil
			},
			Archive: func(dir string, _ bool) error { archived = dir; return nil },
		}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	ledger, err := loop.Client.Ledger(now.Add(-time.Hour))
	if err != nil || !ledger.Held || len(ledger.Lines) != 1 {
		t.Fatalf("ledger: %+v, %v", ledger, err)
	}
	hits, err := loop.Client.SearchConversations("importer", 4)
	if err != nil || len(hits) != 1 || hits[0].Title != "importer" {
		t.Fatalf("search: %+v, %v", hits, err)
	}
	inside := "/srv/.aforge/v3/projects/-srv-code-api/one"
	if err := loop.Client.Archive(inside, true); err != nil || archived != inside {
		t.Fatalf("archive: %q, %v", archived, err)
	}
	if err := loop.Client.Archive("/tmp/not-a-place", true); err == nil {
		t.Fatal("archive accepted a path outside places")
	}
}

// The world crosses whole: the project, the conversation inside it, and the task
// row inside that — which is the one the tasks place reads its rows out of.
func TestTheWorldCrossesTheWire(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{
			Agent:      &fakeAgent{model: "m"},
			Workspace:  "/srv/code/api",
			World:      func() session.World { return farWorld(now) },
			PlacesRoot: "/srv/.aforge/v3/projects",
		}, nil
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	// THE ROOT TRAVELS ON THE WELCOME, because a world is a set of paths and a
	// path needs its disk: the surface puts the conversation it is sitting in
	// back into this walk, and works out which bucket it belongs to from the root
	// ([Welcome.PlacesRoot]).
	if got := loop.Client.Welcome().PlacesRoot; got != "/srv/.aforge/v3/projects" {
		t.Fatalf("the welcome did not carry the engine's places root: %q", got)
	}

	world, err := loop.Client.World()
	if err != nil {
		t.Fatalf("ask for the world: %v", err)
	}
	if len(world.Projects) != 1 || world.Projects[0].Name != "api" {
		t.Fatalf("the project did not cross: %+v", world.Projects)
	}
	if !world.Read.Equal(now) {
		t.Fatalf("the reading's own instant did not cross: %v want %v", world.Read, now)
	}
	rows := world.Projects[0].Sessions
	if len(rows) != 1 || rows[0].Title != "rewriting the importer" {
		t.Fatalf("the conversation did not cross: %+v", rows)
	}
	tasks := rows[0].Tasks.Rows
	if len(tasks) != 1 || tasks[0].Label != "trimming the index" || tasks[0].Cost != 22.54 {
		t.Fatalf("the work did not cross: %+v", tasks)
	}
}

// An engine with no world door REFUSES, and the refusal is not an empty world.
//
// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT EMPTY (CLAUDE.md). An empty world
// answered here would reach the surface as a machine with no projects on it, and
// home would greet somebody with `nothing here yet — say something and this fills
// up` over a server full of work. The error is what lets the surface draw nothing
// instead (cmd/aforge's [hostWorld] keeps `known` false on it).
func TestAnEngineWithNoWorldDoorRefusesRatherThanAnsweringEmpty(t *testing.T) {
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{model: "m"}, Workspace: "/srv/code/api"}, nil
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	if _, err := loop.Client.World(); err == nil {
		t.Fatal("an engine with no world door answered a world")
	}
}

// ── ONE ROW OF THE RECORD, READ ON THE MACHINE THAT HOLDS IT ────────────────
//
// The world above carries what a task row SAYS. The card behind one of those
// rows draws one thing more — the last thing the node itself said — and that is
// in the node's own journal, on the disk of the machine that ran the work. Until
// Places.Task the surface read it off ITS disk, at a path that only exists on the
// other one, and the miss came back as `its transcript is not on this disk any
// more` about a journal sitting perfectly well over there.

// farRecordEngine is an engine whose record is one real journal in a temp places
// root, so these tests exercise [session.ReadTaskRecordUnder] rather than a
// fixture standing in for it.
func farRecordEngine(t *testing.T) (*Loop, string, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "-srv-code-api", "bbbb000000000002", "tasks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(dir, "20260826-094113_1.jsonl")
	lines := `{"type":"message","role":"user","content":"widen the pipe"}` + "\n" +
		`{"type":"message","role":"assistant","content":"thinking about it"}` + "\n" +
		`{"type":"message","role":"assistant","content":"widened the pipe and re-ran the importer."}` + "\n"
	if err := os.WriteFile(journal, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{
			Agent: &fakeAgent{model: "m"}, Workspace: "/srv/code/api",
			World:      func() session.World { return session.World{} },
			PlacesRoot: root,
			TaskRecord: func(uri string, tail int) (session.TaskRecord, error) {
				return session.ReadTaskRecordUnder(root, uri, tail)
			},
		}, nil
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	return loop, root, journal
}

// The report crosses, and it is the LAST thing the node said rather than the
// first — which is the whole of what [session.PeekReport] is for.
func TestOneRowOfTheRecordCrossesTheWire(t *testing.T) {
	loop, _, journal := farRecordEngine(t)
	record, err := loop.Client.TaskRecord("file://"+journal, 0)
	if err != nil {
		t.Fatalf("ask for the record: %v", err)
	}
	if record.Report != "widened the pipe and re-ran the importer." {
		t.Fatalf("the report did not cross: %q", record.Report)
	}
	// AND WHETHER THE JOURNAL IS STILL THERE IS ITS OWN FACT. A folder somebody
	// deleted and a journal that never held a report are two different sentences
	// on the card, and only the machine holding the file can tell them apart.
	if !record.Kept {
		t.Fatal("a journal that is on the engine's disk did not say so")
	}
}

func TestARoomTailCrossesTheRecordDoor(t *testing.T) {
	loop, _, journal := farRecordEngine(t)
	record, err := loop.Client.TaskRecord("file://"+journal, session.TaskJournalTail)
	if err != nil {
		t.Fatalf("ask for the room journal: %v", err)
	}
	if !bytes.Contains(record.Journal, []byte(`"content":"widen the pipe"`)) ||
		!bytes.Contains(record.Journal, []byte(`"content":"widened the pipe and re-ran the importer."`)) {
		t.Fatalf("the journal tail did not cross whole: %q", record.Journal)
	}
}

// A row whose journal has been deleted is an ANSWER and not a refusal: the row
// still names the file, the card still says where it was, and `Kept` false is the
// sentence the card has for exactly this.
func TestAJournalTheEngineNoLongerHasIsAnAnswer(t *testing.T) {
	loop, root, _ := farRecordEngine(t)
	record, err := loop.Client.TaskRecord("file://"+filepath.Join(root, "-srv-code-api", "bbbb000000000002", "tasks", "gone.jsonl"), 0)
	if err != nil {
		t.Fatalf("a deleted journal was refused rather than answered: %v", err)
	}
	if record.Kept || record.Report != "" {
		t.Fatalf("a deleted journal answered as though it were there: %+v", record)
	}
}

// AND NOTHING OUTSIDE THE RECORD CROSSES. The URI came off a row this machine
// wrote, but a door that trusted that would be a permission decision taken on the
// strength of what the other end says — so the root is checked here, on the
// machine that owns it (internal/remote's two-roots law, stated for the one
// directory this door answers about).
func TestNothingOutsideTheEnginesRecordCrosses(t *testing.T) {
	loop, _, _ := farRecordEngine(t)
	outside := filepath.Join(t.TempDir(), "secrets.jsonl")
	if err := os.WriteFile(outside, []byte(`{"type":"message","role":"assistant","content":"no"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loop.Client.TaskRecord("file://"+outside, 0); err == nil {
		t.Fatal("a journal outside the engine's record crossed the wire")
	}
}

// An engine with no record door REFUSES, on the world door's own law: an empty
// record answered here would reach the card as a piece of work that said nothing
// at the end, which is a claim.
func TestAnEngineWithNoRecordDoorRefusesRatherThanAnsweringEmpty(t *testing.T) {
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{model: "m"}, Workspace: "/srv/code/api"}, nil
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	if _, err := loop.Client.TaskRecord("file:///srv/anything.jsonl", 0); err == nil {
		t.Fatal("an engine with no record door answered a record")
	}
}
