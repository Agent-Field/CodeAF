package placegraph

// THE FOLDER A NEW CHAT WORKS IN (Places Architecture Q-P9: "the place's first
// folder source; the rest are referred; with no folder source, the launch
// workspace").
//
// A place lists sources, and a chat is told about all of them. One of them may
// also be where the chat's tools run: this file picks it, from the place's OWN
// listing and in the order the person listed it — never from an ancestor's, since
// working in a folder a parent place happens to list would be a guess about
// which of several unrelated places the person meant.
//
// IT CHECKS EVERY CANDIDATE WITH THE SAME POLICY THE PROMPT IS BUILT WITH
// ([SourcePolicy.inspect]) AND THEN ASKS THE DISK FOR WHAT A WORKING DIRECTORY
// NEEDS: the path is stored resolved, but a disk comes and goes, a folder can be
// replaced by a link, and a mode can change after the source was added. A source
// that no longer qualifies is skipped WITH ITS REASON, so a surface can say why
// the chat is working somewhere else instead of silently looking at the next one.
//
// IT OPENS NOTHING AND CHANGES NOTHING. It stats; the caller moves the engine.

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// WorkingFolder is the answer: the canonical directory, or why none was usable.
type WorkingFolder struct {
	// Path is the resolved (symlinks followed), absolute, existing directory; ""
	// when no folder source of the place qualifies.
	Path     string
	SourceID string
	Label    string
	// Skipped are folder sources listed BEFORE the chosen one (or all of them, when none was chosen)
	// that could not be used, each with a sentence that fits after "skipped":.
	Skipped []SkippedFolder
}

// SkippedFolder is one listed folder that was passed over.
type SkippedFolder struct {
	SourceID string `json:"sourceId"`
	Ref      string `json:"ref"`
	Reason   string `json:"reason"`
}

// Listed reports whether the place has any folder or repo source at all, usable or not. A place with
// none has nothing to say about where its chats work, and a surface says nothing.
func (w WorkingFolder) Listed() bool { return w.Path != "" || len(w.Skipped) > 0 }

// WorkingFolder picks the working directory for a new chat filed in placeID. An unknown place
// answers the zero value.
func (s *Snapshot) WorkingFolder(placeID string, pol SourcePolicy) WorkingFolder {
	place, ok := s.Place(placeID)
	if !ok {
		return WorkingFolder{}
	}
	var out WorkingFolder
	for _, src := range place.Context.Sources {
		if src.Kind != SourceFolder && src.Kind != SourceRepo {
			continue
		}
		path, reason := usableWorkingDir(src, pol)
		if reason != "" {
			out.Skipped = append(out.Skipped, SkippedFolder{SourceID: src.ID, Ref: src.Ref, Reason: reason})
			continue
		}
		out.Path, out.SourceID, out.Label = path, src.ID, strings.TrimSpace(src.Label)
		return out
	}
	return out
}

// UsableDirectory is the canonical directory a chat may be opened in.
// ref comes from a record this program stored — a place source, or the
// workspace a session's own meta.json names — never from a request body.
// A reason means it cannot be a working directory.
func UsableDirectory(ref string, pol SourcePolicy) (string, string) {
	return usableWorkingDir(Source{Kind: SourceFolder, Ref: ref}, pol)
}

// usableWorkingDir answers the canonical directory for a source, or the reason it cannot be one.
func usableWorkingDir(src Source, pol SourcePolicy) (string, string) {
	u := UsedSource{Kind: src.Kind, Ref: canonicalRef(src.Kind, src.Ref)}
	pol.inspect("", &u)
	switch u.Status {
	case SourceRefusedStatus:
		return "", u.Reason
	case SourceOK:
	default:
		return "", u.Reason
	}
	real, err := filepath.EvalSymlinks(u.Ref)
	if err != nil {
		return "", "not on this disk right now"
	}
	if real == filepath.Dir(real) {
		return "", "the top of a disk is not a working folder"
	}
	// A working directory has to be entered and listed. Open needs read on the directory and a lookup
	// of "." inside it needs search; both are asked of the disk, not guessed from mode bits, because
	// ACLs and mounts can say no to a mode that looks open.
	if _, err := os.Stat(real + string(os.PathSeparator) + "."); err != nil {
		return "", "this account cannot enter that folder"
	}
	dir, err := os.Open(real)
	if err != nil {
		return "", "this account cannot read that folder"
	}
	_, readErr := dir.Readdirnames(1)
	dir.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return "", "this account cannot read that folder"
	}
	return real, ""
}
