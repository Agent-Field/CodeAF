package session

// The `folders` tool: file this conversation in a logical folder, take it out,
// move a placement, or list what is there.
//
// Logical folders are membership, not directories. They never change cwd, the
// repo, or `/attach`. Filesystem `/folder` `/place` `/dir` stay filesystem.
// The conversation id for membership is this session's Place.ID() — the 16-hex
// folder name — never a UI path.
//
// THE TOOL IS ABSENT WHEN Config.Folders IS NIL, which is the absence law this
// belt is built on: a verb with nothing behind it is a model told it can file
// this chat, whose every call then refuses.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/workspace"
)

// FolderRef is one logical folder as the folders tool lists it.
type FolderRef struct {
	ID   string
	Name string
}

// Folders is the logical-folder membership seam the `folders` tool talks to.
// The methods wrap wsapi (List / File / Unfile / Move). The interface lives
// here so wsapi does not import session.
//
// NIL IS OFF: no verb on the belt.
type Folders interface {
	List(ctx context.Context) ([]FolderRef, error)
	File(ctx context.Context, collectionID, conversationID string) error
	Unfile(ctx context.Context, collectionID, conversationID string) error
	Move(ctx context.Context, fromID, toID, conversationID string) error
}

func init() { glossField["folders"] = "action" }

const foldersDescription = "File this conversation in a logical folder, take it out, move a placement, or list the folders. Membership does not change the working directory; /folder is still a filesystem path. The current chat is implied and is never passed as an id."

const foldersSchemaJSON = `{
  "type": "object",
  "properties": {
    "action": {
      "type": "string",
      "enum": ["list", "file", "unfile", "move"],
      "description": "list folders, file this chat, unfile it, or move a placement."
    },
    "id": {
      "type": "string",
      "description": "Folder id for file or unfile, or the destination of a move."
    },
    "from": {
      "type": "string",
      "description": "Source folder id for move. Other placements of this chat stay."
    }
  },
  "required": ["action"],
  "additionalProperties": false
}`

func (a *Agent) foldersTools() []bare.Tool {
	if a.config.Folders == nil {
		return nil
	}
	return []bare.Tool{{
		Name:        "folders",
		Description: foldersDescription,
		Schema:      json.RawMessage(foldersSchemaJSON),
		Execute:     a.foldersTool,
	}}
}

func (a *Agent) foldersTool(ctx context.Context, args json.RawMessage) (string, bool, error) {
	var parsed struct {
		Action string `json:"action"`
		ID     string `json:"id"`
		From   string `json:"from"`
	}
	if err := decodeToolArguments(args, &parsed); err != nil {
		return "Invalid arguments: " + err.Error(), true, nil
	}
	action := strings.TrimSpace(parsed.Action)
	id := strings.TrimSpace(parsed.ID)
	from := strings.TrimSpace(parsed.From)
	switch action {
	case "list":
		return a.foldersList(ctx)
	case "file":
		if id == "" {
			return "Invalid arguments: file needs the folder id.", true, nil
		}
		return a.foldersMutate(ctx, func(chat string) error {
			return a.config.Folders.File(ctx, id, chat)
		})
	case "unfile":
		if id == "" {
			return "Invalid arguments: unfile needs the folder id.", true, nil
		}
		return a.foldersMutate(ctx, func(chat string) error {
			return a.config.Folders.Unfile(ctx, id, chat)
		})
	case "move":
		if from == "" || id == "" {
			return "Invalid arguments: move needs from (source folder id) and id (destination folder id).", true, nil
		}
		return a.foldersMutate(ctx, func(chat string) error {
			return a.config.Folders.Move(ctx, from, id, chat)
		})
	default:
		return "Invalid arguments: action must be list, file, unfile or move.", true, nil
	}
}

func (a *Agent) foldersList(ctx context.Context) (string, bool, error) {
	listed, err := a.config.Folders.List(ctx)
	if err != nil {
		return foldersRefusal(err), true, nil
	}
	if len(listed) == 0 {
		return "There are no logical folders.", false, nil
	}
	var b strings.Builder
	for i, folder := range listed {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(folder.ID)
		b.WriteString("  ")
		b.WriteString(folder.Name)
	}
	return b.String(), false, nil
}

func (a *Agent) foldersMutate(ctx context.Context, op func(chat string) error) (string, bool, error) {
	chat := strings.TrimSpace(a.config.Place.ID())
	if chat == "" {
		return "This conversation has no id, so it cannot be filed in a folder.", true, nil
	}
	if err := op(chat); err != nil {
		return foldersRefusal(err), true, nil
	}
	return "ok", false, nil
}

// foldersRefusal turns a store error into result text. Cycles and unknown ids
// must never panic: they are a sentence the model can act on.
func foldersRefusal(err error) string {
	if err == nil {
		return "Could not update folders."
	}
	if errors.Is(err, workspace.ErrCycle) || strings.Contains(strings.ToLower(err.Error()), "cycle") {
		return "Filing there would form a cycle."
	}
	if errors.Is(err, workspace.ErrNotFound) || strings.Contains(strings.ToLower(err.Error()), "not found") ||
		strings.Contains(strings.ToLower(err.Error()), "unknown") {
		return "That folder is unknown."
	}
	return "Could not update folders: " + err.Error()
}
