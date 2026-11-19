package vaultsync

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/keys"
)

// BLIND PASS-THROUGH MOVES A CHAT'S WITHHELD FILES WITHOUT THE VAULT. These
// tests are written from what a person and the files door can observe: which
// files a page offers, in what order, how a received file lands, and what is
// never written or said. They never read how Offer and Receive do it.

// passAt dates a file in a test: whole seconds, so every filesystem keeps the
// time exactly and two dates can be compared for equality.
func passAt(seconds int) time.Time { return time.Unix(1_700_000_000+int64(seconds), 0) }

// carrier is the Files of this workspace: it lists exactly the paths the
// workspace withholds, in the order they were saved.
func (w *workspace) carrier() Files {
	return Files{
		Root: w.root, Chat: "chat",
		Withheld: func() ([]string, error) { return append([]string(nil), w.withheld...), nil },
	}
}

// listPages walks every page of a folder's offer by its cursor and answers the
// pages and the more flag each one gave. It fails the test on an endless walk.
func listPages(t *testing.T, f Files, budget int) ([][]File, []bool) {
	t.Helper()
	var pages [][]File
	var mores []bool
	after := ""
	for range 1000 {
		got, more, err := f.Offer(after, budget)
		must(t, err)
		if len(got) == 0 {
			if more {
				t.Fatal("an empty page said more files remain")
			}
			return pages, mores
		}
		pages = append(pages, got)
		mores = append(mores, more)
		after = got[len(got)-1].Path
		if !more {
			return pages, mores
		}
	}
	t.Fatal("the cursor never reached the end")
	return nil, nil
}

func pagePaths(files []File) []string {
	var out []string
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

func pagesPaths(pages [][]File) []string {
	var out []string
	for _, p := range pages {
		out = append(out, pagePaths(p)...)
	}
	return out
}

func skipWithoutSymlinks(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need a privilege on Windows")
	}
}

// tree lists every entry under root, slash separated, so a test can say that
// NOTHING else appeared: no stray file, no temporary file, no new folder.
func tree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	must(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root {
			rel, _ := filepath.Rel(root, path)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	}))
	sort.Strings(out)
	return out
}

// settle compares the paths a Received names with what the test expects, as
// sets: the order a batch was handled in is not part of the contract.
func settle(t *testing.T, label string, got, want []string) {
	t.Helper()
	g, w := append([]string(nil), got...), append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if len(g) == 0 && len(w) == 0 {
		return
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("%s = %q, want %q", label, g, w)
	}
}

func wantReceived(t *testing.T, got Received, written, same, newer, refused []string) {
	t.Helper()
	settle(t, "Written", got.Written, written)
	settle(t, "Same", got.Same, same)
	settle(t, "Newer", got.Newer, newer)
	settle(t, "Refused", got.Refused, refused)
}

func wantModTime(t *testing.T, path string, want time.Time) {
	t.Helper()
	info, err := os.Stat(path)
	must(t, err)
	if !info.ModTime().Equal(want) {
		t.Fatalf("%s is dated %v, want %v", path, info.ModTime(), want)
	}
}

func wantPermission(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	must(t, err)
	if info.Mode().Perm() != want {
		t.Fatalf("%s has mode %v, want %v", path, info.Mode().Perm(), want)
	}
}

// ---- Offer ----

func TestOfferPagesFilesInSortedOrderUnderABudget(t *testing.T) {
	w := &workspace{root: t.TempDir()}
	names := []string{"z.env", "a.env", "web/app/.env", "m.env", "b/c/.env.local", "k.pem", "d.env", "f.env", "x/y.env", "c.env", "e.env", "h.env"}
	for i, rel := range names {
		// every file is exactly one hundred bytes
		w.save(t, rel, fmt.Sprintf("%02d%s", i, strings.Repeat("v", 98)), 0o600, passAt(i))
	}
	want := append([]string(nil), names...)
	sort.Strings(want)

	// A page ends before the file that would pass the budget, and a budget
	// below one file still moves one file.
	for _, c := range []struct{ budget, perPage int }{
		{1, 1}, {99, 1}, {100, 1}, {199, 1}, {200, 2}, {250, 2}, {299, 2}, {300, 3}, {1 << 20, 12},
	} {
		t.Run(fmt.Sprintf("budget %d", c.budget), func(t *testing.T) {
			pages, mores := listPages(t, w.carrier(), c.budget)
			if got := pagesPaths(pages); !reflect.DeepEqual(got, want) {
				t.Fatalf("the pages carried %q, want every file once in sorted order %q", got, want)
			}
			for i, p := range pages {
				last := i == len(pages)-1
				size := 0
				for _, f := range p {
					size += len(f.Data)
				}
				if !last && len(p) != c.perPage {
					t.Fatalf("page %d holds %d files, want %d", i, len(p), c.perPage)
				}
				if len(p) > 1 && size > c.budget {
					t.Fatalf("page %d holds %d bytes of data, over the budget of %d", i, size, c.budget)
				}
				if mores[i] == last {
					t.Fatalf("page %d of %d said more=%v", i+1, len(pages), mores[i])
				}
			}
		})
	}
}

