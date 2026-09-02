package substore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/filelock"
	"github.com/Agent-Field/aforge-v2/internal/home"
)

// weekly is the bundle most of these tests mint: a manifest that validates, a
// program, one prompt, a seed memory and one eval. It is a function rather than
// a variable because several tests edit one field of it, and a shared map would
// carry that edit into the next test.
func weekly() Files {
	return Files{
		Manifest: exec.Manifest{
			SubharnessInfo: exec.SubharnessInfo{
				Name:    "weekly-marketing",
				Purpose: "the Monday marketing pass for one company",
			},
			Cues:      []string{"marketing", "weekly"},
			Input:     exec.Schema(`{"type":"object","required":["company"],"properties":{"company":{"type":"string"}}}`),
			Output:    exec.Schema(`{"type":"object","properties":{"draft":{"type":"string"}}}`),
			Whitelist: []string{"web_search"},
		},
		Program: []byte("export default async function (input, env) { return {draft: ''} }\n"),
		Prompts: map[string][]byte{"draft.md": []byte("# draft\n\nWrite the week's note.\n")},
		Memory:  []byte("The company spells its own name in lower case.\n"),
		Evals:   map[string][]byte{"smoke.js": []byte("// inert until v3\n")},
	}
}

func storeAt(t *testing.T) *Store {
	t.Helper()
	return At(filepath.Join(t.TempDir(), Root))
}

