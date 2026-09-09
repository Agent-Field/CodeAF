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
	// AND WHAT THEY POINTED AT IS NOT LOST. The snap is real and it must not be
	// silent: the record keeps the directory they chose beside the project they
	// gained, and the model is told both.
	if ref.Chose != canonicalPath(inside) {
		t.Fatalf("the record kept %q as what the person pointed at, want %q", ref.Chose, canonicalPath(inside))
	}
	seen := modelSees(t, agent)
	if !strings.Contains(seen, canonicalPath(repo)) {
		t.Fatalf("the model was not told the folder it actually gained:\n%s", seen)
	}
	if !strings.Contains(seen, "they pointed at "+canonicalPath(inside)+" inside it") {
		t.Fatalf("the model was not told which directory the person actually pointed at:\n%s", seen)
	}
	// AND CHOOSING THE PROJECT ITSELF CLAIMS NO SUBDIRECTORY, so a person who
	// picks the root does not carry last week's answer around with them.
	again, err := agent.ReferPlace(repo, PlaceSaid)
	if err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	if again.Chose != "" {
		t.Fatalf("choosing the project itself still claims %q was pointed at", again.Chose)
	}
	if places := agent.Places(); len(places) != 2 || places[0].Chose != "" || places[1].Chose != canonicalPath(inside) {
		t.Fatalf("the root and the separately selected scope were not both kept: %+v", places)
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

// REMOVING A FOLDER DOES NOT THROW AWAY THE WORK WAITING IN IT. The working
// copies are their own record and they exist nowhere else (standingtree.go);
// somebody tidying an indicator must not silently discard an hour of unlanded
// changes, and the person's own folder is untouched either way.
func TestRemovingAFolderKeepsTheWorkThatIsWaitingToLand(t *testing.T) {
	repo := newTestRepo(t)
	agent, _, _ := standingLab(t, repo)
	writeThrough(t, agent, filepath.Join(repo, "shared.txt"), "the changed line\n")
	if waiting := agent.UnlandedChanges(); len(waiting) != 1 {
		t.Fatalf("the write left %+v waiting, so this test proves nothing", waiting)
	}

	if err := agent.RemovePlace(repo); err != nil {
		t.Fatalf("RemovePlace: %v", err)
	}

	waiting := agent.UnlandedChanges()
	if len(waiting) != 1 || waiting[0].Folder != canonicalPath(repo) {
		t.Fatalf("removing the folder threw away the work waiting in it: %+v", waiting)
	}
	if _, ok := agent.LandingFor(repo); !ok {
		t.Fatal("the landing can no longer be found, so the changes cannot be put back")
	}
	// AND THE PERSON'S OWN FOLDER NEVER MOVED. Removing an attachment is a change
	// to this conversation and to nothing on their disk.
	if got := readFile(t, filepath.Join(repo, "shared.txt")); got != "the original line\n" {
		t.Fatalf("the person's own file now says %q", got)
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

// Uneven files leave a partial budget; the last read must spend only what
// remains rather than allowing another whole file into the next request.
func TestAttachedInstructionsRespectTheRemainingAggregateBudget(t *testing.T) {
	var refs []PlaceRef
	for _, size := range []int{3001, 3001, 3001, 4096, 100} {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, agentsFileName), strings.Repeat("§", size))
		refs = append(refs, PlaceRef{Path: dir, Arrival: PlaceSaid})
	}
	// ASCII sizes produce a partial budget, then a multibyte final file tests
	// that the cut is still valid UTF-8 at that smaller boundary.
	for _, ref := range refs[:3] {
		writeFile(t, filepath.Join(ref.Path, agentsFileName), strings.Repeat("Q", 3001))
	}
	block := attachedBlock(refs, t.TempDir())
	quoted := strings.Count(block, "Q") + 2*strings.Count(block, "§")
	if quoted > attachedFilesBudget || quoted < attachedFilesBudget-1 {
		t.Fatalf("quoted %d instruction bytes, want %d or one fewer at a rune boundary", quoted, attachedFilesBudget)
	}
	if !strings.Contains(block, "the rest is on disk") || !strings.Contains(block, filepath.Join(refs[3].Path, agentsFileName)) {
		t.Fatal("the partial-budget cut must name where the rest can be read")
	}
	if strings.ContainsRune(block, '\uFFFD') {
		t.Fatal("the partial-budget cut split a Unicode character")
	}
}

// A disk read that began before removal may finish after it. Exercise that
// ordering explicitly so this regression does not depend on scheduler timing.
func TestAStaleAttachedReadingCannotResurrectARemovedFolder(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	folder := t.TempDir()
	if _, err := agent.ReferPlace(folder, PlaceSaid); err != nil {
		t.Fatal(err)
	}
	before := agent.referredPlaces()
	stale := attachedBlock(before, workspace)
	if err := agent.RemovePlace(folder); err != nil {
		t.Fatal(err)
	}
	agent.publishAttached(before, stale)
	if seen := modelSees(t, agent); strings.Contains(seen, attachedHeading) || strings.Contains(seen, folder) {
		t.Fatalf("a completed removal was overwritten by an older disk reading: %s", seen)
	}
}

// A selected subfolder inherits its ancestors' rules, but not its siblings'.
func TestAttachedSubfolderLoadsOnlyApplicableNestedInstructions(t *testing.T) {
	repo := newTestRepo(t)
	chosen := filepath.Join(repo, "packages", "café client")
	files := map[string]string{
		filepath.Join(repo, agentsFileName):             "Root house rules.",
		filepath.Join(repo, "packages", claudeFileName): "Package house rules.",
		filepath.Join(chosen, agentsFileName):           "Selected house rules.",
		filepath.Join(repo, "sibling", agentsFileName):  "Unrelated sibling rules.",
	}
	for file, content := range files {
		writeFile(t, file, content)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if _, err := agent.ReferPlace(chosen, PlaceSaid); err != nil {
		t.Fatal(err)
	}
	seen := modelSees(t, agent)
	previous := -1
	for _, dir := range []string{repo, filepath.Join(repo, "packages"), chosen} {
		scope := "THEY HOLD FOR WORK UNDER " + canonicalPath(dir) + " AND NOWHERE ELSE"
		at := strings.Index(seen, scope)
		if at <= previous {
			t.Fatalf("missing or out-of-order scoped rules %q in next-request instructions", scope)
		}
		previous = at
	}
	for file, content := range files {
		want := !strings.Contains(file, "sibling")
		if strings.Contains(seen, content) != want {
			t.Fatalf("instruction inclusion for %s: want %v", file, want)
		}
	}
}

// Two scopes share a working ground without replacing one another's context.
func TestTwoSelectedSubdirectoriesRemainIndependentAttachments(t *testing.T) {
	repo := newTestRepo(t)
	first, second := filepath.Join(repo, "one space"), filepath.Join(repo, "日本語")
	writeFile(t, filepath.Join(first, "AGENTS.md"), "FIRST_SCOPE_RULE")
	writeFile(t, filepath.Join(second, "AGENTS.md"), "SECOND_SCOPE_RULE")
	dir := t.TempDir()
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.SessionFile = filepath.Join(dir, "session.jsonl")
		c.Place = Place{Dir: dir}
	})
	for _, path := range []string{first, second, first} {
		if _, err := a.ReferPlace(path, PlaceSaid); err != nil {
			t.Fatal(err)
		}
	}
	if got := a.Places(); len(got) != 2 {
		t.Fatalf("selected scopes collapsed or duplicated: %+v", got)
	}
	stand := a.resolveTaskGround(taskSpec{deliverable: "the fix", acceptance: "tests pass"})
	if stand.ask != "" || stand.dir != canonicalPath(repo) {
		t.Fatalf("one repository became two grounds: %+v", stand)
	}
	a.keepGround(stand)
	if got := a.Places(); len(got) != 2 {
		t.Fatalf("resolving work changed the selected scopes: %+v", got)
	}
	for _, word := range []string{"FIRST_SCOPE_RULE", "SECOND_SCOPE_RULE"} {
		if !strings.Contains(modelSees(t, a), word) {
			t.Fatalf("missing %s", word)
		}
	}
	if err := a.RemovePlace(first); err != nil {
		t.Fatal(err)
	}
	if got := a.Places(); len(got) != 1 || got[0].Chose != canonicalPath(second) {
		t.Fatalf("removal affected the other scope: %+v", got)
	}
	seen := modelSees(t, a)
	if strings.Contains(seen, "FIRST_SCOPE_RULE") || !strings.Contains(seen, "SECOND_SCOPE_RULE") {
		t.Fatalf("removal composed wrong scoped rules: %s", seen)
	}
	persisted := loadPlaces(dir)
	if len(persisted) != 1 || persisted[0].Chose != canonicalPath(second) {
		t.Fatalf("saved selection differs: %+v", persisted)
	}
	if err := os.RemoveAll(second); err != nil {
		t.Fatal(err)
	}
	if err := a.RemovePlace(second); err != nil {
		t.Fatal(err)
	}
	if len(a.Places()) != 0 {
		t.Fatal("deleted scope cannot be removed")
	}
}