func TestOfferAlwaysMovesAtLeastOneFileWhateverTheBudget(t *testing.T) {
	w := &workspace{root: t.TempDir()}
	w.save(t, "a.env", strings.Repeat("a", 500), 0o600, passAt(0))
	w.save(t, "b.env", strings.Repeat("b", 500), 0o600, passAt(0))
	for _, budget := range []int{0, -1, 1} {
		got, more, err := w.carrier().Offer("", budget)
		must(t, err)
		if !reflect.DeepEqual(pagePaths(got), []string{"a.env"}) || !more {
			t.Fatalf("budget %d offered %q more=%v, want the first file alone and more", budget, pagePaths(got), more)
		}
		got, more, err = w.carrier().Offer("a.env", budget)
		must(t, err)
		if !reflect.DeepEqual(pagePaths(got), []string{"b.env"}) || more {
			t.Fatalf("budget %d after a.env offered %q more=%v, want the last file and no more", budget, pagePaths(got), more)
		}
	}
}

func TestOfferStartsAfterTheCursorAndNeverRepeatsIt(t *testing.T) {
	w := &workspace{root: t.TempDir()}
	for _, rel := range []string{"c.env", "a.env", "e.env", "b.env", "d.env"} {
		w.save(t, rel, "x="+rel+"\n", 0o600, passAt(0))
	}
	for _, c := range []struct {
		after string
		want  []string
	}{
		{"", []string{"a.env", "b.env", "c.env", "d.env", "e.env"}},
		{"a.env", []string{"b.env", "c.env", "d.env", "e.env"}},
		{"b.env", []string{"c.env", "d.env", "e.env"}},
		{"bb", []string{"c.env", "d.env", "e.env"}}, // a cursor that names no file
		{"e.env", nil},
		{"zzz", nil},
	} {
		got, more, err := w.carrier().Offer(c.after, 1<<20)
		must(t, err)
		if !reflect.DeepEqual(pagePaths(got), c.want) || more {
			t.Fatalf("after %q offered %q more=%v, want %q and no more", c.after, pagePaths(got), more, c.want)
		}
	}
}

func TestOfferHandsOverBytesModeAndSaveTimeExactly(t *testing.T) {
	w := &workspace{root: t.TempDir()}
	binary := "\x00\xff\xfe key \x80\r\n"
	w.save(t, ".env", "# my file\nZED='quoted'\nexport A=\"x y\"", 0o644, passAt(10))
	w.save(t, "keys/deploy.p12", binary, 0o600, passAt(20))
	w.save(t, "run.env", "C=3\n", 0o755, passAt(30))
	w.save(t, "empty.env", "", 0o600, passAt(40))
	got, more, err := w.carrier().Offer("", 1<<20)
	must(t, err)
	if more {
		t.Fatal("everything fits one page, yet more was reported")
	}
	want := []File{
		{Path: ".env", Mode: 0o644, ModTime: passAt(10), Data: []byte("# my file\nZED='quoted'\nexport A=\"x y\"")},
		{Path: "empty.env", Mode: 0o600, ModTime: passAt(40), Data: []byte{}},
		{Path: "keys/deploy.p12", Mode: 0o600, ModTime: passAt(20), Data: []byte(binary)},
		{Path: "run.env", Mode: 0o755, ModTime: passAt(30), Data: []byte("C=3\n")},
	}
	if len(got) != len(want) {
		t.Fatalf("offered %q, want %q", pagePaths(got), pagePaths(want))
	}
	for i, f := range got {
		if f.Path != want[i].Path || !bytes.Equal(f.Data, want[i].Data) || !f.ModTime.Equal(want[i].ModTime) {
			t.Fatalf("file %d = %q %q %v, want %q %q %v", i, f.Path, f.Data, f.ModTime, want[i].Path, want[i].Data, want[i].ModTime)
		}
		if runtime.GOOS != "windows" && f.Mode != want[i].Mode {
			t.Fatalf("%s has mode %v, want %v", f.Path, f.Mode, want[i].Mode)
		}
		if f.Mode != f.Mode.Perm() {
			t.Fatalf("%s has mode %v, which carries more than permission bits", f.Path, f.Mode)
		}
	}
}

