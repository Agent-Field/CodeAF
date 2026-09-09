package tui3

// WHAT DISCOVERY OWES A PERSON WHO IS WAITING.
//
// Three claims are pinned here, and each of them is a defect the folder picker
// shipped with: an unreadable directory drawn as an empty one, a background
// walk with no stop on it, and an answer to a question somebody has already
// moved on from being drawn over the answer to the one they are asking.

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// folderTree lays out one fixture: every path is made as a directory, and a
// path ending in `/.git` is made as an empty file so the directory holding it
// reads as a repository without a real one being cloned.
func folderTree(t *testing.T, paths ...string) string {
	t.Helper()
	base := t.TempDir()
	for _, path := range paths {
		full := filepath.Join(base, filepath.FromSlash(path))
		if filepath.Base(path) == ".git" {
			if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
				t.Fatalf("could not make %s: %v", path, err)
			}
			if err := os.WriteFile(full, nil, 0o600); err != nil {
				t.Fatalf("could not mark %s a repository: %v", path, err)
			}
			continue
		}
		if err := os.MkdirAll(full, 0o700); err != nil {
			t.Fatalf("could not make %s: %v", path, err)
		}
	}
	return base
}

func TestAnUnreadableFolderSaysSoInsteadOfLookingEmpty(t *testing.T) {
	// THE DEFECT THIS EXISTS FOR. `os.ReadDir` answers a permission with
	// `nil, err`, and the caller that dropped the error drew the directory as
	// `nothing below here` — which tells somebody their work is gone.
	base := folderTree(t, "shut/inside")
	shut := filepath.Join(base, "shut")
	if err := os.Chmod(shut, 0o000); err != nil {
		t.Fatalf("could not close %s: %v", shut, err)
	}
	t.Cleanup(func() { _ = os.Chmod(shut, 0o700) })

	read := folderReadDir(shut)
	if read.Err == nil {
		t.Fatalf("an unreadable folder answered %v with no error", read.Names)
	}
	if !errors.Is(read.Err, fs.ErrPermission) {
		t.Fatalf("an unreadable folder answered %v, wanted a permission", read.Err)
	}
	if got := folderReadWord(read.Err); got != folderDeniedWord {
		t.Fatalf("a permission drew %q", got)
	}
}

func TestAnEmptyFolderIsNotAnErrorAndSaysNothing(t *testing.T) {
	// The other half of the same law: a directory that is genuinely empty has
	// no error, and a read that worked says nothing at all — the emptiness law,
	// and [folderLeafWord] is the picker's own word for that case.
	base := folderTree(t, "bare")
	read := folderReadDir(filepath.Join(base, "bare"))
	if read.Err != nil || len(read.Names) != 0 {
		t.Fatalf("an empty folder answered %v / %v", read.Names, read.Err)
	}
	if got := folderReadWord(read.Err); got != "" {
		t.Fatalf("a read that worked drew %q", got)
	}
}

func TestAFolderThatIsGoneAndAFileEachSayWhichTheyAre(t *testing.T) {
	base := folderTree(t, "there")
	gone := folderReadDir(filepath.Join(base, "never"))
	if got := folderReadWord(gone.Err); got != folderMissingWord {
		t.Fatalf("a folder that is not there drew %q", got)
	}
	file := filepath.Join(base, "note.md")
	if err := os.WriteFile(file, []byte("hello"), 0o600); err != nil {
		t.Fatalf("could not write %s: %v", file, err)
	}
	read := folderReadDir(file)
	if got := folderReadWord(read.Err); got != folderNotDirWord {
		t.Fatalf("a file drew %q (from %v)", got, read.Err)
	}
}

