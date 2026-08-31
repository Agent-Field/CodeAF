package session

// WHERE A CONVERSATION IS ABOUT, and what the ladder does with it.
//
// The set exists to remove a chore: a person who has said once which project
// this conversation is about — by naming it, or by answering the two-places
// question — is never asked again, and the answer survives the terminal being
// closed. So these tests are written from the person's side of that: what they
// did, and what they are not asked next.

import (
	"path/filepath"
	"strings"
	"testing"
)

// A PLACE SOMEBODY NAMED IS ON THE FOLDER, and it comes back. This is the same
// defect the conversation rung was written for (effort_test.go): a live field is
// not a memory, and a conversation that forgot every folder it was about the
// moment the window closed would be asking the same question every morning.
func TestNamingAPlaceIsWrittenDownAndReadBack(t *testing.T) {
	dir := t.TempDir()
	project := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "session.jsonl")
		config.Place = Place{Dir: dir}
	})

	if got := agent.Places(); len(got) != 0 {
		t.Fatalf("a fresh conversation is about %d places, want none at all", len(got))
	}
	ref, err := agent.ReferPlace(filepath.Join(project, "deep", ".."), PlaceSaid)
	if err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	if ref.Path != canonicalPath(project) {
		t.Fatalf("the place is %q, want the project itself %q", ref.Path, canonicalPath(project))
	}
	if !ref.Repository {
		t.Fatal("git knows this folder and the record says it does not")
	}
	meta, err := LoadMeta(dir)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if len(meta.Places) != 1 || meta.Places[0].Path != canonicalPath(project) {
		t.Fatalf("meta.json holds %+v — the place did not reach the folder", meta.Places)
	}
	if meta.Places[0].Arrival != PlaceSaid {
		t.Fatalf("the place arrived %q, want the person's own act", meta.Places[0].Arrival)
	}

	// AND THE WAY BACK IS THE POINT. A second process on the same folder opens
	// already knowing what this conversation is about.
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	second, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "session.jsonl")
		config.Place = Place{Dir: dir}
	})
	places := second.Places()
	if len(places) != 1 || places[0].Path != canonicalPath(project) {
		t.Fatalf("the reopened conversation is about %+v", places)
	}
}

// The two refusals, in the person's own words. A place is a folder that IS
// there, because the whole value of the set is that the ladder can hand a ground
// to work without stopping to wonder.
func TestAPlaceIsAFolderThatIsThere(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	writeFile(t, filepath.Join(workspace, "notes.md"), "words\n")

	if _, err := agent.ReferPlace(filepath.Join(workspace, "nowhere"), PlaceSaid); err == nil ||
		!strings.HasSuffix(err.Error(), "is not there") {
		t.Fatalf("a folder that is not there answered %v", err)
	}
	if _, err := agent.ReferPlace(filepath.Join(workspace, "notes.md"), PlaceSaid); err == nil {
		t.Fatal("a file was taken as a place")
	}
	if got := agent.Places(); len(got) != 0 {
		t.Fatalf("a refused place still landed on the conversation: %+v", got)
	}
}

// A PLACE THE PERSON NAMED ANSWERS AT SAID, above the evidence. The conversation
// has been reading one repository and is ABOUT another; somebody's own word is
// never overruled by what the calls happened to touch.
func TestASaidPlaceBeatsATouchedRoot(t *testing.T) {
	touched := newTestRepo(t)
	referred := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.messages = append(agent.messages,
		readCallMessage("call-a", filepath.Join(touched, "shared.txt")),
		readCallMessage("call-b", filepath.Join(touched, "shared.txt")))
	agent.mu.Unlock()
	if _, err := agent.ReferPlace(referred, PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}

	stand := agent.resolveTaskGround(taskSpec{deliverable: "the fix", acceptance: "the tests pass"})
	if stand.dir != canonicalPath(referred) || stand.rung != taskGroundSaid {
		t.Fatalf("stand = %+v, want the place the person named at said", stand)
	}
	if stand.ask != "" {
		t.Fatalf("a place somebody named was still a question: %q", stand.ask)
	}
}