func TestOfferSkipsWhatCannotTravelWithoutAnErrorAndWithoutCountingItAsMore(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	must(t, os.Mkdir(outside, 0o755))
	must(t, os.WriteFile(filepath.Join(outside, ".env"), []byte("OUTSIDE-SECRET"), 0o600))
	w := &workspace{root: filepath.Join(base, "work")}
	must(t, os.Mkdir(w.root, 0o755))

	w.save(t, "a.env", "A=1\n", 0o600, passAt(0))
	w.save(t, "b.env", "B=2\n", 0o600, passAt(0))
	// sorted after the good files: they must not make the last page say more
	w.withheld = append(w.withheld, "y-missing.env")
	must(t, os.Mkdir(filepath.Join(w.root, "z-folder"), 0o755))
	w.withheld = append(w.withheld, "z-folder")
	w.save(t, "zz-big.db", strings.Repeat("x", keys.MaxSlotValue), 0o600, passAt(0))
	// sorted before them, and not paths in this folder at all
	w.withheld = append(w.withheld, "../outside/.env", "/etc/passwd", "", ".")
	if runtime.GOOS != "windows" {
		must(t, os.Symlink(filepath.Join(outside, ".env"), filepath.Join(w.root, "zy-link.env")))
		w.withheld = append(w.withheld, "zy-link.env")
		must(t, os.Symlink(outside, filepath.Join(w.root, "zz-nested")))
		w.withheld = append(w.withheld, "zz-nested/.env")
		if os.Geteuid() != 0 {
			w.save(t, "zz-locked.env", "LOCKED=1\n", 0o600, passAt(0))
			must(t, os.Chmod(filepath.Join(w.root, "zz-locked.env"), 0))
			t.Cleanup(func() { _ = os.Chmod(filepath.Join(w.root, "zz-locked.env"), 0o600) })
		}
	}
	f := w.carrier()

	onePage, more, err := f.Offer("", 1<<20)
	must(t, err)
	if !reflect.DeepEqual(pagePaths(onePage), []string{"a.env", "b.env"}) || more {
		t.Fatalf("offered %q more=%v, want only the two readable small files and no more", pagePaths(onePage), more)
	}

	// With room for one file, the page is a.env and b.env still remains; the
	// next page is b.env, and the files that cannot travel are not "more".
	first, more, err := f.Offer("", 1)
	must(t, err)
	if !reflect.DeepEqual(pagePaths(first), []string{"a.env"}) || !more {
		t.Fatalf("first page %q more=%v, want a.env and more", pagePaths(first), more)
	}
	second, more, err := f.Offer("a.env", 1)
	must(t, err)
	if !reflect.DeepEqual(pagePaths(second), []string{"b.env"}) || more {
		t.Fatalf("second page %q more=%v, want b.env and no more, the rest cannot travel", pagePaths(second), more)
	}
	none, more, err := f.Offer("b.env", 1)
	must(t, err)
	if len(none) != 0 || more {
		t.Fatalf("after b.env offered %q more=%v, want nothing", pagePaths(none), more)
	}
	for _, p := range [][]File{onePage, first, second} {
		for _, file := range p {
			if bytes.Contains(file.Data, []byte("OUTSIDE-SECRET")) {
				t.Fatalf("%s carried a file from outside the folder", file.Path)
			}
		}
	}
}

func TestOfferOffersAFileAtTheEdgeOfWhatTheVaultTakes(t *testing.T) {
	// A file the vault could carry travels here too, and one the vault leaves
	// on its computer stays there: the two paths agree on what a secret is.
	w := &workspace{root: t.TempDir()}
	small := strings.Repeat("s", 700_000) // its slot value is 933,333 bytes, under keys.MaxSlotValue
	w.save(t, "edge.env", small, 0o600, passAt(0))
	got, _, err := w.carrier().Offer("", PassThroughPage)
	must(t, err)
	if len(got) != 1 || len(got[0].Data) != len(small) {
		t.Fatalf("a file the vault carries was not offered: %q", pagePaths(got))
	}
	big := &workspace{root: t.TempDir()}
	big.save(t, "huge.db", strings.Repeat("h", keys.MaxSlotValue+1), 0o600, passAt(0))
	got, more, err := big.carrier().Offer("", PassThroughPage)
	must(t, err)
	if len(got) != 0 || more {
		t.Fatalf("a file over keys.MaxSlotValue was offered: %q more=%v", pagePaths(got), more)
	}
}

