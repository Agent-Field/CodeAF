package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── the normalizer ──────────────────────────────────────────────────────────

// THE TABLE IS THE SPEC. Every row here is a shape taken off real material —
// a grep flavour refusing a regex, a Go build, a macOS binary killed on launch,
// the shell's own exit wrapper, a path-bearing failure — and the two ways this
// can be wrong pull in opposite directions: a rule that strips too much makes
// two different errors one key, and a rule that strips too little makes one
// error two.
func TestTheSignatureStripsWhatVariesAndKeepsWhatDiscriminates(t *testing.T) {
	for _, row := range []struct {
		name string
		line string
		want string
	}{{
		name: "a regex flavour naming a byte offset",
		line: "ugrep: error at position 5 (empty (sub)expression)",
		want: "ugrep: error at position n (empty (sub)expression)",
	}, {
		name: "a go build error with a file, a line and a column",
		line: "internal/session/loop.go:412:2: undefined: noteToolOutcome",
		want: "<path>:n: undefined: notetooloutcome",
	}, {
		name: "the same go error from an absolute path",
		line: "/Users/x/code/aforge/internal/session/loop.go:9:1: undefined: noteToolOutcome",
		want: "<path>:n: undefined: notetooloutcome",
	}, {
		name: "a killed binary",
		line: "./bin/aforge: Killed: 9",
		want: "<path>: killed: n",
	}, {
		name: "a bare slash-word is not a path",
		line: "read: Input/output error",
		want: "read: input/output error",
	}, {
		name: "a path-bearing failure",
		line: "cat: /var/folders/9k/T/x-1234/notes.txt: No such file or directory",
		want: "cat: <path>: no such file or directory",
	}, {
		name: "a quoted literal",
		line: "error: pathspec 'feat/fix-store' did not match any file(s) known to git",
		want: "error: pathspec <q> did not match any file(s) known to git",
	}, {
		name: "an apostrophe is not a quote",
		line: "git: 'brnch' is not a git command. See 'git --help'.",
		want: "git: <q> is not a git command. see <q>.",
	}, {
		name: "a commit hash and an address",
		line: "fatal: bad object 4f3a9c1de0b2 at 0x7ffee4b0",
		want: "fatal: bad object <hex> at <hex>",
	}, {
		name: "whitespace is collapsed",
		line: "make:   ***   [build]   Error 2",
		want: "make: *** [build] error n",
	}} {
		t.Run(row.name, func(t *testing.T) {
			if got := fixNormalize(row.line); got != row.want {
				t.Fatalf("fixNormalize(%q)\n got %q\nwant %q", row.line, got, row.want)
			}
		})
	}
}

// Two occurrences of one error must land on ONE key, and two different errors
// must not. This is the whole bargain the normalizer is struck for.
func TestOneErrorIsOneKeyAndTwoErrorsAreTwo(t *testing.T) {
	first, ok := fixSignature("bash", "internal/a/x.go:12:3: undefined: foo\n\nCommand exited with code 1")
	if !ok {
		t.Fatal("the first build error should key on something")
	}
	second, ok := fixSignature("bash", "internal/b/y.go:998:1: undefined: foo\n\nCommand exited with code 2")
	if !ok {
		t.Fatal("the second build error should key on something")
	}
	if first != second {
		t.Fatalf("the same error on two lines should be one key:\n%q\n%q", first, second)
	}
	other, ok := fixSignature("bash", "internal/a/x.go:12:3: cannot use n (untyped string) as int\n")
	if !ok {
		t.Fatal("a different build error should key on something")
	}
	if other == first {
		t.Fatalf("two different errors collided on %q", first)
	}
}

