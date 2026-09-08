package session

// A FOLDER SOMEBODY ATTACHED HAS TO REACH THE MODEL.
//
// The defect these are written against is the one the whole wave is for: the
// picker said `folder · /home/…/thing`, the set was written onto meta.json, and
// the next request went out with no mention of the folder anywhere — so the
// model went looking for it, usually by walking a home directory for a name that
// sounded right. A displayed line is not a message.
//
// So every test here is written from the model's side: what is in front of it on
// the NEXT request, after the person did the thing they did.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// modelSees is message[0] as the next request would carry it — the base prompt
// with every block the conversation currently holds rendered into it
// ([Agent.refreshSystemLocked]). It is the whole point of these tests that this
// is read and not [Agent.placesText]: a block composed and never rendered would
// pass every assertion and reach nobody.
func modelSees(t *testing.T, a *Agent) string {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.messages) == 0 {
		t.Fatal("the conversation has no messages at all, so nothing is in front of the model")
	}
	return messageText(a.messages[0])
}

// THE FOLDER IS IN FRONT OF THE MODEL ON THE VERY NEXT REQUEST, by its exact
// absolute path, said to be the person's own act.
func TestAnAttachedFolderIsNamedToTheModelOnTheNextRequest(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	folder := filepath.Join(t.TempDir(), "the client work")
	writeFile(t, filepath.Join(folder, "notes.md"), "words\n")

	if before := modelSees(t, agent); strings.Contains(before, attachedHeading) {
		t.Fatal("a conversation with nothing attached is being told about attached folders")
	}
	if _, err := agent.ReferPlace(folder, PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}

	seen := modelSees(t, agent)
	if !strings.Contains(seen, attachedHeading) {
		t.Fatalf("the model was never told a folder was attached:\n%s", seen)
	}
	if !strings.Contains(seen, canonicalPath(folder)) {
		t.Fatalf("the model was not given the folder's own path %q:\n%s", canonicalPath(folder), seen)
	}
	// AND IT IS NOT TOLD THE WORKING DIRECTORY MOVED, because it did not: the
	// guard, the relative paths and the project are all still the workspace, and
	// a model handed a second folder with no ranking between them writes into
	// whichever one it read last.
	if !strings.Contains(seen, workspace) {
		t.Fatalf("the working directory %q is no longer named beside the attachment:\n%s", workspace, seen)
	}
	if !strings.Contains(seen, "REFERENCES AND NOT THE WORKING DIRECTORY") {
		t.Fatalf("nothing tells the model an attached folder is not where work happens:\n%s", seen)
	}
	// AND THE WORKSPACE ITSELF DID NOT MOVE, which is the product's own promise:
	// attaching is not a `cd`.
	if agent.config.Workspace != workspace {
		t.Fatalf("attaching a folder moved the working directory to %q", agent.config.Workspace)
	}
}

// A PERSON WHO CLOSES THE TERMINAL COMES BACK TO A CONVERSATION THAT STILL KNOWS.
// The set was already written down (places_test.go), and this is the half that
// was missing: the REOPENED conversation's next request names the folder too.
func TestAnAttachedFolderIsStillInFrontOfTheModelAfterAReopen(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(t.TempDir(), "reports")
	writeFile(t, filepath.Join(folder, "q3.md"), "figures\n")
	open := func(t *testing.T) *Agent {
		t.Helper()
		agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
			config.SessionFile = filepath.Join(dir, "session.jsonl")
			config.Place = Place{Dir: dir}
		})
		return agent
	}

	first := open(t)
	if _, err := first.ReferPlace(folder, PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second := open(t)
	seen := modelSees(t, second)
	if !strings.Contains(seen, canonicalPath(folder)) {
		t.Fatalf("the reopened conversation does not tell the model about %q:\n%s", canonicalPath(folder), seen)
	}
}