func TestOfferListsADuplicatedWithheldPathOnce(t *testing.T) {
	w := &workspace{root: t.TempDir()}
	w.save(t, "a.env", "A=1\n", 0o600, passAt(0))
	w.withheld = append(w.withheld, "a.env", "a.env")
	got, more, err := w.carrier().Offer("", 1<<20)
	must(t, err)
	if !reflect.DeepEqual(pagePaths(got), []string{"a.env"}) || more {
		t.Fatalf("offered %q more=%v, want a.env once", pagePaths(got), more)
	}
}

func TestOfferAnswersTheErrorOfTheWithheldList(t *testing.T) {
	boom := errors.New("the policy could not be read")
	f := Files{Root: t.TempDir(), Chat: "chat", Withheld: func() ([]string, error) { return nil, boom }}
	if _, _, err := f.Offer("", 1<<20); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the list's own error", err)
	}
}

func TestOfferNeverSendsAPathTheOtherSideWouldRefuse(t *testing.T) {
	w := &workspace{root: t.TempDir()}
	w.save(t, "a.env", "A=1\n", 0o600, passAt(0))
	w.save(t, ".git/config", "[remote]\n", 0o600, passAt(0))
	w.save(t, ".cell/state", "cell\n", 0o600, passAt(0))
	got, _, err := w.carrier().Offer("", 1<<20)
	must(t, err)
	if !reflect.DeepEqual(pagePaths(got), []string{"a.env"}) {
		t.Fatalf("offered %q, want a.env alone: the engine's folders never travel", pagePaths(got))
	}
}

func TestAPassThroughPageHoldsAtLeastOneWholeFile(t *testing.T) {
	if PassThroughPage < keys.MaxSlotValue {
		t.Fatalf("PassThroughPage = %d, smaller than the one file keys.MaxSlotValue allows", PassThroughPage)
	}
	if PassThroughPage >= 8<<20 {
		t.Fatalf("PassThroughPage = %d leaves no room for sealing under the door's 8 MiB bound", PassThroughPage)
	}
}

// ---- Receive ----

// destination is a work folder with a sibling outside it, so a test can prove
// that nothing was written beyond the folder.
func destination(t *testing.T) (base string, f Files) {
	t.Helper()
	base = t.TempDir()
	root := filepath.Join(base, "work")
	must(t, os.Mkdir(root, 0o755))
	return base, Files{Root: root}
}

func incoming(path, data string, mode os.FileMode, at time.Time) File {
	return File{Path: path, Mode: mode, ModTime: at, Data: []byte(data)}
}

func TestReceiveWritesNewFilesWithTheirBytesModeAndSaveTime(t *testing.T) {
	_, f := destination(t)
	binary := "\x00\xff\xfe key \x80\r\n"
	got, err := f.Receive([]File{
		incoming(".env", "A=1\n", 0o644, passAt(10)),
		incoming("web/app/client/.env.production", "API=prod\n", 0o600, passAt(20)),
		incoming("keys/deploy.p12", binary, 0o600, passAt(30)),
		incoming("run.env", "C=3\n", 0o755, passAt(40)),
		incoming("empty.env", "", 0o600, passAt(50)),
	})
	must(t, err)
	wantReceived(t, got, []string{".env", "web/app/client/.env.production", "keys/deploy.p12", "run.env", "empty.env"}, nil, nil, nil)
	for _, c := range []struct {
		rel, data string
		mode      os.FileMode
		at        time.Time
	}{
		{".env", "A=1\n", 0o644, passAt(10)},
		{"web/app/client/.env.production", "API=prod\n", 0o600, passAt(20)},
		{"keys/deploy.p12", binary, 0o600, passAt(30)},
		{"run.env", "C=3\n", 0o755, passAt(40)},
		{"empty.env", "", 0o600, passAt(50)},
	} {
		path := filepath.Join(f.Root, filepath.FromSlash(c.rel))
		if got := readFile(t, path); got != c.data {
			t.Fatalf("%s holds %q, want %q", c.rel, got, c.data)
		}
		wantPermission(t, path, c.mode)
		wantModTime(t, path, c.at)
	}
	// no temporary file is left in any folder it touched
	want := []string{".env", "empty.env", "keys", "keys/deploy.p12", "run.env", "web", "web/app", "web/app/client", "web/app/client/.env.production"}
	if got := tree(t, f.Root); !reflect.DeepEqual(got, want) {
		t.Fatalf("the folder now holds %q, want %q", got, want)
	}
}

