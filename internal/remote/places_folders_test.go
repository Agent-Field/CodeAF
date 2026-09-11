package remote

// THE FOLDERS A PERSON ATTACHES, ACROSS THE SEAM.
//
// The defect these are written against is not a remote one at all. The ordinary
// `aforge chat` on this laptop goes through this wire to this machine's own
// session host (cmd/aforge's chatv3_local.go dials with an empty machine name),
// so the surface's folder picker was type-asserting a door onto a *remote.Agent
// that had none — and the assertion failing was, by design, silent: the line
// `folder · <path>` was drawn over a conversation that had gained nothing, and
// the session's own meta.json had no places on it afterwards.
//
// So the tests here are about the road: does the choosing reach the ENGINE'S
// agent, does its answer come back, does the set reach the surface without a
// round trip, and does an engine that cannot hold a folder say so instead of
// pretending.

import (
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE REAL AGENT SATISFIES THE DOOR THE SERVER ASSERTS FOR. The assertion is by
// its nature silent when it fails, so this is the pin: a method renamed on
// [session.Agent] is a compile error here rather than a folder picker that goes
// quiet in the product.
var _ placeKeeper = (*session.Agent)(nil)

// foldersAgent is the scripted engine's agent WITH the places door on it, and it
// keeps the one rule the real one keeps that a caller can observe from here: the
// path it answers with is the engine's, not the caller's.
type foldersAgent struct {
	*fakeAgent

	mu       sync.Mutex
	places   []session.PlaceRef
	referred []string
	removed  []string
	// snap is what this engine turns a chosen path into — the stand-in for the
	// repository-root snap the real one makes. A caller that reported the path it
	// SENT rather than the path it got back would pass every test that did not
	// have this.
	snap func(string) string
	// refuse is the engine saying no, which is the case a surface has to draw as
	// a refusal rather than as a folder it now has.
	refuse error
}

func newFoldersAgent() *foldersAgent {
	return &foldersAgent{fakeAgent: &fakeAgent{model: "m"}}
}

func (f *foldersAgent) ReferPlace(path string, arrival session.PlaceArrival) (session.PlaceRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse != nil {
		return session.PlaceRef{}, f.refuse
	}
	f.referred = append(f.referred, string(arrival)+":"+path)
	if f.snap != nil {
		path = f.snap(path)
	}
	ref := session.PlaceRef{Path: path, Arrival: arrival, Repository: true}
	f.places = append([]session.PlaceRef{ref}, f.places...)
	return ref, nil
}

func (f *foldersAgent) Places() []session.PlaceRef {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]session.PlaceRef, len(f.places))
	copy(out, f.places)
	return out
}

func (f *foldersAgent) RemovePlace(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse != nil {
		return f.refuse
	}
	f.removed = append(f.removed, path)
	kept := f.places[:0:0]
	for _, place := range f.places {
		if place.Path != path {
			kept = append(kept, place)
		}
	}
	f.places = kept
	return nil
}

func (f *foldersAgent) said(what func(*foldersAgent) []string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	got := what(f)
	out := make([]string, len(got))
	copy(out, got)
	return out
}

func foldersLoop(t *testing.T, far WrappedAgent) *Loop {
	t.Helper()
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl"}, nil
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	return loop
}

// CHOOSING A FOLDER REACHES THE ENGINE'S OWN AGENT, and what comes back is the
// ENGINE'S answer to which folder that was.
func TestChoosingAFolderReachesTheEngineAndAnswersWithItsOwnPath(t *testing.T) {
	far := newFoldersAgent()
	far.snap = func(string) string { return "/srv/app/repo" }
	loop := foldersLoop(t, far)

	ref, err := loop.Client.Agent().ReferPlace("/srv/app/repo/internal/session", session.PlaceSaid)
	if err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	if ref.Path != "/srv/app/repo" {
		t.Fatalf("the surface was handed back %q, want the engine's own answer /srv/app/repo", ref.Path)
	}
	if got := far.said(func(f *foldersAgent) []string { return f.referred }); len(got) != 1 ||
		got[0] != "said:/srv/app/repo/internal/session" {
		t.Fatalf("the engine's agent was told %v — the choosing did not cross", got)
	}
}

