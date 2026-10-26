package taskcopy

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// WorkspaceBinding is a complete local placement awaiting registry admission.
type WorkspaceBinding struct {
	Entry WorkspaceEntry
	Path  string
}

// WorkspaceRestore preserves a whole-layout preparation until adoption finishes.
// Callers must admit every returned binding before Acknowledge permits execution.
type WorkspaceRestore struct {
	cell     cell.Cell
	manifest workspaceManifest
	receipt  workspaceReceipt
}
type workspaceReceipt struct {
	Version    int               `json:"version"`
	Checkpoint string            `json:"checkpoint_sha256"`
	Generation string            `json:"generation"`
	Stage      string            `json:"stage"`
	Phase      string            `json:"phase"`
	Complete   []string          `json:"complete"`
	Hashes     map[string]string `json:"hashes"`
}

func workspaceReceiptPath(c cell.Cell) string {
	return filepath.Join(c.Root, ".codeaf", "workspace-restore.json")
}

// PrepareWorkspace validates every carrier before staging a whole owned layout.
// It never prunes, unbundles into, or changes a borrowed project repository.
func (Carry) PrepareWorkspace(c cell.Cell, plan WorkspacePlan) (*WorkspaceRestore, error) {
	incoming, digest, err := ReadWorkspacePlan(c)
	if err != nil {
		return nil, err
	}
	if incoming == nil || !reflect.DeepEqual(*incoming, plan) {
		return nil, errors.New("workspace checkpoint does not match the requested transfer")
	}
	// Acknowledged local state may have rewritten path hints since its original
	// import. Its existing local registry authority is checked independently;
	// incoming ValidateWorkspace remains strict about the exact checkpoint.
	if prior, ok, err := adoptedWorkspace(c, plan); err != nil {
		return nil, err
	} else if ok {
		return prior, nil
	}
	manifest, err := validateWorkspaceCarrier(c, plan, digest)
	if err != nil {
		return nil, err
	}
	private, err := workspacePath(c.Root, ".codeaf")
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(private, 0700); err != nil {
		return nil, err
	}
	restore := &WorkspaceRestore{cell: c, manifest: manifest}
	raw, err := readWorkspaceFile(workspaceReceiptPath(c), workspaceManifestLimit)
	if err == nil {
		if err = json.Unmarshal(raw, &restore.receipt); err != nil {
			return nil, err
		}
		r := restore.receipt
		if r.Version != 2 || r.Checkpoint != digest || r.Generation != manifest.Generation || !validWorkspaceStage(r.Stage) || r.Phase != "preparing" && r.Phase != "ready" && r.Phase != "promoted" && r.Phase != "adopted" {
			return nil, errors.New("workspace restore receipt does not match this transfer; preserved for recovery")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		nonce := make([]byte, 16)
		if _, err = rand.Read(nonce); err != nil {
			return nil, err
		}
		restore.receipt = workspaceReceipt{Version: 2, Checkpoint: digest, Generation: manifest.Generation, Stage: "workspace-stage-" + hex.EncodeToString(nonce), Phase: "preparing"}
		if err = restore.save(); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}
	if restore.receipt.Phase == "adopted" {
		return restore, nil
	}
	if restore.receipt.Phase == "promoted" {
		if err = restore.validateLayout(liveRoot(c)); err != nil {
			return nil, err
		}
		return restore, nil
	}
	stage, err := restore.stage()
	if err != nil {
		return nil, err
	}

	if restore.receipt.Phase == "ready" {
		if _, statErr := os.Lstat(stage); errors.Is(statErr, os.ErrNotExist) {
			if err = restore.validateLayout(liveRoot(c)); err != nil {
				return nil, err
			}
			restore.receipt.Phase = "promoted"
			if err = restore.save(); err != nil {
				return nil, err
			}
			return restore, nil
		}
	}
	if err = os.MkdirAll(stage, 0700); err != nil {
		return nil, err
	}
	complete := map[string]bool{}
	for _, token := range restore.receipt.Complete {
		if _, ok := restore.receipt.Hashes[token]; !ok {
			return nil, errors.New("workspace restore receipt is missing its file proof")
		}
		complete[token] = true
	}
	for _, root := range manifest.Roots {
		destination := filepath.Join(stage, filepath.FromSlash(root.Entry.Layout))
		if complete[root.Token] {
			if err = restore.validateRoot(destination, root); err != nil {
				return nil, err
			}
			continue
		}
		// The exact journaled stage grants ownership of this partial root only.
		if err = os.RemoveAll(destination); err != nil {
			return nil, err
		}
		if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return nil, err
		}
		from := filepath.Join(workspaceCarrierRoot(c), manifest.Generation, root.Token)
		rec, err := readWorkspaceRecord(from)
		if err != nil {
			return nil, err
		}
		if err = restoreStandaloneCopy("", destination, from, rec); err != nil {
			return nil, err
		}
		if err = os.Chmod(destination, 0700); err != nil {
			return nil, err
		}
		if err = validateWorkspaceOverlay(destination, from, rec); err != nil {
			return nil, err
		}
		if err = prepareWorkspaceDirectories(destination, root.Directories); err != nil {
			return nil, err
		}
		if err = applyEOL(destination, rec.EOL); err != nil {
			return nil, err
		}
		if err = overlay(filepath.Join(from, filesDir), destination, rec); err != nil {
			return nil, err
		}
		if err = applyModes(destination, rec.Modes); err != nil {
			return nil, err
		}
		if err = restorePortableBaseline(destination, rec); err != nil {
			return nil, err
		}
		if err = finishWorkspaceDirectories(destination, root.Directories); err != nil {
			return nil, err
		}
		if err = restore.validateRoot(destination, root); err != nil {
			return nil, err
		}

		if restore.receipt.Hashes == nil {
			restore.receipt.Hashes = map[string]string{}
		}
		fingerprint, err := workspaceTreeDigest(destination)
		if err != nil {
			return nil, err
		}
		restore.receipt.Hashes[root.Token] = fingerprint
		restore.receipt.Complete = append(restore.receipt.Complete, root.Token)
		if err = restore.save(); err != nil {
			return nil, err
		}
	}
	restore.receipt.Phase = "ready"
	if err = restore.save(); err != nil {
		return nil, err
	}
	return restore, nil
}
func validWorkspaceStage(name string) bool {
	return strings.HasPrefix(name, "workspace-stage-") && len(name) == len("workspace-stage-")+32 && workspaceSlot(name)
}
func (r *WorkspaceRestore) stage() (string, error) {
	if !validWorkspaceStage(r.receipt.Stage) {
		return "", errors.New("workspace stage identity is invalid")
	}
	return workspacePath(r.cell.Root, filepath.Join(".codeaf", r.receipt.Stage))
}
func (r *WorkspaceRestore) save() error {
	return writeWorkspaceJSON(workspaceReceiptPath(r.cell), r.receipt)
}