func TestReceiveNeedsNoWithheldListAndNoVault(t *testing.T) {
	// Receive installs what it is handed under Root; the policy list and the
	// vault belong to the other road and are never consulted.
	f := Files{Root: t.TempDir()}
	got, err := f.Receive([]File{incoming(".env", "A=1\n", 0o600, passAt(1))})
	must(t, err)
	wantReceived(t, got, []string{".env"}, nil, nil, nil)
}

func TestReceiveLeavesAnIdenticalFileAlone(t *testing.T) {
	for _, c := range []struct {
		name  string
		local time.Time
		send  time.Time
	}{
		{"sender is newer", passAt(10), passAt(500)},
		{"sender is older", passAt(500), passAt(10)},
		{"same time", passAt(10), passAt(10)},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := &workspace{root: t.TempDir()}
			w.save(t, "svc/.env", "K=v\n", 0o600, c.local)
			got, err := w.carrierWithoutList().Receive([]File{incoming("svc/.env", "K=v\n", 0o600, c.send)})
			must(t, err)
			wantReceived(t, got, nil, []string{"svc/.env"}, nil, nil)
			wantModTime(t, filepath.Join(w.root, "svc", ".env"), c.local)
		})
	}
}

func (w *workspace) carrierWithoutList() Files { return Files{Root: w.root, Chat: "chat"} }

func TestReceiveFollowsTheNewestWholeFile(t *testing.T) {
	for _, c := range []struct {
		name         string
		local, send  time.Time
		wantNewer    bool
		wantContent  string
		wantModStamp time.Time
	}{
		{"older here is replaced", passAt(10), passAt(20), false, "from-sender\n", passAt(20)},
		{"the same time is replaced", passAt(20), passAt(20), false, "from-sender\n", passAt(20)},
		{"newer here is kept", passAt(30), passAt(20), true, "local\n", passAt(30)},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := &workspace{root: t.TempDir()}
			w.save(t, ".env", "local\n", 0o600, c.local)
			got, err := w.carrierWithoutList().Receive([]File{incoming(".env", "from-sender\n", 0o640, c.send)})
			must(t, err)
			path := filepath.Join(w.root, ".env")
			if c.wantNewer {
				wantReceived(t, got, nil, nil, []string{".env"}, nil)
				wantPermission(t, path, 0o600)
			} else {
				wantReceived(t, got, []string{".env"}, nil, nil, nil)
				wantPermission(t, path, 0o640)
			}
			if got := readFile(t, path); got != c.wantContent {
				t.Fatalf(".env holds %q, want %q", got, c.wantContent)
			}
			wantModTime(t, path, c.wantModStamp)
		})
	}
}

func TestReceiveTreatsADifferentModeAsADifferentFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits to differ")
	}
	w := &workspace{root: t.TempDir()}
	w.save(t, "a.env", "K=v\n", 0o644, passAt(10))
	w.save(t, "b.env", "K=v\n", 0o644, passAt(30))
	got, err := w.carrierWithoutList().Receive([]File{
		incoming("a.env", "K=v\n", 0o600, passAt(20)), // same bytes, older here: takes the sender's mode
		incoming("b.env", "K=v\n", 0o600, passAt(20)), // same bytes, newer here: kept
	})
	must(t, err)
	wantReceived(t, got, []string{"a.env"}, nil, []string{"b.env"}, nil)
	wantPermission(t, filepath.Join(w.root, "a.env"), 0o600)
	wantPermission(t, filepath.Join(w.root, "b.env"), 0o644)
}

func TestReceiveDoesNotMoveTheTimeOfAFolderItWritesInto(t *testing.T) {
	_, f := destination(t)
	deep := filepath.Join(f.Root, "web", "app")
	must(t, os.MkdirAll(deep, 0o755))
	then := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, dir := range []string{deep, filepath.Dir(deep), f.Root} {
		must(t, os.Chtimes(dir, then, then))
	}
	got, err := f.Receive([]File{incoming("web/app/.env", "K=v\n", 0o600, passAt(1))})
	must(t, err)
	wantReceived(t, got, []string{"web/app/.env"}, nil, nil, nil)
	for _, dir := range []string{deep, filepath.Dir(deep), f.Root} {
		wantModTime(t, dir, then)
	}
}

