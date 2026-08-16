// Package basecontractcheck ports src/session/base-contract-check.ts lines
// 1-83 from swe-pro commit 3b25a1a. The path planner is pure; the copy loop is
// a best-effort filesystem shell behind a minimal interface.
package basecontractcheck

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// CopyPlanEntry mirrors the TS interface.
type CopyPlanEntry struct {
	Rel  string `json:"rel"`
	Src  string `json:"src"`
	Dest string `json:"dest"`
}

// PlanContractCopiesInput mirrors the TS interface. Paths is any so the
// runtime `typeof raw === "string"` guard remains observable for malformed
// arrays; normal callers pass []string.
//
// AssertedPaths is a deliberate divergence from the TS original — see
// BUGS-KEPT.md, "base contract check (src/session/base-contract-check.ts)".
type PlanContractCopiesInput struct {
	Workspace string `json:"workspace"`
	Worktree  string `json:"worktree"`
	Paths     any    `json:"paths"`
	// AssertedPaths names files the contract asserts ABOUT. They are excluded
	// from the copy plan even when they also appear in Paths: an agent that
	// double-lists a deliverable must fail SAFE (no copy), because a copied
	// deliverable makes the base check pass by construction.
	AssertedPaths any `json:"assertedPaths,omitempty"`
}

// PlanContractCopies resolves each registered workspace-relative path into a
// safe workspace-to-worktree copy entry. Invalid/escaping paths are dropped,
// normalized duplicates collapse, and first-occurrence order is preserved.
//
// Asserted paths are excluded — see PlanContractCopiesWithExclusions, which
// this delegates to when the caller does not need the exclusion list.
func PlanContractCopies(input PlanContractCopiesInput) []CopyPlanEntry {
	entries, _ := PlanContractCopiesWithExclusions(input)
	return entries
}

// PlanContractCopiesWithExclusions is PlanContractCopies plus the normalized
// relative paths that were withheld because the contract declared them
// asserted. A non-empty exclusion list is worth logging: it is either the
// schema working as intended, or an agent that listed its deliverable in
// `paths` as well, which is the exact contamination this guard exists to stop.
func PlanContractCopiesWithExclusions(
	input PlanContractCopiesInput,
) ([]CopyPlanEntry, []string) {
	paths := pathValues(input.Paths)
	if len(paths) == 0 {
		return []CopyPlanEntry{}, nil
	}
	wsRoot, err := filepath.Abs(input.Workspace)
	if err != nil {
		return []CopyPlanEntry{}, nil
	}
	wtRoot, err := filepath.Abs(input.Worktree)
	if err != nil {
		return []CopyPlanEntry{}, nil
	}
	asserted := normalizedRelSet(wsRoot, pathValues(input.AssertedPaths))
	excluded := []string{}
	out := []CopyPlanEntry{}
	seen := make(map[string]struct{})
	for _, value := range paths {
		raw, ok := value.(string)
		if !ok {
			continue
		}
		trimmed := jscompat.Trim(raw)
		if trimmed == "" || filepath.IsAbs(trimmed) {
			continue
		}
		src := filepath.Clean(filepath.Join(wsRoot, trimmed))
		rel, err := filepath.Rel(wsRoot, src)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		// Node path.relative(root, root) is "", while filepath.Rel is ".".
		if rel == "." {
			rel = ""
		}
		if rel == "" || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			continue
		}
		if _, duplicate := seen[rel]; duplicate {
			continue
		}
		seen[rel] = struct{}{}
		// Belt and braces: an asserted path is withheld even though the agent
		// also listed it as scaffolding. Copying it in is what turns the base
		// check into a tautology, so the overlap resolves toward NOT copying.
		if _, isAsserted := asserted[rel]; isAsserted {
			excluded = append(excluded, rel)
			continue
		}
		out = append(out, CopyPlanEntry{
			Rel: rel, Src: src, Dest: filepath.Clean(filepath.Join(wtRoot, filepath.FromSlash(rel))),
		})
	}
	return out, excluded
}

// normalizedRelSet resolves declared paths to the same workspace-relative,
// slash-separated form PlanContractCopies keys on, so "./a.txt", "a.txt" and
// "b/../a.txt" all name the same file. Invalid and escaping entries are
// dropped: they can never match a planned copy anyway.
func normalizedRelSet(wsRoot string, values []any) map[string]struct{} {
	set := make(map[string]struct{})
	for _, value := range values {
		raw, ok := value.(string)
		if !ok {
			continue
		}
		trimmed := jscompat.Trim(raw)
		if trimmed == "" || filepath.IsAbs(trimmed) {
			continue
		}
		rel, err := filepath.Rel(wsRoot, filepath.Clean(filepath.Join(wsRoot, trimmed)))
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == "" || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			continue
		}
		set[rel] = struct{}{}
	}
	return set
}

func pathValues(paths any) []any {
	switch values := paths.(type) {
	case nil:
		return nil
	case []any:
		return values
	case []string:
		out := make([]any, len(values))
		for i := range values {
			out[i] = values[i]
		}
		return out
	default:
		// The TS type is readonly string[] | undefined. Other non-array values
		// are outside the typed API.
		return nil
	}
}

type fileSystem interface {
	Exists(path string) bool
	MkdirAll(path string) error
	CopyFile(src, dest string) error
}

type osFileSystem struct{}

func (osFileSystem) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (osFileSystem) MkdirAll(path string) error {
	return os.MkdirAll(path, 0o777)
}

func (osFileSystem) CopyFile(src, dest string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()
	sourceInfo, err := source.Stat()
	if err != nil {
		return err
	}
	if destInfo, err := os.Stat(dest); err == nil && os.SameFile(sourceInfo, destInfo) {
		return errors.New("source and destination are the same file")
	}
	target, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, sourceInfo.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// ApplyContractCopies performs planned copies best-effort. Missing sources and
// per-entry failures are skipped; no error escapes and the return value counts
// only successful copies.
func ApplyContractCopies(entries []CopyPlanEntry) int {
	return applyContractCopies(entries, osFileSystem{})
}

func applyContractCopies(entries []CopyPlanEntry, fs fileSystem) int {
	copied := 0
	for _, entry := range entries {
		if !fs.Exists(entry.Src) {
			continue
		}
		if err := fs.MkdirAll(filepath.Dir(entry.Dest)); err != nil {
			continue
		}
		if err := fs.CopyFile(entry.Src, entry.Dest); err != nil {
			continue
		}
		copied++
	}
	return copied
}
