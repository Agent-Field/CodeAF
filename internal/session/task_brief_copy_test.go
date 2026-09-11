package session

// THE MAP FROM THE FOLDER THE WORK IS ABOUT ONTO THE FOLDER THE WORK HAPPENS
// IN, pinned. [taskCopy.bind] is the whole of issue #566's repair, and the two
// halves of it are equally load-bearing: an address at or below the ground
// becomes the copy's, and an address that is NOT is left exactly as written so
// that nothing outside the copy can be rewritten into something writable.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A GROUND AND ITS DESCENDANTS ARE THE COPY'S; EVERYTHING ELSE IS ITSELF.
func TestBindRewritesOnlyWholePathsAtOrBelowTheGround(t *testing.T) {
	own := newTaskCopy("/x/repo", "/s/trees/1", "")
	// AND THE SAME TWO FOLDERS SPELLED WITH THEIR TRAILING SEPARATORS ARE THE
	// SAME COPY. [newTaskCopy] stores one spelling of a folder, so a ground
	// handed over as `/x/repo/` binds its descendants exactly as `/x/repo` does
	// — rather than matching its own trailing slash and rewriting nothing.
	slashed := newTaskCopy("/x/repo/", "/s/trees/1/", "")

	for _, one := range []struct {
		why  string
		own  taskCopy
		text string
		want string
	}{
		{"the ground itself is the copy", own, "/x/repo", "/s/trees/1"},
		{"a descendant keeps its relative suffix", own, "/x/repo/internal/widget.go", "/s/trees/1/internal/widget.go"},
		{"a trailing separator in the text keeps it", own, "/x/repo/", "/s/trees/1/"},
		{"a ground stored with a trailing separator is still the ground", slashed, "/x/repo", "/s/trees/1"},
		{"a ground stored with a trailing separator binds its descendants", slashed, "/x/repo/internal/widget.go", "/s/trees/1/internal/widget.go"},
		{"a ground stored with a trailing separator leaves a sibling alone", slashed, "/x/repo-old/go.mod", "/x/repo-old/go.mod"},
		{"a path said mid-sentence is still a path", own, "change /x/repo/internal/widget.go so it says new", "change /s/trees/1/internal/widget.go so it says new"},
		{"a quoted path is still a path", own, `read "/x/repo/go.mod"`, `read "/s/trees/1/go.mod"`},
		{"every occurrence moves, not just the first", own, "/x/repo and /x/repo/go.mod", "/s/trees/1 and /s/trees/1/go.mod"},
		{"an occurrence past a longer name still moves", own, "/x/repo-old/go.mod and /x/repo/go.mod", "/x/repo-old/go.mod and /s/trees/1/go.mod"},
		{"an unrelated absolute path is untouched", own, "/y/other/thing.go", "/y/other/thing.go"},
		{"a sibling sharing a name prefix is untouched", own, "/x/repo-old/internal/widget.go", "/x/repo-old/internal/widget.go"},
		{"a longer name that merely ends the same way is untouched", own, "/y/x/repo/internal/widget.go", "/y/x/repo/internal/widget.go"},
		{"a name the ground is only a suffix of is untouched", own, "/x/repository/go.mod", "/x/repository/go.mod"},
		{"nothing at all is nothing at all", own, "", ""},
	} {
		if got := one.own.bind(one.text); got != one.want {
			t.Errorf("%s: bind(%q) = %q, want %q", one.why, one.text, got, one.want)
		}
	}
}

// A GROUND OF `/` IS NOT A COPY OF ANYTHING, and that is the decision this type
// makes rather than an accident of the boundary rule. The whole machine is not a
// folder the work is about, so [taskCopy.real] says no and neither the rewrite
// nor the section that explains it does anything at all.
func TestTheWholeMachineIsNotAGround(t *testing.T) {
	own := newTaskCopy("/", "/s/trees/1", "")
	if own.real() {
		t.Fatalf("the whole machine was taken for a folder the work is a copy of: %+v", own)
	}
	address := "read /etc/hosts and write /x/repo/go.mod"
	if got := own.bind(address); got != address {
		t.Fatalf("a ground of / rewrote every address on the machine: %q", got)
	}
}

