package api

import "github.com/Agent-Field/codeaf/internal/factory"

// ── A WORKSTREAM'S FILES ON THE WIRE ───────────────────────────────────────
//
// Files is a workstream's shared drive ([factory.WorkstreamFile]). Each file
// carries its own rev, so two people changing two files never refuse each
// other.
//
//	GET    /v1/workstreams/{id}/files[?q=words][&deleted=1]  -> WorkstreamFiles
//	POST   /v1/workstreams/{id}/files                     FileNew -> WorkstreamFile (201)
//	PATCH  /v1/workstreams/{id}/files/{fid}  If-Match:file rev  FilePatch -> WorkstreamFile, 412 stale
//	POST   /v1/workstreams/{id}/files/move                FilesMove -> WorkstreamFiles (the moved files)
//	DELETE /v1/workstreams/{id}/files/{fid}               -> WorkstreamFile, now in Recently deleted
//	POST   /v1/workstreams/{id}/files/{fid}/restore       -> WorkstreamFile, back where it was
//	GET    /v1/workstreams/{id}/files/{fid}/content       -> an upload's bytes, typed as uploaded
//
// The first read of a workstream's files folds in what it kept before Files
// existed, once: every page becomes a document, every source a link, upload
// or repo, every canvas made inside it a canvas file, and a workstream with no
// decisions page gets a Decisions document. Nothing is copied: each file
// points at the page, canvas or address it always was.
//
// A child workstream reads its parent's files in From, read-only.
//
// Every change publishes workstream.changed with part "files" and the file's
// id; a change to a file's contents flags the running tasks that use it.

// PartFiles is the part a workstream.changed event names for a file.
const PartFiles = "files"

// WorkstreamFile is one file on the wire.
type WorkstreamFile = factory.WorkstreamFile

// WorkstreamFiles is a workstream's files: the live ones, the deleted ones
// when asked for, and the parent's when it has one the caller can see.
type WorkstreamFiles struct {
	Files []WorkstreamFile `json:"files"`
	// Deleted is Recently deleted, newest first; only with ?deleted=1.
	Deleted []WorkstreamFile `json:"deleted,omitempty"`
	// From is the parent workstream's files, read-only here.
	From *FilesFrom `json:"from,omitempty"`
}

// FilesFrom is the parent's files as a child reads them.
type FilesFrom struct {
	ID    string           `json:"id"`
	Name  string           `json:"name"`
	Files []WorkstreamFile `json:"files"`
}

// FileNew is a file to make. Kind decides which of the rest it needs:
//
//   - folder, document: Name. A document is a new page, BodyMD its first words.
//   - canvas: CanvasID, a canvas already made inside this workstream.
//   - link: URL. repo: Repo ("owner/name").
//   - upload: Data (the bytes, base64 on the wire), Mime and Name.
//   - shortcut: Target, a file in another workstream the caller can see.
//
// MadeBy is the agent that made it, when one did (kind afi or chat, with its
// name); a person's file leaves it out and is stamped as the caller's.
type FileNew struct {
	Kind     string              `json:"kind"`
	Name     string              `json:"name,omitempty"`
	Folder   string              `json:"folder,omitempty"`
	FromTask string              `json:"from_task,omitempty"`
	MadeBy   *factory.FileMaker  `json:"made_by,omitempty"`
	BodyMD   string              `json:"body_md,omitempty"`
	CanvasID string              `json:"canvas_id,omitempty"`
	URL      string              `json:"url,omitempty"`
	Repo     string              `json:"repo,omitempty"`
	Data     []byte              `json:"data,omitempty"`
	Mime     string              `json:"mime,omitempty"`
	Target   *factory.FileTarget `json:"target,omitempty"`
}

// FilePatch is any subset of a file's name, task links, and whether its
// contents were just changed ("person" or "agent"). A document's rename
// renames its page.
type FilePatch struct {
	Name     *string   `json:"name,omitempty"`
	FromTask *string   `json:"from_task,omitempty"`
	UsedBy   *[]string `json:"used_by,omitempty"`
	Edited   string    `json:"edited,omitempty"`
}

// FilesMove moves files into Folder ("" is the top), all or none.
type FilesMove struct {
	Files  []string `json:"files"`
	Folder string   `json:"folder"`
}
