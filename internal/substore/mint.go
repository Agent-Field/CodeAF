package substore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Mint writes a new version of a subharness. It is the only door that writes a
// bundle, and it validates before it writes anything at all, so every version a
// reader finds is one the runtime can be handed.
//
// VERSIONS ARE MINTED, NEVER CHOSEN (PRD §6). The caller does not say which
// version it is making; it says which version it MADE THIS FROM, by that
// version's hash, and one line of why. That is optimistic concurrency in its
// smallest honest form: a design flow that read v2, thought for a minute and
// came back to save finds out here that somebody else saved v3 in the meantime,
// instead of silently minting a v4 that threw their work away.
//
//   - parent empty, nothing on disk: this is v1.
//   - parent empty, versions on disk: refused. Say what this came from.
//   - parent is the head's hash, and the content is the head's content: nothing
//     changed, so nothing is minted and the head is returned. Minting is
//     IDEMPOTENT, which is what "content-addressed" has to mean for a flow that
//     re-saves an unedited bundle.
//   - parent is the head's hash, content differs: v(head+1), recording the
//     parent hash and the why.
//   - parent is anything else: refused, naming the head it disagrees with.
//   - two mints racing for the same version: exactly one wins, and the loser is
//     refused with [ErrExists] rather than shifted along to the next number. See
//     [Store.write] for why a retry would write a lineage that lies.
//
// Content that an ANCESTOR once had is still a new version. A revert is a real
// event with its own place in the lineage, and collapsing it onto the version it
// restored would move the head backwards — which is the one thing the
// no-head-file rule cannot survive.
func (s *Store) Mint(files Files, parent, why string) (Version, error) {
	name := strings.TrimSpace(files.Manifest.Name)
	if err := files.Manifest.Validate(); err != nil {
		return Version{}, err
	}
	if len(files.Program) == 0 {
		return Version{}, fmt.Errorf("%q has no %s — a subharness is a program, and this is a description of one", name, ProgramFile)
	}
	if s.Parse != nil {
		if err := s.Parse(files.Program); err != nil {
			return Version{}, fmt.Errorf("%q's %s does not parse: %w", name, ProgramFile, err)
		}
	}
	for promptName := range files.Prompts {
		if promptFile(promptName) == "" {
			return Version{}, fmt.Errorf("%q has a prompt with no name", name)
		}
	}

	// The bundle is laid out in memory first, because the hash is over the FILES
	// and the same layout is what gets staged. Two passes over one map would be
	// two chances for them to disagree about what was written.
	laid, err := layout(files)
	if err != nil {
		return Version{}, err
	}
	hash := hashFiles(laid)

	records, err := s.records(name)
	if err != nil {
		return Version{}, err
	}
	head, headErr := s.Head(name)
	var headRecord Version
	if headErr == nil {
		headRecord, err = s.Record(name, head)
		if err != nil {
			return Version{}, err
		}
	}

	parent = strings.TrimSpace(parent)
	switch {
	case parent == "" && headErr == nil:
		return Version{}, fmt.Errorf("%q is already at v%d — say which version this one comes from (its hash is %s)",
			name, head, headRecord.Hash)
	case parent != "" && headErr != nil:
		return Version{}, fmt.Errorf("%q has no versions yet, so this one cannot come from %s", name, parent)
	case parent != "" && parent != headRecord.Hash:
		return Version{}, fmt.Errorf("%q has moved on: this was made from %s and the current version is v%d, %s",
			name, parent, head, headRecord.Hash)
	}
	if parent != "" && strings.TrimSpace(why) == "" {
		return Version{}, fmt.Errorf("%q's new version needs one line saying what changed — the lineage is meant to read like a log", name)
	}
	// Nothing changed. The head IS this bundle, so there is nothing to mint and
	// the caller is handed what it already had.
	if headErr == nil && headRecord.Hash == hash {
		return headRecord, nil
	}

	next := 1
	if len(records) > 0 {
		next = records[len(records)-1] + 1
	}
	record := Version{
		Version:       next,
		Hash:          hash,
		Parent:        parent,
		ParentVersion: headRecord.Version,
		Why:           strings.TrimSpace(why),
		At:            s.now().UTC(),
	}
	written, err := s.write(name, record, laid)
	if err != nil {
		return Version{}, err
	}
	return written, nil
}