// AN OUTSIDE PATH CANNOT BECOME AN INSIDE ONE. This is the law that keeps
// [taskGroundGuard] honest: a contract that really does name another repository
// still earns the refusal, because binding never invents a way in.
func TestBindNeverTurnsAnOutsideAddressIntoAnInsideOne(t *testing.T) {
	for _, one := range []struct {
		why     string
		own     taskCopy
		address string
	}{
		{"another person's checkout", newTaskCopy("/x/repo", "/s/trees/1", ""), "/Users/you/code/theirs/NOTES.md"},
		{"a sibling repository sharing the name", newTaskCopy("/x/repo", "/s/trees/1", ""), "/x/repo-old/go.mod"},
		{"a home-relative address nobody resolved", newTaskCopy("/x/repo", "/s/trees/1", ""), "~/elsewhere/x"},
		{"a ground that is the whole machine", newTaskCopy("/", "/s/trees/1", ""), "/etc/hosts"},
		{"a ground stored with a trailing separator", newTaskCopy("/x/repo/", "/s/trees/1", ""), "/x/repo-old/go.mod"},
		{"a prefix that only matches partway", newTaskCopy("/x/repos", "/s/trees/1", ""), "/x/repo/go.mod"},
		{"the ground in the middle of a longer name", newTaskCopy("/x/repo", "/s/trees/1", ""), "/y/x/repo-mirror/go.mod"},
		{"a URL-shaped occurrence", newTaskCopy("/x/repo", "/s/trees/1", ""), "file:///x/repo/a.go"},
	} {
		got := one.own.bind(one.address)
		if got != one.address {
			t.Errorf("%s: bind moved an address that was never under the ground: bind(%q) = %q", one.why, one.address, got)
		}
		if strings.HasPrefix(got, one.own.dir) {
			t.Errorf("%s: an outside address now begins with the copy: bind(%q) = %q", one.why, one.address, got)
		}
	}
}

// A REFERENCE BINDS NOTHING, and that is the promise rather than an omission:
// its folder is deliberately not a copy of its ground, which it may only read.
// IN PLACE and FOLDER are the identity, and the identity is spelled as no copy.
func TestOnlyADirectoryThatIsACopyOfItsGroundBinds(t *testing.T) {
	address := "/x/repo/internal/widget.go"
	for _, one := range []struct {
		mode  TaskMode
		tree  taskTree
		binds bool
	}{
		{TaskModeWorktree, taskTree{ground: "/x/repo", dir: "/s/trees/1", mode: TaskModeWorktree}, true},
		{TaskModeMirror, taskTree{ground: "/x/repo", dir: "/s/trees/1", mode: TaskModeMirror}, true},
		{TaskModeReference, taskTree{ground: "/x/repo", dir: "/s/trees/1", mode: TaskModeReference}, false},
		{TaskModeInPlace, taskTree{ground: "/x/repo", dir: "/x/repo", mode: TaskModeInPlace}, false},
		{TaskModeFolder, taskTree{ground: "/x/repo", dir: "/x/repo", mode: TaskModeFolder}, false},
	} {
		own := taskCopyFor(one.tree)
		if own.real() != one.binds {
			t.Errorf("%s: real() = %v, want %v", one.mode, own.real(), one.binds)
		}
		got := own.bind(address)
		want := address
		if one.binds {
			want = "/s/trees/1/internal/widget.go"
		}
		if got != want {
			t.Errorf("%s: bind(%q) = %q, want %q", one.mode, address, got, want)
		}
	}
	// AND A TREE NOBODY RESOLVED IS THE DOCUMENT AS IT HAS ALWAYS BEEN, which is
	// what [TaskNode.instruction]'s seven non-worker readers are handed.
	if got := taskCopyFor(taskTree{}).bind(address); got != address {
		t.Errorf("the zero tree bound something: %q", got)
	}
}