// The bundle that comes back out is the bundle that went in. Every FILE is byte
// for byte what was handed over — the program, the prompts, the seed memory —
// and the manifest is the same document, which is the whole promise of an
// immutable store: what a run is handed is what somebody minted.
func TestAMintedBundleLoadsBackByteIdentical(t *testing.T) {
	store := storeAt(t)
	files := weekly()
	minted, err := store.Mint(files, "", "")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if minted.Version != 1 {
		t.Fatalf("the first version is v%d", minted.Version)
	}
	bundle, err := store.Load("weekly-marketing", 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got, want := string(bundle.Program), string(files.Program); got != want {
		t.Fatalf("program: got %q, want %q", got, want)
	}
	if got, want := string(bundle.Prompts["draft.md"]), string(files.Prompts["draft.md"]); got != want {
		t.Fatalf("prompt: got %q, want %q", got, want)
	}
	if got, want := string(bundle.Seed), string(files.Memory); got != want {
		t.Fatalf("seed memory: got %q, want %q", got, want)
	}
	if got, want := strings.Join(bundle.Evals, ","), "smoke.js"; got != want {
		t.Fatalf("evals: got %q, want %q", got, want)
	}
	if got, want := bundle.Manifest.Purpose, files.Manifest.Purpose; got != want {
		t.Fatalf("purpose: got %q, want %q", got, want)
	}
	// The schemas are BYTES in the contract, and the document they ride in is
	// indented so a person can read it — so what survives is every key, every
	// value and every order, and not the author's whitespace. See
	// [encodeManifest], and the fixed point below, which is the property
	// content-addressing actually needs.
	if got, want := compact(t, string(bundle.Manifest.Input)), compact(t, string(files.Manifest.Input)); got != want {
		t.Fatalf("input schema: got %s, want %s", got, want)
	}
	if got, want := compact(t, string(bundle.Manifest.Output)), compact(t, string(files.Manifest.Output)); got != want {
		t.Fatalf("output schema: got %s, want %s", got, want)
	}
	if got, want := strings.Join(bundle.Manifest.Cues, ","), "marketing,weekly"; got != want {
		t.Fatalf("cues: got %q, want %q", got, want)
	}
	if got, want := strings.Join(bundle.Manifest.Whitelist, ","), "web_search"; got != want {
		t.Fatalf("whitelist: got %q, want %q", got, want)
	}
}

// The encoding is a fixed point: minting a bundle that came off this store
// produces the same hash it already had, so a flow that loads, looks and saves
// does not mint a version in which nothing changed.
func TestMintingABundleThatCameOffTheStoreChangesNothing(t *testing.T) {
	store := storeAt(t)
	first, err := store.Mint(weekly(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := store.Load("weekly-marketing", 0)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.Mint(Files{
		Manifest: bundle.Manifest,
		Program:  bundle.Program,
		Prompts:  bundle.Prompts,
		Memory:   bundle.Seed,
		Evals:    map[string][]byte{"smoke.js": []byte("// inert until v3\n")},
	}, first.Hash, "a round trip through the store")
	if err != nil {
		t.Fatalf("re-mint: %v", err)
	}
	if again.Hash != first.Hash || again.Version != first.Version {
		t.Fatalf("a round trip through the store minted v%d (%s) out of v%d (%s)",
			again.Version, again.Hash, first.Version, first.Hash)
	}
}

// compact reads a JSON document's content out from under its formatting, for
// the comparisons that are about what a schema SAYS rather than how it was
// spaced.
func compact(t *testing.T, document string) string {
	t.Helper()
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, []byte(document)); err != nil {
		t.Fatalf("not JSON: %q", document)
	}
	return buffer.String()
}

// PRD §6: the directory and the manifest slot exist from v1, so the version that
// runs evals is a new runner and not a bundle format migration.
func TestTheEvalsDirectoryAndSlotExistFromTheFirstVersion(t *testing.T) {
	store := storeAt(t)
	files := weekly()
	files.Evals = nil
	if _, err := store.Mint(files, "", ""); err != nil {
		t.Fatalf("mint: %v", err)
	}
	info, err := os.Stat(filepath.Join(store.VersionDir("weekly-marketing", 1), EvalsDir))
	if err != nil || !info.IsDir() {
		t.Fatalf("a bundle with no evals still needs the directory: %v", err)
	}
	// And the slot rides in the same document as everything else, so a later
	// phase that reads it changes no bundle on disk.
	data, err := os.ReadFile(filepath.Join(store.VersionDir("weekly-marketing", 1), ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("manifest.json is one flat document: %v", err)
	}
	if _, ok := document["name"]; !ok {
		t.Fatalf("the manifest's own fields did not flatten: %v", document)
	}
	files = weekly()
	files.Manifest.Name = "with-evals"
	if _, err := store.Mint(files, "", ""); err != nil {
		t.Fatalf("mint: %v", err)
	}
	data, _ = os.ReadFile(filepath.Join(store.VersionDir("with-evals", 1), ManifestFile))
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if got := compact(t, string(document["evals"])); got != `["smoke.js"]` {
		t.Fatalf("the eval slot: got %q", got)
	}
}

// Minting is idempotent. A design flow that saved an unedited bundle used to be
// the way version numbers ran away; content-addressing is what makes "nothing
// changed" a fact the store can check rather than a discipline callers keep.
func TestMintingTheSameContentTwiceIsTheSameVersion(t *testing.T) {
	store := storeAt(t)
	first, err := store.Mint(weekly(), "", "")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	again, err := store.Mint(weekly(), first.Hash, "nothing changed, and this says so")
	if err != nil {
		t.Fatalf("re-mint: %v", err)
	}
	if again.Version != first.Version || again.Hash != first.Hash {
		t.Fatalf("the same bytes minted v%d (%s) after v%d (%s)", again.Version, again.Hash, first.Version, first.Hash)
	}
	versions, _ := store.Versions("weekly-marketing")
	if len(versions) != 1 {
		t.Fatalf("the same bytes left %d versions on disk", len(versions))
	}
}

// Different content is a new version, and it records where it came from. That
// pair — the parent hash and the line of why — is what makes a lineage read like
// a log instead of like a list of numbers.
func TestDifferentContentMintsTheNextVersionAndRecordsItsParent(t *testing.T) {
	store := storeAt(t)
	first, err := store.Mint(weekly(), "", "")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	changed := weekly()
	changed.Program = []byte("export default async function (input, env) { return {draft: 'x'} }\n")
	second, err := store.Mint(changed, first.Hash, "it was returning an empty draft")
	if err != nil {
		t.Fatalf("second mint: %v", err)
	}
	if second.Version != 2 {
		t.Fatalf("the second version is v%d", second.Version)
	}
	if second.Parent != first.Hash {
		t.Fatalf("v2's parent is %q, and v1 is %q", second.Parent, first.Hash)
	}
	if second.ParentVersion != 1 {
		t.Fatalf("v2 says it came from v%d", second.ParentVersion)
	}
	if second.Hash == first.Hash {
		t.Fatalf("two different bundles share the address %s", second.Hash)
	}
	if second.Why != "it was returning an empty draft" {
		t.Fatalf("the why: %q", second.Why)
	}
	// v1 still loads, unchanged. A pin is durable or it is not a pin.
	pinned, err := store.Load("weekly-marketing", 1)
	if err != nil {
		t.Fatalf("v1 after v2: %v", err)
	}
	if string(pinned.Program) != string(weekly().Program) {
		t.Fatalf("v1's program changed when v2 was minted")
	}
	lineage, err := store.Lineage("weekly-marketing")
	if err != nil || len(lineage) != 2 {
		t.Fatalf("the lineage is %d versions: %v", len(lineage), err)
	}
	if lineage[0].Version != 1 || lineage[1].Version != 2 {
		t.Fatalf("the lineage is not oldest first: %v", lineage)
	}
}

// A version with a parent has to say what changed. A lineage of unexplained
// versions is a list of hashes.
func TestAVersionWithAParentNeedsOneLineOfWhy(t *testing.T) {
	store := storeAt(t)
	first, _ := store.Mint(weekly(), "", "")
	changed := weekly()
	changed.Program = []byte("// different\n")
	_, err := store.Mint(changed, first.Hash, "   ")
	if err == nil {
		t.Fatalf("a silent second version was accepted")
	}
	if !strings.Contains(err.Error(), "what changed") {
		t.Fatalf("the refusal does not say what is missing: %v", err)
	}
}

// A mint made from a version that is no longer the head is refused, naming the
// version that arrived. This is the whole of the optimistic-concurrency story
// and it is what a design flow that thought for a minute needs to be told.
func TestAMintFromAStaleParentIsRefused(t *testing.T) {
	store := storeAt(t)
	first, _ := store.Mint(weekly(), "", "")
	second := weekly()
	second.Program = []byte("// v2\n")
	if _, err := store.Mint(second, first.Hash, "second"); err != nil {
		t.Fatal(err)
	}
	third := weekly()
	third.Program = []byte("// also from v1\n")
	_, err := store.Mint(third, first.Hash, "made from v1, in ignorance of v2")
	if err == nil {
		t.Fatalf("a version made from a stale parent was minted anyway")
	}
	if !strings.Contains(err.Error(), "moved on") {
		t.Fatalf("the refusal does not say the subharness moved: %v", err)
	}
	// And a first mint over a subharness that already exists is the same mistake
	// with no hash at all.
	_, err = store.Mint(weekly(), "", "")
	if err == nil || !strings.Contains(err.Error(), "already at v2") {
		t.Fatalf("a parentless mint over an existing subharness: %v", err)
	}
}

// There is no head file. The head is the highest version present, which is the
// rule that makes a pointer that disagrees with its pages impossible.
func TestTheHeadIsTheHighestVersionAndNoFileSaysSo(t *testing.T) {
	store := storeAt(t)
	record, _ := store.Mint(weekly(), "", "")
	for _, program := range []string{"// two\n", "// three\n", "// four\n"} {
		files := weekly()
		files.Program = []byte(program)
		next, err := store.Mint(files, record.Hash, "another pass")
		if err != nil {
			t.Fatal(err)
		}
		record = next
	}
	head, err := store.Head("weekly-marketing")
	if err != nil || head != 4 {
		t.Fatalf("head is v%d: %v", head, err)
	}
	entries, _ := os.ReadDir(store.nameDir("weekly-marketing"))
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.Name()), "head") {
			t.Fatalf("something on disk claims to be the head: %q", entry.Name())
		}
	}
	// Version 0 means the head, and it is the same bundle asking for v4 gets.
	bundle, err := store.Load("weekly-marketing", 0)
	if err != nil || bundle.Version != 4 {
		t.Fatalf("v0 loaded v%d: %v", bundle.Version, err)
	}
}