// write stages the whole bundle, claims its version, and moves the staging
// directory into place.
//
// THE CLAIM IS THE RECORD FILE AND THE LINK IS WHAT MAKES IT ONE. os.Link
// refuses a name that exists, and it refuses it in the kernel, so two processes
// minting at the same instant cannot both believe they own v2: exactly one link
// succeeds and the other is refused with [ErrExists]. The bundle
// directory is renamed into place only by the winner, and only after the claim,
// so a reader either sees a complete version or sees nothing — never a directory
// filling up under it.
func (s *Store) write(name string, record Version, laid []file) (Version, error) {
	if err := os.MkdirAll(s.nameDir(name), 0o755); err != nil {
		return Version{}, err
	}
	stage, err := os.MkdirTemp(s.nameDir(name), mintPrefix+"*")
	if err != nil {
		return Version{}, err
	}
	defer os.RemoveAll(stage)
	for _, entry := range laid {
		path := filepath.Join(stage, filepath.FromSlash(entry.path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return Version{}, err
		}
		if err := os.WriteFile(path, entry.data, 0o644); err != nil {
			return Version{}, err
		}
	}
	// evals/ exists whether or not anything is in it. See [Bundle.Evals]: the
	// directory is half of the format that does not need a migration.
	if err := os.MkdirAll(filepath.Join(stage, EvalsDir), 0o755); err != nil {
		return Version{}, err
	}
	// os.MkdirTemp makes its directory private to the process that made it, and
	// this one is about to become a version page every reader of the store opens.
	if err := os.Chmod(stage, 0o755); err != nil {
		return Version{}, err
	}

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return Version{}, err
	}
	err = writeNew(s.recordPath(name, record.Version), append(data, '\n'))
	if errors.Is(err, os.ErrExist) {
		// Somebody claimed this version between our count and our link.
		//
		// THE ANSWER IS A REFUSAL AND NOT A RETRY AT v+1. A mint carries the hash
		// of the version it was made from, and a writer that lost this race was
		// made from a version that is no longer the head — minting it one page
		// further along would write a child of v1 into the slot after v2 and
		// leave a lineage that says a thing that did not happen. So the loser is
		// told what [Store.Mint] tells anybody whose parent has moved on: reload,
		// and decide what to do about the version that arrived.
		return Version{}, fmt.Errorf("%w: %s v%d was written by somebody else while this one was being made — load it and mint again from there",
			ErrExists, name, record.Version)
	}
	if err != nil {
		return Version{}, fmt.Errorf("%s: writing v%d: %w", name, record.Version, err)
	}
	if err := os.Rename(stage, s.VersionDir(name, record.Version)); err != nil {
		return Version{}, fmt.Errorf("%s: putting v%d in place: %w", name, record.Version, err)
	}
	return record, nil
}

// Record reads one version's record. Version 0 is the head.
func (s *Store) Record(name string, version int) (Version, error) {
	if version == 0 {
		head, err := s.Head(name)
		if err != nil {
			return Version{}, err
		}
		version = head
	}
	data, err := os.ReadFile(s.recordPath(name, version))
	if errors.Is(err, os.ErrNotExist) {
		return Version{}, fmt.Errorf("%w: %s v%d", ErrNotFound, name, version)
	}
	if err != nil {
		return Version{}, err
	}
	var record Version
	if err := json.Unmarshal(data, &record); err != nil {
		return Version{}, fmt.Errorf("%s v%d: its record is not readable: %w", name, version, err)
	}
	// The record says which version it is and so does its filename. They can only
	// disagree if somebody moved a file, and a version that lies about its own
	// number would be recorded as that lie in every run it does.
	if record.Version != version {
		return Version{}, fmt.Errorf("%s v%d: the record says v%d", name, version, record.Version)
	}
	return record, nil
}

// Lineage is every version of one subharness, oldest first — the log PRD §6 asks
// a subharness's history to read like.
func (s *Store) Lineage(name string) ([]Version, error) {
	versions, err := s.Versions(name)
	if err != nil {
		return nil, err
	}
	lineage := make([]Version, 0, len(versions))
	for _, version := range versions {
		record, err := s.Record(name, version)
		if err != nil {
			s.notice("%s v%d: %v — leaving it out of the lineage", name, version, err)
			continue
		}
		lineage = append(lineage, record)
	}
	return lineage, nil
}

// file is one entry in a bundle's canonical layout: a slash-separated path
// relative to the version directory, and its bytes.
type file struct {
	path string
	data []byte
}

// layout turns the files a caller handed in into the bundle exactly as it will
// sit on disk. It is the one place the layout is decided, so the thing that gets
// hashed and the thing that gets written cannot be two different bundles.
func layout(files Files) ([]file, error) {
	evals := make([]string, 0, len(files.Evals))
	for name := range files.Evals {
		evals = append(evals, name)
	}
	sort.Strings(evals)

	manifest, err := encodeManifest(files.Manifest, evals)
	if err != nil {
		return nil, err
	}
	laid := []file{
		{path: ManifestFile, data: manifest},
		{path: ProgramFile, data: files.Program},
	}
	for _, name := range promptNames(files.Prompts) {
		laid = append(laid, file{path: PromptsDir + "/" + promptFile(name), data: files.Prompts[name]})
	}
	// memory.md is written whether or not there is a seed, because a bundle whose
	// layout depends on whether somebody wrote domain notes is a bundle a reader
	// has to check two shapes for.
	laid = append(laid, file{path: MemoryFile, data: files.Memory})
	for _, name := range evals {
		laid = append(laid, file{path: EvalsDir + "/" + name, data: files.Evals[name]})
	}
	sort.Slice(laid, func(i, j int) bool { return laid[i].path < laid[j].path })
	return laid, nil
}

// hashFiles is the content address: sha256 over every file in the bundle, by
// sorted path, each one length-prefixed.
//
// THE LENGTHS ARE WHAT MAKE IT A HASH OF A FILE SET rather than of a
// concatenation. Without them, moving a byte from the end of one file to the
// start of the next would leave the digest untouched, and two different bundles
// would share an address — which is the one failure a content-addressed store
// cannot have. The path is hashed with its bytes for the same reason: renaming a
// prompt is a change.
func hashFiles(laid []file) string {
	digest := sha256.New()
	for _, entry := range laid {
		fmt.Fprintf(digest, "%d:%s\n%d:", len(entry.path), entry.path, len(entry.data))
		digest.Write(entry.data)
		digest.Write([]byte{'\n'})
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}