// THE PERSON'S WORDS ARE QUOTED, NEVER EDITED — so the copy is STATED instead,
// once, and only where the folder was actually named.
func TestTheBriefStatesTheCopyWhereverTheGroundIsNamedAndNowhereElse(t *testing.T) {
	own := newTaskCopy("/x/repo", "/s/trees/1", "")

	// The person typed the path; the model wrote the contract from it.
	opening := composeBrief(briefWhole, "fix /x/repo/internal/widget.go", "change /x/repo/internal/widget.go",
		"/x/repo/internal/widget.go", "/x/repo/internal/widget.go says new", "", AdmissionContext{}, taskOrigin{}, own)
	if !strings.Contains(opening, briefCopyHeading) || !strings.Contains(opening, briefCopyRule) {
		t.Fatalf("the copy was never stated:\n%s", opening)
	}
	if !strings.Contains(opening, "fix /x/repo/internal/widget.go") {
		t.Fatalf("the person's own sentence was rewritten:\n%s", opening)
	}
	for _, want := range []string{
		briefWorkHeading + "\n\nchange /s/trees/1/internal/widget.go",
		briefMakeHeading + "\n\n/s/trees/1/internal/widget.go",
		briefDoneHeading + "\n\n/s/trees/1/internal/widget.go says new",
	} {
		if !strings.Contains(opening, want) {
			t.Fatalf("the contract was not bound to the copy — missing %q:\n%s", want, opening)
		}
	}
	// The section comes BEFORE the first address it explains.
	if strings.Index(opening, briefCopyHeading) > strings.Index(opening, briefWorkHeading) {
		t.Fatalf("the copy is stated after the addresses it is about:\n%s", opening)
	}

	// AND A BRIEF THAT NEVER SPELLED THE FOLDER OUT GETS THE DOCUMENT IT HAS
	// ALWAYS GOT — the emptiness law, and the reason this is not unconditional.
	quiet := composeBrief(briefWhole, "make the widget say new", "change internal/widget.go", "internal/widget.go", "it says new", "", AdmissionContext{}, taskOrigin{}, own)
	if strings.Contains(quiet, briefCopyHeading) {
		t.Fatalf("a brief that named no folder got a section about one:\n%s", quiet)
	}

	// AND NEITHER DOES A BRIEF WHOSE ONLY OCCURRENCE OF THE FOLDER IS INSIDE A
	// LONGER NAME. `/x/repo-old` is a different repository, so binding rightly
	// leaves every address in this contract exactly as written — and a section
	// saying the addresses below are the worker's own would be stating a mapping
	// that did not happen.
	sibling := composeBrief(briefWhole, "fix /x/repo-old/internal/widget.go", "change /x/repo-old/internal/widget.go",
		"/x/repo-old/internal/widget.go", "/x/repo-old/internal/widget.go says new", "", AdmissionContext{}, taskOrigin{}, own)
	if strings.Contains(sibling, briefCopyHeading) {
		t.Fatalf("a brief that named only a sibling repository was told its addresses had moved:\n%s", sibling)
	}
	if !strings.Contains(sibling, "change /x/repo-old/internal/widget.go") {
		t.Fatalf("a sibling repository's address was rewritten:\n%s", sibling)
	}

	// AND THE ORIGIN POINTER IS NEVER BOUND: a journal address lives outside
	// every worktree, so a bound one would name a file that is not there.
	journal := taskOrigin{journal: "/x/repo/.aforge/v3/sessions/abc.jsonl", line: 12}
	pointed := composeBrief(briefWhole, "", "change /x/repo/internal/widget.go", "", "", "", AdmissionContext{}, journal, own)
	if !strings.Contains(pointed, "/x/repo/.aforge/v3/sessions/abc.jsonl") {
		t.Fatalf("the pointer at the person's own journal was moved into the copy:\n%s", pointed)
	}
}