func TestAReadPrunesTheDotFoldersUnlessTheyAreAskedFor(t *testing.T) {
	base := folderTree(t, "open", ".hidden", "node_modules", ".git")
	read := folderReadDir(base)
	if len(read.Names) != 1 || read.Names[0] != "open" {
		t.Fatalf("a plain read answered %v", read.Names)
	}
	// AND ASKING FOR THEM DOES NOT REOPEN `.git`. A dot directory somebody
	// asked to see is a folder they may browse; `.git` and `node_modules` are
	// not folders anybody browses to, and [skipDirs] still holds.
	all := folderReadHidden(base)
	if len(all.Names) != 2 {
		t.Fatalf("a hidden read answered %v, wanted open and .hidden", all.Names)
	}
	for _, name := range all.Names {
		if skipDirs[name] {
			t.Fatalf("a hidden read offered %q", name)
		}
	}
}

func TestTheWalkFindsRepositoriesAndPlainFoldersAndLeadsWithRepositories(t *testing.T) {
	// A CHOOSER THAT COULD ONLY SEE REPOSITORIES HAD DECIDED WHAT COUNTS AS
	// WORK. People keep work in folders that were never a repository, and the
	// brief asks for both.
	base := folderTree(t,
		"code/aforge-v2/.git", "code/aforge-v2/internal",
		"Documents/tax returns", "scratch",
	)
	answer := folderIndexWalk(context.Background(), folderIndexDefaults(base))
	found := map[string]bool{}
	for _, root := range answer.Roots {
		rel, err := filepath.Rel(base, root.Path)
		if err != nil {
			t.Fatalf("a root outside the base: %v", root.Path)
		}
		found[filepath.ToSlash(rel)] = root.Repo
	}
	for _, want := range []string{"code/aforge-v2", "Documents/tax returns", "scratch", "code", "Documents"} {
		if _, ok := found[want]; !ok {
			t.Errorf("the walk missed %q: found %v", want, found)
		}
	}
	if !found["code/aforge-v2"] {
		t.Error("the repository was not marked as one")
	}
	if found["scratch"] {
		t.Error("a plain folder was marked a repository")
	}
	// AND THE REPOSITORIES LEAD, because the order a source hands its
	// candidates over in is what decides ties inside the picker's own layer.
	if len(answer.Roots) == 0 || !answer.Roots[0].Repo {
		t.Fatalf("the walk did not lead with a repository: %v", folderIndexPaths(answer))
	}
	if answer.Denied != 0 || answer.Bound {
		t.Fatalf("a walk of a small tree was refused %d and bound %v", answer.Denied, answer.Bound)
	}
	if got := folderIndexWord(answer); got != "" {
		t.Fatalf("a walk that saw everything said %q", got)
	}
}

func TestTheWalkNeverDescendsIntoARepositoryOrASkippedName(t *testing.T) {
	// A repository's own subdirectories are not other repositories, and the
	// ones that are — a submodule, a vendored checkout — are reached by typing
	// a path, which is what typing a path is for.
	base := folderTree(t,
		"repo/.git", "repo/inner/deeper", "repo/vendor/thing/.git",
		"node_modules/pkg", ".cache/thing",
	)
	answer := folderIndexWalk(context.Background(), folderIndexDefaults(base))
	for _, root := range answer.Roots {
		rel, _ := filepath.Rel(base, root.Path)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "repo/") {
			t.Errorf("the walk went inside a repository: %q", rel)
		}
		if strings.HasPrefix(rel, "node_modules") || strings.HasPrefix(rel, ".cache") {
			t.Errorf("the walk entered %q", rel)
		}
	}
}

func TestAPlainFolderIsOfferedOnlyNearTheTop(t *testing.T) {
	// The plain depth is shallower than the repository depth on purpose: a
	// directory of notes four levels down is not a project, and offering every
	// one of them is how a chooser fills with rows nobody asked for.
	base := folderTree(t, "a/b/c/d/e", "a/b/c/d/e/repo/.git")
	opts := folderIndexDefaults(base)
	answer := folderIndexWalk(context.Background(), opts)
	for _, root := range answer.Roots {
		rel, _ := filepath.Rel(base, root.Path)
		if !root.Repo && folderDepth(base, root.Path) > opts.Plain {
			t.Errorf("a plain folder %d levels down was offered: %q", folderDepth(base, root.Path), rel)
		}
	}
	// And the repository below that depth is still found, because Depth is
	// what bounds a repository and Plain is not.
	deep := false
	for _, root := range answer.Roots {
		if root.Repo {
			deep = true
		}
	}
	if !deep {
		t.Fatalf("the walk missed the repository at six levels: %v", folderIndexPaths(answer))
	}
}

