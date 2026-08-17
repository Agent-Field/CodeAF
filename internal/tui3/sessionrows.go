package tui3

import (
	"path/filepath"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE NAME A SESSION FALLS BACK TO, NOW THAT A SESSION IS A FOLDER.
//
// Both lists of past conversations on this surface climb the same ladder down
// from a name to a machine's idea of one — the title the session gave itself,
// then what the person opened with, then the transcript's own file name
// (resume.go's [humanName], welcome.go's [welcome.recentRow]). The last rung
// was written when a transcript was a file with a name of its own:
//
//	20260816-150405_a3f2.jsonl
//
// Under Decision 26 it is `<session-id>/transcript.jsonl`, and every unnamed
// session in the list would fall back to the same word — "transcript" — which
// is a rung that has stopped identifying anything. So the fallback asks WHICH
// SHAPE the path is in and answers with the part of it that is the session's
// own: the folder for a session that is one, and exactly today's answer for a
// flat file.
//
// THE QUESTION IS ASKED OF THE CONTRACT AND NOT OF A SPELLING. A path is a
// session folder's transcript precisely when it is the transcript [Place] would
// name for the directory it sits in, so this file holds no copy of that name
// (internal/session's place.go owns it) and a rename there is a rename
// everywhere.

// sessionStem is the transcript's own name for the purpose of naming a session
// that named itself nothing. It answers the file's base name in the flat
// layout — the unchanged rung, extension and all, which is what each caller
// already trims or draws — and the folder's name in the new one.
func sessionStem(file string) string {
	if file == "" {
		return baseName(file)
	}
	dir := filepath.Dir(file)
	if file != (session.Place{Dir: dir}).Transcript() {
		return baseName(file)
	}
	// The folder is the session's id, which is what the flat layout's file name
	// carried too. A transcript sitting directly in a bucket directory with no
	// folder of its own has nothing better to offer than its own name.
	if name := filepath.Base(dir); name != "." && name != string(filepath.Separator) {
		return name
	}
	return baseName(file)
}