// AND THE CONVERSATION'S OWN FOLDER BINDS TOO, into a folder of the same name at
// the copy's root.
//
// THE DEFECT THIS PINS (measured 2026-09-10). A conversation opened nowhere in
// particular keeps its deliverables in `<session>/work`, which is what
// landing.go's [deliverablesDir] answers when anything asks where a finished
// document goes — so the handoff wrote that address into WHAT TO PRODUCE. It is
// not under the ground, the ground rule rightly left it alone, and the worker
// standing in `<session>/trees/1` was refused at its first write with "is
// outside your copy". It then invented `<copy>/work` for itself, wrote there and
// reported the deviation. That is the right address; it should have been in the
// contract rather than discovered by being refused.
func TestTheConversationsOwnFolderIsWrittenAsThePathInsideTheCopy(t *testing.T) {
	own := newTaskCopy("/x/repo", "/s/trees/1", "/s/work")

	for _, one := range []struct {
		why  string
		own  taskCopy
		text string
		want string
	}{
		{"the folder itself is the copy's own", own, "/s/work", "/s/trees/1/work"},
		{"a deliverable under it keeps its suffix", own, "/s/work/notes.md", "/s/trees/1/work/notes.md"},
		{"a path said mid-sentence is still a path", own, "write /s/work/notes.md", "write /s/trees/1/work/notes.md"},
		{"the ground still binds beside it", own, "/x/repo/go.mod", "/s/trees/1/go.mod"},
		{"both folders in one sentence both move", own,
			"read /x/repo/go.mod and write /s/work/notes.md",
			"read /s/trees/1/go.mod and write /s/trees/1/work/notes.md"},
		// AND THE LOOKALIKE RULE HOLDS FOR IT EXACTLY AS FOR THE GROUND: a
		// sibling that merely begins the same way is a different directory.
		{"a sibling sharing the name prefix is untouched", own, "/s/work-old/notes.md", "/s/work-old/notes.md"},
		{"a longer name it is only a suffix of is untouched", own, "/other/s/work/notes.md", "/other/s/work/notes.md"},
	} {
		if got := one.own.bind(one.text); got != one.want {
			t.Errorf("%s: bind(%q) = %q, want %q", one.why, one.text, got, one.want)
		}
	}

	// A SESSION FOLDER THE GROUND OR THE COPY ALREADY HOLDS IS NOT A SECOND
	// FOLDER, and the constructor drops it — or an address under it would be
	// moved by the ground rule and then moved again by this one, landing
	// somewhere neither folder has.
	for _, one := range []struct {
		why  string
		own  taskCopy
		text string
		want string
	}{
		{"a work folder under the ground is the ground's", newTaskCopy("/s", "/s/trees/1", "/s/work"),
			"/s/work/notes.md", "/s/trees/1/work/notes.md"},
		{"a work folder that IS the ground is the ground's", newTaskCopy("/s/work", "/s/trees/1", "/s/work"),
			"/s/work/notes.md", "/s/trees/1/notes.md"},
		{"a work folder inside the copy is already the worker's own", newTaskCopy("/x/repo", "/s/trees/1", "/s/trees/1/work"),
			"/s/trees/1/work/notes.md", "/s/trees/1/work/notes.md"},
		{"a work folder that CONTAINS the copy is dropped, not applied", newTaskCopy("/x/repo", "/s/trees/1", "/s"),
			"/x/repo/internal/widget.md", "/s/trees/1/internal/widget.md"},
		{"the whole machine is not a session folder", newTaskCopy("/x/repo", "/s/trees/1", "/"),
			"/etc/hosts", "/etc/hosts"},
		{"a session that keeps no folder of its own binds nothing extra", newTaskCopy("/x/repo", "/s/trees/1", ""),
			"/s/work/notes.md", "/s/work/notes.md"},
	} {
		if got := one.own.bind(one.text); got != one.want {
			t.Errorf("%s: bind(%q) = %q, want %q", one.why, one.text, got, one.want)
		}
	}
}

// AND THE WORKER IS TOLD, because the sentence that used to end the section is
// false about exactly this folder once it binds: "a path that is not under the
// ground stands as written" was the whole rule, and a worker that trusted it
// would go on writing at an address it is still refused.
func TestTheBriefStatesTheConversationsOwnFolderWhereverItIsNamed(t *testing.T) {
	own := newTaskCopy("/x/repo", "/s/trees/1", "/s/work")

	opening := composeBrief(briefWhole, "write me the flow document", "read /x/repo/internal/widget.go and write it up",
		"/s/work/flow.md", "/s/work/flow.md is there", "", AdmissionContext{}, taskOrigin{}, own)
	for _, want := range []string{
		briefCopyHeading,
		"This conversation's own folder, /s/work, is the one exception.",
		"the same path under /s/trees/1/work",
		briefMakeHeading + "\n\n/s/trees/1/work/flow.md",
		briefDoneHeading + "\n\n/s/trees/1/work/flow.md is there",
	} {
		if !strings.Contains(opening, want) {
			t.Fatalf("the document is missing %q:\n%s", want, opening)
		}
	}
	// AND THE PERSON'S OWN SENTENCE IS STILL QUOTED AS THEY TYPED IT. This is
	// the law the whole binding rests on: only the model-authored half moves.
	quoted := composeBrief(briefPiece, "put it in /s/work/flow.md please", "write it up",
		"/s/work/flow.md", "it is there", "", AdmissionContext{}, taskOrigin{}, own)
	if !strings.Contains(quoted, "put it in /s/work/flow.md please") {
		t.Fatalf("the person's own path was rewritten:\n%s", quoted)
	}
	// AND A BRIEF THAT NAMES ONLY THAT FOLDER STILL GETS THE SECTION, because an
	// address in it HAS moved and a worker told nothing about why would be
	// reading a path that appears nowhere it can check.
	if !strings.Contains(quoted, briefCopyHeading) {
		t.Fatalf("a brief whose only moved address was the conversation's folder was told nothing:\n%s", quoted)
	}
	// AND A SESSION WITH NO FOLDER OF ITS OWN GETS THE DOCUMENT IT ALWAYS GOT —
	// no extra sentence, and nothing at all where no folder was named.
	borrowed := newTaskCopy("/x/repo", "/s/trees/1", "")
	plain := composeBrief(briefWhole, "fix /x/repo/internal/widget.go", "change /x/repo/internal/widget.go",
		"", "", "", AdmissionContext{}, taskOrigin{}, borrowed)
	if strings.Contains(plain, "is the one exception") {
		t.Fatalf("a borrowed session was told about a folder it does not have:\n%s", plain)
	}
	quiet := composeBrief(briefWhole, "make the widget say new", "change internal/widget.go",
		"", "", "", AdmissionContext{}, taskOrigin{}, own)
	if strings.Contains(quiet, briefCopyHeading) {
		t.Fatalf("a brief that named no folder at all got a section about one:\n%s", quiet)
	}
}