func TestReceiveRefusesEveryPathTheSenderCouldUseToEscapeOrReachTheEnginesFolders(t *testing.T) {
	base, f := destination(t)
	absolute := filepath.ToSlash(filepath.Join(base, "abs.env"))
	unsafe := []string{
		absolute, "../escape.env", "a/../../escape.env", "..", ".", "",
		".git/config", ".git", ".cell/x", ".furrow/y", ".GIT/config",
		"sub/.git/hooks/pre-commit", "a/../.git/config", "./.cell/x",
		`back\slash.env`, "nul\x00.env", "a/", "./x.env", "a//b.env",
	}
	var files []File
	for _, p := range unsafe {
		files = append(files, incoming(p, "STOLEN-CONTENT\n", 0o600, passAt(1)))
	}
	// A refusal is a classification, like the vault dropping a name that leaves the
	// folder: it is answered in Refused and is not an I/O failure to report.
	got, err := f.Receive(files)
	must(t, err)
	wantReceived(t, got, nil, nil, nil, unsafe)
	if entries := tree(t, base); !reflect.DeepEqual(entries, []string{"work"}) {
		t.Fatalf("something was written: %q", entries)
	}
	if strings.Contains(fmt.Sprintf("%+v", got), "STOLEN-CONTENT") {
		t.Fatalf("the answer carries file content: %+v", got)
	}
}

func TestReceiveNeverWritesThroughASymlinkLeavingTheFolder(t *testing.T) {
	skipWithoutSymlinks(t)
	base, f := destination(t)
	outside := filepath.Join(base, "outside")
	must(t, os.Mkdir(outside, 0o755))
	must(t, os.WriteFile(filepath.Join(outside, ".env"), []byte("outside-original"), 0o600))
	must(t, os.WriteFile(filepath.Join(outside, "target.env"), []byte("target-original"), 0o600))
	must(t, os.Symlink(outside, filepath.Join(f.Root, "nested")))
	must(t, os.Symlink(filepath.Join(outside, "target.env"), filepath.Join(f.Root, "link.env")))

	got, err := f.Receive([]File{
		incoming("nested/.env", "SENDER-CONTENT", 0o600, passAt(1)),
		incoming("nested/new/deep/.env", "SENDER-CONTENT", 0o600, passAt(1)),
		incoming("link.env", "SENDER-CONTENT", 0o600, passAt(1)),
		incoming("fine.env", "FINE=1\n", 0o600, passAt(1)),
	})
	if err == nil {
		t.Fatal("a write through a link that leaves the folder reported no error")
	}
	wantReceived(t, got, []string{"fine.env"}, nil, nil, []string{"nested/.env", "nested/new/deep/.env", "link.env"})
	wantOutside := []string{".env", "target.env"}
	if entries := tree(t, outside); !reflect.DeepEqual(entries, wantOutside) {
		t.Fatalf("the folder outside now holds %q, want %q", entries, wantOutside)
	}
	if got := readFile(t, filepath.Join(outside, ".env")); got != "outside-original" {
		t.Fatalf("outside .env was changed to %q", got)
	}
	if got := readFile(t, filepath.Join(outside, "target.env")); got != "target-original" {
		t.Fatalf("the link's target was changed to %q", got)
	}
	if strings.Contains(err.Error(), "SENDER-CONTENT") {
		t.Fatalf("the error carries file content: %v", err)
	}
}

func TestReceiveNeverOverwritesALocalFileTooLargeToTravel(t *testing.T) {
	w := &workspace{root: t.TempDir()}
	big := strings.Repeat("x", keys.MaxSlotValue)
	w.save(t, "graph.db", big, 0o600, passAt(10))
	got, err := w.carrierWithoutList().Receive([]File{incoming("graph.db", "small copy from the other computer\n", 0o600, passAt(500))})
	must(t, err)
	wantReceived(t, got, nil, nil, nil, []string{"graph.db"})
	w.want(t, "graph.db", big, 0o600)
	wantModTime(t, filepath.Join(w.root, "graph.db"), passAt(10))
}

