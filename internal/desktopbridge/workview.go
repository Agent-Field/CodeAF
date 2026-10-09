package desktopbridge

// workview.go serves the file and diff tabs. Every byte comes from the engine
// through the remote client, never from this process's disk, because the
// engine may be on another machine than the app. Shapes are remote's own
// (TextFile, FoundFiles, ChangedFiles, FileDiff); see desktop/docs/ENGINE.md.

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// engineStatus maps the engine's refusal sentence to a status: the file-fetch
// mapping plus 400 for a blank name.
func engineStatus(err error) int {
	if strings.Contains(err.Error(), "nothing was named") {
		return 400
	}
	return refusalStatus(err)
}

func (s *conversation) fileText(w http.ResponseWriter, r *http.Request) {
	if !needGet(w, r) {
		return
	}
	if s.conn.ReadText == nil {
		fail(w, 409, "this engine cannot show files")
		return
	}
	file, err := s.conn.ReadText(r.URL.Query().Get("path"))
	if err != nil {
		fail(w, engineStatus(err), err.Error())
		return
	}
	write(w, file)
}

func (s *conversation) fileFind(w http.ResponseWriter, r *http.Request) {
	if !needGet(w, r) {
		return
	}
	if s.conn.FindFiles == nil {
		fail(w, 409, "this engine cannot search files")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	found, err := s.conn.FindFiles(r.URL.Query().Get("q"), limit)
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	write(w, found)
}

// changes lists the files that differ from the base; repeated ?path= narrows
// it to the files a conversation touched.
func (s *conversation) changes(w http.ResponseWriter, r *http.Request) {
	if !needGet(w, r) {
		return
	}
	if s.conn.DiffChanges == nil {
		fail(w, 409, "this engine cannot list changes")
		return
	}
	paths := r.URL.Query()["path"]
	if len(paths) > 500 {
		fail(w, 400, "ask about at most 500 paths at a time")
		return
	}
	list, err := s.conn.DiffChanges(paths)
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	write(w, list)
}

func (s *conversation) fileDiff(w http.ResponseWriter, r *http.Request) {
	if !needGet(w, r) {
		return
	}
	if s.conn.DiffFile == nil {
		fail(w, 409, "this engine cannot show diffs")
		return
	}
	diff, err := s.conn.DiffFile(r.URL.Query().Get("path"))
	if err != nil {
		fail(w, engineStatus(err), err.Error())
		return
	}
	write(w, diff)
}

// EditorTarget is the answer to "open in editor". Abs is a path on the
// ENGINE's disk. Local is the bridge's claim that the engine shares a disk
// with the app (it started the engine as its own child); the app opens an
// editor only when Local holds AND Host equals its own machine name.
type EditorTarget struct {
	Path  string `json:"path"`
	Abs   string `json:"abs"`
	Host  string `json:"host"`
	Local bool   `json:"local"`
}

// fileLocate returns the absolute path of a workspace file the engine says is
// really there. The engine's stat is the confinement check (symlinks that
// leave and traversals answer exists:false); the join is from the engine's own
// reported workspace.
func (s *conversation) fileLocate(w http.ResponseWriter, r *http.Request) {
	if !needGet(w, r) {
		return
	}
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		fail(w, 400, "path required")
		return
	}
	workspace := s.conn.Welcome.Workspace
	if s.conn.StatPaths == nil || workspace == "" {
		fail(w, 409, "this engine cannot place files")
		return
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(workspace, path)
	}
	abs = filepath.Clean(abs)
	if rel, err := filepath.Rel(filepath.Clean(workspace), abs); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		fail(w, 403, "that file is outside this conversation's workspace")
		return
	}
	facts, err := s.conn.StatPaths([]string{abs})
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	if len(facts) != 1 || !facts[0].Exists || facts[0].Dir {
		fail(w, 404, "no such file: "+path)
		return
	}
	host, _ := os.Hostname()
	write(w, EditorTarget{Path: path, Abs: abs, Host: host, Local: s.conn.Local})
}
