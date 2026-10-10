package desktopbridge

// WHAT POST /sessions ASKS FOR (Places Architecture §6d, §9b, §9e).
//
// A window starts a chat by naming a place, never a folder. THE REQUEST HAS NO
// PATH FIELD AND NEVER WILL: the decoder rejects unknown fields, so a body that
// smuggles `workspace` or `path` is a 400 before anything is opened, and the
// only folder an engine can be born in is one the place itself lists, re-checked
// against the source policy at the moment of opening (workingfolder.go).
//
// A SAVED CONVERSATION'S WORKSPACE IS FIXED AT BIRTH. A request that names a
// sessionFile reopens on the folder its own record names, so a place id beside
// one is refused rather than ignored (the handler answers 400).

import (
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

// OpenRequest is the decoded body of POST /sessions.
type OpenRequest struct {
	// SessionFile reopens that saved conversation.
	SessionFile string `json:"sessionFile"`
	// PlaceID starts a NEW conversation in that place.
	PlaceID string `json:"placeId"`
}

// referOtherFolders hands every folder or repo of the chat's resolved bundle
// other than the one it works in to the engine as a referred place, as if the
// person had named it (session.PlaceSaid). They are referred, never moved into:
// the chat's workspace stays the first folder. It is best effort by design — a
// folder the engine will not take is skipped, since the chat is already open
// and filed — and an agent without the door (a remote engine) refers nothing.
func referOtherFolders(agent Agent, snap *placegraph.Snapshot, chatID, working string, pol placegraph.SourcePolicy) {
	door, ok := agent.(interface {
		ReferPlace(string, session.PlaceArrival) (session.PlaceRef, error)
	})
	if !ok || snap == nil {
		return
	}
	bundle := snap.Resolve(chatID, placegraph.ResolveOptions{Sources: pol})
	for _, src := range bundle.Sources {
		if (src.Kind != placegraph.SourceFolder && src.Kind != placegraph.SourceRepo) || src.Status != placegraph.SourceOK {
			continue
		}
		if sameFolder(working, src.Ref) || filepath.Clean(src.Ref) == filepath.Clean(working) {
			continue
		}
		_, _ = door.ReferPlace(src.Ref, session.PlaceSaid)
	}
}

// referRestOfPlace reads the graph after the chat was filed and refers the rest.
func (p *Places) referRestOfPlace(conn Connection, folder *WorkingFolder) {
	if folder == nil || folder.From != "place" {
		return
	}
	snap, err := p.Store.Snapshot()
	if err != nil {
		return
	}
	referOtherFolders(conn.Agent, snap, ChatIDFromSessionFile(conn.Welcome.SessionFile), folder.Path, p.sourcePolicy())
}
