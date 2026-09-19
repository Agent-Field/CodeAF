package session

// WHERE A CONVERSATION IS ABOUT, and what the ladder does with it.
//
// The set exists to remove a chore: a person who has said once which project
// this conversation is about — by naming it, or by answering the two-places
// question — is never asked again, and the answer survives the terminal being
// closed. So these tests are written from the person's side of that: what they
// did, and what they are not asked next.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
func TestBriefPlainlyNamesGroundBeforeConversationPlaces(t *testing.T) {
	ground := newTestRepo(t)
	first := newTestRepo(t)
	second := newTestRepo(t)
	artifactDir := filepath.Join(ground, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	for _, place := range []string{first, second} {
		if _, err := agent.ReferPlace(place, PlaceSaid); err != nil {
			t.Fatalf("ReferPlace: %v", err)
		}
	}

	for i, brief := range []string{
		"Work in " + ground + "; leave the report at " + filepath.Join(artifactDir, "one.txt"),
		"The project folder is " + ground + "; update " + filepath.Join(ground, "shared.txt"),
		"Make the change under " + ground + "; evidence belongs in " + filepath.Join(artifactDir, "three.txt"),
	} {
		stand := agent.resolveTaskGround(taskSpec{brief: brief, deliverable: "the named artifact", acceptance: "the artifact exists"})
		if stand.ask != "" || stand.refusal != "" || stand.dir != canonicalPath(ground) || stand.rung != taskGroundBrief {
			t.Fatalf("proposal %d stand = %+v, want plainly named brief ground", i+1, stand)
		}
	}
}

func TestBriefGroundRequiresOneContainmentAnswer(t *testing.T) {
	ground := newTestRepo(t)
	passing := newTestRepo(t)
	first := newTestRepo(t)
	second := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	for _, place := range []string{first, second} {
		if _, err := agent.ReferPlace(place, PlaceSaid); err != nil {
			t.Fatalf("ReferPlace: %v", err)
		}
	}
	stand := agent.resolveTaskGround(taskSpec{
		brief:       "Work in " + ground + "; compare in passing with " + passing,
		deliverable: "the fix", acceptance: "the tests pass",
	})
	if stand.rung == taskGroundBrief || stand.ask == "" {
		t.Fatalf("unrelated passing path decided the brief ground: %+v", stand)
	}
}

func TestAnExplicitThirdGroundIsAcceptedAndVisibleAsSaid(t *testing.T) {
	first := newTestRepo(t)
	second := newTestRepo(t)
	third := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	for _, place := range []string{first, second} {
		if _, err := agent.ReferPlace(place, PlaceSaid); err != nil {
			t.Fatalf("ReferPlace: %v", err)
		}
	}
	stand := agent.resolveTaskGround(taskSpec{ground: third, brief: "the fix", deliverable: "shared.txt", acceptance: "it changed"})
	if stand.ask != "" || stand.refusal != "" || stand.dir != canonicalPath(third) || stand.rung != taskGroundSaid {
		t.Fatalf("explicit third ground = %+v, want accepted with said provenance", stand)
	}
}

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

// AND HOME READS IT BACK OFF THE FOLDER. The row a person scans on home is
// built from meta.json and never from a live conversation (world.go's
// [readSessionRow]), so a set that only the running agent could answer would be
// a set home could never draw — and every conversation on the screen would look
// like a conversation about exactly one directory again.
func TestTheWorldCarriesTheFoldersAConversationIsAbout(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "-tmp-alpha", "aaaa000000000001")
	elsewhere := t.TempDir()
	if err := SaveMeta(dir, Meta{
		ID: "aaaa000000000001", Workspace: "/tmp/alpha", Title: "Pricing",
		LastUserAt: time.Now(),
		Places:     []PlaceRef{{Path: elsewhere, Arrival: PlaceSaid, Referred: time.Now()}},
	}); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	write(t, Place{Dir: dir}.Transcript(), `{"type":"session","version":1,"id":"aaaa000000000001","timestamp":"t"}`)

	world := ReadWorld(root)
	if len(world.Projects) != 1 || len(world.Projects[0].Sessions) != 1 {
		t.Fatalf("the world read %d projects, want the one written", len(world.Projects))
	}
	row := world.Projects[0].Sessions[0]
	if len(row.Places) != 1 || row.Places[0].Path != elsewhere {
		t.Fatalf("the row is about %+v, want the folder the meta names", row.Places)
	}

	// A CONVERSATION WITH NO SET IS EVERY CONVERSATION WRITTEN BEFORE THIS, and
	// it answers nothing rather than an empty something.
	plain := filepath.Join(root, "-tmp-beta", "bbbb000000000001")
	if err := SaveMeta(plain, Meta{ID: "bbbb000000000001", Workspace: "/tmp/beta", LastUserAt: time.Now()}); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	write(t, Place{Dir: plain}.Transcript(), `{"type":"session","version":1,"id":"bbbb000000000001","timestamp":"t"}`)
	for _, project := range ReadWorld(root).Projects {
		for _, row := range project.Sessions {
			if row.ID == "bbbb000000000001" && row.Places != nil {
				t.Fatalf("a conversation with no folders answered %+v", row.Places)
			}
		}
	}
}

