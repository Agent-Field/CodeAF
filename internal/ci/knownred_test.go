package ci

import (
	"bufio"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// THE KNOWN-RED LEDGER ONLY SHRINKS.
//
// .github/known-red.txt names the tests that fail on a clean tree so the full
// run can skip them and red can still mean something. It is a debt, and on
// 2026-09-02 the ruling was that it burns to zero — every entry fixed for real
// or deleted with a written ruling — and that no new entry is allowed after.
// This test is the ratchet that holds that ruling, THE WAY complexity_test.go
// HOLDS ENDINGS AND SIZE-BUDGET HOLDS BYTES: one number that a change may not
// push up, and that a change which lowered it must write down.
//
// It is transitional. The commit that deletes the ledger deletes this file with
// it; nothing else in the tree reads the ledger except the one `make test`
// reading, which skips nothing when the file is gone.

// knownRedEntries is the number of tests the ledger names right now. Measured
// on 2026-09-02 at 1de89c08, after #408 took five out. A change that fixes one
// lowers this by one in the same commit; a change that would raise it has no
// road.
const knownRedEntries = 12

// knownRedPath is where the ledger lives, relative to the repository root.
const knownRedPath = ".github/known-red.txt"

func TestTheKnownRedLedgerOnlyShrinksAndNamesRealTests(t *testing.T) {
	root := repositoryRoot(t)
	entries, present := readKnownRed(t, filepath.Join(root, knownRedPath))
	if !present {
		if knownRedEntries != 0 {
			t.Fatalf("%s is gone and knownRedEntries still says %d — the ledger has been burned down; "+
				"delete this test in the same change", knownRedPath, knownRedEntries)
		}
		return
	}
	if complaint := ratchetComplaint(len(entries), knownRedEntries); complaint != "" {
		t.Error(complaint)
	}
	// AND EVERY NAME IS A TEST THAT EXISTS. A renamed or deleted test that stays
	// listed is a skip nobody can account for, and the day it is fixed nobody
	// will know to remove the line.
	declared := testFunctions(t, root)
	for _, name := range entries {
		if !declared[name] {
			t.Errorf("%s names %q and no *_test.go in the tree declares func %s( — remove the line, "+
				"or spell the test as it is spelled now", knownRedPath, name, name)
		}
	}
}

// readKnownRed returns the ledger's entries — one Go test name per line, blank
// lines and # comments ignored — and whether the file exists at all. It reads
// the file exactly the way the Makefile's KNOWN_RED does, so what this counts
// is what `make test` skips.
func readKnownRed(t *testing.T, path string) (entries []string, present bool) {
	t.Helper()
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer file.Close()
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, line)
	}
	if err := lines.Err(); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return entries, true
}

// testFunctions is every `func TestX(` declared in a *_test.go under root,
// read with go/parser the way every other law in this tree reads it — which
// is also what puts this file on the laws target (scripts/laws.sh), where it
// belongs.
func testFunctions(t *testing.T, root string) map[string]bool {
	t.Helper()
	found := make(map[string]bool)
	files := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "third_party", "node_modules", "bin":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(files, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				found[fn.Name.Name] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return found
}

// repositoryRoot is two directories up from this file, which is where go.mod
// and .github are; it is located from the source rather than the working
// directory so the test answers the same from any package or IDE.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller gave no file")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s (%v); this file moved and the root did not follow", root, err)
	}
	return root
}

// TestTheRatchetRefusesBothDirections is the rule put to numbers rather than to
// the real ledger, so that a change to the ledger cannot also be the change
// that silences the test about it. It exercises the same comparison the test
// above enforces with — there is one, not a copy.
func TestTheRatchetRefusesBothDirections(t *testing.T) {
	for _, tc := range []struct {
		entries, ratchet int
		wantComplaint    bool
	}{
		{12, 12, false},
		{13, 12, true},
		{11, 12, true},
		{0, 0, false},
	} {
		got := ratchetComplaint(tc.entries, tc.ratchet) != ""
		if got != tc.wantComplaint {
			t.Errorf("%d entries against a ratchet of %d: complaint=%v, want %v", tc.entries, tc.ratchet, got, tc.wantComplaint)
		}
	}
}

// ratchetComplaint is the whole rule, one sentence per direction and "" when
// the ledger and the ratchet agree. The enforcing test and the table above
// both call it, so the comparison exists once.
func ratchetComplaint(entries, ratchet int) string {
	switch {
	case entries > ratchet:
		return knownRedPath + " names " + strconv.Itoa(entries) + " tests and the ratchet is at " +
			strconv.Itoa(ratchet) + ". There is no road to a new entry: the ledger only shrinks " +
			"(ruled 2026-09-02). Fix the test, or delete it with a written ruling, and remove the line."
	case entries < ratchet:
		return knownRedPath + " is down to " + strconv.Itoa(entries) + " tests and knownRedEntries still says " +
			strconv.Itoa(ratchet) + " — lower it to " + strconv.Itoa(entries) + " in this change. A ratchet " +
			"left slack would let the next change put an entry back with the gate green throughout."
	}
	return ""
}
