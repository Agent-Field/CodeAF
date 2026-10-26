package taskcopy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// PortableEntry records the supported working representation, independently of Git filters.
type PortableEntry struct {
	Kind   string      `json:"kind"`
	Mode   fs.FileMode `json:"mode"`
	Size   int64       `json:"size,omitempty"`
	Digest string      `json:"digest,omitempty"`
	Link   string      `json:"link,omitempty"`
}

// PortableState excludes repository machinery and the isolated root's permissions.
// Timestamps are not part of the portable apply contract.
type PortableState struct {
	Version  int                      `json:"version"`
	Entries  map[string]PortableEntry `json:"entries"`
	Withheld map[string]PortableEntry `json:"withheld,omitempty"`
}

// Digest binds exact raw bytes, file kinds and supported permission metadata.
func (s PortableState) Digest() (string, error) {
	if s.Version != 1 || s.Entries == nil {
		return "", errors.New("portable metadata version is unsupported")
	}
	if err := validatePortableEntries(s.Withheld); err != nil {
		return "", err
	}
	if len(s.Entries)+len(s.Withheld) > workspacePathLimit {
		return "", errors.New("portable path metadata exceeds its bound")
	}
	if err := validatePortableEntries(s.Entries); err != nil {
		return "", err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	if len(raw) > WorkspaceCheckpointLimit {
		return "", errors.New("portable metadata exceeds its bound")
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// InspectPortableState reads the representation without following symbolic links.
func InspectPortableState(root string) (PortableState, error) {
	return inspectPortableState(root, false)
}

// InspectPortableFamilyInput records the bounded whole-folder inputs mirrored
// into a plain family's local history, including ignored runtime dependencies.
func InspectPortableFamilyInput(root string) (PortableState, error) {
	return inspectPortableState(root, true)
}

func inspectPortableState(root string, wholeFamily bool) (PortableState, error) {
	canonical, err := canonicalPortableRoot(root)
	if err != nil {
		return PortableState{}, err
	}
	root = canonical
	if err := portableNativeMetadataDirectory(root); err != nil {
		return PortableState{}, err
	}
	indexRoot, cleanup, err := portableInspectionIndex(root)
	if err != nil {
		return PortableState{}, err
	}
	defer cleanup()
	raw, err := portableInspectionGit(indexRoot, "--work-tree="+root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return PortableState{}, err
	}
	if len(raw) > WorkspaceCheckpointLimit {
		return PortableState{}, errors.New("portable enumeration exceeds its bound")
	}
	if wholeFamily {
		extra, extraErr := portableInspectionGit(indexRoot, "--work-tree="+root, "ls-files", "--others", "--ignored", "--exclude-standard", "-z")
		if extraErr != nil {
			return PortableState{}, extraErr
		}
		raw += extra
	}
	dirs, err := captureWorkspaceDirectoriesScope(root, indexRoot, wholeFamily || independentBaseline(root))
	if err != nil {
		return PortableState{}, err
	}
	paths := strings.Split(raw, "\x00")
	for path := range dirs {
		paths = append(paths, path)
	}
	state, err := InspectPortablePaths(root, paths)
	if err != nil {
		return state, err
	}
	state.Withheld = portableWithheldEntries(root, state.Entries)
	return state, nil
}

func portableInspectionIndex(root string) (string, func(), error) {
	if isRepository(root) {
		return root, func() {}, nil
	}
	temporary, err := os.MkdirTemp("", "codeaf-portable-index-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(temporary) }
	if _, err = portableInspectionGit(temporary, "init", "-q"); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if _, err = portableInspectionGit(temporary, "config", "--local", "core.fsmonitor", "false"); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return temporary, cleanup, nil
}

func inspectPortableEntry(path string, info fs.FileInfo) (PortableEntry, error) {
	entry := PortableEntry{Mode: info.Mode().Perm()}
	if info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return entry, errors.New("portable apply does not support special permission bits")
	}
	switch {
	case info.IsDir():
		entry.Kind = "dir"
	case info.Mode()&os.ModeSymlink != 0:
		entry.Kind = "symlink"
		link, err := os.Readlink(path)
		entry.Link = link
		return entry, err
	case info.Mode().IsRegular():
		entry.Kind = "file"
		entry.Size = info.Size()
		file, err := os.Open(path)
		if err != nil {
			return entry, err
		}
		opened, statErr := file.Stat()
		if statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			file.Close()
			return entry, errors.New("portable file identity changed before reading")
		}
		hash := sha256.New()
		n, err := io.Copy(hash, file)
		err = errors.Join(err, file.Close())
		if err != nil {
			return entry, err
		}
		if n != entry.Size {
			return entry, errors.New("portable file changed while being read")
		}
		entry.Digest = hex.EncodeToString(hash.Sum(nil))
	default:
		return entry, errors.New("portable apply encountered an unsupported file type")
	}
	return entry, nil
}

// VerifyPortableState refuses changes to a previously reviewed representation.
func VerifyPortableState(root string, expected PortableState) error {
	if _, err := expected.Digest(); err != nil {
		return err
	}
	actual, err := InspectPortableState(root)
	if err != nil {
		return err
	}
	if actual.Version != expected.Version || !reflect.DeepEqual(actual.Entries, expected.Entries) {
		return errors.New("the reviewed portable representation changed; review this root again")
	}
	return nil
}

// CapturePortableSnapshot reconstructs an independent reviewed representation
// through the same carrier machinery used by portable checkpoints. Both output
// paths must be separately owned and absent; the original copy is never edited.
func CapturePortableSnapshot(source, carrier, destination string) (PortableState, error) {
	var err error
	source, err = canonicalPortableRoot(source)
	if err != nil {
		return PortableState{}, err
	}
	carrier, err = canonicalPortableOutput(carrier)
	if err != nil {
		return PortableState{}, err
	}
	destination, err = canonicalPortableOutput(destination)
	if err != nil {
		return PortableState{}, err
	}

	before, err := InspectPortableState(source)
	if err != nil {
		return PortableState{}, err
	}
	if err = absentPortableOutput(carrier); err != nil {
		return PortableState{}, err
	}
	if err = absentPortableOutput(destination); err != nil {
		return PortableState{}, err
	}
	directories, err := captureWorkspaceDirectories(source)
	if err != nil {
		return PortableState{}, err
	}
	if err = carryWithHistory(source, carrier, true); err != nil {
		return PortableState{}, err
	}
	rec, err := readWorkspaceRecord(carrier)
	if err != nil {
		return PortableState{}, err
	}
	if err = validateWorkspaceRecord(carrier, rec); err != nil {
		return PortableState{}, err
	}
	if err = materializePortableSnapshot(carrier, destination, rec, directories); err != nil {
		return PortableState{}, err
	}
	if err := materializePortableSnapshotLinks(source, destination, before); err != nil {
		return PortableState{}, err
	}
	if independentBaseline(source) {
		if err := MarkIndependentBaseline(destination); err != nil {
			return PortableState{}, err
		}
	}
	state, err := InspectPortableState(destination)
	if err != nil {
		return PortableState{}, err
	}
	if err = verifyCapturedPortableSource(source, before, state); err != nil {
		return PortableState{}, err
	}
	state.Withheld = before.Withheld
	return state, nil
}

func absentPortableOutput(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("portable output must be an owned absolute path")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("portable output already exists or cannot be inspected")
	}
	return nil
}

func materializePortableSnapshot(carrier, destination string, rec record, directories map[string]fs.FileMode) error {
	if err := prepareResultCheckout("", destination, carrier, rec); err != nil {
		return err
	}
	if err := validateWorkspaceOverlay(destination, carrier, rec); err != nil {
		return err
	}
	if err := prepareWorkspaceDirectories(destination, directories); err != nil {
		return err
	}
	if err := applyEOL(destination, rec.EOL); err != nil {
		return err
	}
	if err := overlay(filepath.Join(carrier, filesDir), destination, rec); err != nil {
		return err
	}
	if err := applyModes(destination, rec.Modes); err != nil {
		return err
	}
	if err := finishWorkspaceDirectories(destination, directories); err != nil {
		return err
	}
	return restorePortableBaseline(destination, rec)
}

func verifyCapturedPortableSource(source string, before, captured PortableState) error {
	after, err := inspectPortableCoverage(source, before)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(before, after) {
		return errors.New("private copy changed during review; review it again")
	}
	for path := range before.Entries {
		if _, present := captured.Entries[path]; !present {
			if _, withheld := before.Withheld[path]; !withheld {
				return fmt.Errorf("portable snapshot omitted supported input %s; private copy retained", path)
			}
		}
	}
	for path, entry := range captured.Entries {
		if before.Entries[path] != entry {
			return fmt.Errorf("portable snapshot could not preserve %s; private copy retained", path)
		}
	}
	return nil
}

// CheckPortableMutationMetadata refuses unsupported metadata only on paths an
// apply intends to change. Unrelated project metadata grants no write authority.
func CheckPortableMutationMetadata(root string, paths []string) error {
	return CheckPortableMutationMetadataContext(context.Background(), root, paths)
}

// CheckPortableMutationMetadataContext bounds platform probes to this operation.
func CheckPortableMutationMetadataContext(ctx context.Context, root string, paths []string) error {
	canonical, err := canonicalPortableRoot(root)
	if err != nil {
		return err
	}
	root = canonical
	seen := map[string]bool{}
	var existing []string
	for _, rel := range paths {
		if err = ctx.Err(); err != nil {
			return err
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		if !filepath.IsLocal(filepath.FromSlash(rel)) {
			return errors.New("portable mutation path escapes its root")
		}
		parent, err := workspacePath(root, filepath.Dir(filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		path := filepath.Join(parent, filepath.Base(filepath.FromSlash(rel)))
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err = portablePlatformMetadata(path, info); err != nil {
			return fmt.Errorf("portable metadata for %s: %w", rel, err)
		}
		existing = append(existing, path)
	}
	return portablePlatformMetadataBatch(ctx, existing)
}

// MaterializePortableInput restores source representation into a newly owned
// private Git copy before any worker can use it.
func MaterializePortableInput(source, destination string, baseline PortableState) error {
	var err error
	source, err = canonicalPortableRoot(source)
	if err != nil {
		return err
	}
	destination, err = canonicalPortableRoot(destination)
	if err != nil {
		return err
	}

	if err := verifyCapturedPortableSource(source, baseline, baseline); err != nil {
		return err
	}
	directories, err := captureWorkspaceDirectoriesWithIndex(source, destination)
	if err != nil {
		return err
	}
	if err = prepareWorkspaceDirectories(destination, directories); err != nil {
		return err
	}
	paths, err := portableInspectionGit(destination, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	sourceRoot, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer sourceRoot.Close()
	targetRoot, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer targetRoot.Close()
	selected := map[string]bool{}
	for _, path := range strings.Split(paths, "\x00") {
		selected[path] = true
	}
	for path, entry := range baseline.Entries {
		if entry.Kind != "dir" {
			selected[path] = true
		}
	}
	for path := range selected {
		if path == "" || cell.IsTaskDropping(path) || portableNativeMetadataPath(path) {
			continue
		}
		entry, present := baseline.Entries[path]
		if !present {
			if err = targetRoot.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		if err = materializePortableInputEntry(sourceRoot, targetRoot, path, entry); err != nil {
			return err
		}
	}
	if err = finishWorkspaceDirectories(destination, directories); err != nil {
		return err
	}
	represented, err := InspectPortableState(destination)
	if err != nil {
		return err
	}
	return verifyCapturedPortableSource(source, baseline, represented)
}

func materializePortableInputEntry(source, target *os.Root, path string, entry PortableEntry) error {
	if entry.Kind == "dir" {
		return nil
	}
	if !filepath.IsLocal(filepath.FromSlash(path)) {
		return errors.New("portable input path is unsafe")
	}
	if err := target.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if entry.Kind == "symlink" {
		if err := target.Symlink(entry.Link, path); err != nil {
			return err
		}
		return setPortableSymlinkMode(target, path, entry.Mode)
	}
	if entry.Kind != "file" {
		return errors.New("portable input kind is unsupported")
	}
	file, err := source.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	named, statErr := source.Lstat(path)
	if statErr != nil || !info.Mode().IsRegular() || !os.SameFile(info, named) {
		return errors.New("portable source changed its file identity")
	}
	out, err := target.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, hash), file)
	err = errors.Join(err, out.Close())
	if err != nil {
		return err
	}
	if n != entry.Size || hex.EncodeToString(hash.Sum(nil)) != entry.Digest {
		return errors.New("portable source changed during input materialization")
	}
	return target.Chmod(path, entry.Mode)
}

// PortableBaselinePath holds historical source metadata, never landing permission.
const PortableBaselinePath = cell.TaskDropping + "/portable-baseline.json"

// WritePortableBaseline creates historical metadata exactly once.
func WritePortableBaseline(root string, state PortableState) (string, error) {
	digest, err := state.Digest()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Join(root, cell.TaskDropping), 0700); err != nil {
		return "", err
	}
	boundary, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer boundary.Close()
	file, err := boundary.OpenFile(PortableBaselinePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, err = file.Write(raw)
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return digest, err
	}
	directory, err := boundary.Open(cell.TaskDropping)
	if err != nil {
		return digest, err
	}
	err = errors.Join(directory.Sync(), directory.Close())
	return digest, err
}

// ReadPortableBaseline never refreshes historical metadata from today's source.
func ReadPortableBaseline(root string) (PortableState, string, error) {
	raw, err := readWorkspaceFile(filepath.Join(root, PortableBaselinePath), WorkspaceCheckpointLimit)
	if err != nil {
		return PortableState{}, "", err
	}
	return decodePortableBaseline(raw)
}

func decodePortableBaseline(raw []byte) (PortableState, string, error) {
	var state PortableState
	if len(raw) > WorkspaceCheckpointLimit {
		return state, "", errors.New("portable baseline exceeds its bound")
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, "", err
	}
	digest, err := state.Digest()
	return state, digest, err
}

func restorePortableBaseline(root string, rec record) error {
	if len(rec.PortableBaseline) == 0 {
		return nil
	}
	state, digest, err := decodePortableBaseline(rec.PortableBaseline)
	if err != nil {
		return err
	}
	_, existing, err := ReadPortableBaseline(root)
	if err == nil {
		if existing != digest {
			return errors.New("existing creation baseline differs; preserved both copies")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err = WritePortableBaseline(root, state)
	return err
}

func validatePortableEntries(entries map[string]PortableEntry) error {
	if len(entries) > workspacePathLimit {
		return errors.New("portable path count exceeds its bound")
	}
	for path, entry := range entries {
		if portableNativeMetadataPath(path) {
			return errors.New("portable entry names native control metadata")
		}
		if !filepath.IsLocal(filepath.FromSlash(path)) || path == "." || len(path) > 4096 || strings.Contains(path, "\\") || filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) != path {
			return errors.New("portable entry path is unsafe")
		}
		for _, part := range strings.Split(path, "/") {
			if part == ".git" || cell.IsTaskDropping(part) {
				return errors.New("portable entry names administrative state")
			}
		}
		if err := validatePortableEntry(entry); err != nil {
			return err
		}
	}
	return nil
}

func validatePortableEntry(entry PortableEntry) error {
	if entry.Mode != entry.Mode.Perm() {
		return errors.New("portable permission metadata is unsupported")
	}
	switch entry.Kind {
	case "file":
		raw, err := hex.DecodeString(entry.Digest)
		if err != nil || len(raw) != sha256.Size || entry.Size < 0 || entry.Link != "" {
			return errors.New("portable file metadata is malformed")
		}
	case "dir":
		if entry.Digest != "" || entry.Link != "" || entry.Size != 0 {
			return errors.New("portable directory metadata is malformed")
		}
	case "symlink":
		if entry.Digest != "" || entry.Size != 0 || entry.Link == "" {
			return errors.New("portable symlink metadata is malformed")
		}
	default:
		return errors.New("portable file kind is unsupported")
	}
	return nil
}

// InspectPortablePaths includes explicit collisions even when ignored. Directories
// are inspected as entries; unrelated descendant bytes are never read.
func InspectPortablePaths(root string, paths []string) (PortableState, error) {
	canonical, err := canonicalPortableRoot(root)
	if err != nil {
		return PortableState{}, err
	}
	root = canonical
	if err := portableNativeMetadataDirectory(root); err != nil {
		return PortableState{}, err
	}
	state := PortableState{Version: 1, Entries: map[string]PortableEntry{}}
	for _, rel := range paths {
		if rel == "" || rel == "." || cell.IsTaskDropping(rel) || portableNativeMetadataPath(rel) {
			continue
		}
		if _, ok := state.Entries[rel]; ok {
			continue
		}
		if len(state.Entries) >= workspacePathLimit {
			return state, errors.New("portable path count exceeds its bound")
		}
		if !filepath.IsLocal(filepath.FromSlash(rel)) {
			return state, errors.New("portable inspection path is unsafe")
		}
		parent, err := workspacePath(root, filepath.Dir(filepath.FromSlash(rel)))
		if err != nil {
			return state, err
		}
		path := filepath.Join(parent, filepath.Base(filepath.FromSlash(rel)))
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return state, err
		}
		entry, err := inspectPortableEntry(path, info)
		if err != nil {
			return state, err
		}
		state.Entries[filepath.ToSlash(rel)] = entry
	}
	_, err = state.Digest()
	return state, err
}

// Resolve the caller-supplied anchor once. Descendant symlinks still refuse in
// workspacePath; a platform alias such as Darwin /var is not a child redirect.
func canonicalPortableRoot(root string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", errors.New("portable root must be absolute")
	}
	return filepath.EvalSymlinks(root)
}
func canonicalPortableOutput(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", errors.New("portable output must be absolute")
	}
	parent, err := canonicalPortableRoot(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

func portableInspectionGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	args = append([]string{"-c", "core.fsmonitor=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(portableInspectionEnvironment(), "GIT_OPTIONAL_LOCKS=0")
	stderr := portableBoundedOutput{Limit: 64 << 10}
	stdout := portableBoundedOutput{Limit: WorkspaceCheckpointLimit}
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	err := cmd.Run()
	raw := stdout.Bytes()
	if ctx.Err() != nil {
		return "", fmt.Errorf("portable Git enumeration timed out: %w", ctx.Err())
	}
	if err != nil {
		return "", fmt.Errorf("portable Git enumeration failed: %w: %s", err, stderr.String())
	}
	if len(raw) > WorkspaceCheckpointLimit {
		return "", errors.New("portable Git enumeration exceeds its bound")
	}
	return string(raw), nil
}

type portableBoundedOutput struct {
	bytes.Buffer
	Limit int
}

func (b *portableBoundedOutput) Write(raw []byte) (int, error) {
	if b.Len()+len(raw) > b.Limit {
		return 0, errors.New("portable Git output exceeds its bound")
	}
	return b.Buffer.Write(raw)
}

func verifyRestoredPortableBaseline(root string, rec record) error {
	if len(rec.PortableBaseline) == 0 {
		return nil
	}
	_, expected, err := decodePortableBaseline(rec.PortableBaseline)
	if err != nil {
		return err
	}
	_, actual, err := ReadPortableBaseline(root)
	if err != nil {
		return err
	}
	if actual != expected {
		return errors.New("restored creation baseline changed; preserved the independent carrier")
	}
	return nil
}

func portableWithheldEntries(root string, entries map[string]PortableEntry) map[string]PortableEntry {
	var files []string
	for path, entry := range entries {
		if entry.Kind == "file" {
			files = append(files, path)
		}
	}
	allowed := map[string]bool{}
	for _, path := range withoutSecrets(root, files) {
		allowed[path] = true
	}
	var withheld map[string]PortableEntry
	for _, path := range files {
		if !allowed[path] {
			if withheld == nil {
				withheld = map[string]PortableEntry{}
			}
			withheld[path] = entries[path]
		}
	}
	return withheld
}

func portableInspectionEnvironment() []string {
	var result []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		name = strings.ToUpper(name)
		if strings.HasPrefix(name, "GIT_") && name != "GIT_CONFIG_GLOBAL" && name != "GIT_CONFIG_SYSTEM" && name != "GIT_CONFIG_NOSYSTEM" {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func materializePortableSnapshotLinks(source, destination string, state PortableState) error {
	input, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer output.Close()
	for path, entry := range state.Entries {
		if entry.Kind != "symlink" {
			continue
		}
		if err = materializePortableInputEntry(input, output, path, entry); err != nil {
			return err
		}
	}
	return nil
}

// CheckPortableRootInheritance checks native metadata inherited by new children,
// without changing or requiring ownership of the existing project root.
func CheckPortableRootInheritance(ctx context.Context, root string) error {
	canonical, err := canonicalPortableRoot(root)
	if err != nil {
		return err
	}
	info, err := os.Lstat(canonical)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("portable destination root is not a directory")
	}
	if err = portableUnsupportedNativeMetadata(canonical, info); err != nil {
		return err
	}
	return portablePlatformMetadataBatch(ctx, []string{canonical})
}

func inspectPortableCoverage(root string, expected PortableState) (PortableState, error) {
	state, err := InspectPortableState(root)
	if err != nil {
		return state, err
	}
	var missing []string
	for path := range expected.Entries {
		if _, ok := state.Entries[path]; !ok {
			missing = append(missing, path)
		}
	}
	explicit, err := InspectPortablePaths(root, missing)
	if err != nil {
		return state, err
	}
	for path, entry := range explicit.Entries {
		state.Entries[path] = entry
	}
	for path := range expected.Withheld {
		if entry, ok := state.Entries[path]; ok {
			if state.Withheld == nil {
				state.Withheld = map[string]PortableEntry{}
			}
			state.Withheld[path] = entry
		}
	}
	return state, nil
}

func setPortableSymlinkMode(root *os.Root, path string, mode fs.FileMode) error {
	parent, err := root.Open(filepath.Dir(filepath.FromSlash(path)))
	if err != nil {
		return err
	}
	defer parent.Close()
	return SetPortableSymlinkModeAt(int(parent.Fd()), filepath.Base(filepath.FromSlash(path)), mode)
}

// The native engine reserves this top-level directory for its workspace IDs.
// Nested directories with the same spelling remain ordinary project inputs.
func portableNativeMetadataPath(path string) bool {
	return path == ".furrow" || strings.HasPrefix(path, ".furrow/")
}
func portableNativeMetadataDirectory(root string) error {
	info, err := os.Lstat(filepath.Join(root, ".furrow"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("native workspace metadata is not a directory; preserve this private copy")
	}
	return nil
}