// AND THE TREE IS WHERE THE CONVERSATION'S FOLDER COMES FROM, so nothing has to
// be threaded to reach it: a tree already carries the session it belongs to.
func TestTheCopyTakesTheConversationsFolderFromTheTree(t *testing.T) {
	place := Place{Dir: "/s", Owned: true}
	tree := taskTree{ground: "/x/repo", dir: "/s/trees/1", mode: TaskModeWorktree, place: place}
	if got := taskCopyFor(tree).bind("/s/work/notes.md"); got != "/s/trees/1/work/notes.md" {
		t.Errorf("a worktree under an owned session did not bind its folder: %q", got)
	}
	// A BORROWED SESSION KEEPS NO work/ AT ALL, which is itself the record of
	// which kind it was ([Place.Work]), so there is nothing here to bind.
	borrowed := tree
	borrowed.place = Place{Dir: "/s"}
	if got := taskCopyFor(borrowed).bind("/s/work/notes.md"); got != "/s/work/notes.md" {
		t.Errorf("a borrowed session bound a folder it does not keep: %q", got)
	}
	// AND THE LEGACY LAYOUT CARRIES THE ZERO PLACE, which is the same answer.
	legacy := tree
	legacy.place = Place{}
	if got := taskCopyFor(legacy).bind("/s/work/notes.md"); got != "/s/work/notes.md" {
		t.Errorf("the legacy layout invented a session folder: %q", got)
	}
}

// A PATH ALREADY INSIDE THE COPY STAYS PUT. This is the disagreement the
// work-folder bind can invent: the ground rule writes `/s/trees/1/…`, a second
// rule whose folder is an ancestor of that copy then moves it again, and the
// worker following the address it was given is refused as outside the copy.
//
// THE CONSTRUCTOR DROPS THAT ANCESTOR, and this pins both the drop and the
// ordinary sibling (`work/` next to `trees/`) so a path the guard will accept
// cannot be rewritten into one it will not.
func TestBindingTheConversationsFolderDoesNotMoveAPathAlreadyInsideTheCopy(t *testing.T) {
	place := Place{Dir: "/s", Owned: true}
	tree := taskTree{ground: "/x/repo", dir: "/s/trees/1", mode: TaskModeWorktree, place: place}
	own := taskCopyFor(tree)
	already := "/s/trees/1/internal/widget.md"
	if got := own.bind(already); got != already {
		t.Fatalf("a path already inside the copy was moved: bind(%q) = %q", already, got)
	}
	if got := own.bind("/x/repo/internal/widget.md"); got != already {
		t.Fatalf("the ground path did not bind to the copy: %q", got)
	}
	if got := own.bind("/s/work/notes.md"); got != "/s/trees/1/work/notes.md" {
		t.Fatalf("the conversation folder did not bind: %q", got)
	}

	// AND A FOLDER THAT CONTAINS THE COPY MUST NOT BE A SECOND MAP. Handing
	// the session directory itself as work/ is the shape that rewrote
	// `/s/trees/1/…` into `/s/trees/1/s/…` before the drop.
	swallowed := newTaskCopy("/x/repo", "/s/trees/1", "/s")
	if swallowed.work != "" {
		t.Fatalf("a work folder that contains the copy was kept: %+v", swallowed)
	}
	if got := swallowed.bind(already); got != already {
		t.Fatalf("an ancestor work folder moved a path already inside the copy: %q", got)
	}
	if got := swallowed.bind("/x/repo/internal/widget.md"); got != already {
		t.Fatalf("dropping the ancestor also dropped the ground map: %q", got)
	}
}