// folderWide makes enough directories to exercise discovery limits without
// relying on any folders outside this test.
func folderWide(t *testing.T, n int) string {
	t.Helper()
	paths := make([]string, 0, n)
	for at := 0; at < n; at++ {
		paths = append(paths, "d"+strconv.Itoa(1000+at))
	}
	return folderTree(t, paths...)
}

func TestTheWalkStopsOnItsCapAndSaysThatItDid(t *testing.T) {
	base := folderWide(t, 40)
	opts := folderIndexDefaults(base)
	opts.Cap = 5
	answer := folderIndexWalk(context.Background(), opts)
	if len(answer.Roots) != 5 {
		t.Fatalf("a cap of 5 collected %d roots", len(answer.Roots))
	}
	if !answer.Bound {
		t.Fatal("a walk that stopped on its cap did not say so")
	}
	// AN INDEX THAT IS A PREFIX OF THE TRUTH IS A DIFFERENT THING FROM ONE
	// THAT IS THE TRUTH, and only the answer can say which is in hand.
	if got := folderIndexWord(answer); got == "" {
		t.Fatal("a bound walk said nothing about being bound")
	}
}

func TestTheWalkStopsOnItsClockAndOnACancel(t *testing.T) {
	base := folderWide(t, 192)

	opts := folderIndexDefaults(base)
	opts.Budget = time.Nanosecond
	if answer := folderIndexWalk(context.Background(), opts); !answer.Bound {
		t.Fatalf("a walk with no time left ran to the end: %d roots", len(answer.Roots))
	}

	// AND A CANCEL IS THE SAME STOP. The surface that started the walk may have
	// been closed, and a background walk nobody is waiting for is work taken
	// off a machine somebody is still using.
	stopped, stop := context.WithCancel(context.Background())
	stop()
	fresh := folderIndexDefaults(base)
	if answer := folderIndexWalk(stopped, fresh); !answer.Bound {
		t.Fatalf("a cancelled walk ran to the end: %d roots", len(answer.Roots))
	}
}

func TestTheWalkCountsWhatItWasRefusedAndSaysSo(t *testing.T) {
	// NEVER EQUATE PERMISSION DENIED WITH EMPTY, said of the walk rather than
	// of one read: a home directory with unreadable folders in it is a machine
	// where this list is quietly incomplete, and nobody can be told that by a
	// list of what was found.
	base := folderTree(t, "open/inside", "shut/inside")
	shut := filepath.Join(base, "shut")
	if err := os.Chmod(shut, 0o000); err != nil {
		t.Fatalf("could not close %s: %v", shut, err)
	}
	t.Cleanup(func() { _ = os.Chmod(shut, 0o700) })

	answer := folderIndexWalk(context.Background(), folderIndexDefaults(base))
	if answer.Denied != 1 {
		t.Fatalf("the walk counted %d refusals, wanted one", answer.Denied)
	}
	word := folderIndexWord(answer)
	if !strings.Contains(word, "1 folder ") || !strings.Contains(word, "could not be read") {
		t.Fatalf("a refused walk said %q", word)
	}
	// AND IT STILL ANSWERED. An index that refused to exist because of one
	// permission is worth less than an index with one directory missing.
	if len(answer.Roots) == 0 {
		t.Fatal("one refusal emptied the whole walk")
	}
}

