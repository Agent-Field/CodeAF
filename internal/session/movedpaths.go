package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const movedCopyNoteOpening = "A note from the session, not from the person: this conversation moved to another folder. Facts, not requests."

// movedCopy reads this machine's arrival facts, never a folder from a previous
// machine's transcript. A take-back records the copy it came back from too.
func (a *Agent) movedCopy() (Meta, string) {
	root := a.config.Workspace
	if a.config.Place.Dir == "" {
		return Meta{}, root
	}
	meta, _ := LoadMeta(a.config.Place.Dir)
	if meta.FromWorkspace != "" {
		meta.Origin = meta.FromWorkspace
	}
	if meta.Origin == "" || filepath.Clean(meta.Origin) == filepath.Clean(root) {
		return Meta{}, root
	}
	return meta, root
}

// landMovedCopyLocked keeps the location correction in the model's context,
// including after reopening or compaction; it is never a person's message.
func (a *Agent) landMovedCopyLocked() {
	meta, root := a.movedCopy()
	if meta.Origin == "" {
		return
	}
	from := "another computer"
	if meta.FromDevice != "" {
		from = fmt.Sprintf("device %q", meta.FromDevice)
	}
	a.landNoteLocked(movedCopyNoteOpening, fmt.Sprintf("This chat moved from %s. The old folder was %s; the workspace here is %s. Paths in earlier turns under %s now live at the same relative paths under %s. Work in the workspace here, even if the old folder exists on this machine.", from, meta.Origin, root, meta.Origin, root))
}

// movedToolArguments rebases file-tool paths and shell commands before the
// executor records their effects, so an old absolute path cannot edit the
// other computer's project when both homes happen to be on one machine.
func (a *Agent) movedToolArguments(tool string, args json.RawMessage) (json.RawMessage, string, error) {
	a.mu.Lock()
	meta, root := a.movedCopy()
	a.mu.Unlock()
	if meta.Origin == "" {
		return args, "", nil
	}
	switch tool {
	case "read", "write", "edit", "grep", "find", "ls", "bash":
	default:
		return args, "", nil
	}
	// Arguments that are not one object are passed on untouched: the tool's own
	// decode refuses them in the one grammar every tool speaks.
	var fields map[string]json.RawMessage
	if decodeToolArguments(args, &fields) != nil {
		return args, "", nil
	}
	changed := false
	for _, field := range []string{"path", "cwd", "dir", "command"} {
		var value string
		if json.Unmarshal(fields[field], &value) != nil {
			continue
		}
		mapped := value
		if field == "command" {
			var err error
			mapped, err = movedCommand(value, meta.Origin, root)
			if err != nil {
				return nil, "", err
			}
		} else if filepath.IsAbs(value) {
			clean := filepath.Clean(value)
			if rel := relativeTo(meta.Origin, clean); len(rel) > 0 {
				mapped = filepath.Join(root, filepath.FromSlash(rel[0]))
				if err := checkMovedDestination(root, mapped); err != nil {
					return nil, "", err
				}
			}
		}
		if mapped != value {
			fields[field], _ = json.Marshal(mapped)
			changed = true
		}
	}
	if !changed {
		return args, "", nil
	}
	mapped, err := json.Marshal(fields)
	return mapped, fmt.Sprintf("Paths from the old folder %s were mapped into the workspace here: %s.", meta.Origin, root), err
}

// movedCommand replaces whole absolute path prefixes, including quoted paths
// and flag values, while leaving similarly named sibling folders untouched.
func movedCommand(command, origin, workspace string) (string, error) {
	origin = filepath.Clean(origin)
	var out strings.Builder
	var quote byte
	escaped := false
	for len(command) > 0 {
		at := strings.Index(command, origin)
		if at < 0 {
			out.WriteString(command)
			break
		}
		end := at + len(origin)
		before := at == 0 || strings.ContainsRune(" \t\r\n\"'`=(:;|&<>{[", rune(command[at-1]))
		after := end == len(command) || strings.ContainsRune("/ \t\r\n\"'`;|&<>)}]", rune(command[end]))
		for i := 0; i < at; i++ {
			c := command[i]
			switch {
			case escaped:
				escaped = false
			case c == '\\' && quote != '\'':
				escaped = true
			case quote != 0:
				if c == quote {
					quote = 0
				}
			case c == '\'' || c == '"':
				quote = c
			}
		}
		out.WriteString(command[:at])
		if before && after {
			pathEnd := end
			for pathEnd < len(command) && !strings.ContainsRune(" \t\r\n\"'`;|&<>():,[]{}", rune(command[pathEnd])) {
				pathEnd++
			}
			path := filepath.Clean(workspace + command[end:pathEnd])
			if err := checkMovedDestination(workspace, path); err != nil {
				return "", err
			}
			// The new home can have spaces or shell punctuation even when the old
			// one did not. Escape only the replacement for its current shell quote.
			switch quote {
			case '\'':
				out.WriteString(strings.ReplaceAll(workspace, "'", "'\"'\"'"))
			case '"':
				out.WriteString(strings.NewReplacer("\\", "\\\\", "$", "\\$", "`", "\\`", "\"", "\\\"").Replace(workspace))
			default:
				out.WriteString("'" + strings.ReplaceAll(workspace, "'", "'\"'\"'") + "'")
			}
		} else {
			out.WriteString(origin)
		}
		command = command[end:]
	}
	return out.String(), nil
}

// checkMovedDestination refuses a copied link that would send an explicitly
// rebased file operation outside the new workspace. Missing leaf paths are
// checked through their nearest existing ancestor before write creates them.
func checkMovedDestination(root, path string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			if len(relativeTo(realRoot, resolved)) == 0 {
				return fmt.Errorf("the moved chat's path resolves outside its workspace: %s", path)
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return err
		}
		path = parent
	}
}
