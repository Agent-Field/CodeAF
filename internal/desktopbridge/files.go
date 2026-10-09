package desktopbridge

import (
	"encoding/base64"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
)

// EngineFile is one workspace file for the renderer. Inline is false for the
// kinds a browser would execute (svg, html, xml): they are data to save or show
// as text, never a document to render.
type EngineFile struct {
	Name       string `json:"name"`
	Mime       string `json:"mime"`
	Size       int64  `json:"size"`
	Hash       string `json:"hash"`
	Inline     bool   `json:"inline"`
	DataBase64 string `json:"dataBase64"`
}

// PathFact is one stat answer. Outside means the engine would refuse the path
// because it lies beyond the workspace and the conversation's own folder.
type PathFact struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime,omitempty"`
	Outside bool   `json:"outside,omitempty"`
}

// inlineAllowed is filedoor's allowlist: pictures, words, sound, moving
// pictures and a PDF render inline; anything XML enough to be a program does not.
func inlineAllowed(kind string) bool {
	media, _, err := mime.ParseMediaType(kind)
	if err != nil {
		return false
	}
	switch media = strings.ToLower(media); media {
	case "image/svg+xml", "image/svg", "text/html", "text/xml", "application/xhtml+xml", "application/xml":
		return false
	case "application/pdf":
		return true
	}
	if strings.HasSuffix(media, "+xml") {
		return false
	}
	for _, prefix := range []string{"image/", "text/", "audio/", "video/"} {
		if strings.HasPrefix(media, prefix) {
			return true
		}
	}
	return false
}

// refusalStatus maps the engine's sentence to an HTTP status.
func refusalStatus(err error) int {
	text := err.Error()
	switch {
	case strings.Contains(text, "outside"):
		return http.StatusForbidden
	case strings.Contains(text, "no such file"), strings.Contains(text, "is not a file"), strings.Contains(text, "nothing was named"):
		return http.StatusNotFound
	}
	return http.StatusBadGateway
}

func (s *conversation) readFile(w http.ResponseWriter, r *http.Request) {
	if !needGet(w, r) {
		return
	}
	path := r.URL.Query().Get("path")
	if strings.TrimSpace(path) == "" {
		fail(w, 400, "path required")
		return
	}
	if s.conn.FetchFile == nil {
		fail(w, 409, "this engine cannot hand over files")
		return
	}
	file, err := s.conn.FetchFile(path)
	if err != nil {
		fail(w, refusalStatus(err), err.Error())
		return
	}
	write(w, EngineFile{Name: file.Name, Mime: file.MIME, Size: file.Size, Hash: file.Hash, Inline: inlineAllowed(file.MIME), DataBase64: base64.StdEncoding.EncodeToString(file.Bytes)})
}

const statMax = 64

func (s *conversation) statFiles(w http.ResponseWriter, r *http.Request) {
	if !needPost(w, r) {
		return
	}
	var ask struct {
		Paths []string `json:"paths"`
	}
	if !decode(w, r, &ask) {
		return
	}
	if len(ask.Paths) > statMax {
		fail(w, 400, "ask about at most 64 paths at a time")
		return
	}
	if s.conn.StatPaths == nil {
		fail(w, 409, "this engine cannot look at paths")
		return
	}
	facts := []PathFact{}
	if len(ask.Paths) > 0 {
		found, err := s.conn.StatPaths(ask.Paths)
		if err != nil {
			fail(w, 502, err.Error())
			return
		}
		for _, f := range found {
			facts = append(facts, s.pathFact(f))
		}
	}
	write(w, facts)
}

func (s *conversation) pathFact(f remote.PathFact) PathFact {
	out := PathFact{Path: f.Path, Exists: f.Exists, Dir: f.Dir, Size: f.Size}
	if f.ModTime != 0 {
		out.ModTime = time.Unix(f.ModTime, 0).UTC().Format(time.RFC3339)
	}
	out.Outside = !f.Exists && s.outsideRoots(f.Path)
	return out
}

// outsideRoots is a lexical reading of the engine's two-roots law, used only to
// say why a path that does not exist was refused.
func (s *conversation) outsideRoots(path string) bool {
	workspace := s.conn.Welcome.Workspace
	if workspace == "" || strings.TrimSpace(path) == "" {
		return false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	path = filepath.Clean(path)
	roots := []string{workspace}
	if file := s.conn.Welcome.SessionFile; file != "" {
		roots = append(roots, filepath.Dir(file))
	}
	for _, root := range roots {
		rel, err := filepath.Rel(filepath.Clean(root), path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false
		}
	}
	return true
}
