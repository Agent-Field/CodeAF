package tui3

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/remote"
)

// openFixture is a far disk with one file on it and a state root of its own, so
// that nothing here writes into the developer's ~/.aforge.
func openFixture(t *testing.T) (*app, *fakeWire, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(home.EnvVar, dir)
	a, wire, _ := hostedFixture(t)
	wire.files["/srv/app/out/report.md"] = remote.FetchedFile{
		Name: "report.md", MIME: "text/markdown", Size: 12, Bytes: []byte("hello world\n"),
	}
	return a, wire, dir
}

// CACHE BY CONTENT: a file opened twice crosses the wire once.
func TestRemoteFetchPrefersTheCacheOverTheWire(t *testing.T) {
	a, wire, _ := openFixture(t)
	r := a.rfiles

	first, file, err := r.fetch("/srv/app/out/report.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(file.Bytes) != "hello world\n" || file.Name != "report.md" || file.MIME != "text/markdown" {
		t.Fatalf("the first fetch answered %#v", file)
	}
	second, again, err := r.fetch("/srv/app/out/report.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(wire.fetched()) != 1 {
		t.Fatalf("the bytes crossed %d times", len(wire.fetched()))
	}
	if first.ref != second.ref || string(again.Bytes) != "hello world\n" {
		t.Fatalf("the cached answer differs: %#v vs %#v", first, second)
	}
	// AND THE KEY IS THE SURFACE'S OWN ARITHMETIC. A content-addressed store
	// whose keys are somebody else's hash is not content-addressed.
	sum := sha256.Sum256([]byte("hello world\n"))
	if !strings.Contains(string(first.ref), hex.EncodeToString(sum[:])) {
		t.Fatalf("the blob is not keyed by its own digest: %q", first.ref)
	}
}

// A digest that does not describe the bytes is a file that arrived damaged, and
// it is refused rather than stored under a key that is a lie.
func TestRemoteFetchRefusesBytesThatDoNotMatchTheDigest(t *testing.T) {
	a, wire, _ := openFixture(t)
	wire.files["/srv/app/out/report.md"] = remote.FetchedFile{
		Name: "report.md", Bytes: []byte("hello world\n"), Hash: strings.Repeat("a", 64),
	}
	if _, _, err := a.rfiles.fetch("/srv/app/out/report.md"); err == nil {
		t.Fatal("bytes that do not match their digest were kept")
	}
}

// THE MIRROR IS WHAT THE VIEWER IS HANDED, because a CAS blob is named by its
// digest and a window title reading `3f9a…c17b` names nothing.
func TestRemoteOpenHandsTheViewerAFileWithItsOwnName(t *testing.T) {
	a, _, dir := openFixture(t)
	opened := ""
	restore := processOpener
	processOpener = func(target string) error { opened = target; return nil }
	t.Cleanup(func() { processOpener = restore })

	if err := a.rfiles.open("/srv/app/out/report.md"); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "v3", "remote", "mirror", "devbox", "srv", "app", "out", "report.md")
	if opened != want {
		t.Fatalf("the platform was handed %q, wanted %q", opened, want)
	}
	body, err := os.ReadFile(opened)
	if err != nil || string(body) != "hello world\n" {
		t.Fatalf("the mirror does not hold the file: %v %q", err, body)
	}
	// A SECOND OPEN IS THE SAME OBJECT, not a second copy: the mirror is a
	// hardlink to the blob wherever the filesystem allows one.
	if err := a.rfiles.open("/srv/app/out/report.md"); err != nil {
		t.Fatal(err)
	}
}

