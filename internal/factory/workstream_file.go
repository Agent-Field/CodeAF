package factory

import "time"

// ── A WORKSTREAM'S FILES ───────────────────────────────────────────────────
//
// Files is a workstream's shared drive: everything worth keeping, in folders,
// made by people or by agents. A file is a thin row that POINTS at what it
// holds rather than holding it, so nothing an organization already keeps is
// copied:
//
//   - a document IS one of the workstream's pages (PageID); its words are the
//     page's live document, its versions the page's versions;
//   - a canvas IS one of the organization's canvases (CanvasID);
//   - an upload is bytes in the server's blob store (Upload.Blob, the sha256);
//   - a link and a repo carry their address;
//   - a shortcut names a file in another workstream (Target) and copies
//     nothing, so the file it shows stays one file.
//
// MOVING A FILE NEVER BREAKS WHAT POINTS TO IT: everything that refers to a
// file — a task's result, a shortcut, a link — refers to its id, and a move
// only changes Folder.
//
// A DELETE IS NOT A DESTRUCTION. It stamps DeletedAt and the file sits in
// Recently deleted for [FileKeptDeleted]; a restore before then brings it
// back where it was. A folder's delete takes everything under it, each marked
// with DeletedWith so a restore brings back exactly what went with it.

// FileKeptDeleted is how long a deleted file is kept before it is let go.
const FileKeptDeleted = 30 * 24 * time.Hour

// The kinds a file may be.
const (
	FileFolder   = "folder"
	FileDocument = "document"
	FileCanvas   = "canvas"
	FileUpload   = "upload"
	FileLink     = "link"
	FileRepo     = "repo"
	FileShortcut = "shortcut"
)

// FileKinds are every kind, in the order the New menu offers them.
var FileKinds = []string{FileFolder, FileDocument, FileCanvas, FileUpload, FileLink, FileRepo, FileShortcut}

// The makers a file may have.
const (
	MakerPerson = "person"
	MakerAfi    = "afi"
	MakerChat   = "chat"
)

// WorkstreamFile is one file of one workstream as the server keeps it. Rev
// counts every change to this one file, so two people changing two files
// never refuse each other.
type WorkstreamFile struct {
	ID string `json:"id"`
	WS string `json:"ws"`
	// Rev counts every change to this file.
	Rev int `json:"rev"`
	// Folder is the id of the folder it sits in, empty at the top.
	Folder string    `json:"folder,omitempty"`
	Kind   string    `json:"kind" jsonschema:"enum=folder,enum=document,enum=canvas,enum=upload,enum=link,enum=repo,enum=shortcut"`
	Name   string    `json:"name"`
	MadeBy FileMaker `json:"made_by"`
	// FromTask is the task whose work made it.
	FromTask string `json:"from_task,omitempty"`
	// UsedBy are the tasks whose agents read it; changing the file flags the
	// ones still running.
	UsedBy    []string  `json:"used_by"`
	CreatedAt time.Time `json:"created_at"`
	ChangedAt time.Time `json:"changed_at"`
	// ChangedBy is the handle of whoever changed it last.
	ChangedBy string `json:"changed_by,omitempty"`
	// Versions and OpenComments are counted when the file is read, never
	// kept; absent when there are none.
	Versions     int `json:"versions,omitempty"`
	OpenComments int `json:"open_comments,omitempty"`
	// PersonEdited is whether a person has changed this file's contents. AN
	// AGENT EDITS DIRECTLY ONLY A FILE IT MADE THAT NO PERSON HAS EDITED YET;
	// every other change it makes arrives as a suggestion.
	PersonEdited bool        `json:"person_edited,omitempty"`
	PageID       string      `json:"page_id,omitempty"`
	CanvasID     string      `json:"canvas_id,omitempty"`
	URL          string      `json:"url,omitempty"`
	Repo         string      `json:"repo,omitempty"`
	Upload       *UploadBlob `json:"upload,omitempty"`
	Target       *FileTarget `json:"target,omitempty"`
	DeletedAt    *time.Time  `json:"deleted_at,omitempty"`
	// DeletedWith is the folder whose delete took this file with it; a
	// restore of that folder brings back exactly the files it took.
	DeletedWith string `json:"deleted_with,omitempty"`
	// Source is the id of the workstream source this file was folded in
	// from, kept so the fold never makes a second file for one source.
	Source string `json:"source,omitempty"`
}

// FileMaker is who made a file: a person (their handle), Afi, or a chat. An
// agent's file also carries For, the person whose agent made it.
type FileMaker struct {
	Kind string `json:"kind" jsonschema:"enum=person,enum=afi,enum=chat"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
	For  string `json:"for,omitempty"`
}

// UploadBlob is an uploaded file's bytes, by the sha256 they are kept under.
// Blob is empty for a file a source named by path before uploads existed.
type UploadBlob struct {
	Size int64  `json:"size"`
	Mime string `json:"mime,omitempty"`
	Blob string `json:"blob,omitempty"`
}

// FileTarget is the file a shortcut shows: its workstream and its id.
type FileTarget struct {
	WS   string `json:"ws"`
	File string `json:"file"`
}

// AgentMayEdit is whether an agent may change f's contents directly rather
// than by suggestion: an agent made it and no person has edited it since.
func (f WorkstreamFile) AgentMayEdit() bool {
	return f.MadeBy.Kind != MakerPerson && !f.PersonEdited
}
