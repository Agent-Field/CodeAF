package taskcopy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

// WorkspaceCarrierPath is the separately versioned portable multi-root carrier.
const WorkspaceCarrierPath = cell.StateDir + "/workspaces-v2"
const workspaceCarrierDir = "workspaces-v2"
const workspaceManifestLimit = 4 << 20
const workspacePathLimit = 100000

type workspaceManifest struct {
	Version    int             `json:"version"`
	Checkpoint string          `json:"checkpoint_sha256"`
	Plan       WorkspacePlan   `json:"plan"`
	Generation string          `json:"generation"`
	Roots      []workspaceRoot `json:"roots"`
}
type workspaceRoot struct {
	Entry            WorkspaceEntry         `json:"entry"`
	Token            string                 `json:"token"`
	Head             string                 `json:"head"`
	DirectoryVersion int                    `json:"directory_version"`
	Directories      map[string]fs.FileMode `json:"directories"`
}

func workspaceCarrierRoot(c cell.Cell) string {
	return filepath.Join(c.Root, cell.StateDir, workspaceCarrierDir)
}
func sourceWorkspace(c cell.Cell, e WorkspaceEntry) (string, error) {
	registry := home.Join("workspace-identities")
	resolve := func(uri string) (workspaceidentity.Record, error) {
		return workspaceidentity.ResolveExisting(registry, uri)
	}
	run, err := resolve(e.CopyURI)
	if err != nil {
		return "", err
	}
	chat, err := resolve(e.ChatURI)
	if err != nil {
		return "", err
	}
	project, err := resolve(e.ProjectURI)
	if err != nil {
		return "", err
	}
	runProject, err := resolve(run.ProjectURI)
	if err != nil {
		return "", err
	}
	if run.Role != workspaceidentity.Run || run.OwnerURI != e.ChatURI || chat.Role != workspaceidentity.Chat || chat.Path != filepath.Clean(c.Root) || project.Role != workspaceidentity.Project || runProject.URI != project.URI {
		return "", errors.New("workspace source ownership does not match its plan")
	}
	expected, err := workspacePath(c.Root, filepath.Join(cell.TreesDir, filepath.FromSlash(e.Layout)))
	if err != nil {
		return "", err
	}
	if run.Path != expected {
		return "", errors.New("workspace source path does not match its admitted layout")
	}
	for _, uri := range []string{e.RootURI, e.GroundURI} {
		link, err := resolve(uri)
		if err != nil {
			return "", err
		}
		if link.Role != workspaceidentity.Project && link.Role != workspaceidentity.Run {
			return "", errors.New("workspace source link is not a project or copy")
		}
		if link.Role == workspaceidentity.Run && link.OwnerURI != e.ChatURI {
			return "", errors.New("workspace source link belongs to another chat")
		}
		lp, err := resolve(link.ProjectURI)
		if err != nil {
			return "", err
		}
		if lp.URI != project.URI {
			return "", errors.New("workspace source link belongs to another project")
		}
	}
	if !isRepository(expected) {
		return "", errors.New("workspace source copy is missing its repository")
	}
	return expected, nil
}
func (k Carry) composeWorkspacePlan(c cell.Cell, plan WorkspacePlan, digest string) error {
	entries, err := workspaceEntries(plan)
	if err != nil {
		return err
	}
	copySet := map[string]bool{}
	for _, entry := range entries {
		copySet[entry.CopyURI] = true
	}
	for _, entry := range entries {
		for _, uri := range []string{entry.RootURI, entry.GroundURI} {
			link, err := workspaceidentity.ResolveExisting(home.Join("workspace-identities"), uri)
			if err != nil {
				return err
			}
			if link.Role == workspaceidentity.Run && !copySet[uri] {
				return errors.New("workspace source parent is absent from the transfer closure")
			}
		}
	}
	sources := make([]string, len(entries))
	for i, e := range entries {
		sources[i], err = sourceWorkspace(c, e)
		if err != nil {
			return err
		}
	}
	root, err := workspacePath(c.Root, filepath.Join(cell.StateDir, workspaceCarrierDir))
	if err != nil {
		return err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return err
	}
	previous, priorErr := readWorkspaceManifest(c)
	if priorErr != nil && !errors.Is(priorErr, os.ErrNotExist) {
		return priorErr
	}
	stage, err := prepareWorkspaceCapture(c, previous)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			os.RemoveAll(stage)
		}
	}()
	manifest := workspaceManifest{Version: 2, Checkpoint: digest, Plan: plan, Generation: filepath.Base(stage)}
	for i, e := range entries {
		token := workspaceToken(e.CopyURI)
		dest := filepath.Join(stage, token)
		if err := carryWithHistory(sources[i], dest, true); err != nil {
			return fmt.Errorf("carry workspace %s: %w", e.Layout, err)
		}
		rec, err := readWorkspaceRecord(dest)
		if err != nil {
			return err
		}
		directories, err := captureWorkspaceDirectories(sources[i])
		if err != nil {
			return fmt.Errorf("carry workspace directories %s: %w", e.Layout, err)
		}
		directoryVersion := 1
		if rec.Symlinks != nil {
			directoryVersion = 2
		}
		portable := workspaceRoot{Entry: e, Token: token, Head: rec.Head, DirectoryVersion: directoryVersion, Directories: directories}
		if _, err = validateWorkspaceDirectories(portable, rec); err != nil {
			return err
		}
		manifest.Roots = append(manifest.Roots, portable)
	}
	after, afterDigest, err := ReadWorkspacePlan(c)
	if err != nil {
		return err
	}
	if after == nil || afterDigest != digest || !reflect.DeepEqual(*after, plan) {
		return errors.New("workspace checkpoint changed during capture")
	}
	if err := writeWorkspaceJSON(filepath.Join(root, "manifest.json"), manifest); err != nil {
		return err
	}
	published = true
	// Only the preceding manifest grants ownership of the old generation. Unknown
	// siblings are retained; carrier capture is not a generic directory sweeper.
	if priorErr == nil && previous.Generation != manifest.Generation && validGeneration(previous.Generation) {
		if err := os.RemoveAll(filepath.Join(root, previous.Generation)); err != nil {
			return err
		}
	}
	return nil
}
func validGeneration(s string) bool {
	if !strings.HasPrefix(s, "generation-") || len(s) != len("generation-")+32 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(s, "generation-"))
	return err == nil
}
func readWorkspaceManifest(c cell.Cell) (workspaceManifest, error) {
	var m workspaceManifest
	root, err := workspacePath(c.Root, filepath.Join(cell.StateDir, workspaceCarrierDir))
	if err != nil {
		return m, err
	}
	raw, err := readWorkspaceFile(filepath.Join(root, "manifest.json"), workspaceManifestLimit)
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(raw, &m)
	if err != nil {
		return m, err
	}
	if m.Version != 2 || !validGeneration(m.Generation) || len(m.Checkpoint) != 64 {
		return m, errors.New("workspace carrier manifest is invalid")
	}
	return m, nil
}
func writeWorkspaceJSON(path string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) > workspaceManifestLimit {
		return errors.New("workspace manifest exceeds its bound")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".workspace-write-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(raw)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// workspaceCaptureReceipt records the only in-progress carrier generation the
// harness may reclaim after interruption. Other folders never become garbage
// merely because they happen to have a similar name.
type workspaceCaptureReceipt struct {
	Version  int    `json:"version"`
	Stage    string `json:"stage"`
	Previous string `json:"previous,omitempty"`
}

func prepareWorkspaceCapture(c cell.Cell, previous workspaceManifest) (string, error) {
	private, err := workspacePath(c.Root, ".codeaf")
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(private, 0700); err != nil {
		return "", err
	}
	journal := filepath.Join(private, "workspace-capture.json")
	raw, err := readWorkspaceFile(journal, workspaceManifestLimit)
	if err == nil {
		var prior workspaceCaptureReceipt
		if err = json.Unmarshal(raw, &prior); err != nil {
			return "", err
		}
		if prior.Version != 2 || !validGeneration(prior.Stage) || prior.Previous != "" && !validGeneration(prior.Previous) {
			return "", errors.New("workspace capture journal is invalid; preserved for inspection")
		}
		for _, owned := range []string{prior.Stage, prior.Previous} {
			if owned == "" || owned == previous.Generation {
				continue
			}
			path, err := workspacePath(c.Root, filepath.Join(cell.StateDir, workspaceCarrierDir, owned))
			if err != nil {
				return "", err
			}
			if err = os.RemoveAll(path); err != nil {
				return "", err
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	nonce := make([]byte, 16)
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	name := "generation-" + hex.EncodeToString(nonce)
	receipt := workspaceCaptureReceipt{Version: 2, Stage: name, Previous: previous.Generation}
	if err = writeWorkspaceJSON(journal, receipt); err != nil {
		return "", err
	}
	stage := filepath.Join(workspaceCarrierRoot(c), name)
	if err = os.Mkdir(stage, 0700); err != nil {
		return "", err
	}
	return stage, nil
}

// captureWorkspaceDirectories carries only the visible working tree. Git names
// ignored subtrees once, so large caches are not walked just to omit their dirs.
func captureWorkspaceDirectories(tree string) (map[string]fs.FileMode, error) {
	return captureWorkspaceDirectoriesScope(tree, tree, independentBaseline(tree))
}

func captureWorkspaceDirectoriesWithIndex(tree, indexRoot string) (map[string]fs.FileMode, error) {
	return captureWorkspaceDirectoriesScope(tree, indexRoot, false)
}

func captureWorkspaceDirectoriesScope(tree, indexRoot string, wholeFamily bool) (map[string]fs.FileMode, error) {
	if err := portableNativeMetadataDirectory(tree); err != nil {
		return nil, err
	}
	ignored, err := portableInspectionGit(indexRoot, "--work-tree="+tree, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	if err != nil {
		return nil, err
	}
	tracked, err := portableInspectionGit(indexRoot, "ls-files", "--cached", "-z")
	if err != nil {
		return nil, err
	}
	if len(ignored)+len(tracked) > WorkspaceCheckpointLimit {
		return nil, errors.New("workspace directory enumeration exceeds its bound")
	}
	ignoredDirs := map[string]bool{}
	for _, path := range strings.Split(ignored, "\x00") {
		if strings.HasSuffix(path, "/") {
			ignoredDirs[strings.TrimSuffix(path, "/")] = true
		}
	}
	trackedParents := map[string]bool{}
	for _, path := range strings.Split(tracked, "\x00") {
		for parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(path))); parent != "." && parent != "/"; parent = filepath.ToSlash(filepath.Dir(filepath.FromSlash(parent))) {
			trackedParents[parent] = true
		}
	}
	out := map[string]fs.FileMode{}
	scanner := keys.Scanner{}
	err = filepath.WalkDir(tree, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(tree, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !utf8.ValidString(rel) {
			return errors.New("workspace directory spelling is not valid UTF-8")
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == ".git" || cell.IsTaskDropping(d.Name()) || portableNativeMetadataPath(rel) {
			return filepath.SkipDir
		}
		ignored := false
		for part := rel; part != "."; part = filepath.ToSlash(filepath.Dir(filepath.FromSlash(part))) {
			if ignoredDirs[part] {
				ignored = true
				break
			}
		}
		if ignored && !trackedParents[rel] && !wholeFamily {
			return filepath.SkipDir
		}
		finding, err := scanner.Reader(rel, strings.NewReader(""))
		if err != nil {
			return err
		}
		if finding.Rule != "" {
			return filepath.SkipDir
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || info.Mode().Perm()&0700 != 0700 {
			return errors.New("workspace directory permissions require an unsupported retained case")
		}
		out[rel] = info.Mode().Perm()
		if len(out) > workspacePathLimit {
			return errors.New("workspace directory count exceeds its bound")
		}
		return nil
	})
	return out, err
}
