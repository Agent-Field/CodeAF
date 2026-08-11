package registry

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// TestEntriesHaveUniqueIDs is the registry's own identity gate: two entries
// answering to the same id would make ByID ambiguous and any surface that
// keys UI state off an entry's id (a palette selection, a focus ring) would
// have no way to tell them apart.
func TestEntriesHaveUniqueIDs(t *testing.T) {
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.ID == "" {
			t.Fatalf("entry %+v has an empty ID", entry)
		}
		if seen[entry.ID] {
			t.Fatalf("id %q is registered more than once", entry.ID)
		}
		seen[entry.ID] = true
	}
}

// TestLiveKeyBindingsAreUniquePerScope is 5.22's law made mechanical for
// keys: a scope in which two entries answer to the same chord is a scope in
// which the surface cannot know which one the user meant. Two entries may
// share a key across DISJOINT scopes — "c" means nothing in ScopeThread and
// "cancel the inspected worker" in ScopeNode — because a surface only ever
// asks ByKey for the scope it currently holds.
func TestLiveKeyBindingsAreUniquePerScope(t *testing.T) {
	for i, a := range entries {
		if a.Key == "" {
			continue
		}
		for j, b := range entries {
			if i == j || b.Key != a.Key {
				continue
			}
			if a.Scope.Has(b.Scope) {
				t.Fatalf("key %q is bound to both %q and %q in an overlapping scope", a.Key, a.ID, b.ID)
			}
		}
	}
}

// TestSlashAliasesAreUnique is the same law for the composer's own filtered
// view of the catalog (5.22 rule 3): typing "/cancel" has to resolve to
// exactly one row.
func TestSlashAliasesAreUnique(t *testing.T) {
	seen := map[string]string{}
	for _, entry := range entries {
		if entry.Slash == "" {
			continue
		}
		key := toLower(entry.Slash)
		if owner, ok := seen[key]; ok {
			t.Fatalf("slash alias %q is registered by both %q and %q", entry.Slash, owner, entry.ID)
		}
		seen[key] = entry.ID
	}
}

// validStoreCommandKinds mirrors store.validCommandKind (internal/store/thread.go)
// using only its exported constants — that switch itself is unexported, so
// this is a hand-maintained restatement rather than a direct call into it.
// Every value below is a typed store.CommandKind constant: if store ever
// renamed or removed one, this file stops compiling before the test can even
// run, which is the closest an unexported source-of-truth switch lets an
// outside package get to "the actual constants." A future wave that adds a
// journaled command kind and forgets to add it here gets a failing
// TestJournalKindsAreRealCommandKinds instead — a safe direction to be wrong
// in, since it blocks a claim rather than silently green-lighting it.
var validStoreCommandKinds = map[store.CommandKind]bool{
	store.CommandSplice: true, store.CommandAmend: true, store.CommandCancel: true,
	store.CommandRedirect: true, store.CommandExpedite: true, store.CommandPause: true,
	store.CommandResume: true, store.CommandReprioritize: true, store.CommandRestart: true,
	store.CommandSetModel: true, store.CommandCharterRatify: true, store.CommandCharterPause: true,
	store.CommandCharterRetire: true, store.CommandCharterCadence: true, store.CommandCharterWording: true,
	store.CommandCharterOnce: true, store.CommandCharterFire: true, store.CommandCharterDecline: true,
	store.CommandCharterAlways: true, store.CommandCharterNever: true, store.CommandCharterProbation: true,
	store.CommandServiceStop: true, store.CommandServiceRestart: true, store.CommandServiceAutoRestart: true,
	store.CommandStandingWatchEnable: true, store.CommandStandingWatchDecline: true, store.CommandHandover: true,
}

// TestJournalKindsAreRealCommandKinds is the registry-fix checklist's
// "no aspirational entries" rule, checked mechanically: every non-empty
// Journal.Kind seeded in catalog.go has to be one of store's own command
// kinds.
func TestJournalKindsAreRealCommandKinds(t *testing.T) {
	for _, entry := range entries {
		if entry.Journal.Kind == "" {
			continue
		}
		if !validStoreCommandKinds[entry.Journal.Kind] {
			t.Fatalf("%s journals kind %q, which is not one of store's command kinds", entry.ID, entry.Journal.Kind)
		}
	}
}