// A FOLDER OUTSIDE EVERY REPOSITORY IS WHERE OUTPUT GOES, AND IT DOES NOT VOTE
// when the brief also names a folder inside one. The owner's proposals worked in
// one repository and wrote their reports to a scratch folder beside it: while
// that folder did not exist the brief had one answer, and once the first round
// of tasks had made it, the two read as rivals and the person was asked a
// question every brief had already answered. With no repository named at all,
// a plain folder still decides.
func TestAnOutputFolderOutsideEveryRepositoryDoesNotRivalTheRepositoryTheBriefNames(t *testing.T) {
	ground, first, second := newTestRepo(t), newTestRepo(t), newTestRepo(t)
	scratch := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	for _, place := range []string{first, second} {
		if _, err := agent.ReferPlace(place, PlaceSaid); err != nil {
			t.Fatalf("ReferPlace: %v", err)
		}
	}
	brief := "Work in " + ground + " and write the report to " + filepath.Join(scratch, "report.md")
	stand := agent.resolveTaskGround(taskSpec{brief: brief, deliverable: filepath.Join(scratch, "report.md"), acceptance: "the report exists"})
	if stand.ask != "" || stand.refusal != "" || stand.dir != canonicalPath(ground) || stand.rung != taskGroundBrief {
		t.Fatalf("stand = %+v, want the repository the brief names, on the brief's rung", stand)
	}

	alone := agent.resolveTaskGround(taskSpec{brief: "Collect the notes under " + scratch, deliverable: "the notes", acceptance: "they exist"})
	if alone.dir != canonicalPath(scratch) || alone.rung != taskGroundBrief {
		t.Fatalf("stand = %+v, want the one plain folder the brief names", alone)
	}
}

// A FOLDER INSIDE A REPOSITORY IS THAT REPOSITORY, on this rung as on the said
// one. A brief that names a repository's subfolder and only files under it has
// two folders holding everything it wrote down, the subfolder and the
// repository around it, and they are ONE ground: the rung answers the
// repository and never the deeper of two nested rivals.
func TestABriefThatNamesOnlyASubfolderStandsOnTheRepositoryAroundIt(t *testing.T) {
	ground := newTestRepo(t)
	first := newTestRepo(t)
	second := newTestRepo(t)
	inner := filepath.Join(ground, "inner", "deep")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	for _, place := range []string{first, second} {
		if _, err := agent.ReferPlace(place, PlaceSaid); err != nil {
			t.Fatalf("ReferPlace: %v", err)
		}
	}
	stand := agent.resolveTaskGround(taskSpec{
		brief:       "Everything is under " + inner + "; change " + filepath.Join(inner, "one.txt") + " and " + filepath.Join(inner, "two.txt"),
		deliverable: "the two files, changed", acceptance: "both read differently",
	})
	if stand.ask != "" || stand.refusal != "" || stand.rung != taskGroundBrief || stand.dir != canonicalPath(ground) {
		t.Fatalf("stand = %+v, want the repository %s on the brief rung", stand, canonicalPath(ground))
	}
}

// EXACTLY ONE, OTHERWISE NOT THIS RUNG. Two holding candidates that are still
// two grounds once each is read as the ground it would become are two answers,
// and the rung never picks between them: it reports nothing and the ladder
// climbs on to the evidence and the question below.
func TestTwoGroundsThatBothHoldTheBriefsPlacesAreNoAnswer(t *testing.T) {
	outer := canonicalPath(newTestRepo(t))
	inner := filepath.Join(outer, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, inner, "init", "-q")
	inner = canonicalPath(inner)
	refs := []string{filepath.Join(inner, "a")}
	if err := os.MkdirAll(refs[0], 0o755); err != nil {
		t.Fatal(err)
	}
	if dir, ok := theOneGroundHolding(refs, []string{outer, inner}); ok {
		t.Fatalf("two grounds hold the brief's places and the rung answered %q; it must answer nothing", dir)
	}
	if dir, ok := theOneGroundHolding(refs, []string{inner, filepath.Join(inner, "a")}); !ok || dir != inner {
		t.Fatalf("nested candidates inside one repository = %q, %v; want the repository %q", dir, ok, inner)
	}
	if dir, ok := theOneGroundHolding(refs, nil); ok {
		t.Fatalf("no candidate answered %q", dir)
	}
}

// THE RECEIPT SAYS THE FOLDER, IN A PERSON'S WORDS. Which rung of the ladder
// answered is a log's word; what the receipt owes is where the work went and
// whose word put it there, so a ground nobody offered is seen at once.
func TestTheReceiptSaysWhereATaskWorksInWordsAndNeverTheRungsName(t *testing.T) {
	spec := taskSpec{title: "the fix"}
	for _, c := range []struct {
		stand taskStand
		want  string
	}{
		{taskStand{dir: "/work/one", rung: taskGroundBrief}, "Its copy is cut from /work/one, the one folder its brief names as ground."},
		{taskStand{dir: "/work/two", rung: taskGroundSaid}, "Its copy is cut from /work/two, the folder this proposal gave as its ground."},
		{taskStand{dir: "/work/three", rung: taskGroundSaid, kept: true}, ""},
		{taskStand{dir: "/work/four", rung: taskGroundTouched}, ""},
		{taskStand{dir: "/work/five", rung: taskGroundStandingIn}, ""},
	} {
		for _, state := range []TaskState{TaskRunning, TaskQueued} {
			receipt := taskReceipt(7, spec, state, c.stand, "")
			if c.want == "" {
				if strings.Contains(receipt, "It works in /") {
					t.Fatalf("rung %q: the receipt names a folder nobody's proposal chose:\n%s", c.stand.rung, receipt)
				}
				continue
			}
			if !strings.Contains(receipt, c.want) {
				t.Fatalf("rung %q: the receipt does not say %q:\n%s", c.stand.rung, c.want, receipt)
			}
			for _, word := range []string{"rung", "provenance", "`brief`", "`said`"} {
				if strings.Contains(receipt, word) {
					t.Fatalf("the receipt spells the ladder's own word %q:\n%s", word, receipt)
				}
			}
		}
	}
}
