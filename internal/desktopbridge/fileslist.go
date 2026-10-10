package desktopbridge

import (
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/remote"
)

// listFilesCap is the engine's listing ceiling (listDirMax in
// internal/remote/file.go). The engine already cuts there; this cut is the
// same ceiling, so a door that returned a longer page cannot hand the window
// more rows than the engine would.
const listFilesCap = 2000

// folderEntry is one row of a listing as the window reads it. ModTime is the
// engine's unix seconds (DirEntry.ModTime, `mtime` on the remote wire), under
// the name the rest of this bridge uses for a file's time.
type folderEntry struct {
	Name    string `json:"name"`
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime,omitempty"`
}

// folderListing is one directory. Path is the path the engine resolved, and
// is never rebuilt here. Truncated is true when the tail was cut.
type folderListing struct {
	Path      string        `json:"path"`
	Entries   []folderEntry `json:"entries"`
	Truncated bool          `json:"truncated"`
}

func (s *conversation) listFiles(w http.ResponseWriter, r *http.Request) {
	ListFiles(w, r, s.conn.ListDir)
}

// ListFiles answers GET /sessions/{id}/files/list. listDir nil is an engine
// that cannot browse. The path is passed through unchanged — confinement is
// the engine's two-roots rule, not a check this process repeats, because the
// engine may be on another machine and is the only one that can see the disk.
// A missing path asks for the workspace root, which the engine spells ".".
func ListFiles(w http.ResponseWriter, r *http.Request, listDir func(string) (remote.DirListing, error)) {
	if !needGet(w, r) {
		return
	}
	if listDir == nil {
		fail(w, http.StatusConflict, "this engine cannot list folders")
		return
	}
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		path = "."
	}
	listing, err := listDir(path)
	if err != nil {
		fail(w, refusalStatus(err), err.Error())
		return
	}
	write(w, folderListingFrom(listing))
}

// folderListingFrom copies the engine's listing into the window's shape and
// cuts the tail at listFilesCap. The order is the engine's: directories
// first, then files, each half by name. Nothing here re-sorts.
func folderListingFrom(listing remote.DirListing) folderListing {
	truncated := listing.Truncated
	entries := listing.Entries
	if len(entries) > listFilesCap {
		entries = entries[:listFilesCap]
		truncated = true
	}
	rows := make([]folderEntry, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, folderEntry{Name: entry.Name, Dir: entry.Dir, Size: entry.Size, ModTime: entry.ModTime})
	}
	return folderListing{Path: listing.Path, Entries: rows, Truncated: truncated}
}
