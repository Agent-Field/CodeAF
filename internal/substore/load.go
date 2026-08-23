package substore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// Load reads one version of one subharness whole. Version 0 is the head, and any
// other number is that version and only that version — which is what makes a pin
// durable: v1 loads the same bytes after v2 is minted.
//
// A BUNDLE THAT DOES NOT VALIDATE IS AN ERROR HERE AND ABSENT ABOVE. This door
// answers honestly, because whoever called it named a version and deserves to be
// told what is wrong with it. The doors a person's lists are drawn from —
// [Store.Runner] and [Store.Manifests] — turn that error into a journal line and
// an absence instead, which is the codebase's law about a capability that cannot
// work: absent, never present-and-broken.
func (s *Store) Load(name string, version int) (Bundle, error) {
	if version == 0 {
		head, err := s.Head(name)
		if err != nil {
			return Bundle{}, err
		}
		version = head
	}
	dir := s.VersionDir(name, version)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return Bundle{}, fmt.Errorf("%w: %s v%d", ErrNotFound, name, version)
	}

	manifestBytes, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if errors.Is(err, os.ErrNotExist) {
		return Bundle{}, fmt.Errorf("%s v%d has no %s", name, version, ManifestFile)
	}
	if err != nil {
		return Bundle{}, err
	}
	manifest, evals, err := decodeManifest(manifestBytes)
	if err != nil {
		return Bundle{}, fmt.Errorf("%s v%d: %w", name, version, err)
	}
	if err := manifest.Validate(); err != nil {
		return Bundle{}, fmt.Errorf("%s v%d: %w", name, version, err)
	}
	// The directory a bundle lives in IS its name, and a manifest that says
	// otherwise would be dispatched to under one name and describe itself with
	// another — the same disagreement the old store refused between a page and its
	// filename, for the same reason.
	if manifest.Name != name {
		return Bundle{}, fmt.Errorf("%s v%d calls itself %q — the directory is the name", name, version, manifest.Name)
	}

	program, err := os.ReadFile(filepath.Join(dir, ProgramFile))
	if errors.Is(err, os.ErrNotExist) {
		return Bundle{}, fmt.Errorf("%s v%d has no %s", name, version, ProgramFile)
	}
	if err != nil {
		return Bundle{}, err
	}
	if len(program) == 0 {
		return Bundle{}, fmt.Errorf("%s v%d's %s is empty", name, version, ProgramFile)
	}

	prompts, err := readPrompts(filepath.Join(dir, PromptsDir))
	if err != nil {
		return Bundle{}, fmt.Errorf("%s v%d: reading its prompts: %w", name, version, err)
	}
	seed, err := os.ReadFile(filepath.Join(dir, MemoryFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Bundle{}, err
	}
	sort.Strings(evals)

	return Bundle{
		Name:     name,
		Version:  version,
		Dir:      dir,
		Manifest: manifest,
		Program:  program,
		Prompts:  prompts,
		Seed:     seed,
		Evals:    evals,
		Memory:   s.Memory(name),
	}, nil
}

// Manifest reads one version's manifest without its program, for the lists.
// Reading a whole bundle in order to draw a row would put every program.js in
// the tree into memory at launch.
func (s *Store) Manifest(name string, version int) (exec.Manifest, error) {
	if version == 0 {
		head, err := s.Head(name)
		if err != nil {
			return exec.Manifest{}, err
		}
		version = head
	}
	data, err := os.ReadFile(filepath.Join(s.VersionDir(name, version), ManifestFile))
	if errors.Is(err, os.ErrNotExist) {
		return exec.Manifest{}, fmt.Errorf("%w: %s v%d's %s", ErrNotFound, name, version, ManifestFile)
	}
	if err != nil {
		return exec.Manifest{}, err
	}
	manifest, _, err := decodeManifest(data)
	if err != nil {
		return exec.Manifest{}, fmt.Errorf("%s v%d: %w", name, version, err)
	}
	if err := manifest.Validate(); err != nil {
		return exec.Manifest{}, fmt.Errorf("%s v%d: %w", name, version, err)
	}
	if manifest.Name != name {
		return exec.Manifest{}, fmt.Errorf("%s v%d calls itself %q — the directory is the name", name, version, manifest.Name)
	}
	return manifest, nil
}

// readPrompts reads prompts/*.md into the map an ai() call site looks a ref up
// in. A bundle with no prompts directory has no prompts, which is not a fault: a
// program whose every step is a tool call spends nothing on the model and names
// no prompt.
func readPrompts(dir string) (map[string][]byte, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	prompts := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		prompts[entry.Name()] = data
	}
	return prompts, nil
}