// REMOVING IT TAKES IT OFF THE CONVERSATION AND OUT OF THE MODEL'S CONTEXT. A
// folder indicator a person can see and cannot dismiss is a mistake they have to
// open a new conversation to correct.
func TestRemovingAFolderTakesItOutOfWhatTheModelIsTold(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(t.TempDir(), "wrong one")
	writeFile(t, filepath.Join(folder, "a.txt"), "x\n")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "session.jsonl")
		config.Place = Place{Dir: dir}
	})
	if _, err := agent.ReferPlace(folder, PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	if err := agent.RemovePlace(folder); err != nil {
		t.Fatalf("RemovePlace: %v", err)
	}

	if got := agent.Places(); len(got) != 0 {
		t.Fatalf("the folder is still on the conversation: %+v", got)
	}
	if seen := modelSees(t, agent); strings.Contains(seen, canonicalPath(folder)) ||
		strings.Contains(seen, attachedHeading) {
		t.Fatalf("the model is still being told about a folder that was removed:\n%s", seen)
	}
	// AND IT IS OFF THE DISK RECORD, so the next process does not bring it back.
	meta, err := LoadMeta(dir)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if len(meta.Places) != 0 {
		t.Fatalf("meta.json still holds %+v after the folder was removed", meta.Places)
	}
}

// A FOLDER THIS CONVERSATION IS NOT ABOUT IS REFUSED RATHER THAN IGNORED. A
// caller told "done" about a path that was never attached has been told
// something false about which folders are attached.
func TestRemovingAFolderThatWasNeverAttachedIsRefused(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	elsewhere := t.TempDir()
	err := agent.RemovePlace(elsewhere)
	if err == nil {
		t.Fatal("removing a folder nobody attached was answered as done")
	}
	if !strings.Contains(err.Error(), "not about") {
		t.Fatalf("the refusal reads %q, which does not say the conversation is not about it", err)
	}
	if err := agent.RemovePlace("  "); err == nil {
		t.Fatal("a path with nothing in it was taken as a folder")
	}
}

// A FOLDER THAT HAS SINCE BEEN DELETED IS STILL REMOVABLE. The set is a history
// and keeps a record whose directory is gone (places.go), so a remove that
// insisted on stat'ing first would leave exactly those records stuck on the
// conversation forever.
func TestAFolderThatIsGoneFromTheDiskCanStillBeRemoved(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	folder := filepath.Join(t.TempDir(), "gone")
	writeFile(t, filepath.Join(folder, "a.txt"), "x\n")
	ref, err := agent.ReferPlace(folder, PlaceSaid)
	if err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	if err := os.RemoveAll(ref.Path); err != nil {
		t.Fatalf("removing the folder from the disk: %v", err)
	}
	if err := agent.RemovePlace(ref.Path); err != nil {
		t.Fatalf("RemovePlace on a folder that is no longer there: %v", err)
	}
	if got := agent.Places(); len(got) != 0 {
		t.Fatalf("the record survived the removal: %+v", got)
	}
}

// AND WHILE IT IS STILL ATTACHED AND ALREADY GONE, the model is told so rather
// than left to spend three calls finding out.
func TestAnAttachedFolderThatIsNoLongerThereSaysSo(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	folder := filepath.Join(t.TempDir(), "vanishing")
	writeFile(t, filepath.Join(folder, "a.txt"), "x\n")
	ref, err := agent.ReferPlace(folder, PlaceSaid)
	if err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	if err := os.RemoveAll(ref.Path); err != nil {
		t.Fatalf("removing the folder from the disk: %v", err)
	}
	// The block is rebuilt on the next deliberate act; a reopened conversation is
	// one, and is the shape a person actually meets this in.
	agent.keepAttached()
	if seen := modelSees(t, agent); !strings.Contains(seen, "NOT on this disk right now") {
		t.Fatalf("a folder that is no longer there is described as though it were:\n%s", seen)
	}
}