// AND THE PERSON'S WORD ABOUT HOW WORK HAPPENS THERE RIDES WITH IT. "In place"
// is never guessed and never recomputed off the deliverable; a person who said
// it about one folder said it about that folder.
func TestAPlaceCarriesThePersonsOwnModeWord(t *testing.T) {
	project := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if _, err := agent.ReferPlace(project, PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	if err := agent.SetPlaceMode(project, "in place"); err != nil {
		t.Fatalf("SetPlaceMode: %v", err)
	}

	stand := agent.resolveTaskGround(taskSpec{deliverable: "shared.txt, changed", acceptance: "it changed"})
	if stand.dir != canonicalPath(project) || stand.mode != TaskModeInPlace {
		t.Fatalf("stand = %+v, want the person's own mode on their own place", stand)
	}
	// A folder this conversation is not about has no mode to set, and inventing
	// the place to hang one on would be the surface guessing.
	if err := agent.SetPlaceMode(t.TempDir(), "in place"); err == nil {
		t.Fatal("a mode was set on a place the conversation is not about")
	}
}

// THE WHOLE ANTI-CHORE MECHANISM, END TO END. The conversation has been in two
// repositories, so the first proposal is a question; the person answers it once;
// and the next piece of work does not ask again.
func TestAResolvedGroundIsKeptSoNobodyIsAskedTwice(t *testing.T) {
	first := newTestRepo(t)
	second := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.messages = append(agent.messages,
		readCallMessage("call-a", filepath.Join(first, "shared.txt")),
		readCallMessage("call-b", filepath.Join(second, "shared.txt")))
	agent.mu.Unlock()
	work := taskSpec{deliverable: "the fix", acceptance: "the tests pass"}

	if stand := agent.resolveTaskGround(work); stand.ask == "" {
		t.Fatalf("two places with real weight were not a question: %+v", stand)
	}
	// The person answers, and the answer arrives the way it always does: the
	// model proposes again with `ground` set to what they said.
	answered := work
	answered.ground = first
	if stand := agent.taskGroundOrStandingIn(answered); stand.dir != canonicalPath(first) {
		t.Fatalf("the answer did not settle the ground: %+v", stand)
	}

	stand := agent.resolveTaskGround(work)
	if stand.ask != "" {
		t.Fatalf("the same question was asked twice: %q", stand.ask)
	}
	if stand.dir != canonicalPath(first) || stand.rung != taskGroundSaid {
		t.Fatalf("stand = %+v, want the answer the person already gave", stand)
	}
	// AND IT IS A CACHED ANSWER AND NOT SOMEBODY'S WORD, which is what lets a
	// brief that knows better still re-ground the work (taskstands.go's
	// groundLint).
	places := agent.Places()
	if len(places) != 1 || places[0].Arrival != PlaceKept {
		t.Fatalf("the conversation kept %+v", places)
	}
}

// TWO PLACES THE CONVERSATION IS ABOUT ARE STILL A QUESTION. Being about two
// projects says no more about which one this work is for than having read two of
// them does — and the question is better, because both names are folders the
// person put there themselves.
func TestTwoReferredPlacesAreStillAQuestion(t *testing.T) {
	first := newTestRepo(t)
	second := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	for _, place := range []string{first, second} {
		if _, err := agent.ReferPlace(place, PlaceSaid); err != nil {
			t.Fatalf("ReferPlace: %v", err)
		}
	}

	stand := agent.resolveTaskGround(taskSpec{deliverable: "the fix", acceptance: "the tests pass"})
	if stand.ask == "" {
		t.Fatalf("two referred places were guessed between: %+v", stand)
	}
	for _, want := range []string{
		"this conversation is about two places",
		canonicalPath(first), canonicalPath(second), "which one this task is about",
	} {
		if !strings.Contains(stand.ask, want) {
			t.Fatalf("the question does not say %q: %q", want, stand.ask)
		}
	}

	// AND A CONTRACT THAT NAMES ONE OF THEM SETTLES IT WITHOUT ASKING. Work whose
	// brief spells out a path inside one of the two is plainly about that one.
	stand = agent.resolveTaskGround(taskSpec{
		brief:       "the fix belongs in " + filepath.Join(second, "shared.txt"),
		deliverable: "shared.txt, changed",
		acceptance:  "the line reads differently",
	})
	if stand.ask != "" || stand.dir != canonicalPath(second) {
		t.Fatalf("stand = %+v, want the place the contract named", stand)
	}
}

// A KEPT PLACE DECAYS AND A SAID ONE DOES NOT. The conversation resolved one
// project a while ago and has spent every call since in another; it is about the
// other one now, and a cache that outranked what the person is visibly doing
// would be this design's own chore wearing the opposite face.
func TestAStaleKeptPlaceDoesNotOutrankFreshTouchedEvidence(t *testing.T) {
	stale := newTestRepo(t)
	busy := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.refer(PlaceRef{Path: canonicalPath(stale), Arrival: PlaceKept})
	agent.mu.Lock()
	for _, id := range []string{"call-a", "call-b", "call-c", "call-d"} {
		agent.messages = append(agent.messages, readCallMessage(id, filepath.Join(busy, "shared.txt")))
	}
	agent.mu.Unlock()
	work := taskSpec{deliverable: "the fix", acceptance: "the tests pass"}

	stand := agent.resolveTaskGround(work)
	if stand.dir != canonicalPath(busy) || stand.rung != taskGroundTouched {
		t.Fatalf("stand = %+v, want the repository the conversation is actually in", stand)
	}
	// The record is not thrown away, though: what the conversation was about last
	// hour is history and not a lie.
	if places := agent.Places(); len(places) == 0 || places[len(places)-1].Path != canonicalPath(stale) {
		t.Fatalf("the stale place left the conversation entirely: %+v", places)
	}

	// AND THE SAME PLACE, NAMED BY THE PERSON, IS NOT LEFT BEHIND AT ALL.
	if _, err := agent.ReferPlace(stale, PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	if stand := agent.resolveTaskGround(work); stand.dir != canonicalPath(stale) {
		t.Fatalf("stand = %+v, want the place the person named", stand)
	}
}

// A PART STANDS WHERE ITS PARENT STANDS, and the places rung is climbed no more
// than the touched one is for it: a sub-task's branch is cut from its parent's
// worktree and merges back into it, so a part re-grounded onto a folder the
// conversation happens to be about is a part whose work can never come home.
func TestAPartIsNotMovedByTheConversationsPlaces(t *testing.T) {
	repo := newTestRepo(t)
	elsewhere := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.Workspace = repo })
	if _, err := agent.ReferPlace(elsewhere, PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}

	stand := agent.resolveTaskGround(taskSpec{
		parent: 3, depth: 2,
		deliverable: "shared.txt, changed", acceptance: "the line reads differently",
	})
	if stand.dir != canonicalPath(repo) || stand.rung != taskGroundStandingIn {
		t.Fatalf("a part was re-grounded onto a referred place: %+v", stand)
	}
}