// The tool is part of the key: the same words from two hands are two problems.
func TestTheSameWordsFromTwoHandsAreTwoKeys(t *testing.T) {
	fromBash, _ := fixSignature("bash", "no such file or directory: notes.txt")
	fromGrep, _ := fixSignature("grep", "no such file or directory: notes.txt")
	if fromBash == fromGrep {
		t.Fatal("bash and grep should not share a signature")
	}
	if fixToolOf(fromBash) != "bash" || fixToolOf(fromGrep) != "grep" {
		t.Fatalf("the tool should read back out of the key: %q %q", fromBash, fromGrep)
	}
	if fixErrorOf(fromBash) != fixErrorOf(fromGrep) {
		t.Fatal("the normalized error should be the same on both sides")
	}
}

// A failure that says only THAT it failed is a failure with no key. Keying on
// the wrapper is the collision the normalization law exists to forbid.
func TestAWrapperAloneIsNoSignature(t *testing.T) {
	for _, text := range []string{
		"Command exited with code 1",
		"Command timed out after 120 seconds",
		"Command aborted",
		"exit status 128",
		"(no output)\n\nCommand exited with code 2",
		"",
		"   \n\n  ",
		"1234\n",
	} {
		if signature, ok := fixSignature("bash", text); ok {
			t.Fatalf("%q should key on nothing; it keyed on %q", text, signature)
		}
	}
}

// The diagnostic line is chosen past Go's package header, which is the same
// string for every different error in one package.
func TestTheDiagnosticLineSkipsThePackageHeaderAndTheWrapper(t *testing.T) {
	line, found := fixDiagnosticLine("# github.com/Agent-Field/aforge-v2/internal/session\ninternal/session/x.go:4:2: undefined: foo\n\nCommand exited with code 1")
	if !found {
		t.Fatal("a build failure should have a diagnostic line")
	}
	if line != "internal/session/x.go:4:2: undefined: foo" {
		t.Fatalf("wrong line chosen: %q", line)
	}
}

// ── the store ───────────────────────────────────────────────────────────────

func testStore(t *testing.T, at time.Time) *fixStore {
	t.Helper()
	store := newFixStore(filepath.Join(t.TempDir(), fixesFileName))
	store.now = func() time.Time { return at }
	return store
}

func TestAStoreRemembersAFixAcrossAReopen(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, fixesFileName)
	signature, _ := fixSignature("bash", "ugrep: error at position 5 (empty (sub)expression)")

	first := newFixStore(path)
	first.confirm(signature, "grep -F '(sub)' .")

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the store should be on disk: %v", err)
	}
	second := newFixStore(path)
	found := second.consult(signature)
	if len(found) != 1 {
		t.Fatalf("a reopened store should still know the fix; got %d", len(found))
	}
	if found[0].Fix != "grep -F '(sub)' ." {
		t.Fatalf("wrong fix came back: %q", found[0].Fix)
	}
}

// A patch that has not earned it says nothing at all. Silence is the product
// here: a coin toss dressed as advice is worse than no line.
func TestAStoreIsSilentBelowTheSuccessRatio(t *testing.T) {
	store := testStore(t, time.Now())
	signature, _ := fixSignature("bash", "cannot find package foo")

	store.confirm(signature, "go mod tidy")
	store.blame(signature, "go mod tidy")
	if found := store.consult(signature); len(found) != 0 {
		t.Fatalf("1/2 is below %v and should say nothing; it said %q", fixMinSuccessRatio, found[0].Fix)
	}
	store.confirm(signature, "go mod tidy")
	store.confirm(signature, "go mod tidy")
	if found := store.consult(signature); len(found) != 1 {
		t.Fatalf("3/4 is above %v and should speak", fixMinSuccessRatio)
	}
}

// One patch, and it is the one that has worked best. Two more that also pass
// the gate stay unsaid.
func TestAStoreOffersOnlyTheBestPatch(t *testing.T) {
	store := testStore(t, time.Now())
	signature, _ := fixSignature("bash", "no space left on device while linking")

	for i := 0; i < 5; i++ {
		store.confirm(signature, "the good one")
	}
	store.confirm(signature, "the thin one")
	store.confirm(signature, "the beaten one")
	store.blame(signature, "the beaten one")
	store.blame(signature, "the beaten one")

	found := store.consult(signature)
	if len(found) != fixAdviceLimit {
		t.Fatalf("at most %d patches may be offered; got %d", fixAdviceLimit, len(found))
	}
	// 5/5 and 1/1 tie on ratio; the confirmations break it, which is the whole
	// reason the tiebreak exists.
	if found[0].Fix != "the good one" {
		t.Fatalf("the best patch should win; got %q", found[0].Fix)
	}
}