// A GROUND THE LADDER RESOLVED IS NOT AN ATTACHMENT, and saying it was would put
// a claim about somebody's intent into their own instructions. It is also the
// cache check: a kept place must not re-price the conversation by rewriting
// message[0].
func TestAGroundTheWorkResolvedIsNotPresentedAsSomethingThePersonAttached(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	resolved := t.TempDir()
	before := modelSees(t, agent)

	agent.refer(PlaceRef{Path: canonicalPath(resolved), Arrival: PlaceKept})

	if places := agent.Places(); len(places) != 1 {
		t.Fatalf("the resolved ground did not reach the set: %+v", places)
	}
	seen := modelSees(t, agent)
	if strings.Contains(seen, attachedHeading) {
		t.Fatalf("a ground the work resolved is being called an attached folder:\n%s", seen)
	}
	if seen != before {
		t.Fatal("a resolved ground rewrote message[0], which re-prices the whole conversation for nothing")
	}
}

// TWO FOLDERS KEEP TWO SCOPES. Attaching two repositories with contradictory
// house rules is an ordinary thing to do, and a prompt that ran their rules
// together would hand the model one composite project that does not exist.
func TestTwoAttachedFoldersKeepTheirInstructionsApart(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	first := filepath.Join(t.TempDir(), "service")
	second := filepath.Join(t.TempDir(), "site")
	writeFile(t, filepath.Join(first, agentsFileName), "Tabs, always.\n")
	writeFile(t, filepath.Join(second, agentsFileName), "Spaces, always.\n")
	for _, folder := range []string{first, second} {
		if _, err := agent.ReferPlace(folder, PlaceSaid); err != nil {
			t.Fatalf("ReferPlace(%s): %v", folder, err)
		}
	}

	seen := modelSees(t, agent)
	for _, folder := range []string{first, second} {
		heading := attachedRules + canonicalPath(folder)
		if !strings.Contains(seen, heading) {
			t.Fatalf("no scoped heading %q in what the model is told:\n%s", heading, seen)
		}
		scope := "THEY HOLD FOR WORK UNDER " + canonicalPath(folder) + " AND NOWHERE ELSE"
		if !strings.Contains(seen, scope) {
			t.Fatalf("the rules quoted for %s do not say where they hold:\n%s", folder, seen)
		}
	}
	if !strings.Contains(seen, "Tabs, always.") || !strings.Contains(seen, "Spaces, always.") {
		t.Fatalf("one of the two folders' own rules never reached the model:\n%s", seen)
	}
	// The two headings are in the order the set is held in, newest first, which
	// is the order the person's own attention is in.
	if strings.Index(seen, canonicalPath(second)) > strings.Index(seen, canonicalPath(first)) {
		t.Fatal("the folders are told to the model oldest first")
	}
}

// AN ATTACHED FOLDER'S RULES ARE BOUNDED AND THE CUT IS NAMED. A chooser that
// pulled a repository's documentation into the prompt would spend somebody's
// whole context on a folder they wanted one file out of.
func TestAnAttachedFoldersOwnRulesAreBoundedAndSayWhereTheRestIs(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	folder := filepath.Join(t.TempDir(), "verbose")
	long := strings.Repeat("a rule that goes on and on.\n", (attachedFileLimit/28)+400)
	writeFile(t, filepath.Join(folder, agentsFileName), long)
	if _, err := agent.ReferPlace(folder, PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}

	seen := modelSees(t, agent)
	if len(seen) > len(long) {
		t.Fatalf("the whole %d-byte file rode into the prompt", len(long))
	}
	if !strings.Contains(seen, "the rest is on disk") {
		t.Fatalf("the prompt was cut and never says so:\n%s", promptTail(seen))
	}
	if !strings.Contains(seen, filepath.Join(canonicalPath(folder), agentsFileName)) {
		t.Fatalf("the cut does not name the file to read the rest from:\n%s", promptTail(seen))
	}
}

