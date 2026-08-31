package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE FILES A CONVERSATION LEFT BEHIND ────────────────────────────────────
//
// ONE READING OF THE INDEX SERVES EVERY CARD ON THE SCREEN, and that is the
// whole architecture of this file. The deliverables index is ONE file for the
// whole machine — every file every conversation ever made, appended — and a card
// wants the handful of rows carrying its own session id. This band used to read
// and parse the WHOLE index inside its own draw, cached per conversation and
// expiring on home's beat, which made two costs nobody could see:
//
//   - EVERY ARRIVAL ON A ROW paid for the whole file, because the cursor moving
//     to a row is a cache key nobody has read yet. Measured on a real machine
//     with a 900KB index: 10ms per keystroke and per hovered row, against 0.16ms
//     for the whole rest of the frame — sixty times the cost of everything else
//     on the screen put together, spent walking rows that belong to other
//     conversations.
//   - AND SO DID EVERY THIRD SECOND OF SITTING STILL, because the per-row entry
//     expired on home's refresh clock, so a screen nobody was touching re-parsed
//     a megabyte of JSON forever.
//
// A cache keyed by the SUBJECT over a reading that is GLOBAL is the shape of
// that bug: it multiplies one file read by the number of rows a person walks
// past. So the reading is taken once, filed by the conversation that made each
// file, and every card is a map lookup — which is the bargain
// homestanding.go's [app.standWeek] already makes for the week's ledger, in the
// same words.
//
// AND IT IS RE-PARSED ONLY WHEN THE FILE CHANGED. Home's beat asks for the
// reading three times a minute and gets one os.Stat for its trouble; the JSON is
// walked again only when a conversation has actually written a file since. So a
// resting screen costs nothing at all, and a file made a second ago still
// appears on home's own beat like every other arrival.

// homeArtifactIndex is that one reading: the machine's deliverables index, filed
// by the conversation that made each file, and the stat it was taken at so a
// beat can tell whether anything has happened since.
type homeArtifactIndex struct {
	path string
	size int64
	mod  time.Time
	// world is the world reading the rows came in with, which is what turns the
	// reading over on a HOSTED surface: the far machine's index cannot be
	// statted from here, so its rows ride with the world and the reading is as
	// old as the world is ([session.World.Artifacts]).
	world time.Time
	// by is the rows, by session id, newest first within each — which is the
	// order [session.ReadArtifacts] hands them over in and the order the band
	// draws them in.
	by map[string][]session.Artifact
}

// readArtifactIndex is the reading itself, behind a name so a test can count
// how often it is taken — which is the whole claim this file makes and the one
// thing a test of "read once" cannot see any other way (homeband_repo.go's
// [homeGitStatus] is the same door for the same reason).
var readArtifactIndex = session.ReadArtifacts

func init() {
	registerHomeBand(homeBand{name: "deliverables", order: bandOrderDeliverable, draw: drawDeliverablesBand})
}

// readHomeArtifacts takes the reading. IT IS CALLED FROM `open` AND FROM THE
// BEAT and never from a draw (home.go's [app.openHome], [app.refreshHome]),
// which is ARCHITECTURE.md's fourth law: `open` and `tick` may read the disk and
// `body` may not.
func (a *app) readHomeArtifacts() {
	index := &a.home.artifacts
	if a.hosted() {
		// THE INDEX BELONGS TO THE MACHINE THAT MADE THE FILES. A hosted surface
		// receives those rows with the cached world; reading the path below would
		// read THIS machine's index and file unrelated rows under far
		// conversation ids.
		if index.by != nil && index.world.Equal(a.home.world.Read) {
			return
		}
		index.world, index.by = a.home.world.Read, artifactsBySession(a.home.world.Artifacts)
		return
	}
	path := a.artifactsIndex()
	info, err := os.Stat(path)
	if err != nil {
		// No index is an empty reading and not an unread one, or every draw
		// would go back to the disk to be told the same thing again.
		if index.by == nil || index.path != path {
			*index = homeArtifactIndex{path: path, by: map[string][]session.Artifact{}}
		}
		return
	}
	if index.by != nil && index.path == path && index.size == info.Size() && index.mod.Equal(info.ModTime()) {
		return
	}
	*index = homeArtifactIndex{
		path: path, size: info.Size(), mod: info.ModTime(),
		by: artifactsBySession(readArtifactIndex(path)),
	}
}

// artifactsBySession files a flat index under the conversations that wrote it.
// A row naming no conversation belongs to no card and is dropped here rather
// than skipped in every reader.
func artifactsBySession(rows []session.Artifact) map[string][]session.Artifact {
	by := make(map[string][]session.Artifact, len(rows))
	for _, row := range rows {
		id := strings.TrimSpace(row.Session)
		if id == "" {
			continue
		}
		by[id] = append(by[id], row)
	}
	return by
}

// homeArtifactsOf is what one card's session made, out of the reading.
//
// IT TAKES THE READING WHEN THERE HAS NEVER BEEN ONE, which is the single door
// left open into the disk from a draw and it is deliberate: a card can be asked
// for before home has opened — the ambient band tests do exactly that, and so
// would any surface that grows a card of its own — and a band that answered
// `nothing` until the next beat would be drawing a conversation's files three
// seconds after everything else on the card. After that first reading this is a
// map lookup, on every frame, forever.
func (a *app) homeArtifactsOf(sessionID string) []session.Artifact {
	if a.home.artifacts.by == nil {
		a.readHomeArtifacts()
	}
	return a.home.artifacts.by[strings.TrimSpace(sessionID)]
}

func drawDeliverablesBand(a *app, ctx bandContext) []string {
	rows := a.homeArtifactsOf(ctx.subject.row.ID)
	if len(rows) == 0 {
		return nil
	}
	drawn := make([][]string, 0, len(rows))
	for _, artifact := range rows {
		name := filepath.Base(strings.TrimSpace(artifact.Path))
		labelInk := func(text string) string { return ctx.pal.muted(a.pathLink(artifact.Path, text)) }
		drawn = append(drawn, bandSidesWithSeparator(ctx.width, 2, 8, "· ", name,
			sinceAt(artifact.Created, ctx.now), labelInk, ctx.pal.dim))
	}
	return a.bandFoldPacked(ctx, "deliverables", drawn, 3, "files")
}