func TestAStaleListingIsRecognisedByItsGeneration(t *testing.T) {
	base := folderTree(t, "here/one", "here/two")
	msg, ok := folderReadCmd(filepath.Join(base, "here"), 7)().(folderReadMsg)
	if !ok {
		t.Fatal("a read off the loop did not answer a listing")
	}
	if len(msg.read.Names) != 2 || msg.read.Err != nil {
		t.Fatalf("the listing was %v / %v", msg.read.Names, msg.read.Err)
	}
	if folderStale(msg, 7) {
		t.Fatal("an answer to the question being asked read as stale")
	}
	// SOMEBODY WALKING `→ → ←` FAST IS STANDING IN A DIRECTORY THEY HAVE
	// ALREADY BEEN IN, so an answer about the right path can still be an answer
	// to the wrong moment. Only the counter can tell them apart.
	if !folderStale(msg, 8) {
		t.Fatal("an answer from before the surface moved on was drawn anyway")
	}
	if !folderStale(folderReadMsg{read: msg.read, gen: 6}, 7) {
		t.Fatal("an answer from two moments ago was drawn anyway")
	}
	// And the command is a command, which is what keeps the read off the loop.
	var cmd tea.Cmd = folderReadCmd(base, 1)
	if cmd == nil {
		t.Fatal("the read is not a command")
	}
}

func TestTheListingCacheHoldsARefusalAndDropsTheOldest(t *testing.T) {
	var cache folderReadCache
	if _, ok := cache.get("/nowhere"); ok {
		t.Fatal("an empty cache answered")
	}
	// A DIRECTORY SOMEBODY MAY NOT READ IS STILL AN ANSWER, and asking the
	// disk again on every keystroke would be paying repeatedly for the same
	// refusal.
	refused := folderRead{Dir: "/shut", Err: fs.ErrPermission}
	cache.put(refused)
	got, ok := cache.get("/shut")
	if !ok || !errors.Is(got.Err, fs.ErrPermission) {
		t.Fatalf("the cache lost a refusal: %v %v", got, ok)
	}

	for at := 0; at < folderReadKeep; at++ {
		cache.put(folderRead{Dir: "/d" + strconv.Itoa(at)})
	}
	if _, ok := cache.get("/shut"); ok {
		t.Fatal("the cache grew past its bound rather than dropping the oldest")
	}
	if len(cache.order) != folderReadKeep || len(cache.by) != folderReadKeep {
		t.Fatalf("the cache holds %d keys and %d listings", len(cache.order), len(cache.by))
	}
	// Putting a directory twice does not hold two places in the order.
	cache.put(folderRead{Dir: "/d0", Names: []string{"again"}})
	if len(cache.order) != folderReadKeep {
		t.Fatalf("a second put grew the order to %d", len(cache.order))
	}
	cache.drop("/d0")
	if _, ok := cache.get("/d0"); ok {
		t.Fatal("a dropped directory was still held")
	}
	if len(cache.order) != folderReadKeep-1 {
		t.Fatalf("a drop left %d keys in the order", len(cache.order))
	}
}

func TestTheWalkOffTheLoopCarriesItsGeneration(t *testing.T) {
	base := folderTree(t, "one/.git", "two")
	msg, ok := folderIndexCmd(folderIndexDefaults(base), 3)().(folderIndexMsg)
	if !ok {
		t.Fatal("a walk off the loop did not answer an index")
	}
	if msg.gen != 3 {
		t.Fatalf("the walk came back stamped %d", msg.gen)
	}
	if len(msg.answer.Roots) == 0 {
		t.Fatalf("the walk found nothing under %s", base)
	}
	if msg.answer.Took <= 0 {
		t.Fatal("the walk did not say what it spent")
	}
}

func TestAWalkWithNoBaseDoesNothingRatherThanWalkingTheDisk(t *testing.T) {
	answer := folderIndexWalk(context.Background(), folderIndexOpts{})
	if len(answer.Roots) != 0 || answer.Denied != 0 || answer.Bound {
		t.Fatalf("a walk with no base answered %v", answer)
	}
}