// The counts are halved on the interval, and what reaches nothing is dropped.
func TestCountsAreHalvedOnTheIntervalAndStaleFixesFallOut(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, fixesFileName)
	monday := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)

	early := newFixStore(path)
	early.now = func() time.Time { return monday }
	signature, _ := fixSignature("bash", "ld: symbol(s) not found for architecture arm64")
	for i := 0; i < 8; i++ {
		early.confirm(signature, "make clean && make build")
	}
	thin, _ := fixSignature("bash", "dyld: Library not loaded somewhere")
	early.confirm(thin, "brew reinstall openssl")

	later := newFixStore(path)
	later.now = func() time.Time { return monday.Add(fixDecayInterval + time.Hour) }
	found := later.consult(signature)
	if len(found) != 1 {
		t.Fatalf("a fix confirmed eight times should survive one halving; got %d", len(found))
	}
	if found[0].ok() != 4 {
		t.Fatalf("8 confirmations should halve to 4; got %d", found[0].ok())
	}
	if gone := later.consult(thin); len(gone) != 0 {
		t.Fatalf("a fix confirmed once and never again should be gone; got %q", gone[0].Fix)
	}

	// And the halving is on the file, not only in this session's head.
	written := readFixDocument(path)
	if len(written.Entries) != 1 || written.Entries[0].OK != 4 {
		t.Fatalf("the halved counts should be on disk: %+v", written.Entries)
	}
}

// The counters are the product metric, and they are in the file so the question
// can be answered with `cat`.
func TestTheStoreCountsWhatItWasAskedAndWhatItAnswered(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, fixesFileName)
	store := newFixStore(path)

	known, _ := fixSignature("bash", "ugrep: error at position 5 (empty (sub)expression)")
	unknown, _ := fixSignature("bash", "something nobody has ever seen before here")
	store.confirm(known, "grep -F")
	store.consult(known)
	store.consult(unknown)
	store.consult(unknown)
	store.outcome(true)
	store.outcome(false)

	document := readFixDocument(path)
	if document.Asked != 3 || document.Found != 1 {
		t.Fatalf("asked/found should be 3/1; got %d/%d", document.Asked, document.Found)
	}
	if document.Worked != 1 || document.Failed != 1 {
		t.Fatalf("worked/failed should be 1/1; got %d/%d", document.Worked, document.Failed)
	}
	if document.Type != "fixes" || document.Version != fixesFileVersion {
		t.Fatalf("the document should stamp itself: %q v%d", document.Type, document.Version)
	}
}

// ── the write ───────────────────────────────────────────────────────────────

// The rename is the one write that cannot half-happen, and nothing is left
// beside the file afterwards.
func TestTheWriteIsAtomicAndLeavesNoLitter(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, fixesFileName)
	store := newFixStore(path)
	signature, _ := fixSignature("bash", "permission denied while opening the port")
	store.confirm(signature, "sudo lsof -i :8080")

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read the directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != fixesFileName {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("only the store should be left behind; found %v", names)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the store: %v", err)
	}
	var document fixDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("the store should be whole JSON: %v", err)
	}
}

// An unwritable place is dropped in silence. A session whose disk is full is
// still a session.
func TestAnUnwritableStoreIsSilent(t *testing.T) {
	directory := t.TempDir()
	blocked := filepath.Join(directory, "wall")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write the wall: %v", err)
	}
	store := newFixStore(filepath.Join(blocked, "under", fixesFileName))
	signature, _ := fixSignature("bash", "some error worth keying on here")
	store.confirm(signature, "the fix")
	if found := store.consult(signature); len(found) != 1 {
		t.Fatal("the in-memory store should still work when the disk refuses")
	}
}