// Promote exposes all sibling roots in one directory rename. A durable receipt
// spans the rename and the caller's separate registry transaction.
func (r *WorkspaceRestore) Promote() ([]WorkspaceBinding, error) {
	if r == nil {
		return nil, errors.New("workspace restore is missing")
	}
	if r.receipt.Phase == "adopted" {
		return r.bindings(), nil
	}
	if r.receipt.Phase == "promoted" {
		if err := r.validateLayout(liveRoot(r.cell)); err != nil {
			return nil, err
		}
		return r.bindings(), nil
	}
	if r.receipt.Phase != "ready" {
		return nil, errors.New("workspace layout is incomplete")
	}
	stage, err := r.stage()
	if err != nil {
		return nil, err
	}
	if _, err = os.Stat(stage); errors.Is(err, os.ErrNotExist) {
		// A crash may occur after the rename but before its receipt update. Only
		// exact complete contents can reconcile that window.
		if err = r.validateLayout(liveRoot(r.cell)); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		if err = r.validateLayout(stage); err != nil {
			return nil, err
		}
		dest, err := workspacePath(r.cell.Root, cell.TreesDir)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(dest)
		if err == nil {
			if len(entries) != 0 {
				return nil, errors.New("workspace destination is occupied; preserved existing copies")
			}
			if err = os.Remove(dest); err != nil {
				return nil, err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err = os.Rename(stage, dest); err != nil {
			return nil, err
		}
	}
	r.receipt.Phase = "promoted"
	if err = r.save(); err != nil {
		return nil, err
	}
	return r.bindings(), nil
}

// Acknowledge records that the caller durably admitted every returned binding.
// The receipt remains as a recovery fence rather than deleting carried data.
func (r *WorkspaceRestore) Acknowledge() error {
	if r != nil && r.receipt.Phase == "adopted" {
		return nil
	}
	if r == nil || r.receipt.Phase != "promoted" && r.receipt.Phase != "adopted" {
		return errors.New("workspace layout has not been promoted")
	}
	if err := r.validateLayout(liveRoot(r.cell)); err != nil {
		return err
	}
	r.receipt.Phase = "adopted"
	return r.save()
}
func (r *WorkspaceRestore) bindings() []WorkspaceBinding {
	out := make([]WorkspaceBinding, 0, len(r.manifest.Roots))
	for _, root := range r.manifest.Roots {
		out = append(out, WorkspaceBinding{Entry: root.Entry, Path: filepath.Join(liveRoot(r.cell), filepath.FromSlash(root.Entry.Layout))})
	}
	return out
}
func (r *WorkspaceRestore) validateLayout(base string) error {
	for _, root := range r.manifest.Roots {
		path, err := workspacePath(base, root.Entry.Layout)
		if err != nil {
			return err
		}
		if err = r.validateRoot(path, root); err != nil {
			return err
		}
	}
	return nil
}
func (r *WorkspaceRestore) validateRoot(destination string, root workspaceRoot) error {
	if _, err := workspacePath(destination, "."); err != nil {
		return err
	}
	head, err := git(destination, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(head) != root.Head {
		return errors.New("restored workspace head does not match its carrier")
	}
	if err := verifyWorkspaceDirectories(destination, root.Directories); err != nil {
		return err
	}
	from := filepath.Join(workspaceCarrierRoot(r.cell), r.manifest.Generation, root.Token)
	rec, err := readWorkspaceRecord(from)
	if err != nil {
		return err
	}
	if err = verifyRestoredPortableBaseline(destination, rec); err != nil {
		return err
	}
	if err = validateWorkspaceOverlay(destination, from, rec); err != nil {
		return err
	}
	err = filepath.WalkDir(filepath.Join(from, filesDir), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(filepath.Join(from, filesDir), path)
		if err != nil {
			return err
		}
		equal, err := workspaceFileEqual(path, filepath.Join(destination, rel))
		if err != nil {
			return err
		}
		if !equal {
			return errors.New("restored workspace overlay differs from its carrier")
		}

		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, rel := range rec.Deleted {
		if _, err = os.Lstat(filepath.Join(destination, rel)); !errors.Is(err, os.ErrNotExist) {
			return errors.New("restored workspace deletion is missing")
		}
	}
	for rel, mode := range rec.Modes {
		info, err := os.Stat(filepath.Join(destination, filepath.FromSlash(rel)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode().Perm() != mode.Perm() {
			return errors.New("restored workspace permissions differ from its carrier")
		}
	}
	if wanted, ok := r.receipt.Hashes[root.Token]; ok {
		actual, err := workspaceTreeDigest(destination)
		if err != nil {
			return err
		}
		if wanted != actual {
			return errors.New("prepared workspace files changed; preserved for inspection")
		}
	}
	return nil
}
func validateWorkspaceCarrier(c cell.Cell, plan WorkspacePlan, digest string) (workspaceManifest, error) {
	manifest, err := readWorkspaceManifest(c)
	if err != nil {
		return manifest, err
	}
	entries, err := workspaceEntries(plan)
	if err != nil {
		return manifest, err
	}
	if manifest.Checkpoint != digest || !reflect.DeepEqual(manifest.Plan, plan) || len(entries) != len(manifest.Roots) {
		return manifest, errors.New("workspace manifest does not match its checkpoint")
	}
	byURI := map[string]WorkspaceEntry{}
	for _, e := range entries {
		byURI[e.CopyURI] = e
	}
	seen := map[string]bool{}
	metadataBytes := int64(0)
	for _, root := range manifest.Roots {
		if e, ok := byURI[root.Entry.CopyURI]; !ok || e != root.Entry || seen[root.Token] || root.Token != workspaceToken(root.Entry.CopyURI) {
			return manifest, errors.New("workspace manifest root is duplicated or mismatched")
		}
		seen[root.Token] = true
		from, err := workspacePath(c.Root, filepath.Join(cell.StateDir, workspaceCarrierDir, manifest.Generation, root.Token))
		if err != nil {
			return manifest, err
		}
		info, err := os.Lstat(filepath.Join(from, recordName))
		if err != nil {
			return manifest, err
		}
		metadataBytes += info.Size()
		if metadataBytes > WorkspaceCheckpointLimit {
			return manifest, errors.New("workspace total path metadata exceeds its bound")
		}
		rec, err := readWorkspaceRecord(from)
		if err != nil {
			return manifest, err
		}
		if !rec.FullHistory || rec.Head != root.Head {
			return manifest, errors.New("workspace carrier lacks its complete recorded history")
		}
		if err = validateWorkspaceRecord(from, rec); err != nil {
			return manifest, err
		}
		directoryBytes, err := validateWorkspaceDirectories(root, rec)
		if err != nil {
			return manifest, err
		}
		metadataBytes += int64(directoryBytes)
		if metadataBytes > WorkspaceCheckpointLimit {
			return manifest, errors.New("workspace total directory metadata exceeds its bound")
		}

		if err = screenFullHistory(filepath.Join(from, bundleName)); err != nil {
			return manifest, err
		}
		bundle, err := os.Open(filepath.Join(from, bundleName))
		if err != nil {
			return manifest, err
		}
		header, err := readFullBundleHeader(bundle)
		bundle.Close()
		if err != nil {
			return manifest, err
		}
		if header.head != rec.Head {
			return manifest, errors.New("workspace bundle does not name its recorded head")
		}

	}
	return manifest, nil
}
func validateWorkspaceRecord(from string, rec record) error {
	if err := validatePortableLinks(rec.Symlinks); err != nil {
		return err
	}
	if len(rec.PortableBaseline) > 0 {
		if _, _, err := decodePortableBaseline(rec.PortableBaseline); err != nil {
			return err
		}
	}
	if _, err := hex.DecodeString(rec.Head); err != nil || len(rec.Head) != 40 && len(rec.Head) != 64 {
		return errors.New("workspace commit identity is malformed")
	}
	if rec.Branch != "" {
		if _, err := git(from, "check-ref-format", "--branch", rec.Branch); err != nil {
			return errors.New("workspace branch identity is invalid")
		}
	}
	if len(rec.Deleted)+len(rec.Modes)+len(rec.EOL)+portableLinkCount(rec) > workspacePathLimit {
		return errors.New("workspace path metadata exceeds its bound")
	}
	check := func(rel string) error {
		if !filepath.IsLocal(filepath.FromSlash(rel)) || len(rel) > 4096 || strings.Contains(rel, "\\") || rel == ".git" || strings.HasPrefix(rel, ".git/") {
			return errors.New("workspace record path is unsafe")
		}
		return nil
	}
	for _, rel := range rec.Deleted {
		if err := check(rel); err != nil {
			return err
		}
	}
	for rel := range rec.Modes {
		if err := check(rel); err != nil {
			return err
		}
	}
	for rel, style := range rec.EOL {
		if style != "lf" && style != "crlf" {
			return errors.New("workspace line ending style is invalid")
		}
		if err := check(rel); err != nil {
			return err
		}
	}
	files := filepath.Join(from, filesDir)
	count := 0
	err := filepath.WalkDir(files, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(files, path)
		if err != nil {
			return err
		}
		if rel != "." {
			if err = check(filepath.ToSlash(rel)); err != nil {
				return err
			}
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() && !info.Mode().IsRegular() {
			return errors.New("workspace overlay contains an unsupported file")
		}
		count++
		if count > workspacePathLimit {
			return errors.New("workspace overlay path count exceeds its bound")
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func validateWorkspaceOverlay(destination, from string, rec record) error {
	paths := append([]string(nil), rec.Deleted...)
	for rel := range rec.Modes {
		paths = append(paths, rel)
	}
	for rel := range rec.EOL {
		paths = append(paths, rel)
	}
	err := filepath.WalkDir(filepath.Join(from, filesDir), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, err := filepath.Rel(filepath.Join(from, filesDir), path)
			if err != nil {
				return err
			}
			paths = append(paths, rel)
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, rel := range paths {
		if _, err := workspacePath(destination, filepath.FromSlash(rel)); err != nil {
			return fmt.Errorf("workspace overlay path: %w", err)
		}
	}
	return nil
}

// workspaceTreeDigest seals the prepared working files, including clean files
// that an overlay comparison cannot judge. Git's mutable administrative files
// are excluded; the recorded HEAD is checked independently.
func workspaceTreeDigest(root string) (string, error) {
	hash := sha256.New()
	err := walkWorkspaceTree(root, func(path, rel string, info fs.FileInfo) error {
		fmt.Fprintf(hash, "%d:%s:%o:%d:", len(rel), filepath.ToSlash(rel), info.Mode(), info.Size())
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(hash, "%d:%s", len(link), link)
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("prepared workspace contains an unsupported file")
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(hash, file)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func walkWorkspaceTree(root string, visit func(string, string, fs.FileInfo) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == ".git" || cell.IsTaskDropping(filepath.ToSlash(rel)) {
			return skipEntry(d)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		return visit(path, rel, info)
	})
}

func readWorkspaceRecord(from string) (record, error) {
	var rec record
	raw, err := readWorkspaceFile(filepath.Join(from, recordName), workspaceManifestLimit)
	if err != nil {
		return rec, err
	}
	err = json.Unmarshal(raw, &rec)
	return rec, err
}
func workspaceFileEqual(a, b string) (bool, error) {
	fingerprint := func(path string) (string, int64, error) {
		file, err := os.Open(path)
		if err != nil {
			return "", 0, err
		}
		defer file.Close()
		hash := sha256.New()
		n, err := io.Copy(hash, file)
		return hex.EncodeToString(hash.Sum(nil)), n, err
	}
	ah, an, err := fingerprint(a)
	if err != nil {
		return false, err
	}
	bh, bn, err := fingerprint(b)
	return ah == bh && an == bn, err
}

// ValidateWorkspace proves checkpoint membership and every portable root without
// creating a restore receipt or changing a receiver's working directories.
func ValidateWorkspace(c cell.Cell, plan WorkspacePlan) error {
	incoming, digest, err := ReadWorkspacePlan(c)
	if err != nil {
		return err
	}
	if incoming == nil || !reflect.DeepEqual(*incoming, plan) {
		return errors.New("workspace checkpoint does not match the requested transfer")
	}
	_, err = validateWorkspaceCarrier(c, plan, digest)
	return err
}

// WorkspaceReady fences workers until an incoming whole layout was admitted.
// A locally captured source keeps its existing admitted authority. An adopted
// receiver may edit its working files and rewrite local checkpoint paths without
// changing the immutable provenance recorded by its restore receipt.
func WorkspaceReady(c cell.Cell) error {
	manifest, err := readWorkspaceManifest(c)
	if errors.Is(err, os.ErrNotExist) {
		plan, _, readErr := ReadWorkspacePlan(c)
		if readErr != nil {
			return readErr
		}
		if plan == nil {
			return nil
		}
		return localWorkspacePlanReady(c, *plan)
	}
	if err != nil {
		return err
	}
	raw, err := readWorkspaceFile(workspaceReceiptPath(c), workspaceManifestLimit)
	if errors.Is(err, os.ErrNotExist) {
		entries, err := workspaceEntries(manifest.Plan)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if _, err = sourceWorkspace(c, entry); err != nil {
				return errors.New("incoming workspace roots have not been admitted")
			}
		}
		return nil
	}
	if err != nil {
		return err
	}
	var receipt workspaceReceipt
	if err = json.Unmarshal(raw, &receipt); err != nil {
		return err
	}
	if receipt.Version != 2 || receipt.Phase != "adopted" || !validWorkspaceStage(receipt.Stage) {
		return errors.New("workspace restore is incomplete; preserved for recovery before starting work")
	}
	if receipt.Checkpoint != manifest.Checkpoint || receipt.Generation != manifest.Generation {
		return localWorkspacePlanReady(c, manifest.Plan)
	}

	return nil
}

func localWorkspacePlanReady(c cell.Cell, plan WorkspacePlan) error {
	entries, err := workspaceEntries(plan)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err = sourceWorkspace(c, entry); err != nil {
			return err
		}
	}
	return nil
}
func adoptedWorkspace(c cell.Cell, plan WorkspacePlan) (*WorkspaceRestore, bool, error) {
	raw, err := readWorkspaceFile(workspaceReceiptPath(c), workspaceManifestLimit)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var receipt workspaceReceipt
	if err = json.Unmarshal(raw, &receipt); err != nil {
		return nil, false, err
	}
	if receipt.Phase != "adopted" {
		return nil, false, nil
	}
	if receipt.Version != 2 || !validWorkspaceStage(receipt.Stage) {
		return nil, false, errors.New("workspace adoption receipt is invalid")
	}
	manifest, err := readWorkspaceManifest(c)
	if err != nil {
		return nil, false, err
	}
	if !reflect.DeepEqual(manifest.Plan, plan) {
		return nil, false, nil
	}
	if err = localWorkspacePlanReady(c, plan); err != nil {
		return nil, false, err
	}
	return &WorkspaceRestore{cell: c, manifest: manifest, receipt: receipt}, true, nil
}

// WorkspaceResultSelection identifies a settled copy's exact recorded baseline.
type WorkspaceResultSelection struct {
	CopyURI    string `json:"copy_uri"`
	BaseCommit string `json:"base_commit"`
}

// RestoreCompletedWorkspaceResults retains changed outputs without runnable Git
// copies. Full history remains in the independent portable carrier namespace.
func RestoreCompletedWorkspaceResults(c cell.Cell, plan WorkspacePlan, destination string, selections []WorkspaceResultSelection) (map[string]string, error) {
	manifest, err := readWorkspaceManifest(c)
	if err != nil {
		return nil, err
	}
	manifest, err = validateWorkspaceCarrier(c, plan, manifest.Checkpoint)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(destination) || filepath.Clean(destination) != destination {
		return nil, errors.New("workspace result destination must be an owned absolute folder")
	}
	if len(selections) > workspaceLimit {
		return nil, errors.New("workspace result selection exceeds its bound")
	}
	roots := map[string]workspaceRoot{}
	for _, root := range manifest.Roots {
		roots[root.Entry.CopyURI] = root
	}
	chosen := map[string]bool{}
	for _, selection := range selections {
		if chosen[selection.CopyURI] {
			return nil, errors.New("workspace result selection is duplicated")
		}
		chosen[selection.CopyURI] = true
		if _, ok := roots[selection.CopyURI]; !ok {
			return nil, errors.New("workspace result selection is outside the carrier")
		}
		if selection.BaseCommit == "" {
			return nil, errors.New("this older task did not record its Git baseline; full saved history is preserved, but changed outputs cannot be selected safely")
		}
		if _, err := hex.DecodeString(selection.BaseCommit); err != nil || len(selection.BaseCommit) != 40 && len(selection.BaseCommit) != 64 {
			return nil, errors.New("workspace result baseline is malformed")
		}
	}
	expected := workspaceResultsRecord{Version: 2, Checkpoint: manifest.Checkpoint, Selections: selections}
	if _, err = os.Lstat(destination); err == nil {
		return reopenWorkspaceResults(destination, expected)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	parent := filepath.Dir(destination)
	if _, err = workspacePath(parent, filepath.Base(destination)); err != nil {
		return nil, err
	}
	stage, err := prepareWorkspaceResultStage(c, destination)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	output := filepath.Join(stage, "output")
	if err = os.Mkdir(output, 0700); err != nil {
		return nil, err
	}
	expected.Roots = map[string]workspaceResultRoot{}
	for _, selection := range selections {
		root := roots[selection.CopyURI]
		from := filepath.Join(workspaceCarrierRoot(c), manifest.Generation, root.Token)
		rec, err := readWorkspaceRecord(from)
		if err != nil {
			return nil, err
		}
		checkout := filepath.Join(stage, "checkout-"+root.Token)
		if err = prepareResultCheckout("", checkout, from, rec); err != nil {
			return nil, err
		}
		if _, err = git(checkout, "cat-file", "-e", selection.BaseCommit+"^{commit}"); err != nil {
			return nil, errors.New("workspace output baseline is unavailable; full carrier history was preserved")
		}
		if _, err = git(checkout, "merge-base", "--is-ancestor", selection.BaseCommit, rec.Head); err != nil {
			return nil, errors.New("workspace result baseline is not an ancestor of its recorded head")
		}
		if err = validateWorkspaceOverlay(checkout, from, rec); err != nil {
			return nil, err
		}
		if err = os.Chmod(checkout, 0700); err != nil {
			return nil, err
		}
		if err = prepareWorkspaceDirectories(checkout, root.Directories); err != nil {
			return nil, err
		}
		if err = applyEOL(checkout, rec.EOL); err != nil {
			return nil, err
		}
		if err = overlay(filepath.Join(from, filesDir), checkout, rec); err != nil {
			return nil, err
		}
		if err = applyModes(checkout, rec.Modes); err != nil {
			return nil, err
		}
		if err = finishWorkspaceDirectories(checkout, root.Directories); err != nil {
			return nil, err
		}
		diff, err := git(checkout, "diff", "--name-only", "-z", "--no-renames", selection.BaseCommit, rec.Head, "--")
		if err != nil {
			return nil, err
		}
		dirty, err := changedPaths(checkout)
		if err != nil {
			return nil, err
		}
		paths := map[string]bool{}
		for _, path := range append(strings.Split(diff, "\x00"), dirty...) {
			if path != "" && !cell.IsTaskDropping(path) {
				paths[path] = true
			}
		}
		artifact := filepath.Join(output, filepath.FromSlash(root.Entry.Layout))
		if err = os.MkdirAll(artifact, 0700); err != nil {
			return nil, err
		}
		result := workspaceResultRoot{Layout: root.Entry.Layout, Head: rec.Head, BaseCommit: selection.BaseCommit}
		ordered := make([]string, 0, len(paths))
		for path := range paths {
			ordered = append(ordered, path)
		}
		sort.Strings(ordered)
		for _, path := range ordered {
			if _, err = workspacePath(checkout, filepath.Dir(path)); err != nil {
				return nil, err
			}
			if _, err = os.Lstat(filepath.Join(checkout, path)); errors.Is(err, os.ErrNotExist) {
				result.Deleted = append(result.Deleted, filepath.ToSlash(path))
				continue
			} else if err != nil {
				return nil, err
			}
			if err = copySelectedResult(checkout, from, artifact, path, rec); err != nil {
				return nil, err
			}
			result.Paths = append(result.Paths, filepath.ToSlash(path))
		}
		result.Directories, err = workspaceOutputDirectories(checkout, selection.BaseCommit, root.Directories, result.Paths)
		if err != nil {
			return nil, err
		}
		if err = prepareWorkspaceDirectories(artifact, result.Directories); err != nil {
			return nil, err
		}
		if err = finishWorkspaceDirectories(artifact, result.Directories); err != nil {
			return nil, err
		}
		result.Digest, err = workspaceTreeDigest(artifact)
		if err != nil {
			return nil, err
		}
		expected.Roots[selection.CopyURI] = result
		if err = os.RemoveAll(checkout); err != nil {
			return nil, err
		}
	}
	metadata := filepath.Join(output, ".codeaf")
	if err = os.Mkdir(metadata, 0700); err != nil {
		return nil, err
	}
	if err = writeWorkspaceJSON(filepath.Join(metadata, "workspace-results.json"), expected); err != nil {
		return nil, err
	}
	if err = os.Rename(output, destination); err != nil {
		return nil, err
	}
	return workspaceResultPaths(destination, expected), nil
}

type workspaceResultRoot struct {
	Directories map[string]fs.FileMode `json:"directories"`
	Layout      string                 `json:"layout"`
	Head        string                 `json:"head"`
	BaseCommit  string                 `json:"base_commit"`
	Digest      string                 `json:"digest"`
	Paths       []string               `json:"paths"`
	Deleted     []string               `json:"deleted"`
}
type workspaceResultsRecord struct {
	Version    int                            `json:"version"`
	Checkpoint string                         `json:"checkpoint_sha256"`
	Selections []WorkspaceResultSelection     `json:"selections"`
	Roots      map[string]workspaceResultRoot `json:"roots"`
}

func workspaceResultPaths(destination string, record workspaceResultsRecord) map[string]string {
	out := map[string]string{}
	for uri, root := range record.Roots {
		out[uri] = filepath.Join(destination, filepath.FromSlash(root.Layout))
	}
	return out
}
func reopenWorkspaceResults(destination string, expected workspaceResultsRecord) (map[string]string, error) {
	raw, err := readWorkspaceFile(filepath.Join(destination, ".codeaf", "workspace-results.json"), workspaceManifestLimit)
	if err != nil {
		return nil, errors.New("workspace history destination is occupied; preserved its existing files")
	}
	var record workspaceResultsRecord
	if err = json.Unmarshal(raw, &record); err != nil {
		return nil, err
	}
	if record.Version != 2 || record.Checkpoint != expected.Checkpoint || !reflect.DeepEqual(record.Selections, expected.Selections) || len(record.Roots) != len(expected.Selections) {
		return nil, errors.New("workspace history destination belongs to another result set")
	}
	for _, selection := range record.Selections {
		root, ok := record.Roots[selection.CopyURI]
		if !ok {
			return nil, errors.New("workspace history result is missing")
		}
		path, err := workspacePath(destination, root.Layout)
		if err != nil {
			return nil, err
		}
		digest, err := workspaceTreeDigest(path)
		if err != nil {
			return nil, err
		}
		if digest != root.Digest {
			return nil, errors.New("workspace history output changed; preserved its files")
		}
	}
	return workspaceResultPaths(destination, record), nil
}
func prepareWorkspaceResultStage(c cell.Cell, destination string) (string, error) {
	private, err := workspacePath(c.Root, ".codeaf")
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(private, 0700); err != nil {
		return "", err
	}
	journal := filepath.Join(private, "workspace-results-stage.json")
	target := workspaceToken(destination)
	var prior struct {
		Version int    `json:"version"`
		Target  string `json:"target"`
		Stage   string `json:"stage"`
	}
	raw, err := readWorkspaceFile(journal, workspaceManifestLimit)
	if err == nil {
		if err = json.Unmarshal(raw, &prior); err != nil {
			return "", err
		}
		if prior.Version != 2 || prior.Target != target || !validWorkspaceResultStage(prior.Stage) {
			return "", errors.New("workspace result stage belongs to another operation; preserved for recovery")
		}
		path, err := workspacePath(c.Root, filepath.Join(".codeaf", prior.Stage))
		if err != nil {
			return "", err
		}
		if err = os.RemoveAll(path); err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	nonce := make([]byte, 16)
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	prior.Version = 2
	prior.Target = target
	prior.Stage = "workspace-results-stage-" + hex.EncodeToString(nonce)
	if err = writeWorkspaceJSON(journal, prior); err != nil {
		return "", err
	}
	stage := filepath.Join(private, prior.Stage)
	if err = os.Mkdir(stage, 0700); err != nil {
		return "", err
	}
	return stage, nil
}

func validWorkspaceResultStage(name string) bool {
	return strings.HasPrefix(name, "workspace-results-stage-") && len(name) == len("workspace-results-stage-")+32 && workspaceSlot(name)
}

func validateWorkspaceDirectories(root workspaceRoot, rec record) (int, error) {
	expectedVersion := 1
	if rec.Symlinks != nil {
		expectedVersion = 2
	}
	if root.DirectoryVersion != expectedVersion || root.Directories == nil {
		return 0, errors.New("workspace carrier lacks supported directory metadata; preserve it and recapture with the current prototype")
	}
	if len(root.Directories)+len(rec.Modes)+len(rec.EOL)+len(rec.Deleted)+portableLinkCount(rec) > workspacePathLimit {
		return 0, errors.New("workspace path metadata exceeds its bound")
	}
	bytes := 0
	caser := cases.Fold()
	fold := func(path string) string { return norm.NFC.String(caser.String(norm.NFC.String(path))) }
	files := map[string]bool{}
	for path := range rec.Modes {
		files[fold(path)] = true
	}
	folded := map[string]bool{}
	for path, mode := range root.Directories {
		alias := fold(path)
		if folded[alias] {
			return 0, errors.New("workspace directory spellings alias on another filesystem")
		}
		folded[alias] = true

		if !utf8.ValidString(path) || path == "." || !filepath.IsLocal(filepath.FromSlash(path)) || filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) != path || strings.Contains(path, "\\") || len(path) > 4096 {
			return 0, errors.New("workspace directory path is unsafe")
		}
		for _, part := range strings.Split(path, "/") {
			if part == ".git" || cell.IsTaskDropping(part) {
				return 0, errors.New("workspace directory names private administrative state")
			}
		}
		if mode != mode.Perm() || mode&0700 != 0700 {
			return 0, errors.New("workspace directory permissions are unsupported")
		}
		for part := path; part != "."; part = filepath.ToSlash(filepath.Dir(filepath.FromSlash(part))) {
			if files[fold(part)] {
				return 0, errors.New("workspace directory conflicts with a regular file")
			}
			if part != path {
				if _, exists := root.Directories[part]; !exists {
					return 0, errors.New("workspace directory is missing its parent metadata")
				}
			}
		}
		bytes += len(path) + 8
		if bytes > workspaceManifestLimit {
			return 0, errors.New("workspace directory metadata exceeds its bound")
		}
	}
	return bytes, nil
}
func workspaceDirectoryOrder(directories map[string]fs.FileMode, deepest bool) []string {
	paths := make([]string, 0, len(directories))
	for path := range directories {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		di, dj := strings.Count(paths[i], "/"), strings.Count(paths[j], "/")
		if di != dj {
			if deepest {
				return di > dj
			}
			return di < dj
		}
		return paths[i] < paths[j]
	})
	return paths
}
func prepareWorkspaceDirectories(destination string, directories map[string]fs.FileMode) error {
	// Judge every existing component before creating any declared directory.
	for path := range directories {
		target, err := workspacePath(destination, filepath.FromSlash(path))
		if err != nil {
			return err
		}
		info, err := os.Lstat(target)
		if err == nil && !info.IsDir() {
			return errors.New("workspace directory collides with a checkout file")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for _, path := range workspaceDirectoryOrder(directories, false) {
		if err := os.MkdirAll(filepath.Join(destination, filepath.FromSlash(path)), 0700); err != nil {
			return err
		}
	}
	return nil
}
func finishWorkspaceDirectories(destination string, directories map[string]fs.FileMode) error {
	var absent []string
	err := filepath.WalkDir(destination, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(destination, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.Name() == ".git" || cell.IsTaskDropping(d.Name()) {
			return filepath.SkipDir
		}
		rel = filepath.ToSlash(rel)
		if _, keep := directories[rel]; !keep {
			absent = append(absent, rel)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(absent, func(i, j int) bool { return strings.Count(absent[i], "/") > strings.Count(absent[j], "/") })
	for _, path := range absent {
		target := filepath.Join(destination, filepath.FromSlash(path))
		entries, err := os.ReadDir(target)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			if err = os.Remove(target); err != nil {
				return err
			}
		} else {
			return errors.New("workspace carrier omits a nonempty directory; preserved its private preparation")
		}
	}
	for _, path := range workspaceDirectoryOrder(directories, true) {
		if _, err := workspacePath(destination, filepath.FromSlash(path)); err != nil {
			return err
		}
		if err := os.Chmod(filepath.Join(destination, filepath.FromSlash(path)), directories[path]); err != nil {
			return err
		}
	}
	return nil
}
func verifyWorkspaceDirectories(destination string, directories map[string]fs.FileMode) error {
	err := filepath.WalkDir(destination, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(destination, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.Name() == ".git" || cell.IsTaskDropping(d.Name()) {
			return filepath.SkipDir
		}
		if _, ok := directories[filepath.ToSlash(rel)]; !ok {
			return errors.New("workspace contains a directory absent from its carrier metadata")
		}
		return nil
	})
	if err != nil {
		return err
	}
	for path, mode := range directories {
		target, err := workspacePath(destination, filepath.FromSlash(path))
		if err != nil {
			return err
		}
		info, err := os.Lstat(target)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode().Perm() != mode {
			return errors.New("restored workspace directory mode or type differs from its carrier")
		}
	}
	return nil
}

// workspaceOutputDirectories retains output parents and empty directories not
// represented by the Git baseline. Git does not record directory permissions,
// so mode-only changes to an otherwise unchanged input directory remain in the
// full carrier rather than being attributed to this changed-output subset.
func workspaceOutputDirectories(checkout, base string, directories map[string]fs.FileMode, files []string) (map[string]fs.FileMode, error) {
	raw, err := git(checkout, "ls-tree", "-r", "-d", "--name-only", "-z", base, "--")
	if err != nil {
		return nil, err
	}
	if len(raw) > WorkspaceCheckpointLimit {
		return nil, errors.New("workspace baseline directory metadata exceeds its bound")
	}
	baseline := map[string]bool{}
	for _, path := range strings.Split(raw, "\x00") {
		baseline[path] = true
	}
	out := map[string]fs.FileMode{}
	add := func(path string) error {
		for path != "." {
			mode, exists := directories[path]
			if !exists {
				return errors.New("workspace output parent is absent from its carrier directory metadata")
			}
			out[path] = mode
			path = filepath.ToSlash(filepath.Dir(filepath.FromSlash(path)))
		}
		return nil
	}
	for _, path := range files {
		if err = add(filepath.ToSlash(filepath.Dir(filepath.FromSlash(path)))); err != nil {
			return nil, err
		}
	}
	for path := range directories {
		if baseline[path] {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(checkout, filepath.FromSlash(path)))
		if err != nil {
			return nil, err
		}
		if len(entries) == 0 {
			if err = add(path); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func portableLinkCount(rec record) int {
	if rec.Symlinks == nil {
		return 0
	}
	return len(rec.Symlinks.Entries)
}
