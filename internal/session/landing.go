package session

// landing.go answers "where does this file go" for everything a session puts on
// disk that is not the journal itself — the droppings it makes for its own use,
// and the deliverables it makes for the person.
//
// It exists as one file because it is one decision. place.go says what a
// session folder CONTAINS; this says which of those directories each writer
// reaches for, and it says it once so that jobs.go, stub.go,
// tools_image.go and internal/remote cannot drift into five answers.
//
// ── DROPPINGS GO IN THE FOLDER, DELIVERABLES GO WHERE THE PERSON WILL LOOK ──
//
// A dropping — a job log, a stubbed tool result — is
// re-creatable, carries the sweep's 7-day TTL, and is never something anybody
// asked for. It belongs beside the transcript that explains what it was for, so
// that deleting a session is removing one folder (docs/CHAT-V3.md, Decision 26)
// and so that NOTHING OF OURS LIVES IN THE PERSON'S FOLDER.
//
// A deliverable is the opposite claim: somebody may want it back. An OWNED
// session's workspace is already ours to fill, so a picture lands in it like
// any other file the person's work produced. A BORROWED session's workspace is
// somebody's repository, so a picture lands in the session's own artifacts/
// instead — and the row in the global index (artifacts.go) is how it is found
// again, since nothing about the path is memorable.
//
// ── THE ZERO PLACE IS THE LEGACY LAYOUT ──
//
// Every answer below falls back to <workspace>/.aforge-v3/<kind>, exactly where
// a flat-layout session has always written, so the folder lands seam-first: a
// caller that has not adopted a Place keeps the behavior it had.

import (
	"path/filepath"
	"strings"
)

// droppingsLegacyDir is the flat layout's dot directory under the workspace. It
// is on the aforge scheme and stays there until the one late rename (Decision
// 26, "One home, one seam, one late rename").
const droppingsLegacyDir = ".aforge-v3"

// The kinds of dropping. They name a subdirectory of [Place.Logs] and, under
// the legacy layout, a subdirectory of [droppingsLegacyDir] — one word, both
// places, so a person who learned where job logs live in one layout can find
// them in the other.
const (
	droppingJobs  = "jobs"
	droppingStubs = "stubs"
)

// droppingsDir names the directory one kind of dropping lands in.
func droppingsDir(place Place, workspace, kind string) string {
	if logs := place.Logs(); logs != "" {
		return filepath.Join(logs, kind)
	}
	return filepath.Join(workspace, droppingsLegacyDir, kind)
}

// ImagesDir is where a picture lands when nobody named a path: the workspace
// itself for an owned session, the session's artifacts/ for a borrowed one, and
// the legacy dot directory for a session with no folder at all.
//
// It is EXPORTED because the engine writes pictures too (internal/remote's
// image.go) and a picture that landed in two different places depending on
// which machine received it would be a picture the journal's reference cannot
// describe in one sentence.
func ImagesDir(place Place, workspace string) string {
	return deliverablesDir(place, workspace, imageDirectory)
}

// AudioDir is where a spoken file lands when nobody named a path, and VideoDir
// is where a rendered one does. They are [ImagesDir] with a different legacy
// leaf and NOTHING else, because the law is about the kind of thing the file is
// and not about its format: a deliverable is a deliverable whether it is looked
// at, listened to, or watched (tools_speak.go, tools_video.go).
func AudioDir(place Place, workspace string) string {
	return deliverablesDir(place, workspace, audioDirectory)
}

func VideoDir(place Place, workspace string) string {
	return deliverablesDir(place, workspace, videoDirectory)
}

// deliverablesDir is the one ladder all three climb: the owned workspace, then
// the borrowed session's own artifacts/, then the legacy dot directory. It is
// one function rather than three copies for the reason this whole file exists —
// three copies of a three-rung ladder is three chances for a rung to move.
func deliverablesDir(place Place, workspace, legacy string) string {
	if work := place.Work(); work != "" {
		return work
	}
	if artifacts := place.Artifacts(); artifacts != "" {
		return artifacts
	}
	return filepath.Join(workspace, filepath.FromSlash(legacy))
}

// TranscriptName is what the journal is called inside a session folder. It is
// place.go's constant under an exported name, for the surfaces that have a path
// and no Place: a transcript by this name is one a session folder holds, and
// its directory is therefore the session's (see [PlaceSession]).
const TranscriptName = placeTranscript

// PlaceSession is the session id a folder names. A session folder is named for
// its session (Decision 26, and [Meta.ID] says the same), so the id is the
// folder's own name and no file has to be opened to learn it.
//
// A session with no folder answers NOTHING rather than a guess made out of a
// file name — an artifact row's session is a citation, and a citation nobody
// can verify is worse than a row without one (design-law §EMPTINESS).
func PlaceSession(place Place) string {
	if strings.TrimSpace(place.Dir) == "" {
		return ""
	}
	return filepath.Base(place.Dir)
}
