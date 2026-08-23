package substore

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// Bundle is one version of one subharness, whole, in memory.
//
// PROVISIONAL, AND FLAGGED AS SUCH. The runtime lane owns what a bundle has to
// carry in order to be run, and at the time this was written its worktree had no
// Bundle type of its own to code against. This is the store's honest answer to
// "what is on disk": the manifest, the program, the prompts every ai() call site
// names, the memory door, and the eval names that are inert until v3. If the
// runtime landed a different shape, [Build] is the seam where the two meet — the
// coordinator points it at an adapter, or this type moves into internal/exec
// beside the interface it feeds. Nothing else in this package depends on the
// field list.
type Bundle struct {
	// Name and Version are the identity. Version is the version this bundle
	// actually is, never the head at the time it was asked for, so a runner that
	// holds a bundle open across a mint keeps running what it loaded.
	Name    string
	Version int

	// Dir is the version directory on disk. A runner that wants to open a file
	// this struct did not read — an eval, an attachment a program writes beside
	// its prompts — starts here rather than rebuilding the layout.
	Dir string

	// Manifest is the bundle's manifest.json, validated. Its Provenance is
	// whatever the file claimed and means nothing: the registry stamps that field
	// from the layer the bundle was found at, which is the law
	// exec.Manifest.Provenance states.
	Manifest exec.Manifest

	// Program is program.js, as bytes. The store never interprets it; see
	// [Store.Parse] for the one check it makes, and who supplies it.
	Program []byte

	// Prompts is prompts/*.md by the name an ai() call site refers to them by —
	// the file's own name, "weekly-brief.md", not a path. Every ai() call names
	// one of these (PRD §6) and a promptRef that is not a key here is a bundle
	// that will fail at the call rather than at the load, because the store
	// cannot know which refs the program will reach.
	Prompts map[string][]byte

	// Seed is the memory.md this version was minted with. It is NOT the memory a
	// run reads and writes: a version is immutable, so what a run accumulates
	// cannot live inside one. See [Bundle.Memory] and [Store.Memory].
	Seed []byte

	// Evals names the checks in evals/, sorted. INERT UNTIL v3 and present from
	// v1 on purpose (PRD §6): the slot and the directory exist so that the
	// version that runs them is a new runner and not a bundle format migration.
	// Nothing in this build reads one.
	Evals []string

	// Memory is the live, subharness-scoped memory door — the backing for the
	// runtime's remember() and recall(). It is per NAME and not per version, and
	// it is seeded from [Bundle.Seed] the first time it is touched.
	Memory *Memory
}

// Files is what [Store.Mint] is handed: a bundle before it has a version.
type Files struct {
	// Manifest is the description. Its Name is the subharness being minted, and
	// [exec.Manifest.Validate] is what decides whether it may be written at all.
	Manifest exec.Manifest
	// Program is program.js. A bundle with no program is refused — that is the
	// one file that makes a directory a subharness rather than a description of
	// one.
	Program []byte
	// Prompts is prompts/*.md by filename. A name with no `.md` gets one, so a
	// caller that thought in refs and a caller that thought in files write the
	// same bundle.
	Prompts map[string][]byte
	// Memory is the seed memory.md. Empty is ordinary: most subharnesses learn
	// their domain notes rather than being shipped with them.
	Memory []byte
	// Evals is evals/* by filename, and the directory exists whether or not this
	// is empty.
	Evals map[string][]byte
}

// manifestFile is manifest.json as it is actually written: the contract's
// manifest, plus the eval names.
//
// THE EVAL SLOT LIVES HERE AND NOT IN exec.Manifest, which is what keeps PRD
// §6's "the directory and manifest slot exist from v1 so bundles do not need a
// format migration" true without growing the contract a field nothing in this
// phase reads. The embedding flattens, so the document on disk is one object:
// every field exec.Manifest writes, plus "evals". A later phase that wants the
// runner to see them moves the field one struct up and no bundle on disk
// changes a byte.
type manifestFile struct {
	exec.Manifest
	Evals []string `json:"evals,omitempty"`
}

// Version is one version's record — the v<N>.json beside the v<N>/ it describes.
//
// A SUBHARNESS'S LINEAGE READS LIKE A LOG (PRD §6) and these three fields are
// what make it one: what this version is, what it came from, and why somebody
// made it. The parent is a HASH and not a version number, because a number says
// only "the one before" and a hash says "these exact bytes" — which is the
// question anybody reading a lineage after a directory has been copied,
// restored, or pulled actually has.
type Version struct {
	// Version is the page number, which is also the directory name.
	Version int `json:"version"`
	// Hash is the content address: sha256 over the bundle's files, canonically
	// ordered. See [hashFiles] for exactly what is covered.
	Hash string `json:"hash"`
	// Parent is the hash of the version this one was minted from, and empty for
	// the first.
	Parent string `json:"parent,omitempty"`
	// ParentVersion is that same version's number, carried for readability. The
	// hash is the authority; this is so a person reading the file does not have
	// to grep for it.
	ParentVersion int `json:"parent_version,omitempty"`
	// Why is one line: what changed and what it was for. It is required of every
	// version that has a parent, because a lineage of unexplained versions is a
	// list of hashes and not a log.
	Why string `json:"why,omitempty"`
	// At is when it was minted, UTC.
	At time.Time `json:"at"`
}

// prompts writes a prompt map out in the shape the bundle directory holds it —
// sorted names, `.md` supplied where a caller thought in refs.
func promptNames(prompts map[string][]byte) []string {
	names := make([]string, 0, len(prompts))
	for name := range prompts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// promptFile is the filename one prompt is stored under.
func promptFile(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if !strings.HasSuffix(name, ".md") {
		return name + ".md"
	}
	return name
}

// encodeManifest writes a manifest document the way every one on disk is
// written: indented, newline-terminated, so that a person editing a bundle by
// hand and `git diff` over a project store both get something readable.
//
// THE BYTES ARE THE VERSION'S IDENTITY, so this function is the one place they
// are produced. A second spelling of the same manifest — different indentation,
// no trailing newline — would hash differently and mint a version in which
// nothing changed.
//
// One consequence worth stating rather than discovering. exec.Schema is the
// bytes a schema was written as, and indenting the document it rides in
// RE-INDENTS IT: a schema handed to [Store.Mint] compact comes back off disk
// spaced out. The schema's CONTENT is untouched — every key, every value, every
// order — and the encoding is a FIXED POINT, so minting a bundle that was loaded
// from this store produces the identical bytes and therefore the identical hash.
// That fixed point is the property content-addressing actually needs; byte-exact
// preservation of an author's whitespace is not, and buying it would cost the
// format its readability, which PRD §7's whole org-sharing story is built on.
// The files beside the manifest — program.js, the prompts, memory.md, the evals
// — are never re-encoded and are byte-identical, always.
func encodeManifest(manifest exec.Manifest, evals []string) ([]byte, error) {
	data, err := json.MarshalIndent(manifestFile{Manifest: manifest, Evals: evals}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("this manifest cannot be written down: %w", err)
	}
	return append(data, '\n'), nil
}

// decodeManifest reads one back, and answers in prose because the reader who
// needs to hear it is whoever wrote the file by hand.
func decodeManifest(data []byte) (exec.Manifest, []string, error) {
	var file manifestFile
	if err := json.Unmarshal(data, &file); err != nil {
		return exec.Manifest{}, nil, fmt.Errorf("its %s is not JSON: %w", ManifestFile, err)
	}
	return file.Manifest, file.Evals, nil
}
