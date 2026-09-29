package session

import (
	"os"
	"path/filepath"
)

// layout answers where a session folder keeps its truth, and which folder a
// transcript path belongs to. It is the one place that knows a folder can
// spell its truth two ways: at the top (legacy) or inside .cell/ (cell), where
// it is sealed with the workspace. Derived files (card, meta.json) sit at the
// top in both.
type layout interface {
	// holds reports whether dir is laid out this way.
	holds(dir string) bool
	// transcript is the journal's path inside dir.
	transcript(dir string) string
	// truth is where dir keeps the truth file called name (state.json,
	// tasks.json, team.json) or the directory called name (tasks/).
	truth(dir, name string) string
	// metaTruth is the file holding the truth-only fields of the session's
	// meta, or "" when they ride in meta.json with the derived summary.
	metaTruth(dir string) string
	// folder is the session folder a transcript path belongs to.
	folder(transcript string) (string, bool)
}

// layouts is tried in order; the last entry claims everything.
var layouts = []layout{cellLayout{}, legacyLayout{}}

func layoutOf(dir string) layout {
	for _, l := range layouts[:len(layouts)-1] {
		if l.holds(dir) {
			return l
		}
	}
	return layouts[len(layouts)-1]
}

// legacyLayout keeps the journal at the folder's top: <dir>/transcript.jsonl.
type legacyLayout struct{}

func (legacyLayout) holds(string) bool { return true }

func (legacyLayout) transcript(dir string) string { return filepath.Join(dir, placeTranscript) }

func (legacyLayout) truth(dir, name string) string { return filepath.Join(dir, name) }

func (legacyLayout) metaTruth(string) string { return "" }

func (legacyLayout) folder(transcript string) (string, bool) {
	if filepath.Base(transcript) != placeTranscript {
		return "", false
	}
	return filepath.Dir(transcript), true
}

// cellLayout keeps the journal and every other truth file in the cell's state
// directory: <dir>/.cell/transcript.jsonl, .cell/state.json and so on. A folder is a cell when it has that directory,
// which internal/cell creates before anything is written.
type cellLayout struct{}

const (
	cellStateDir = ".cell"
	// placeMetaTruth holds the fields of meta.json no transcript can restate.
	placeMetaTruth = "session.json"
)

// truthNames are the files, and truthTrees the directories, a legacy folder
// carries into .cell/ when it migrates (session.TruthCarriers). The node
// journals are truth: the checkpoint names them, and a chat moved without them
// keeps its task list but loses what each task said and did. The artifacts are
// truth for the same reason: a chat moved without them keeps the rows a person
// finds its deliverables by and loses the deliverables.
var (
	truthNames = []string{placeState, placeTasks, placeTeamCursors, placeMemories}
	truthTrees = []string{placeNodeJournals, placeArtifacts}
)

func (cellLayout) holds(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, cellStateDir))
	return err == nil && info.IsDir()
}

func (cellLayout) transcript(dir string) string {
	return filepath.Join(dir, cellStateDir, placeTranscript)
}

func (c cellLayout) truth(dir, name string) string {
	return filepath.Join(dir, cellStateDir, name)
}

func (c cellLayout) metaTruth(dir string) string { return c.truth(dir, placeMetaTruth) }

func (cellLayout) folder(transcript string) (string, bool) {
	state := filepath.Dir(transcript)
	if filepath.Base(transcript) != placeTranscript || filepath.Base(state) != cellStateDir {
		return "", false
	}
	return filepath.Dir(state), true
}

// FolderOf is the session folder a transcript path belongs to, and false for
// a path that is not a folder's transcript (a flat legacy file).
func FolderOf(transcript string) (string, bool) {
	for _, l := range layouts {
		if dir, ok := l.folder(transcript); ok {
			return dir, true
		}
	}
	return "", false
}

// BucketOf is the directory holding the session folder a transcript belongs to,
// or the transcript's own directory for a flat file.
func BucketOf(transcript string) string {
	if dir, ok := FolderOf(transcript); ok {
		return filepath.Dir(dir)
	}
	return filepath.Dir(transcript)
}

// DirOf is the folder a transcript belongs to; a flat file's own directory
// stands in for it, exactly as the legacy layout always read it.
func DirOf(transcript string) string {
	if dir, ok := FolderOf(transcript); ok {
		return dir
	}
	return filepath.Dir(transcript)
}

// truthPath is where the folder dir keeps the truth file called name.
func truthPath(dir, name string) string { return layoutOf(dir).truth(dir, name) }
