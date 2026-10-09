package desktopbridge

// WHERE A NEW CHAT IN A PLACE WORKS (Places Architecture Q-P9).
//
// A chat started in a place works in that place's first usable folder source
// (placegraph.Snapshot.WorkingFolder); a place with none, or none that can be
// used, leaves it where it always was — the folder this bridge was launched on —
// and says so when it had folders it could not use.
//
// THE FOLDER IS NEVER THE CLIENT'S. The request carries a place id and nothing
// that is a path: the path comes from the place's own listing, re-checked at the
// moment of opening against the same source policy the prompt is built with, with
// symlinks followed and permissions asked of the disk. A window cannot make this
// door open an engine anywhere a place does not already list.
//
// THE ENGINE IS MOVED THE ONLY WAY IT CAN BE: by dialling the session host of that
// folder. A host holds ONE workspace (it changes directory into it once), so a chat
// in another folder is a connection to that folder's host — never a chdir inside a
// host that holds a different one. That is [OpenIn]. A bridge with no such door
// (a remote engine, a test) keeps every chat in its own workspace and says so.
//
// ONLY A NEW CHAT ASKS THE PLACE. A saved conversation reopens on the workspace
// its own meta.json names, re-checked with the same rules, and only when that
// file is one this install stored. A chat already filed in a place is not
// moved when the place later gains a folder: reopening reads the record, not
// the place's current list.

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

// OpenIn opens a conversation on the session host of workspace, a directory
// the bridge already validated. An empty sessionFile mints a new conversation
// there. A named file reopens that saved conversation on the host that holds
// its folder — never by changing directory inside a host that holds another.
type OpenIn func(workspace, sessionFile string) (Connection, error)

// UseOpenIn gives the bridge the door that opens a chat in a folder other than
// its own workspace. Without it, place chats work where the bridge was launched.
func (b *Bridge) UseOpenIn(open OpenIn) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.openIn = open
}

// WorkingFolder is what a new place chat reports about where it works. It is
// drawn only when a place had a folder to say something about (the emptiness law).
type WorkingFolder struct {
	// From is "place" when the chat works in one of the place's folders, and
	// "launch" when the place listed folders but none could be used.
	From string `json:"from"`
	// Path is the folder (From "place"). Label is the source's own label.
	Path  string `json:"path,omitempty"`
	Label string `json:"label,omitempty"`
	// Skipped are the folders listed first that could not be used, each with why.
	Skipped []placegraph.SkippedFolder `json:"skipped,omitempty"`
	// Note is the sentence for the From "launch" case.
	Note string `json:"note,omitempty"`
}

// folderFor answers where a new chat filed in placeID should work: the folder to
// dial, and what to report. A zero Path and nil report is "nothing to say". It runs under b.mu.
func (b *Bridge) folderFor(p *Places, placeID string) (string, *WorkingFolder) {
	canOpen := b.openIn != nil // the caller (POST /sessions) holds b.mu
	snap, err := p.Store.Snapshot()
	if err != nil {
		return "", nil
	}
	found := snap.WorkingFolder(placeID, p.sourcePolicy())
	switch {
	case !found.Listed():
		return "", nil
	case found.Path == "":
		return "", &WorkingFolder{From: "launch", Skipped: found.Skipped, Note: skippedSentence(found.Skipped, "The place's folders can't be used, so this chat works where codeaf was started.")}
	case !canOpen:
		return "", &WorkingFolder{From: "launch", Skipped: found.Skipped, Note: "This engine can't open a chat in a place's folder, so it works where codeaf was started."}
	}
	return found.Path, &WorkingFolder{From: "place", Path: found.Path, Label: found.Label, Skipped: found.Skipped}
}

// folderPolicy is the source policy the working-folder checks use. The places
// door carries the engine's own list; without one, the default for this user.
// The caller holds b.mu.
func (b *Bridge) folderPolicy() placegraph.SourcePolicy {
	if b.places != nil {
		return b.places.sourcePolicy()
	}
	userHome, _ := os.UserHomeDir()
	return placegraph.DefaultSourcePolicy(userHome, home.Dir())
}

// recordedFolder is the workspace a saved conversation's own meta.json names,
// or "" when there is nothing safe to follow and the launch host should open it.
//
// THE CLIENT NAMES THE FILE AND NOTHING ELSE. Only a transcript that really
// lives in this install's state root is read, and only when its identity
// matches the folder. The workspace in that file is then checked the same way
// a place folder is — canonical, not denied, not the top of a disk, enterable —
// and a workspace inside the state root is refused. That is where an owned
// session keeps its work/ and where the journals live; a host born there would
// be a second engine inside the library, which a place folder never is.
func recordedFolder(libraryRoot, sessionFile string, pol placegraph.SourcePolicy) string {
	libraryRoot = filepath.Clean(strings.TrimSpace(libraryRoot))
	if libraryRoot == "" || libraryRoot == string(filepath.Separator) {
		return ""
	}
	real, ok := insideRoot(libraryRoot, sessionFile)
	if !ok {
		return ""
	}
	dir := filepath.Dir(real)
	if (session.Place{Dir: dir}).Transcript() != real {
		return ""
	}
	meta, err := session.LoadMeta(dir)
	if err != nil || strings.TrimSpace(meta.ID) == "" || meta.ID != filepath.Base(dir) {
		return ""
	}
	path, reason := placegraph.UsableDirectory(meta.Workspace, pol)
	if reason != "" || path == "" || inside(libraryRoot, path) {
		return ""
	}
	return path
}

// insideRoot is path resolved and confirmed to sit inside root. A missing
// path, a relative one, and a link that leaves root are all outside.
func insideRoot(root, path string) (string, bool) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", false
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil || !inside(root, real) {
		return "", false
	}
	return real, true
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func skippedSentence(skipped []placegraph.SkippedFolder, tail string) string {
	if len(skipped) == 0 {
		return tail
	}
	first := skipped[0]
	return "Can't use " + filepath.Base(first.Ref) + ": " + strings.TrimSuffix(first.Reason, ".") + ". " + tail
}

// sameFolder reports that the engine opened the chat where it was asked to.
func sameFolder(want, got string) bool {
	a, errA := filepath.EvalSymlinks(want)
	b, errB := filepath.EvalSymlinks(strings.TrimSpace(got))
	return errA == nil && errB == nil && a == b
}