// AND ANOTHER TREE OF THIS CONVERSATION BINDS TOO, to the same path inside
// the copy this task was given.
//
// THE DEFECT THIS PINS (#839). The conversation learns the address of a
// task's private copy from the first worker's report, so the model that
// proposes the next piece of work writes `<session>/trees/1/…` into the
// contract. That path is not under the ground and not under work/, so both
// earlier rules left it as written, and the worker standing in
// `<session>/trees/2` was refused "is outside your copy" about a directory
// the harness itself had invented. Another tree of the same conversation is
// a copy of the same folder; the equivalent address is the same path under
// this one.
func TestAnotherTreeOfTheSameConversationIsWrittenAsThePathInsideThisCopy(t *testing.T) {
	place := Place{Dir: "/s", Owned: true}
	own := taskCopyFor(taskTree{ground: "/x/repo", dir: "/s/trees/2", mode: TaskModeWorktree, place: place})

	for _, one := range []struct {
		why  string
		text string
		want string
	}{
		{"another tree itself is this copy", "/s/trees/1", "/s/trees/2"},
		{"a file under another tree keeps its suffix", "/s/trees/1/internal/widget.go", "/s/trees/2/internal/widget.go"},
		{"a path said mid-sentence is still a path", "finish /s/trees/1/internal/widget.go", "finish /s/trees/2/internal/widget.go"},
		{"the worker's own copy is left as written", "/s/trees/2/internal/widget.go", "/s/trees/2/internal/widget.go"},
		{"the ground still binds beside it", "/x/repo/go.mod", "/s/trees/2/go.mod"},
		{"the conversation's own folder still binds beside it", "/s/work/notes.md", "/s/trees/2/work/notes.md"},
		{"both a sibling tree and the ground in one sentence both move",
			"the last attempt left /s/trees/1/internal/widget.go unfinished; the project is /x/repo/internal/widget.go",
			"the last attempt left /s/trees/2/internal/widget.go unfinished; the project is /s/trees/2/internal/widget.go"},
		{"the trees folder itself is not a tree", "/s/trees", "/s/trees"},
		{"a sibling sharing the name prefix is untouched", "/s/trees-old/1/widget.go", "/s/trees-old/1/widget.go"},
		{"a longer name it is only a suffix of is untouched", "/other/s/trees/1/widget.go", "/other/s/trees/1/widget.go"},
		{"a genuinely other repository is untouched", "/Users/you/code/theirs/NOTES.md", "/Users/you/code/theirs/NOTES.md"},
	} {
		if got := own.bind(one.text); got != one.want {
			t.Errorf("%s: bind(%q) = %q, want %q", one.why, one.text, got, one.want)
		}
	}

	// THE GROUND RULE WINS WHERE BOTH COULD APPLY. A trees folder that sits
	// under the ground is dropped, so `/s/trees/1/…` is the ground's to bind
	// and becomes this copy's `trees/1/…` rather than this copy's `…`.
	underGround := taskCopyFor(taskTree{ground: "/s", dir: "/s/trees/2", mode: TaskModeWorktree, place: place})
	if got, want := underGround.bind("/s/trees/1/internal/widget.go"), "/s/trees/2/trees/1/internal/widget.go"; got != want {
		t.Errorf("ground did not win over a sibling tree: bind() = %q, want %q", got, want)
	}

	// A LEGACY TREE CARRIES THE ZERO PLACE, so there is no trees folder to
	// bind and a sibling address stands as written — the document this
	// composed before the session had a folder of its own.
	legacy := taskCopyFor(taskTree{ground: "/x/repo", dir: "/s/trees/2", mode: TaskModeWorktree, place: Place{}})
	if got := legacy.bind("/s/trees/1/internal/widget.go"); got != "/s/trees/1/internal/widget.go" {
		t.Errorf("the legacy layout invented a trees folder: %q", got)
	}
}