// A store file that is nonsense is a store that starts empty rather than a
// session that will not start.
func TestAMangledStoreStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), fixesFileName)
	if err := os.WriteFile(path, []byte("{not json at all"), 0o600); err != nil {
		t.Fatalf("write the mangled store: %v", err)
	}
	store := newFixStore(path)
	signature, _ := fixSignature("bash", "an error that keys on something")
	if found := store.consult(signature); len(found) != 0 {
		t.Fatal("a mangled store should know nothing")
	}
	store.confirm(signature, "the fix")
	if found := readFixDocument(path); len(found.Entries) != 1 {
		t.Fatalf("the store should have been rewritten whole; got %+v", found.Entries)
	}
}

// ── two sessions, one project ───────────────────────────────────────────────

// Two terminals open on one project are two processes on one file. Neither may
// silently undo what the other learned — which for a store made entirely of
// accumulated counts is the feature turning itself off.
func TestTwoSessionsOnOneProjectStoreBothKeepTheirCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), fixesFileName)
	signature, _ := fixSignature("bash", "ld: symbol(s) not found for architecture arm64")

	// Both load the same (empty) file before either writes, which is the losing
	// interleaving for a whole-file last-writer-wins.
	left, right := newFixStore(path), newFixStore(path)
	left.mu.Lock()
	left.loadLocked()
	left.mu.Unlock()
	right.mu.Lock()
	right.loadLocked()
	right.mu.Unlock()

	left.confirm(signature, "make clean && make build")
	right.confirm(signature, "make clean && make build")

	document := readFixDocument(path)
	if len(document.Entries) != 1 {
		t.Fatalf("one fix, one entry; got %d", len(document.Entries))
	}
	if document.Entries[0].OK != 2 {
		t.Fatalf("both confirmations should survive the merge; got %d", document.Entries[0].OK)
	}

	// And a fix only one of them knows survives the other's write.
	mine, _ := fixSignature("bash", "no space left on device while linking")
	left.confirm(mine, "rm -rf /tmp/build")
	right.confirm(signature, "make clean && make build")
	document = readFixDocument(path)
	if len(document.Entries) != 2 {
		t.Fatalf("neither session's entries may be dropped; got %d", len(document.Entries))
	}
}

// The same file written from many goroutines at once stays whole and loses
// nothing. This is the batch's shape: one turn's tool calls run in parallel.
func TestConcurrentWritersKeepTheStoreWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), fixesFileName)
	signature, _ := fixSignature("bash", "ld: symbol(s) not found for architecture arm64")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store := newFixStore(path)
			store.confirm(signature, "make clean && make build")
		}()
	}
	wg.Wait()

	document := readFixDocument(path)
	if len(document.Entries) != 1 {
		t.Fatalf("eight writers, one entry; got %d", len(document.Entries))
	}
	if document.Entries[0].OK != 8 {
		t.Fatalf("every confirmation should have landed; got %d", document.Entries[0].OK)
	}
}

// ── the two scopes ──────────────────────────────────────────────────────────