// The engine's refusal is the only account of the engine's law this side has,
// so it is said EXACTLY as it arrived.
func TestRemoteOpenSaysTheEnginesOwnSentence(t *testing.T) {
	a, _, _ := openFixture(t)
	refusal := "engine: big.iso is 40MB and the most one file may cross this connection is 16MB"
	a.rfiles.opening["/srv/app/big.iso"] = true
	a.remoteOpened(remoteOpenedMsg{target: "/srv/app/big.iso", err: errors.New(refusal)})
	last := a.entries[len(a.entries)-1]
	if last.kind != entryNote || last.text != refusal {
		t.Fatalf("the refusal was rewritten as %q", last.text)
	}
	if a.rfiles.opening["/srv/app/big.iso"] {
		t.Fatal("the flow is still marked in flight")
	}
}

// THE EMPTINESS LAW OWNS THE WAIT: a fetch that finished says nothing at all.
func TestRemoteOpenIsSilentWhenItWorks(t *testing.T) {
	a, _, _ := openFixture(t)
	a.entries = nil
	a.rfiles.opening["/srv/app/out/report.md"] = true
	a.remoteOpened(remoteOpenedMsg{target: "/srv/app/out/report.md"})
	if len(a.entries) != 0 {
		t.Fatalf("a fetch that worked wrote %d rows", len(a.entries))
	}
	// And the quiet window's own clock says nothing once the flow is done.
	a.remoteOpenSlow(remoteOpenSlowMsg{target: "/srv/app/out/report.md"})
	if len(a.entries) != 0 {
		t.Fatalf("the quiet note fired for a finished fetch: %q", a.entries[0].text)
	}
}

// Past the window it draws ONE line, and the weight rides along when this
// surface already knows it.
func TestRemoteOpenNotesOneLineWhenItIsSlow(t *testing.T) {
	a, _, _ := openFixture(t)
	a.entries = nil
	a.rfiles.opening["/srv/app/out/big.csv"] = true
	a.rfiles.learn("/srv/app/out/big.csv", remoteFact{file: true, size: 4 << 20})
	a.remoteOpenSlow(remoteOpenSlowMsg{target: "/srv/app/out/big.csv"})
	if len(a.entries) != 1 {
		t.Fatalf("the slow note drew %d rows", len(a.entries))
	}
	if got := a.entries[0].text; got != "fetching big.csv — 4.0 MB" {
		t.Fatalf("the slow note reads %q", got)
	}
}

// And with no weight known it says the file and nothing else, which is the
// emptiness law rather than a missing figure.
func TestRemoteOpenSlowNoteDrawsNoWeightItDoesNotHave(t *testing.T) {
	a, _, _ := openFixture(t)
	a.entries = nil
	a.rfiles.opening["/srv/app/out/x.log"] = true
	a.remoteOpenSlow(remoteOpenSlowMsg{target: "/srv/app/out/x.log"})
	if got := a.entries[0].text; got != "fetching x.log" {
		t.Fatalf("the slow note reads %q", got)
	}
}

// A name that is not a path inside the far workspace is refused before anything
// is asked of the connection.
func TestRemoteOpenRefusesANameOutsideTheFarWorkspace(t *testing.T) {
	a, wire, _ := openFixture(t)
	a.openRemotePath("../../etc/passwd")
	last := a.entries[len(a.entries)-1]
	if last.kind != entryNote || last.text != filesNotAPathWord {
		t.Fatalf("it said %q", last.text)
	}
	if len(wire.fetched()) != 0 {
		t.Fatalf("it asked the connection anyway: %v", wire.fetched())
	}
}

// The host becomes a directory name and nothing else: an ssh destination is
// allowed a user, a colon and a port, and none of those may name a place.
func TestHostDirNameIsANameAndNotAPath(t *testing.T) {
	for from, want := range map[string]string{
		"devbox":         "devbox",
		"me@devbox:2222": "me-devbox-2222",
		// Every separator becomes a dash and the leading dots are trimmed, so
		// what comes out is a NAME — one directory under the mirror, never a
		// climb out of it.
		"../../etc": "etc",
		"..":        "",
		"":          "",
		"a/b":       "a-b",
	} {
		if got := hostDirName(from); got != want {
			t.Fatalf("hostDirName(%q) is %q, wanted %q", from, got, want)
		}
	}
}