func TestReceiveRefusesAFileTooLargeToTravel(t *testing.T) {
	_, f := destination(t)
	got, err := f.Receive([]File{
		incoming("huge.db", strings.Repeat("h", keys.MaxSlotValue+1), 0o600, passAt(1)),
		incoming("small.env", "S=1\n", 0o600, passAt(1)),
	})
	must(t, err)
	wantReceived(t, got, []string{"small.env"}, nil, nil, []string{"huge.db"})
	if _, err := os.Stat(filepath.Join(f.Root, "huge.db")); !os.IsNotExist(err) {
		t.Fatalf("a file over keys.MaxSlotValue was written: %v", err)
	}
}

func TestReceiveLeavesAnEntryThatIsNotARegularFileAlone(t *testing.T) {
	w := &workspace{root: t.TempDir()}
	must(t, os.Mkdir(filepath.Join(w.root, "dir.env"), 0o755))
	must(t, os.WriteFile(filepath.Join(w.root, "dir.env", "keep"), []byte("kept"), 0o600))
	got, err := w.carrierWithoutList().Receive([]File{incoming("dir.env", "D=1\n", 0o600, passAt(1))})
	must(t, err)
	wantReceived(t, got, nil, nil, nil, []string{"dir.env"})
	if readFile(t, filepath.Join(w.root, "dir.env", "keep")) != "kept" {
		t.Fatal("a local folder was disturbed")
	}
	// and a file asked to land inside a path that is a plain file is no write either
	must(t, os.WriteFile(filepath.Join(w.root, "plain"), []byte("plain"), 0o600))
	got, err = w.carrierWithoutList().Receive([]File{incoming("plain/.env", "X=1\n", 0o600, passAt(1))})
	if err == nil {
		t.Fatal("a file asked to land beneath a plain file reported no error")
	}
	wantReceived(t, got, nil, nil, nil, []string{"plain/.env"})
	if readFile(t, filepath.Join(w.root, "plain")) != "plain" {
		t.Fatal("a local file was disturbed")
	}
}