// The project is asked first, and a confirmation is written to both files: the
// project's because it is precise, the machine's because it outlives the
// project being deleted.
func TestTheProjectStoreIsAskedFirstAndBothAreWritten(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AFORGE_HOME", root)
	bucket := filepath.Join(root, "v3", "projects", "some-workspace")
	shelf := newFixShelf(bucket)

	signature, _ := fixSignature("bash", "ld: symbol(s) not found for architecture arm64")
	shelf.confirm(signature, "make clean && make build")

	if _, err := os.Stat(filepath.Join(bucket, fixesFileName)); err != nil {
		t.Fatalf("the project store should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "v3", fixesFileName)); err != nil {
		t.Fatalf("the machine store should exist: %v", err)
	}

	// A patch only the machine knows is still found, which is what makes the
	// first failure in a fresh checkout cheap.
	global := newFixStore(filepath.Join(root, "v3", fixesFileName))
	elsewhere, _ := fixSignature("bash", "dyld: Library not loaded libssl")
	for i := 0; i < 2; i++ {
		global.confirm(elsewhere, "brew reinstall openssl")
	}
	fresh := newFixShelf(filepath.Join(root, "v3", "projects", "another-workspace"))
	advice := fresh.consult(elsewhere)
	if len(advice) != 1 || advice[0].patch != "brew reinstall openssl" {
		t.Fatalf("the machine store should answer where the project cannot: %+v", advice)
	}
	if advice[0].from != fresh.global {
		t.Fatal("the advice should say it came from the machine store")
	}

	// And the project's answer wins where both have one.
	project := newFixShelf(bucket)
	both, _ := fixSignature("bash", "some error both stores know about")
	project.project.confirm(both, "the project answer")
	project.global.confirm(both, "the machine answer")
	answered := project.consult(both)
	if len(answered) != 1 || answered[0].patch != "the project answer" {
		t.Fatalf("the project should be asked first: %+v", answered)
	}
}

// A conversation with no project directory still gets the machine's store.
func TestAShelfWithNoProjectStillHasTheMachineStore(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	shelf := newFixShelf("")
	if shelf.project != nil {
		t.Fatal("a session with no bucket should have no project store")
	}
	if shelf.global == nil {
		t.Fatal("every session has the machine's store")
	}
	signature, _ := fixSignature("bash", "an error worth keying on here")
	shelf.confirm(signature, "the fix")
	if advice := shelf.consult(signature); len(advice) != 1 {
		t.Fatal("the machine store should still answer")
	}
}

// ── where the files live ────────────────────────────────────────────────────

func TestTheProjectBucketIsTheOneTasksUse(t *testing.T) {
	place := filepath.Join("/state", "v3", "projects", "encoded-workspace", "session-7")
	config := Config{Place: Place{Dir: place}}
	if got, want := config.fixesBucket(), filepath.Dir(place); got != want {
		t.Fatalf("the bucket is the directory above the session folder: got %q want %q", got, want)
	}

	flat := Config{SessionFile: filepath.Join("/state", "v3", "sessions", "ws", "chat.jsonl")}
	if got, want := flat.fixesBucket(), filepath.Join("/state", "v3", "sessions", "ws"); got != want {
		t.Fatalf("the legacy layout keeps the file beside the transcript: got %q want %q", got, want)
	}

	handed := Config{fixesDir: "/handed/down", Place: Place{Dir: place}}
	if got := handed.fixesBucket(); got != "/handed/down" {
		t.Fatalf("a node uses the bucket it was handed: %q", got)
	}

	if got := (Config{}).fixesBucket(); got != "" {
		t.Fatalf("a memory-only session has no bucket: %q", got)
	}
}

// A patch is bounded and single-line on the way in, because it comes back out
// into a person's transcript.
func TestAPatchIsCleanedOnTheWayIn(t *testing.T) {
	if got := fixCleanPatch("  go build   ./...  \n"); got != "go build ./..." {
		t.Fatalf("a patch should be trimmed and collapsed: %q", got)
	}
	if got := fixCleanPatch("go build\x1b[31m ./..."); strings.Contains(got, "\x1b") {
		t.Fatalf("a patch should carry no escapes: %q", got)
	}
	long := fixCleanPatch(strings.Repeat("x", fixPatchLimit*2))
	if len(long) > fixPatchLimit {
		t.Fatalf("a patch should be clipped to %d bytes; got %d", fixPatchLimit, len(long))
	}
	if got := fixCleanPatch("cat <<EOF\nhello\nEOF"); got != "cat <<EOF hello EOF" {
		t.Fatalf("a multi-line command should survive as one line: %q", got)
	}
}
