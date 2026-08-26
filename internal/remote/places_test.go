package remote

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
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
		Artifacts: []session.Artifact{{
			Path:    "/srv/.aforge/v3/projects/-srv-code-api/bbbb000000000002/artifacts/chart.png",
			Session: "bbbb000000000002", Title: "the sales chart", Kind: "image", Created: now,
		}},
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
	if len(world.Artifacts) != 1 || world.Artifacts[0].Title != "the sales chart" {
		t.Fatalf("the deliverables did not cross: %+v", world.Artifacts)
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