func TestOneFailureDoesNotStopTheOthersAndTheErrorNamesOnlyPaths(t *testing.T) {
	skipWithoutSymlinks(t)
	base, f := destination(t)
	outside := filepath.Join(base, "outside")
	must(t, os.Mkdir(outside, 0o755))
	must(t, os.Symlink(outside, filepath.Join(f.Root, "nested")))
	const secret = "s3cr3t-value-7f3a"
	got, err := f.Receive([]File{
		incoming("one.env", "ONE="+secret+"\n", 0o600, passAt(1)),
		incoming("../bad.env", "BAD="+secret+"\n", 0o600, passAt(1)),
		incoming("two/.env", "TWO="+secret+"\n", 0o600, passAt(1)),
		incoming("nested/.env", "LINK="+secret+"\n", 0o600, passAt(1)),
		incoming("three.env", "THREE="+secret+"\n", 0o600, passAt(1)),
	})
	if err == nil {
		t.Fatal("failures were not reported")
	}
	wantReceived(t, got, []string{"one.env", "two/.env", "three.env"}, nil, nil, []string{"../bad.env", "nested/.env"})
	if !strings.Contains(err.Error(), "nested/.env") {
		t.Fatalf("the error does not name the path that failed: %v", err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(fmt.Sprintf("%+v", got), secret) {
		t.Fatalf("file content leaked into the answer: %v %+v", err, got)
	}
	for _, rel := range []string{"one.env", "two/.env", "three.env"} {
		if !strings.Contains(readFile(t, filepath.Join(f.Root, filepath.FromSlash(rel))), secret) {
			t.Fatalf("%s did not arrive", rel)
		}
	}
	if entries := tree(t, outside); len(entries) != 0 {
		t.Fatalf("the folder outside now holds %q", entries)
	}
}

func TestEveryReceivedFileIsAnsweredExactlyOnce(t *testing.T) {
	w := &workspace{root: t.TempDir()}
	w.save(t, "same.env", "S=1\n", 0o600, passAt(10))
	w.save(t, "newer.env", "mine\n", 0o600, passAt(90))
	w.save(t, "older.env", "mine\n", 0o600, passAt(5))
	got, err := w.carrierWithoutList().Receive([]File{
		incoming("same.env", "S=1\n", 0o600, passAt(10)),
		incoming("newer.env", "theirs\n", 0o600, passAt(50)),
		incoming("older.env", "theirs\n", 0o600, passAt(50)),
		incoming("new.env", "N=1\n", 0o600, passAt(50)),
		incoming("../out.env", "O=1\n", 0o600, passAt(50)),
	})
	must(t, err)
	wantReceived(t, got,
		[]string{"older.env", "new.env"}, []string{"same.env"}, []string{"newer.env"}, []string{"../out.env"})
}

func TestAFileOfferedByOneFolderArrivesInAnotherExactlyAndASecondRoundChangesNothing(t *testing.T) {
	src := &workspace{root: t.TempDir()}
	src.save(t, ".env", "A=1\n", 0o644, passAt(10))
	src.save(t, "svc/api/.env.local", "B=2\n", 0o600, passAt(20))
	src.save(t, "tools/run.env", "C=3\n", 0o755, passAt(30))
	src.save(t, "keys/deploy.p12", "\x00\xff\xfe\x80", 0o600, passAt(40))
	_, dst := destination(t)

	pages, _ := listPages(t, src.carrier(), 5)
	for _, page := range pages {
		got, err := dst.Receive(page)
		must(t, err)
		if len(got.Written) != len(page) {
			t.Fatalf("a page of %d files wrote %d: %+v", len(page), len(got.Written), got)
		}
	}
	for _, rel := range src.withheld {
		from, to := filepath.Join(src.root, filepath.FromSlash(rel)), filepath.Join(dst.Root, filepath.FromSlash(rel))
		if readFile(t, from) != readFile(t, to) {
			t.Fatalf("%s arrived different", rel)
		}
		a, b := stat(t, from), stat(t, to)
		if runtime.GOOS != "windows" && a.Mode().Perm() != b.Mode().Perm() {
			t.Fatalf("%s has mode %v, want %v", rel, b.Mode().Perm(), a.Mode().Perm())
		}
		if !a.ModTime().Equal(b.ModTime()) {
			t.Fatalf("%s is dated %v, want %v", rel, b.ModTime(), a.ModTime())
		}
	}

	var again []string
	for _, page := range pages {
		got, err := dst.Receive(page)
		must(t, err)
		again = append(again, got.Same...)
		if len(got.Written)+len(got.Newer)+len(got.Refused) != 0 {
			t.Fatalf("a second round changed something: %+v", got)
		}
	}
	sort.Strings(again)
	want := append([]string(nil), src.withheld...)
	sort.Strings(want)
	if !reflect.DeepEqual(again, want) {
		t.Fatalf("the second round called %q the same, want %q", again, want)
	}
}

func stat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	must(t, err)
	return info
}

func TestNoContentIsEverInAnErrorOrAnAnswer(t *testing.T) {
	const secret = "s3cr3t-value-7f3a"
	w := &workspace{root: t.TempDir()}
	must(t, os.Mkdir(filepath.Join(w.root, "dir.env"), 0o755))
	w.save(t, "local.env", "LOCAL="+secret+"\n", 0o600, passAt(90))
	must(t, os.WriteFile(filepath.Join(w.root, "plain"), []byte("PLAIN="+secret), 0o600))
	got, err := w.carrierWithoutList().Receive([]File{
		incoming("dir.env", "D="+secret+"\n", 0o600, passAt(1)),
		incoming("local.env", "T="+secret+"\n", 0o600, passAt(1)),
		incoming("../x.env", "X="+secret+"\n", 0o600, passAt(1)),
		incoming(".git/config", "G="+secret+"\n", 0o600, passAt(1)),
		incoming("plain/.env", "P="+secret+"\n", 0o600, passAt(1)), // beneath a plain file: a failure, with an error
	})
	if err == nil {
		t.Fatal("the failed write reported no error, so there was nothing to read")
	}
	wantReceived(t, got, nil, nil, []string{"local.env"}, []string{"dir.env", "../x.env", ".git/config", "plain/.env"})
	var said strings.Builder
	fmt.Fprintf(&said, "%+v %#v %v", got, got, err)
	if strings.Contains(said.String(), secret) {
		t.Fatalf("file content leaked into what Receive said: %s", said.String())
	}
}

// Write still leaves the save time to the write: the vault's own restore keeps
// dating a file by when it was put back, and only pass-through dates it by the sender.
func TestTheVaultsWriteStillLeavesTheSaveTimeToTheWrite(t *testing.T) {
	root := t.TempDir()
	m := fileMedium{root: root, path: filepath.Join(root, "new", ".env")}
	before := time.Now().Add(-time.Minute)
	must(t, m.Write(encodeFile(0o600, []byte("K=v\n"))))
	info := stat(t, m.path)
	if info.ModTime().Before(before) {
		t.Fatalf("the vault's write dated the file %v", info.ModTime())
	}
}
