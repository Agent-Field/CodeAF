package appwire

import "strings"

// FILES.READ: a file chip opens a read-only preview (V63, V70 "File too large").
// The app never writes through this door and never holds a whole file: a text
// file of at most [FileReadMax] bytes is answered one page of at most
// [FilePageMax] bytes at a time; anything else is answered by its facts alone.

// The bounds of files.read.
const (
	// FileReadMax is the largest file previewed as text. Larger is too_large:
	// facts only, no bytes, whatever page is asked for.
	FileReadMax = 1 << 20
	// FilePageMax is the most text one answer carries.
	FilePageMax = 64 << 10
)

// What a read found.
const (
	ReadText       = "text"       // UTF-8 text without NUL, at most FileReadMax
	ReadBinary     = "binary"     // a NUL byte or bytes that are not UTF-8; never shown
	ReadTooLarge   = "too_large"  // over FileReadMax; never read
	ReadDirectory  = "directory"  // a folder; never read
	ReadMissing    = "missing"    // nothing there now (deleted, or a dangling link)
	ReadUnreadable = "unreadable" // there, but not a regular file, or not readable
)

// FileRead is one path as files.read finds it, and a page of its text when it
// is text. Resolved is the canonical absolute path, every link followed;
// Outside says it lies outside the agent's workspace once resolved. The
// preview is read-only wherever the file is.
type FileRead struct {
	Path     string `json:"path"`
	Resolved string `json:"resolved,omitempty"`
	Kind     string `json:"kind"`
	Outside  bool   `json:"outside,omitempty"`
	Symlink  bool   `json:"symlink,omitempty"`
	Size     *int64 `json:"size,omitempty"`
	Modified string `json:"modified,omitempty"`
	Add      *int   `json:"add,omitempty"`
	Del      *int   `json:"del,omitempty"`
	Lines    *int   `json:"lines,omitempty"`
	Offset   int64  `json:"offset"`
	Text     string `json:"text"`
	Next     *int64 `json:"next,omitempty"`
}

// FileReader is the optional engine half of files.read. One that is not a
// FileReader refuses files.read as unknown.
type FileReader interface {
	// ReadFile answers path (relative to the agent's workspace, or absolute)
	// and, for text, the page from offset of at most limit bytes, cut at a
	// line or character boundary. A limit of zero answers the facts alone.
	ReadFile(agent, path string, offset int64, limit int) (FileRead, error)
}

// FileReadResult is files.read's answer.
type FileReadResult struct {
	File FileRead `json:"file"`
}

func init() {
	methods["files.read"] = typed(func(e Engine, p struct {
		Agent  string `json:"agent"`
		Path   string `json:"path"`
		Offset int64  `json:"offset"`
		Limit  int    `json:"limit"`
	}) (*FileReadResult, error) {
		r, ok := e.(FileReader)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve files.read")
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.Path) == "" || p.Offset < 0 || p.Limit < 0 {
			return nil, Errorf(CodeBadParams, "files.read needs an agent, a path, and an offset and limit of zero or more")
		}
		file, err := r.ReadFile(p.Agent, p.Path, p.Offset, min(p.Limit, FilePageMax))
		if err != nil {
			return nil, err
		}
		return &FileReadResult{file}, nil
	})
}
