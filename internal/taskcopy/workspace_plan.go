package taskcopy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/cell"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// WorkspacePlan is the session-produced projection of a structured checkpoint.
// Paths are portable layouts, never authority over a receiving project.
type WorkspacePlan struct {
	Version int              `json:"version"`
	ChatURI string           `json:"chat_uri"`
	Groups  []WorkspaceGroup `json:"groups"`
}

// WorkspaceGroup records all roots required by one execution.
type WorkspaceGroup struct {
	TaskID     uint64           `json:"task_id"`
	Required   bool             `json:"required"`
	Incomplete bool             `json:"incomplete"`
	Primary    string           `json:"primary"`
	Entries    []WorkspaceEntry `json:"entries"`
}

// WorkspaceEntry binds a stable copy identity to its portable sibling layout.
type WorkspaceEntry struct {
	CopyURI    string `json:"copy_uri"`
	ChatURI    string `json:"chat_uri"`
	ProjectURI string `json:"project_uri"`
	RootURI    string `json:"root_uri"`
	GroundURI  string `json:"ground_uri"`
	Layout     string `json:"layout"`
	Access     string `json:"access"`
}

const workspaceLimit = 1024

// WorkspaceCheckpointLimit bounds the checkpoint shared by capture and admission.
const WorkspaceCheckpointLimit = 16 << 20

// ReadWorkspacePlan reads only the typed projection and checkpoint envelope.
// A legacy checkpoint has no plan; malformed structured checkpoints fail closed.
func ReadWorkspacePlan(c cell.Cell) (*WorkspacePlan, string, error) {
	path := filepath.Join(c.Root, cell.StateDir, "tasks.json")
	raw, err := readWorkspaceFile(path, WorkspaceCheckpointLimit)
	if errors.Is(err, os.ErrNotExist) {
		raw, err = readWorkspaceFile(filepath.Join(c.Root, "tasks.json"), WorkspaceCheckpointLimit)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	var envelope struct {
		Type    string         `json:"type"`
		Version int            `json:"version"`
		Plan    *WorkspacePlan `json:"workspace_transfer"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil {
		return nil, "", err
	}
	if envelope.Type != "tasks" {
		return nil, "", errors.New("workspace checkpoint type is invalid")
	}
	if envelope.Version == 1 && envelope.Plan == nil {
		return nil, "", nil
	}
	if envelope.Version != 2 || envelope.Plan == nil {
		return nil, "", errors.New("structured workspace checkpoint requires its transfer projection")
	}
	if _, err := workspaceEntries(*envelope.Plan); err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(raw)
	return envelope.Plan, hex.EncodeToString(digest[:]), nil
}

func readWorkspaceFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("workspace metadata is not a bounded regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errors.New("workspace metadata exceeds its bound")
	}
	return raw, nil
}
func workspaceEntries(plan WorkspacePlan) ([]WorkspaceEntry, error) {
	if plan.Version != 2 || plan.ChatURI == "" || len(plan.Groups) == 0 || len(plan.Groups) > workspaceLimit {
		return nil, errors.New("workspace transfer plan is invalid")
	}
	copies := map[string]WorkspaceEntry{}
	layouts := map[string]string{}
	tasks := map[uint64]bool{}
	var out []WorkspaceEntry
	for _, g := range plan.Groups {
		if g.TaskID == 0 || tasks[g.TaskID] || len(g.Entries) == 0 || len(g.Entries) > workspaceLimit || g.Primary == "" || g.Required && g.Incomplete {
			return nil, errors.New("workspace group is incomplete or invalid")
		}
		tasks[g.TaskID] = true
		primary := false
		layoutParent := ""
		for _, e := range g.Entries {
			parts := strings.Split(e.Layout, "/")
			if len(parts) != 2 {
				return nil, errors.New("workspace layout is invalid")
			}
			parent, err := strconv.ParseUint(parts[0], 10, 64)
			if err != nil || parent == 0 || strconv.FormatUint(parent, 10) != parts[0] || !ValidWorkspaceSlot(parts[1]) || e.RootURI == "" || e.ChatURI != plan.ChatURI || len(e.CopyURI) > 512 || len(e.Layout) > 280 || e.Access != "read" && e.Access != "write" {
				return nil, errors.New("workspace entry is invalid")
			}
			if layoutParent != "" && layoutParent != parts[0] {
				return nil, errors.New("workspace group roots are not siblings")
			}
			layoutParent = parts[0]
			primary = primary || parts[1] == g.Primary
			if e.CopyURI == "" {
				if !g.Incomplete {
					return nil, errors.New("workspace copy identity is missing")
				}
				continue
			}
			if e.ProjectURI == "" || e.GroundURI == "" {
				return nil, errors.New("workspace copy ownership is missing")
			}

			if prior, ok := copies[e.CopyURI]; ok {
				if prior != e {
					return nil, errors.New("workspace identity has conflicting layouts")
				}
				continue
			}
			folded := strings.ToLower(e.Layout)
			if _, ok := layouts[folded]; ok {
				return nil, errors.New("workspace layouts collide")
			}
			layouts[folded] = e.CopyURI
			copies[e.CopyURI] = e
			out = append(out, e)
			if len(out) > workspaceLimit {
				return nil, errors.New("workspace root count exceeds its bound")
			}
		}
		if !primary {
			return nil, errors.New("workspace primary slot is missing")
		}
	}
	visiting := map[string]bool{}
	done := map[string]bool{}
	var visit func(string) error
	visit = func(uri string) error {
		if done[uri] {
			return nil
		}
		if visiting[uri] {
			return errors.New("workspace ground links contain a cycle")
		}
		visiting[uri] = true
		e := copies[uri]
		if _, ok := copies[e.GroundURI]; ok {
			if err := visit(e.GroundURI); err != nil {
				return err
			}
		}
		done[uri] = true
		visiting[uri] = false
		return nil
	}
	for _, e := range out {
		if err := visit(e.CopyURI); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ValidWorkspaceSlot preserves supported portable ASCII names without aliases.
func ValidWorkspaceSlot(slot string) bool {
	if slot == "" || len(slot) > 255 || strings.HasPrefix(slot, ".") || strings.TrimSpace(slot) != slot || strings.HasSuffix(slot, ".") || strings.ContainsAny(slot, `<>:"/\|?*`) {
		return false
	}
	for _, b := range []byte(slot) {
		if b < 32 || b > 126 {
			return false
		}
	}
	stem := strings.ToLower(strings.SplitN(slot, ".", 2)[0])
	switch stem {
	case "con", "prn", "aux", "nul", "clock$", "conin$", "conout$":
		return false
	}
	if len(stem) == 4 && (strings.HasPrefix(stem, "com") || strings.HasPrefix(stem, "lpt")) && stem[3] >= '1' && stem[3] <= '9' {
		return false
	}
	return true
}
func workspaceSlot(s string) bool { return ValidWorkspaceSlot(s) }

func workspaceToken(uri string) string {
	sum := sha256.Sum256([]byte(uri))
	return hex.EncodeToString(sum[:])
}

// workspacePath rejects substitutions in any existing path component. Ownership
// still comes from the registry; containment alone never grants it.
func workspacePath(root, rel string) (string, error) {
	if !filepath.IsAbs(root) || !filepath.IsLocal(rel) {
		return "", errors.New("workspace path is not local")
	}
	current := filepath.Clean(root)
	for _, part := range strings.Split(filepath.FromSlash(rel), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("workspace path contains a symbolic link")
		}
	}
	actual, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	if actual != filepath.Clean(root) {
		return "", errors.New("workspace root is redirected")
	}
	return current, nil
}