// A version record with no bundle beside it is the one crash window a mint has.
// Such a version is dead rather than half-present: it is not the head, it is not
// listed, and its number is never handed out again.
func TestAVersionWhoseBundleNeverLandedIsNotTheHead(t *testing.T) {
	store := storeAt(t)
	var lines []string
	store.Notice = func(line string) { lines = append(lines, line) }
	first, _ := store.Mint(weekly(), "", "")
	second := weekly()
	second.Program = []byte("// two\n")
	if _, err := store.Mint(second, first.Hash, "second"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(store.VersionDir("weekly-marketing", 2)); err != nil {
		t.Fatal(err)
	}
	head, err := store.Head("weekly-marketing")
	if err != nil || head != 1 {
		t.Fatalf("head after losing v2's directory is v%d: %v", head, err)
	}
	if len(lines) == 0 {
		t.Fatalf("the dangling version was skipped with nothing in the journal")
	}
	// The number is spent. A third mint is v3, because a hash somebody wrote down
	// for v2 may not come to mean different bytes.
	third := weekly()
	third.Program = []byte("// three\n")
	minted, err := store.Mint(third, first.Hash, "carrying on")
	if err != nil {
		t.Fatal(err)
	}
	if minted.Version != 3 {
		t.Fatalf("the mint after a dead v2 is v%d", minted.Version)
	}
}

// A bundle that does not validate is ABSENT from every list a person reads, and
// says so once in the journal. Present-and-broken is the failure this store is
// written to make impossible.
func TestABundleThatFailsValidationIsAbsentFromTheListsAndLogged(t *testing.T) {
	store := storeAt(t)
	if _, err := store.Mint(weekly(), "", ""); err != nil {
		t.Fatal(err)
	}
	good := weekly()
	good.Manifest.Name = "triage-flake"
	good.Manifest.Purpose = "look at one flaky test"
	if _, err := store.Mint(good, "", ""); err != nil {
		t.Fatal(err)
	}
	// A manifest edited by hand into something that no longer describes a
	// subharness: the purpose every list draws is gone.
	broken := filepath.Join(store.VersionDir("weekly-marketing", 1), ManifestFile)
	if err := os.WriteFile(broken, []byte(`{"name":"weekly-marketing"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var lines []string
	store.Notice = func(line string) { lines = append(lines, line) }
	source := store.Source(func(Bundle) (exec.Runner, error) { return stubRunner{}, nil })

	manifests := source.Manifests()
	if len(manifests) != 1 || manifests[0].Name != "triage-flake" {
		t.Fatalf("the broken bundle is still on the list: %v", manifests)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "weekly-marketing") {
		t.Fatalf("the absence left no findable trace: %v", lines)
	}
	if _, ok := source.Runner("weekly-marketing"); ok {
		t.Fatalf("a broken bundle answered a lookup")
	}
	if _, ok := source.Runner("triage-flake"); !ok {
		t.Fatalf("the good bundle beside it stopped resolving")
	}
	// And the door that NAMED a version still answers honestly, because whoever
	// called it deserves to be told what is wrong.
	if _, err := store.Load("weekly-marketing", 1); err == nil {
		t.Fatalf("Load hid the fault the lists route around")
	}
}

// A crowd of mints racing from one version produces exactly one winner and a
// refusal for everybody else, and never two writers believing they own the same
// page — nor, which is the same fault wearing a different number, two versions
// that both call v1 their parent. The gate one mint at a time holds is what
// makes that a kernel fact rather than a timing hope.
//
// THE RACE IS RUN MANY TIMES, and it has to be. The hole this pins (#274) was a
// window between two syscalls: [Store.write] links v2's record and then renames
// v2's bundle into place, and for that instant v2 is a spent number to the count
// and no version at all to the head. A writer the scheduler dropped in there
// read "the head is still v1, the next number is 3" and minted a SECOND CHILD OF
// v1. One round of eight writers walked into that window about twice in a
// hundred, so the test that raced eight once went green all afternoon and named
// the bug on somebody else's machine, under somebody else's full-tree run. The
// crowd and the rounds together are what turn "sometimes, on a busy box" into
// every run.
func TestConcurrentMintsNeverShareAVersion(t *testing.T) {
	const writers, rounds = 24, 100
	for round := range rounds {
		store := storeAt(t)
		first, err := store.Mint(weekly(), "", "")
		if err != nil {
			t.Fatal(err)
		}
		var group sync.WaitGroup
		var lock sync.Mutex
		won, lost := 0, 0
		for index := range writers {
			group.Add(1)
			go func() {
				defer group.Done()
				files := weekly()
				files.Program = []byte("// writer " + string(rune('a'+index)) + "\n")
				_, err := store.Mint(files, first.Hash, "one of a crowd at once")
				lock.Lock()
				defer lock.Unlock()
				if err == nil {
					won++
					return
				}
				lost++
				// A LOSER LOSES THE CLAIM, NEVER THE GATE. Twenty-four writers
				// contending for one name drain in milliseconds, so nothing here
				// comes close to mintGateBound — and if one of them were ever
				// refused with ErrMintBusy the bound would be too mean for real
				// contention, which is the thing this line is watching for.
				if errors.Is(err, ErrMintBusy) {
					t.Errorf("round %d: real contention was refused by the gate bound: %v", round, err)
				}
				if !errors.Is(err, ErrExists) && !strings.Contains(err.Error(), "moved on") {
					t.Errorf("round %d: a losing writer was told something else: %v", round, err)
				}
			}()
		}
		group.Wait()
		if won != 1 {
			t.Fatalf("round %d: %d of %d writers minted a version (and %d were refused)", round, won, writers, lost)
		}
		versions, err := store.Versions("weekly-marketing")
		if err != nil || len(versions) != 2 {
			t.Fatalf("round %d: %d racing writers left %v: %v", round, writers, versions, err)
		}
		// The lineage is a line and not a tree. A writer that read a stale head
		// while counting past a fresh claim leaves two versions naming one
		// parent, which is the fault the count above can miss whenever the loser
		// crashes into somebody else's number instead of its own.
		lineage, err := store.Lineage("weekly-marketing")
		if err != nil {
			t.Fatalf("round %d: reading the lineage: %v", round, err)
		}
		children := make(map[int]int, len(lineage))
		for _, record := range lineage {
			children[record.ParentVersion]++
			if children[record.ParentVersion] > 1 {
				t.Fatalf("round %d: v%d is another version minted from v%d — the lineage forked",
					round, record.Version, record.ParentVersion)
			}
		}
		// Every version on disk is complete and loads. A loser that had renamed its
		// staging directory into place would show up here.
		for _, version := range versions {
			if _, err := store.Load("weekly-marketing", version); err != nil {
				t.Fatalf("round %d: v%d does not load: %v", round, version, err)
			}
		}
	}
}

// A mint that cannot have the gate is REFUSED INSIDE THE BOUND, and it writes
// nothing on the way out.
//
// The holder here is a second os.OpenFile of the same path. flock is per open
// file description rather than per process, so this contends with Store.gated's
// own handle exactly as another process would — which the test proves rather
// than assumes: the mint below has to be refused, and it could only be refused
// by a lock this test is holding.
//
// This is the scripted verification for the whole change: a blocking acquire
// would sit here until the deferred unlock, and there is no unlock before the
// assertion.
func TestAMintRefusesRatherThanWaitForAGateSomebodyElseHolds(t *testing.T) {
	store := storeAt(t)
	first, err := store.Mint(weekly(), "", "")
	if err != nil {
		t.Fatal(err)
	}

	gate, err := os.OpenFile(store.gatePath("weekly-marketing"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Close()
	if err := filelock.Lock(gate, true, true); err != nil {
		t.Skipf("this filesystem does not take an advisory lock, so there is no gate to hold: %v", err)
	}
	defer filelock.Unlock(gate)

	files := weekly()
	files.Program = []byte("// the one that finds the gate held\n")
	started := time.Now()
	_, err = store.Mint(files, first.Hash, "while somebody else holds the gate")
	waited := time.Since(started)

	if !errors.Is(err, ErrMintBusy) {
		t.Fatalf("a mint against a held gate answered %v, want ErrMintBusy", err)
	}
	if waited >= mintGateBound+time.Second {
		t.Fatalf("the refusal took %s, which is not inside the %s bound", waited, mintGateBound)
	}
	// A REFUSAL LEAVES NOTHING BEHIND. Not a second version, not a record whose
	// bundle never landed, and not a staging directory: the gate is taken before
	// anything is read, so a refused mint has not touched the store at all.
	versions, err := store.Versions("weekly-marketing")
	if err != nil || len(versions) != 1 || versions[0] != 1 {
		t.Fatalf("the refused mint left %v: %v", versions, err)
	}
	entries, err := os.ReadDir(filepath.Join(store.Dir(), "weekly-marketing"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), mintPrefix) {
			t.Fatalf("the refused mint left %s behind", entry.Name())
		}
		if recorded, ok := recordVersion(entry.Name()); ok && recorded != 1 {
			t.Fatalf("the refused mint claimed v%d", recorded)
		}
	}
}

// The store moves wholesale with AFORGE_HOME, through the one package that reads
// that variable, like everything else durable aforge writes.
func TestTheHomeStoreFollowsTheStateRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	store := Home()
	if got, want := store.Dir(), filepath.Join(root, Root); got != want {
		t.Fatalf("the home store is at %q, want %q", got, want)
	}
	if _, err := store.Mint(weekly(), "", ""); err != nil {
		t.Fatalf("mint: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, Root, "weekly-marketing", "v1", ProgramFile)); err != nil {
		t.Fatalf("the bundle did not land under the override: %v", err)
	}

	// Move the root and the same call answers a different, empty store — which is
	// the whole reason the override exists: a disposable run writes nowhere near
	// the person's own subharnesses.
	elsewhere := t.TempDir()
	t.Setenv(home.EnvVar, elsewhere)
	names, err := Home().Names()
	if err != nil || len(names) != 0 {
		t.Fatalf("the relocated store sees %v: %v", names, err)
	}
	t.Setenv(home.EnvVar, root)
	if names, _ := Home().Names(); len(names) != 1 || names[0] != "weekly-marketing" {
		t.Fatalf("coming back, the store sees %v", names)
	}
}

// A store with no runtime behind it is not a source at all. That is the
// codebase's law about a capability that cannot work, stated at the earliest
// place it can be: nothing about subharnesses is drawn rather than a list of
// names that refuse to run.
func TestAStoreWithNoRuntimeOffersNothing(t *testing.T) {
	store := storeAt(t)
	if _, err := store.Mint(weekly(), "", ""); err != nil {
		t.Fatal(err)
	}
	if source := store.Source(nil); source != nil {
		t.Fatalf("a store with no builder became a source: %#v", source)
	}
	// And the registry's own door takes that answer as a no-op, so the wiring
	// reads the same whether or not the runtime is in this build.
	registry := &exec.Registry{}
	registry.UseBundles(exec.LayerHome, store.Source(nil))
	if manifests := registry.Manifests(); len(manifests) != 0 {
		t.Fatalf("the registry picked up bundles it has no runtime for: %v", manifests)
	}
}

// The store plugs into the registry as one layer and the registry stamps the
// provenance — a manifest on disk does not get to claim it is built in.
func TestBundlesReachTheRegistryAsOneLayerWithItsProvenance(t *testing.T) {
	store := storeAt(t)
	if _, err := store.Mint(weekly(), "", ""); err != nil {
		t.Fatal(err)
	}
	claimed := weekly()
	claimed.Manifest.Name = "pretender"
	claimed.Manifest.Provenance = exec.FromBinary
	if _, err := store.Mint(claimed, "", ""); err != nil {
		t.Fatal(err)
	}
	registry := &exec.Registry{}
	registry.UseBundles(exec.LayerHome, store.Source(func(Bundle) (exec.Runner, error) { return stubRunner{}, nil }))

	manifests := registry.Manifests()
	if len(manifests) != 2 {
		t.Fatalf("the registry sees %d subharnesses", len(manifests))
	}
	for _, manifest := range manifests {
		if manifest.Provenance != exec.FromYou {
			t.Fatalf("%q is drawn as %q", manifest.Name, manifest.Provenance)
		}
	}
	if _, err := registry.Subharness("weekly-marketing"); err != nil {
		t.Fatalf("the registry cannot resolve a stored bundle: %v", err)
	}
	if _, err := registry.Subharness("no-such-thing"); !errors.Is(err, exec.ErrNoSubharness) {
		t.Fatalf("an unknown name answered %v", err)
	}
}

// remember() appends and recall() reads back, subharness-scoped and per name —
// so a new version of a subharness inherits everything it has learnt instead of
// starting over.
func TestMemoryAccumulatesAcrossVersions(t *testing.T) {
	store := storeAt(t)
	first, err := store.Mint(weekly(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	memory := store.Memory("weekly-marketing")
	// The seed the bundle was minted with is there from the start.
	notes, err := memory.Recall(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || !strings.Contains(notes[0].Text, "lower case") {
		t.Fatalf("the seed did not reach the live memory: %v", notes)
	}
	if err := memory.Remember(t.Context(), "the Q3 launch moved to October"); err != nil {
		t.Fatal(err)
	}
	if err := memory.Remember(t.Context(), "their newsletter goes out on Thursdays"); err != nil {
		t.Fatal(err)
	}
	notes, _ = memory.Recall(t.Context(), "")
	if len(notes) != 3 {
		t.Fatalf("three notes, got %d: %v", len(notes), notes)
	}
	// Newest first: what it learnt last week beats what it learnt on day one.
	if !strings.Contains(notes[0].Text, "Thursdays") {
		t.Fatalf("the notes are not newest first: %v", notes)
	}
	if notes[0].At == "" {
		t.Fatalf("a remembered note carries no time: %v", notes[0])
	}
	found, _ := memory.Recall(t.Context(), "newsletter")
	if len(found) != 1 || !strings.Contains(found[0].Text, "Thursdays") {
		t.Fatalf("recall by substring: %v", found)
	}

	// A new version does not reset it. This is why the live file is beside the
	// versions rather than inside one.
	changed := weekly()
	changed.Program = []byte("// v2\n")
	changed.Memory = []byte("a seed nobody should see now\n")
	if _, err := store.Mint(changed, first.Hash, "second pass"); err != nil {
		t.Fatal(err)
	}
	notes, _ = store.Memory("weekly-marketing").Recall(t.Context(), "")
	if len(notes) != 3 {
		t.Fatalf("minting v2 changed the memory to %d notes: %v", len(notes), notes)
	}
	// The version's own memory.md is untouched — it is history, not state.
	seed, err := os.ReadFile(filepath.Join(store.VersionDir("weekly-marketing", 1), MemoryFile))
	if err != nil || string(seed) != string(weekly().Memory) {
		t.Fatalf("v1's seed memory changed under it: %q, %v", seed, err)
	}
	// And a bundle carries the door, so the runtime has it without rebuilding a
	// path.
	bundle, _ := store.Load("weekly-marketing", 0)
	if bundle.Memory == nil || bundle.Memory.Path() != memory.Path() {
		t.Fatalf("a loaded bundle does not carry the memory door")
	}
}

// Recalling from a subharness nobody has run is an empty answer, not a mutation
// and not an error. Remembering from one with no bundle on disk still works,
// which is what a Go-native subharness's notes need — it will never have a
// bundle, and its memory belongs beside everybody else's.
func TestMemoryWorksForASubharnessWithNoBundleOnDisk(t *testing.T) {
	store := storeAt(t)
	notes, err := store.Memory("never-run").Recall(t.Context(), "anything")
	if err != nil || len(notes) != 0 {
		t.Fatalf("got %v: %v", notes, err)
	}
	if _, err := os.Stat(store.Dir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("asking created %s", store.Dir())
	}
	if err := store.Memory("linear").Remember(t.Context(), "this repository builds with make"); err != nil {
		t.Fatalf("a compiled-in subharness could not remember: %v", err)
	}
	notes, err = store.Memory("linear").Recall(t.Context(), "make")
	if err != nil || len(notes) != 1 {
		t.Fatalf("got %v: %v", notes, err)
	}
	// And it is still not a bundle: a name with notes and no versions is on no
	// list.
	if names, _ := store.Names(); len(names) != 0 {
		t.Fatalf("a memory made a subharness out of nothing: %v", names)
	}
}

// The last-run note is one line per name, overwritten, and it renders nothing.
// A subharness nobody has run has no note at all, which is what the emptiness
// law needs the surface to be told.
func TestTheLastRunNoteIsOnePerNameAndAbsentUntilThereIsOne(t *testing.T) {
	store := storeAt(t)
	if _, ok := store.LastRun("weekly-marketing"); ok {
		t.Fatalf("a subharness nobody has run has a last run")
	}
	if err := store.RecordRun("weekly-marketing", RunNote{Version: 1, Finished: true, CostUSD: 0.42}); err != nil {
		t.Fatal(err)
	}
	note, ok := store.LastRun("weekly-marketing")
	if !ok || !note.Finished || note.CostUSD != 0.42 || note.Version != 1 {
		t.Fatalf("the note came back as %#v (%v)", note, ok)
	}
	if note.At.IsZero() {
		t.Fatalf("the note has no time on it")
	}
	if err := store.RecordRun("weekly-marketing", RunNote{Version: 2, Why: "nobody was there to answer"}); err != nil {
		t.Fatal(err)
	}
	note, _ = store.LastRun("weekly-marketing")
	if note.Finished || note.Why != "nobody was there to answer" || note.Version != 2 {
		t.Fatalf("the second run did not replace the first: %#v", note)
	}
	entries, _ := os.ReadDir(store.nameDir("weekly-marketing"))
	count := 0
	for _, entry := range entries {
		if entry.Name() == lastRunFile {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d last-run notes on disk", count)
	}
}

// Mint validates before it writes, so every version a reader finds is one that
// can be handed to the runtime. Nothing is left on disk by a refusal.
func TestMintRefusesABundleThatCouldNotRunAndLeavesNothingBehind(t *testing.T) {
	store := storeAt(t)
	for _, one := range []struct {
		what  string
		files func() Files
		says  string
	}{
		{"a name with a space in it", func() Files {
			files := weekly()
			files.Manifest.Name = "weekly marketing"
			return files
		}, "one word"},
		{"no purpose", func() Files {
			files := weekly()
			files.Manifest.Purpose = ""
			return files
		}, "needs a purpose"},
		{"no program", func() Files {
			files := weekly()
			files.Program = nil
			return files
		}, "is a program"},
		{"an input schema that is not an object", func() Files {
			files := weekly()
			files.Manifest.Input = exec.Schema(`{"type":"string"}`)
			return files
		}, "has to be an object"},
	} {
		_, err := store.Mint(one.files(), "", "")
		if err == nil {
			t.Fatalf("%s was minted", one.what)
		}
		if !strings.Contains(err.Error(), one.says) {
			t.Fatalf("%s: the refusal says %q", one.what, err)
		}
	}
	if names, _ := store.Names(); len(names) != 0 {
		t.Fatalf("a refused mint left %v on disk", names)
	}
	entries, err := os.ReadDir(store.Dir())
	if err == nil && len(entries) != 0 {
		t.Fatalf("a refused mint left %d entries in the store", len(entries))
	}
}

// A mint that names a parent for a subharness with no versions is refused after
// it has taken that name's gate, and the gate is on no list.
//
// A DIRECTORY IS A SUBHARNESS WHEN IT HOLDS A VERSION, and that rule is what
// lets [Store.gated] make a name's directory and its `.mint.lock` before it can
// possibly know whether the mint will be allowed. The refusal spends no version
// number either: the next mint is still v1.
func TestARefusalAfterTheGateLeavesANameOnNoList(t *testing.T) {
	store := storeAt(t)
	_, err := store.Mint(weekly(), "sha256:nothing", "made from a version that is not there")
	if err == nil || !strings.Contains(err.Error(), "no versions yet") {
		t.Fatalf("a mint from a parent that was never written said %v", err)
	}
	if names, _ := store.Names(); len(names) != 0 {
		t.Fatalf("the refused mint put %v on the list", names)
	}
	if versions, err := store.Versions("weekly-marketing"); err != nil || len(versions) != 0 {
		t.Fatalf("the refused mint left %v: %v", versions, err)
	}
	minted, err := store.Mint(weekly(), "", "")
	if err != nil || minted.Version != 1 {
		t.Fatalf("the mint after the refusal is v%d: %v", minted.Version, err)
	}
}

// The syntax check is the runtime's, handed in as a field. A program that does
// not parse never reaches the disk.
func TestTheProgramIsParsedBeforeItIsWrittenWhenAParserIsWired(t *testing.T) {
	store := storeAt(t)
	store.Parse = func(program []byte) error {
		if strings.Contains(string(program), "syntax error") {
			return errors.New("unexpected token at line 1")
		}
		return nil
	}
	files := weekly()
	files.Program = []byte("syntax error\n")
	_, err := store.Mint(files, "", "")
	if err == nil || !strings.Contains(err.Error(), "does not parse") {
		t.Fatalf("a program that does not parse was minted: %v", err)
	}
	if names, _ := store.Names(); len(names) != 0 {
		t.Fatalf("it landed anyway: %v", names)
	}
	if _, err := store.Mint(weekly(), "", ""); err != nil {
		t.Fatalf("a program that parses was refused: %v", err)
	}
}

// runtimeMemory is jsrun.Memory, restated here rather than imported.
//
// The store may not import the runtime — that is the direction the [Build] seam
// exists to keep — so the way this package proves it still fits the door it
// feeds is by writing the door down and satisfying it. If the runtime's
// interface moves, this line is what says so, at the merge, instead of a nil
// Memory field discovered at somebody's first run.
type runtimeMemory interface {
	Remember(ctx context.Context, note string) error
	Recall(ctx context.Context, query string) ([]exec.Note, error)
}

func TestTheMemoryDoorIsTheOneTheRuntimeAsksFor(t *testing.T) {
	store := storeAt(t)
	var door runtimeMemory = store.Memory("weekly-marketing")
	if err := door.Remember(t.Context(), "it fits"); err != nil {
		t.Fatal(err)
	}
	notes, err := door.Recall(t.Context(), "fits")
	if err != nil || len(notes) != 1 {
		t.Fatalf("through the runtime's own door: %v, %v", notes, err)
	}
}

// stubRunner stands in for whatever the runtime lane builds out of a bundle.
// This package never runs one; it only has to hand one over.
type stubRunner struct{}

func (stubRunner) Manifest() exec.Manifest { return exec.Manifest{} }

func (stubRunner) Run(context.Context, json.RawMessage, exec.Env) (exec.RunResult, error) {
	return exec.RunResult{}, nil
}