// TestJournalToolsAreRealBeltTools checks the other half of the same rule
// for entries reached through the head belt (ScopeTalk). The belt's tool
// names (beltToolControl, beltToolRevise, …) are unexported package-private
// constants in internal/head/toolbelt.go, so there is no typed symbol this
// package can import and compare against the way it can for store's command
// kinds. Instead this test reads that file's own source and extracts the
// literal string every beltTool* constant is assigned — the same technique
// internal/thread's own completeness gate uses to check callers of
// thread.Post — so the assertion is against the belt's actual constant
// table, not a hand-copied guess of what it says.
func TestJournalToolsAreRealBeltTools(t *testing.T) {
	root := repositoryRoot(t)
	toolNames, err := beltToolConstantValues(filepath.Join(root, "internal", "head", "toolbelt.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(toolNames) < 10 {
		t.Fatalf("found only %d beltTool* constants; the extraction is not reading the file", len(toolNames))
	}
	for _, entry := range entries {
		if entry.Journal.Tool == "" {
			continue
		}
		if !toolNames[entry.Journal.Tool] {
			t.Fatalf("%s journals tool %q, which is not one of internal/head's beltTool* constants", entry.ID, entry.Journal.Tool)
		}
	}
}

// beltToolConstantValues parses one Go source file and returns the string
// literal value of every constant whose name starts with "beltTool" — read
// as text, not imported, which is what lets a package outside internal/head
// check against internal/head's unexported names at all.
func beltToolConstantValues(path string) (map[string]bool, error) {
	positions := token.NewFileSet()
	file, err := parser.ParseFile(positions, path, nil, 0)
	if err != nil {
		return nil, err
	}
	values := map[string]bool{}
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		for _, spec := range general.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, name := range value.Names {
				if !strings.HasPrefix(name.Name, "beltTool") || index >= len(value.Values) {
					continue
				}
				literal, ok := value.Values[index].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				unquoted, err := strconv.Unquote(literal.Value)
				if err != nil {
					continue
				}
				values[unquoted] = true
			}
		}
	}
	return values, nil
}

// TestCatalogTextIsASCII holds the assumption toLower and scoreSubsequence
// both document: every seeded verb, description, and alias is plain ASCII,
// so byte-indexed lowercasing and matching are exact rather than merely fast.
// A future entry that fails this test needs those two functions revisited
// before it ships, not a silent wrong match.
func TestCatalogTextIsASCII(t *testing.T) {
	for _, entry := range entries {
		for _, field := range []string{entry.ID, entry.Verb, entry.Description, entry.Key, entry.Slash} {
			for index := 0; index < len(field); index++ {
				if field[index] > 0x7f {
					t.Fatalf("%s has a non-ASCII byte in %q", entry.ID, field)
				}
			}
		}
	}
}

// TestQueriesNeverPanicOnEmptyOrUnknownScope is the quality bar's "zero
// panics on empty/unknown scopes" line, checked directly: a scope with no
// bits set, and a scope bit this package has never defined, both have to
// read as "matches nothing" rather than crash a caller that got its focus
// state wrong for a frame.
func TestQueriesNeverPanicOnEmptyOrUnknownScope(t *testing.T) {
	for _, scope := range []Scope{0, Scope(1 << 7)} {
		if rows := ForScope(scope); len(rows) != 0 {
			t.Fatalf("ForScope(%d) = %d rows, want 0", scope, len(rows))
		}
		if _, ok := ByKey(scope, "c"); ok {
			t.Fatalf("ByKey matched under scope %d, which owns no entries", scope)
		}
		if matches := FuzzyMatch(scope, ""); len(matches) != 0 {
			t.Fatalf("FuzzyMatch(%d, \"\") = %d matches, want 0", scope, len(matches))
		}
	}
	if _, ok := ByID(""); ok {
		t.Fatal("ByID(\"\") unexpectedly matched an entry")
	}
	if _, ok := BySlash(""); ok {
		t.Fatal("BySlash(\"\") unexpectedly matched an entry")
	}
	if _, ok := ByKey(ScopeAny, ""); ok {
		t.Fatal("ByKey with an empty key unexpectedly matched an entry")
	}
}

// TestByIDAndBySlashAndByKeyFindSeededRows is a sanity check that the
// catalog is actually reachable through every documented query, not just
// internally consistent.
func TestByIDAndBySlashAndByKeyFindSeededRows(t *testing.T) {
	if _, ok := ByID("slash.cancel"); !ok {
		t.Fatal("ByID did not find the seeded /cancel entry")
	}
	if entry, ok := BySlash("CANCEL"); !ok || entry.ID != "slash.cancel" {
		t.Fatalf("BySlash(\"CANCEL\") = %+v, %v, want slash.cancel, true", entry, ok)
	}
	if entry, ok := ByKey(ScopeNode, "c"); !ok || entry.ID != "key.node.cancel" {
		t.Fatalf("ByKey(ScopeNode, \"c\") = %+v, %v, want key.node.cancel, true", entry, ok)
	}
	if _, ok := ByKey(ScopeThread, "c"); ok {
		t.Fatal("ByKey(ScopeThread, \"c\") matched a ScopeNode-only entry")
	}
}