// AND THE BLOCK NEVER LISTS WHAT IS INSIDE A FOLDER. An attachment is a
// reference and not a tree: the model is told how to look rather than handed a
// walk nobody asked for.
func TestTheAttachedBlockNamesTheFolderAndNeverWalksIt(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	folder := filepath.Join(t.TempDir(), "big")
	for _, name := range []string{"one.go", "two.go", "three.go"} {
		writeFile(t, filepath.Join(folder, name), "package p\n")
	}
	if _, err := agent.ReferPlace(folder, PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}

	seen := modelSees(t, agent)
	for _, name := range []string{"one.go", "two.go", "three.go"} {
		if strings.Contains(seen, name) {
			t.Fatalf("the prompt lists %s, so the folder was walked into the model's context:\n%s", name, seen)
		}
	}
	if !strings.Contains(seen, "`ls` on one of the exact paths above is the overview") {
		t.Fatalf("the model is not told how to look inside an attached folder:\n%s", seen)
	}
}

// A PATH WITH SPACES AND WITH LETTERS NOBODY'S KEYBOARD HAS survives whole. The
// model is handed the path it will pass to a tool, so a path mangled here is a
// tool call that fails on the second turn for a reason nobody can see.
func TestAPathWithSpacesAndUnicodeReachesTheModelExactly(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	folder := filepath.Join(t.TempDir(), "Ünïcode dir — with spaces")
	writeFile(t, filepath.Join(folder, "a.txt"), "x\n")
	ref, err := agent.ReferPlace(folder, PlaceSaid)
	if err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	if ref.Path != canonicalPath(folder) {
		t.Fatalf("the path came back %q, want %q", ref.Path, canonicalPath(folder))
	}
	if seen := modelSees(t, agent); !strings.Contains(seen, canonicalPath(folder)) {
		t.Fatalf("the exact path never reached the model:\n%s", seen)
	}
}

// AND A CHOSEN SUBDIRECTORY IS NOT MISREPRESENTED. The root snap is real — a
// ground is cut from a repository and not from a directory inside it — so what
// the model is told is the path the conversation actually gained, and the
// caller's own answer says the same thing rather than the surface reporting one
// path while the engine holds another.
func TestChoosingASubdirectoryOfARepositoryReportsTheRootItSnappedTo(t *testing.T) {
	repo := newTestRepo(t)
	inside := filepath.Join(repo, "internal", "session")
	writeFile(t, filepath.Join(inside, "a.go"), "package session\n")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	ref, err := agent.ReferPlace(inside, PlaceSaid)
	if err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	if ref.Path != canonicalPath(repo) {
		t.Fatalf("the answer is %q, want the repository root %q — the caller cannot report what it does not get back", ref.Path, canonicalPath(repo))
	}
	seen := modelSees(t, agent)
	if !strings.Contains(seen, canonicalPath(repo)) {
		t.Fatalf("the model was not told the folder it actually gained:\n%s", seen)
	}
	if strings.Contains(seen, canonicalPath(inside)) {
		t.Fatalf("the model is told about a subdirectory the conversation did not attach:\n%s", seen)
	}
}

// THE SET RIDES THE CONVERSATION'S OWN PHOTOGRAPH. It is what a surface across a
// connection draws the folder indicator from, and it is on [Facts] rather than
// behind a reading of its own because that indicator is drawn on a frame
// (internal/remote's replica.go states the law).
func TestTheConversationsFoldersRideItsFacts(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	folder := filepath.Join(t.TempDir(), "attached")
	writeFile(t, filepath.Join(folder, "a.txt"), "x\n")

	if places := agent.Facts().Places; len(places) != 0 {
		t.Fatalf("a fresh conversation photographs %+v", places)
	}
	if _, err := agent.ReferPlace(folder, PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	places := agent.Facts().Places
	if len(places) != 1 || places[0].Path != canonicalPath(folder) || places[0].Arrival != PlaceSaid {
		t.Fatalf("the photograph holds %+v, want the folder the person attached", places)
	}
}

// promptTail is the end of a long prompt, for a failure message that has to be
// read. It is not replay_test.go's `tail`, which is about a transcript.
func promptTail(text string) string {
	if len(text) <= 1200 {
		return text
	}
	return "…" + text[len(text)-1200:]
}
