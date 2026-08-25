package remote

// browse_test.go drives the two read-only doors onto the engine's disk — the
// listing and the stat — plus the digest a fetch now carries, and it drives
// them over [Loopback] rather than by calling the handlers: these three are the
// first methods whose whole value is on the SURFACE's side of the wire (a
// browse page, a pathlink that only lights when the far machine says the word
// is real, a cache that skips a fetch it already holds), so the test asks the
// question the way that surface will ask it.
//
// What every case here is really about is one law said three ways: the two
// roots. A listing of a place outside them is refused, a link that leaves them
// is described and never followed, and a path outside them is reported as not
// being there at all.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// browseLoop is a live client against a real engine whose workspace and session
// folder are two directories on this machine — the same two roots every door in
// file.go measures against.
func browseLoop(t *testing.T) (*Loop, string, string) {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	folder := filepath.Join(root, "session")
	for _, dir := range []string{workspace, folder} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("make %s: %v", dir, err)
		}
	}
	place := session.Place{Dir: folder, Workspace: workspace}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{model: "m"}, Workspace: workspace, Place: place}, nil
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	return loop, workspace, folder
}

func write(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func names(entries []DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name)
	}
	return out
}

// A LISTING IS DIRECTORIES THEN FILES, EACH HALF SORTED BY NAME, because that
// is the order a person reads a place in and the surface must not have to sort
// what the engine already knew. The facts beside each row — a size, a moment, a
// type — are the engine's too: they are about its disk and cannot be derived
// here.
func TestAListingIsDirectoriesThenFilesEachSortedByNameWithItsFacts(t *testing.T) {
	loop, workspace, _ := browseLoop(t)
	for _, dir := range []string{"src", "assets"} {
		if err := os.Mkdir(filepath.Join(workspace, dir), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	write(t, filepath.Join(workspace, "notes.md"), "# notes\n")
	write(t, filepath.Join(workspace, "chart.png"), "not really a png")
	write(t, filepath.Join(workspace, "a.log"), "line\n")

	listing, err := loop.Client.ListDir(workspace)
	if err != nil {
		t.Fatalf("the workspace has to list: %v", err)
	}
	if got := names(listing.Entries); strings.Join(got, ",") != "assets,src,a.log,chart.png,notes.md" {
		t.Fatalf("directories first, then files, each half by name — got %v", got)
	}
	if listing.Truncated {
		t.Fatal("five entries under the cap is not a truncated listing")
	}
	// THE PATH IS THE ENGINE'S ANSWER, resolved on the machine that owns it, so
	// the surface never has to build one out of a path it cannot see.
	if resolved, err := filepath.EvalSymlinks(workspace); err != nil || listing.Path != resolved {
		t.Fatalf("the listing has to name the path it resolved: %q against %q (%v)", listing.Path, resolved, err)
	}

	by := map[string]DirEntry{}
	for _, entry := range listing.Entries {
		by[entry.Name] = entry
	}
	if !by["src"].Dir {
		t.Fatal("a directory has to say it is one")
	}
	// A DIRECTORY'S BYTE COUNT IS A FILESYSTEM ARTIFACT and its type is nothing
	// anybody can name, so both stay empty by the emptiness law.
	if by["src"].Size != 0 || by["src"].MIME != "" {
		t.Fatalf("a directory carries no size and no type: %+v", by["src"])
	}
	if by["a.log"].Dir || by["a.log"].Size != int64(len("line\n")) {
		t.Fatalf("a file carries its own size: %+v", by["a.log"])
	}
	// The type is asserted where Go's own table has one rather than against
	// whatever /etc/mime.types this machine happens to hold — file_test.go's own
	// reasoning about the same helper.
	if by["chart.png"].MIME != "image/png" {
		t.Fatalf("a png has to be named as one: %q", by["chart.png"].MIME)
	}
	for _, entry := range listing.Entries {
		if entry.ModTime <= 0 {
			t.Fatalf("every row carries when it last changed: %+v", entry)
		}
	}
}

// A SYMLINKED ENTRY IS DESCRIBED AND NEVER FOLLOWED OUT OF THE ROOTS. Inside
// them the target describes it, because it names a place this conversation can
// reach anyway; outside them the row admits the name exists and says nothing
// that would invite a fetch [handOver] is about to refuse.
func TestALinkOutOfTheRootsIsListedAsANameAndNothingElse(t *testing.T) {
	loop, workspace, _ := browseLoop(t)
	outside := filepath.Join(t.TempDir(), "secrets.txt")
	write(t, outside, "shhh, at length")
	if err := os.Mkdir(filepath.Join(workspace, "real"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "escape.txt")); err != nil {
		t.Skipf("this filesystem has no symlinks: %v", err)
	}
	if err := os.Symlink(filepath.Join(workspace, "real"), filepath.Join(workspace, "inside")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	listing, err := loop.Client.ListDir(workspace)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	by := map[string]DirEntry{}
	for _, entry := range listing.Entries {
		by[entry.Name] = entry
	}
	escape, ok := by["escape.txt"]
	if !ok {
		t.Fatalf("the name is really there and the row has to admit it: %v", names(listing.Entries))
	}
	if escape.Dir || escape.Size != 0 {
		t.Fatalf("a link that leaves the roots is a plain name of no size: %+v", escape)
	}
	if !by["inside"].Dir {
		t.Fatalf("a link whose target stays inside is described by that target: %+v", by["inside"])
	}
	if got := names(listing.Entries); strings.Join(got, ",") != "inside,real,escape.txt" {
		t.Fatalf("the resolved kind decides which half a link sorts into: %v", got)
	}
}

// THE TAIL IS CUT AND THE LISTING SAYS SO. A listing that quietly ended early
// would be this engine lying about that machine's disk, which is the one thing
// a browse view may never do.
func TestAListingOverTheCapIsCutAndSaysItWasCut(t *testing.T) {
	loop, workspace, _ := browseLoop(t)
	// The cap is lowered rather than met, for the reason [listDirMax] is a var:
	// two thousand files is seconds of somebody's disk for a fact three files
	// establish just as well.
	was := listDirMax
	listDirMax = 3
	t.Cleanup(func() { listDirMax = was })

	for _, name := range []string{"a", "b", "c", "d", "e"} {
		write(t, filepath.Join(workspace, name+".txt"), name)
	}
	listing, err := loop.Client.ListDir(workspace)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !listing.Truncated {
		t.Fatal("a listing that dropped two rows has to say so")
	}
	if got := names(listing.Entries); strings.Join(got, ",") != "a.txt,b.txt,c.txt" {
		t.Fatalf("what is cut is the tail of the order the surface would draw: %v", got)
	}
}

// The listing answers under handOver's law and under no other, so a directory
// outside the two roots is refused in the engine's own sentence — and a file is
// refused for the one thing that differs, which is that it is not a directory.
func TestNothingOutsideTheRootsListsAndAFileIsNotADirectory(t *testing.T) {
	loop, workspace, folder := browseLoop(t)
	outside := t.TempDir()

	if _, err := loop.Client.ListDir(outside); err == nil {
		t.Fatal("a directory outside both roots was listed")
	} else if !strings.Contains(err.Error(), "outside this conversation's workspace and its own folder") {
		t.Fatalf("the refusal has to say why: %v", err)
	}
	// The session's own folder is the other root and lists like the first.
	if _, err := loop.Client.ListDir(folder); err != nil {
		t.Fatalf("the session's own folder has to list: %v", err)
	}

	write(t, filepath.Join(workspace, "report.md"), "# findings\n")
	if _, err := loop.Client.ListDir(filepath.Join(workspace, "report.md")); err == nil {
		t.Fatal("a file was listed as though it were a directory")
	} else if !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("the refusal has to name what is wrong: %v", err)
	}
	if _, err := loop.Client.ListDir(filepath.Join(workspace, "nowhere")); err == nil {
		t.Fatal("a directory that is not there listed anyway")
	}
	// A RELATIVE PATH IS THE WORKSPACE'S, which is what every relative path in
	// this conversation already means.
	if _, err := loop.Client.ListDir("."); err != nil {
		t.Fatalf("a relative path is resolved against the workspace: %v", err)
	}
}

// THE ANSWERS COME BACK IN THE ORDER THEY WERE ASKED, because the surface pairs
// them with its own rows by position — and a path this session would refuse
// reports that it is NOT THERE, which is wire.go's [PathFact] law: to a surface
// deciding whether to draw a door, a file that will refuse to open IS absent.
func TestStatKeepsTheOrderAndCallsARefusedPathAbsent(t *testing.T) {
	loop, workspace, folder := browseLoop(t)
	write(t, filepath.Join(workspace, "report.md"), "# findings\n")
	write(t, filepath.Join(folder, "transcript.jsonl"), "{}\n")
	if err := os.Mkdir(filepath.Join(workspace, "src"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "secrets.txt")
	write(t, outside, "shhh")

	asked := []string{
		filepath.Join(workspace, "report.md"),
		outside,
		filepath.Join(workspace, "src"),
		filepath.Join(workspace, "nothing-here"),
		"report.md",
		filepath.Join(folder, "transcript.jsonl"),
	}
	facts, err := loop.Client.StatPaths(asked)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if len(facts) != len(asked) {
		t.Fatalf("one answer per question: asked %d, got %d", len(asked), len(facts))
	}
	for i, fact := range facts {
		if fact.Path != asked[i] {
			t.Fatalf("answer %d is about %q and the question was %q", i, fact.Path, asked[i])
		}
	}
	want := []struct {
		exists bool
		dir    bool
	}{{true, false}, {false, false}, {true, true}, {false, false}, {true, false}, {true, false}}
	for i, expected := range want {
		if facts[i].Exists != expected.exists || facts[i].Dir != expected.dir {
			t.Fatalf("%q: exists %v dir %v, and it should be exists %v dir %v",
				asked[i], facts[i].Exists, facts[i].Dir, expected.exists, expected.dir)
		}
	}

	// An empty batch is not a round trip at all, which is the batching being
	// worth having.
	if facts, err := loop.Client.StatPaths(nil); err != nil || facts != nil {
		t.Fatalf("nothing to ask about is nothing to ask: %v %v", facts, err)
	}
}

// OVER THE CEILING THE CALL IS REFUSED AND NOT TRIMMED. A surface told about
// the first 64 of its 200 candidates would draw 136 doors that silently did not
// exist, and a wrong answer arriving quietly is worse than no answer at all.
func TestStatOverItsCeilingIsRefusedRatherThanTrimmed(t *testing.T) {
	loop, workspace, _ := browseLoop(t)
	paths := make([]string, 0, statPathsMax+1)
	for i := range statPathsMax + 1 {
		paths = append(paths, filepath.Join(workspace, fmt.Sprintf("row-%d.txt", i)))
	}
	if _, err := loop.Client.StatPaths(paths); err == nil {
		t.Fatal("a batch over the ceiling was answered anyway")
	} else if !strings.Contains(err.Error(), fmt.Sprintf("the most one call may ask is %d", statPathsMax)) {
		t.Fatalf("the refusal has to name the limit: %v", err)
	}
	// And exactly the ceiling is an ordinary call, because a limit that refused
	// its own number would be one the surface could never actually reach.
	if _, err := loop.Client.StatPaths(paths[:statPathsMax]); err != nil {
		t.Fatalf("a full batch is a legal batch: %v", err)
	}
}

// A FETCH NOW SAYS WHAT IT IS CARRYING. The digest is the one internal/cas keys
// on, so a surface that already holds the blob can answer the next click
// without asking this machine anything — which is only true if the hash is of
// the bytes that actually crossed.
func TestAFetchedFileCarriesItsOwnSizeAndDigest(t *testing.T) {
	loop, workspace, _ := browseLoop(t)
	body := strings.Repeat("deliverable\n", 500)
	write(t, filepath.Join(workspace, "made.txt"), body)

	got, err := loop.Client.FetchFile(filepath.Join(workspace, "made.txt"))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	sum := sha256.Sum256([]byte(body))
	if got.Hash != hex.EncodeToString(sum[:]) {
		t.Fatalf("the digest is of the bytes: %q against %q", got.Hash, hex.EncodeToString(sum[:]))
	}
	if got.Hash != strings.ToLower(got.Hash) {
		t.Fatalf("lowercase hex or a cache will miss on its own key: %q", got.Hash)
	}
	if got.Size != int64(len(body)) || got.Size != int64(len(got.Bytes)) {
		t.Fatalf("the size describes the file that crossed: %d of %d", got.Size, len(got.Bytes))
	}
}