// TestSurfaceResolvesTheReceiptsBinding is the seam this axis was added to
// close. The chat surface binds ctrl+r for the receipts fold and had to say so
// in a code comment, because the registry could only offer v1's "v" — which
// that surface cannot bind at all — so the footer had no honest row to render
// and the accelerator went untaught.
func TestSurfaceResolvesTheReceiptsBinding(t *testing.T) {
	entry, ok := ByID("key.thread.receipts")
	if !ok {
		t.Fatal("the receipts row is gone")
	}
	if got := entry.KeyOn(SurfaceDefault); got != "v" {
		t.Fatalf("the v1 window's binding changed: %q", got)
	}
	if got := entry.KeyOn(SurfaceComposerFirst); got != "ctrl+r" {
		t.Fatalf("the chat surface's binding is %q, want ctrl+r", got)
	}
	bound, ok := entry.On(SurfaceComposerFirst)
	if !ok {
		t.Fatal("the receipts row has no accelerator on a composer-first surface")
	}
	if bound.Key != "ctrl+r" || bound.Verb != entry.Verb || bound.ID != entry.ID {
		t.Fatalf("projection lost the row's identity or words: %+v", bound)
	}
	if found, ok := ByKeyOn(ScopeThread, SurfaceComposerFirst, "ctrl+r"); !ok || found.ID != entry.ID {
		t.Fatalf("ByKeyOn could not route ctrl+r: %+v, %v", found, ok)
	}
	if _, ok := ByKeyOn(ScopeThread, SurfaceComposerFirst, "v"); ok {
		t.Fatal("a composer-first surface resolved a bare letter — that letter is draft text")
	}
	if found, ok := ByKey(ScopeThread, "v"); !ok || found.ID != entry.ID {
		t.Fatal("the surface-blind query stopped answering the way it always did")
	}
}

// A bare letter is not bindable where a composer holds the keyboard, and the
// registry says so instead of handing a surface a key that would do nothing.
// A chord is bindable everywhere and comes back unchanged.
func TestBareLettersDoNotBindOnAComposerFirstSurface(t *testing.T) {
	for _, entry := range entries {
		key := entry.KeyOn(SurfaceComposerFirst)
		if key != "" && barePrintable(key) {
			t.Errorf("entry %q offers bare %q to a surface whose composer would eat it",
				entry.ID, key)
		}
		if entry.ChordKey == "" && !barePrintable(entry.Key) && key != entry.Key {
			t.Errorf("entry %q lost its chord %q on a composer-first surface", entry.ID, entry.Key)
		}
		if got := entry.KeyOn(SurfaceDefault); got != entry.Key {
			t.Errorf("entry %q: the default surface no longer reports Key (%q vs %q)",
				entry.ID, got, entry.Key)
		}
	}
	if _, ok := (Entry{Key: "y", Verb: "copy answer"}).On(SurfaceComposerFirst); ok {
		t.Error("a bare-letter entry claimed an accelerator on a composer-first surface")
	}
}

// A ChordKey is a record of a binding that exists, never one we wish existed:
// it has to be a real chord, and it may not collide inside a scope any more
// than a Key may (the uniqueness law of TestLiveKeyBindingsAreUniquePerScope,
// asked on the other surface).
func TestChordKeysAreRealAndUniquePerScope(t *testing.T) {
	for _, entry := range entries {
		if entry.ChordKey == "" {
			continue
		}
		if barePrintable(entry.ChordKey) {
			t.Errorf("entry %q records a bare %q as its composer-first binding", entry.ID, entry.ChordKey)
		}
		if entry.ChordKey == entry.Key {
			t.Errorf("entry %q repeats its Key as a ChordKey, which records nothing", entry.ID)
		}
	}
	for i, a := range entries {
		keyA := a.KeyOn(SurfaceComposerFirst)
		if keyA == "" {
			continue
		}
		for j, b := range entries {
			if i == j || b.KeyOn(SurfaceComposerFirst) != keyA {
				continue
			}
			if a.Scope.Has(b.Scope) {
				t.Fatalf("chord %q is bound to both %q and %q in an overlapping scope",
					keyA, a.ID, b.ID)
			}
		}
	}
}

// TestFuzzyMatchRanksPrefixesFirst pins the scoring behavior FuzzyMatch's
// doc comment promises: a query that prefixes a verb outright beats the same
// letters found scattered through a longer one.
func TestFuzzyMatchRanksPrefixesFirst(t *testing.T) {
	matches := FuzzyMatch(ScopeAny, "cancel")
	if len(matches) == 0 {
		t.Fatal("no matches for \"cancel\"")
	}
	if matches[0].Entry.Verb != "cancel" && matches[0].Entry.Verb != "cancel work" {
		t.Fatalf("best match for \"cancel\" was %q, want a cancel verb first", matches[0].Entry.Verb)
	}
}

// TestFuzzyMatchRespectsScope confirms a ScopeTalk-only entry (no key, no
// slash — reachable only through the belt) never surfaces in a ScopeThread
// query, which is the property a palette relies on to never show a door
// that scope cannot actually open.
func TestFuzzyMatchRespectsScope(t *testing.T) {
	for _, match := range FuzzyMatch(ScopeThread, "reprioritize") {
		if match.Entry.ID == "belt.reprioritize" {
			t.Fatal("a ScopeTalk-only entry surfaced in a ScopeThread query")
		}
	}
	found := false
	for _, match := range FuzzyMatch(ScopeTalk, "reprioritize") {
		if match.Entry.ID == "belt.reprioritize" {
			found = true
		}
	}
	if !found {
		t.Fatal("belt.reprioritize did not surface in its own scope")
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the registry package")
		}
		dir = parent
	}
}