// AND THE WORKER IS TOLD, because the sentence that used to end the section
// is false about another tree of this conversation once it binds: "a path
// that is not under the ground stands as written" was the whole rule, and a
// worker that trusted it would go on writing at an address it is still
// refused.
func TestTheBriefStatesAnotherTreeOfThisConversationWhereverItIsNamed(t *testing.T) {
	place := Place{Dir: "/s", Owned: true}
	own := taskCopyFor(taskTree{ground: "/x/repo", dir: "/s/trees/2", mode: TaskModeWorktree, place: place})

	opening := composeBrief(briefWhole, "that did not work, try again", "the last attempt left /s/trees/1/internal/widget.go unfinished",
		"/s/trees/1/internal/widget.go", "/s/trees/1/internal/widget.go says new", "", AdmissionContext{}, taskOrigin{}, own)
	for _, want := range []string{
		briefCopyHeading,
		"Another task in this conversation may have a copy of its own under " + place.Trees() + ", and those are an exception too.",
		"the same path under /s/trees/2",
		briefMakeHeading + "\n\n/s/trees/2/internal/widget.go",
		briefDoneHeading + "\n\n/s/trees/2/internal/widget.go says new",
	} {
		if !strings.Contains(opening, want) {
			t.Fatalf("the document is missing %q:\n%s", want, opening)
		}
	}
	// AND THE PERSON'S OWN SENTENCE IS STILL QUOTED AS THEY TYPED IT.
	quoted := composeBrief(briefPiece, "finish what is in /s/trees/1/internal/widget.go please", "finish it",
		"/s/trees/1/internal/widget.go", "it says new", "", AdmissionContext{}, taskOrigin{}, own)
	if !strings.Contains(quoted, "finish what is in /s/trees/1/internal/widget.go please") {
		t.Fatalf("the person's own path was rewritten:\n%s", quoted)
	}
	if !strings.Contains(quoted, briefCopyHeading) {
		t.Fatalf("a brief whose only moved address was another tree was told nothing:\n%s", quoted)
	}
}