// AND THE SET IS READ WITHOUT TOUCHING THE WIRE, because the folder indicator is
// drawn on a frame. The engine states the set unasked; this is what the surface
// answers from.
func TestTheAttachedFoldersAreReadFromTheFactsAndNotOverTheWire(t *testing.T) {
	far := newFoldersAgent()
	loop := foldersLoop(t, far)
	agent := loop.Client.Agent()

	if got := agent.Places(); len(got) != 0 {
		t.Fatalf("a fresh conversation is about %+v", got)
	}
	// THE ENGINE HOLDS A ROW THE ANSWER DOES NOT CARRY — a ground its own work
	// resolved — so what arrives can only have come from the engine STATING the
	// set. Without this the surface's own optimism about the folder it just chose
	// would satisfy every assertion below and the push could be missing entirely.
	far.mu.Lock()
	far.places = []session.PlaceRef{{Path: "/srv/app/resolved", Arrival: session.PlaceKept}}
	far.mu.Unlock()

	if _, err := agent.ReferPlace("/srv/app/notes", session.PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	waitFor(t, "the engine to state the whole set it holds", func() bool {
		got := agent.Places()
		return len(got) == 2 && got[0].Path == "/srv/app/notes" && got[0].Arrival == session.PlaceSaid &&
			got[1].Path == "/srv/app/resolved" && got[1].Arrival == session.PlaceKept
	})

	// AND IT SURVIVES THE CONNECTION GOING, which is what makes it a memory read
	// rather than a call: what cannot be fetched cannot be drawn, and the last
	// stated set is the honest picture of a conversation that is not moving.
	if err := loop.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := agent.Places(); len(got) != 2 || got[0].Path != "/srv/app/notes" {
		t.Fatalf("the set emptied itself when the pipe closed: %+v", got)
	}
}

// AND A SURFACE THAT SITS DOWN AT A CONVERSATION ALREADY ABOUT SOMEWHERE KNOWS
// AT THE DOOR. The welcome carries the fact set, so a person reopening a
// conversation sees its folders in the first frame rather than after a call.
func TestAConnectionOpensAlreadyKnowingTheConversationsFolders(t *testing.T) {
	far := newFoldersAgent()
	far.places = []session.PlaceRef{{Path: "/srv/app/repo", Arrival: session.PlaceSaid, Repository: true}}
	loop := foldersLoop(t, far)

	got := loop.Client.Agent().Places()
	if len(got) != 1 || got[0].Path != "/srv/app/repo" || got[0].Arrival != session.PlaceSaid {
		t.Fatalf("the welcome did not carry the conversation's folders: %+v", got)
	}
}

// REMOVING ONE REACHES THE ENGINE TOO, and the set the surface draws from
// follows.
func TestRemovingAFolderReachesTheEngineAndTheSetFollows(t *testing.T) {
	far := newFoldersAgent()
	loop := foldersLoop(t, far)
	agent := loop.Client.Agent()

	if _, err := agent.ReferPlace("/srv/app/notes", session.PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	if err := agent.RemovePlace("/srv/app/notes"); err != nil {
		t.Fatalf("RemovePlace: %v", err)
	}
	if got := far.said(func(f *foldersAgent) []string { return f.removed }); len(got) != 1 ||
		got[0] != "/srv/app/notes" {
		t.Fatalf("the engine's agent was told to remove %v", got)
	}
	waitFor(t, "the engine to state the conversation is about nothing again", func() bool {
		return len(agent.Places()) == 0
	})
}

// THE ENGINE'S REFUSAL CROSSES AS A REFUSAL. A surface that drew a folder as
// attached because a call returned would be showing a chip over a conversation
// that gained nothing — which is the exact defect this wave is here for, one
// layer down.
func TestAnEngineThatRefusesAFolderIsNotReportedAsHavingTakenIt(t *testing.T) {
	far := newFoldersAgent()
	far.refuse = errNoSuchFolder
	loop := foldersLoop(t, far)
	agent := loop.Client.Agent()

	if _, err := agent.ReferPlace("/srv/app/gone", session.PlaceSaid); err == nil {
		t.Fatal("a folder the engine refused was answered as attached")
	} else if !strings.Contains(err.Error(), "is not there") {
		t.Fatalf("the refusal reached the surface as %q, losing the engine's own words", err)
	}
	if got := agent.Places(); len(got) != 0 {
		t.Fatalf("a refused folder landed on the surface's set anyway: %+v", got)
	}
	if err := agent.RemovePlace("/srv/app/gone"); err == nil {
		t.Fatal("removing a folder the engine refused to remove was answered as done")
	}
}

// THE ENGINE SAYS AT THE DOOR WHETHER IT CAN HOLD A FOLDER AT ALL, and that is
// the fact a surface needs before it opens a picker. Its own type assertion
// cannot answer it: *remote.Agent carries the three methods whatever is behind
// the pipe, so the assertion says yes for every connection there has ever been.
func TestTheEngineSaysAtTheDoorWhetherItCanHoldAFolder(t *testing.T) {
	if capable := foldersLoop(t, newFoldersAgent()); !capable.Client.Agent().KeepsFolders() {
		t.Fatal("an engine whose agent keeps folders said at the door that it does not")
	}
	if plain := foldersLoop(t, &fakeAgent{model: "m"}); plain.Client.Agent().KeepsFolders() {
		t.Fatal("an engine that cannot keep a folder said at the door that it can")
	}
}

// AND AN ENGINE THAT KEEPS NO FOLDERS AT ALL SAYS SO IN THE MACHINE'S OWN TERMS,
// rather than answering an empty nothing a surface would draw as success. A
// capability that cannot work is absent, not broken.
func TestAnEngineWithNoPlacesDoorRefusesRatherThanPretending(t *testing.T) {
	loop := foldersLoop(t, &fakeAgent{model: "m"})
	agent := loop.Client.Agent()

	_, err := agent.ReferPlace("/srv/app/notes", session.PlaceSaid)
	if err == nil {
		t.Fatal("an engine that cannot keep a folder said it had taken one")
	}
	if !strings.Contains(err.Error(), foldersOffWord) {
		t.Fatalf("the refusal reads %q, which does not say what the machine cannot do", err)
	}
	if err := agent.RemovePlace("/srv/app/notes"); err == nil {
		t.Fatal("an engine that cannot keep a folder said it had removed one")
	}
}

// errNoSuchFolder is the shape of refusal internal/session answers for a folder
// that is not there, spelled here so the test does not depend on that package's
// exact sentence to prove the words survive the crossing.
var errNoSuchFolder = errFolder("/srv/app/gone is not there")

type errFolder string

func (e errFolder) Error() string { return string(e) }