// AND ANOTHER TREE SPELLED THROUGH A SYMLINK IS STILL THAT TREE. The trees
// folder is spelled the way [Place.Trees] resolves it while a contract's
// addresses are spelled the way their author was standing, and on a Mac those
// two differ by a `/private` nobody typed — which is exactly the machine the
// full tagged package that found #839 runs on. A rewrite comparing bytes alone
// would leave the worker pointed at the other tree, which is the whole defect
// with one spelling changed. The link is made by hand rather than borrowed from
// the machine, so this holds on every platform.
func TestBindFollowsAnAliasSpellingOfAnotherTree(t *testing.T) {
	root := t.TempDir()
	trees := filepath.Join(root, "trees")
	if err := os.MkdirAll(filepath.Join(trees, "1", "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A lookalike beside it, reachable through its own link: the whole-path rule
	// holds for an alias as it does for the folder's own spelling, or a rewrite
	// would invent a way into a directory nobody asked about.
	if err := os.MkdirAll(filepath.Join(root, "trees-old", "1"), 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(trees, alias); err != nil {
		t.Skipf("this filesystem does not make symlinks: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "trees-old"), alias+"-old"); err != nil {
		t.Fatal(err)
	}

	// THE COPY IS SPELLED THE WAY THE ENGINE SPELLS IT, which is canonically:
	// [taskOwnFolder] resolves the directory before git ever registers it, so
	// the folder that decides "this is my own copy" and the folder an alias
	// resolves to are one spelling.
	copyDir := canonicalPath(filepath.Join(trees, "2"))
	own := newTaskCopyOf("/x/repo", copyDir, "", canonicalPath(trees))
	inCopy := func(parts ...string) string {
		return filepath.Join(append([]string{copyDir}, parts...)...)
	}
	for _, one := range []struct {
		why  string
		text string
		want string
	}{
		{"another tree through an alias is this copy", filepath.Join(alias, "1"), copyDir},
		{"a file under it keeps its suffix", filepath.Join(alias, "1", "internal", "widget.go"), inCopy("internal", "widget.go")},
		{"a path said mid-sentence is still a path", "finish " + filepath.Join(alias, "1", "internal", "widget.go"),
			"finish " + inCopy("internal", "widget.go")},
		{"this worker's own copy through an alias is left as written", filepath.Join(alias, "2", "internal", "widget.go"),
			filepath.Join(alias, "2", "internal", "widget.go")},
		{"the trees folder itself through an alias is not a tree", alias, alias},
		{"a lookalike alias is untouched", filepath.Join(alias+"-old", "1", "go.mod"), filepath.Join(alias+"-old", "1", "go.mod")},
	} {
		if got := own.bind(one.text); got != one.want {
			t.Errorf("%s: bind(%q) = %q, want %q", one.why, one.text, got, one.want)
		}
	}

	// AND THE OTHER WAY ROUND, which is the spelling the defect actually
	// arrives in: the session names its own folder however it was opened while
	// the copies inside it are named as git resolved them, so the address a
	// worker reports — and the next contract carries — is the resolved one.
	throughAlias := newTaskCopyOf("/x/repo", copyDir, "", alias)
	for _, one := range []struct {
		why  string
		text string
		want string
	}{
		{"a resolved address under another tree still binds", filepath.Join(trees, "1", "internal", "widget.go"), inCopy("internal", "widget.go")},
		{"the alias spelling binds beside it", filepath.Join(alias, "1", "internal", "widget.go"), inCopy("internal", "widget.go")},
	} {
		if got := throughAlias.bind(one.text); got != one.want {
			t.Errorf("%s: bind(%q) = %q, want %q", one.why, one.text, got, one.want)
		}
	}
}

// A FOLDER SPELLED THROUGH A SYMLINK IS STILL THE FOLDER. A contract's addresses
// are spelled the way their author was standing while the ground is spelled the
// way git resolves it, and a bind comparing bytes alone left the worker pointed
// at the folder its copy was made of. The link is made by hand rather than
// borrowed from the machine, so this holds on every platform.
func TestBindFollowsAnAliasSpellingOfTheGround(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(filepath.Join(real, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A sibling whose name begins the same way, reachable through its own link:
	// the lookalike rule holds for an alias as it does for the ground's own
	// spelling, or a rewrite would invent a way into a folder nobody asked about.
	if err := os.MkdirAll(filepath.Join(root, "real-old"), 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("this filesystem does not make symlinks: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "real-old"), alias+"-old"); err != nil {
		t.Fatal(err)
	}

	copyDir := filepath.Join(root, "trees", "1")
	own := newTaskCopy(canonicalPath(real), copyDir, "")
	inCopy := func(parts ...string) string {
		return filepath.Join(append([]string{copyDir}, parts...)...)
	}
	for _, one := range []struct {
		why  string
		text string
		want string
	}{
		{"the ground through its alias is the copy", alias, copyDir},
		{"a file that is there", filepath.Join(alias, "internal"), inCopy("internal")},
		// WHAT A TASK PRODUCES IS NOT THERE YET, which is the ordinary case and
		// the one a resolver that only answers about existing paths would miss.
		{"a file the work has still to write", filepath.Join(alias, "internal", "widget.go"), inCopy("internal", "widget.go")},
		{"a whole directory the work has still to make", filepath.Join(alias, "brand", "new", "report.md"), inCopy("brand", "new", "report.md")},
		{"a path said mid-sentence is still a path", "change " + filepath.Join(alias, "internal", "widget.go") + " so it says new",
			"change " + inCopy("internal", "widget.go") + " so it says new"},
		{"a quoted path is still a path", `read "` + filepath.Join(alias, "go.mod") + `"`, `read "` + inCopy("go.mod") + `"`},
		{"a sibling reached through a lookalike alias is untouched", filepath.Join(alias+"-old", "go.mod"), filepath.Join(alias+"-old", "go.mod")},
		{"the sibling's own spelling is untouched", filepath.Join(root, "real-old", "go.mod"), filepath.Join(root, "real-old", "go.mod")},
		{"a relative name is already the worker's own and is left alone", "internal/widget.go", "internal/widget.go"},
	} {
		if got := own.bind(one.text); got != one.want {
			t.Errorf("%s: bind(%q) = %q, want %q", one.why, one.text, got, one.want)
		}
	}

	// AND THE SECTION THAT EXPLAINS THE MAPPING ASKS THE SAME QUESTION. A
	// contract bound through an alias with no copy stated would be a worker told
	// nothing about why its addresses are not the ones the person typed.
	if !namesGround(own.ground, "change "+filepath.Join(alias, "internal", "widget.go")) {
		t.Error("a contract naming the ground through an alias was read as naming no folder")
	}
	if namesGround(own.ground, "change "+filepath.Join(alias+"-old", "go.mod")) {
		t.Error("a contract naming only a lookalike sibling was read as naming the ground")
	}
}
