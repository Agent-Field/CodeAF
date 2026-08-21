package main

// chatv3_exchange.go is the door's half of `ask here` — home's second action
// row, which answers an errand ("remind me at 6", "tell me when CI goes red")
// without opening a conversation the list would then have to hold forever
// (tui3's homeexchange.go, docs/AMBIENT.md Part 5).
//
// ───────────────────────────────────────────────────────────────────────────
// MERGE NOTE (lane errand). Everything in this file is new, and the only line
// this lane added outside it is the `Errand:` / `StandingRoot:` pair in
// chatv3.go's tui3.Options literal. If that literal has moved under another
// lane, the pair is the whole of what has to be carried across.
// ───────────────────────────────────────────────────────────────────────────
//
// IT IS THE /new DOOR WITH THE FOLDER TAKEN OFF IT. [v3NextSession] mints a
// session folder in THIS project's bucket and hands back where it put it, which
// is exactly the thing an errand must not have: a folder under v3/projects is a
// row on home, and asking from home exists so that an errand is not one. So the
// surface names the folder — under the standing root, where nothing scans — and
// this points the same config at it. The model, the roles, the rail and the
// gate are all properties of the LAUNCH and are inherited unchanged; the gate is
// re-read AS IT STANDS NOW for the reason chatv3_approval.go gives.

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// v3Errand is [tui3.Options.Errand]: one agent, writing into a folder the
// surface already made, working in the project the errand belongs to.
//
// THE WORKSPACE IS THE CALLER'S AND NOT THE LAUNCH'S, which is the one way this
// differs from Fresh. An errand asked with the cursor on another project is
// about THAT project — a CI watch belongs to the repository it watches — and the
// launch's own workspace is only the fallback for a caller that named none.
// Nothing else about the session moves with it: the approval gate is still this
// launch's, re-read now, because a window's rules are the person's rules
// wherever the sentence points.
func v3Errand(cfg session.Config, workspace, profileDir string, yolo bool) func(string, string) (tui3.Agent, error) {
	return func(dir, ws string) (tui3.Agent, error) {
		if strings.TrimSpace(dir) == "" {
			return nil, fmt.Errorf("an errand needs a folder to write into")
		}
		if strings.TrimSpace(ws) == "" {
			ws = workspace
		}
		fresh := v3CurrentGate(cfg, ws, profileDir, yolo)
		fresh.Workspace = ws
		// A BORROWED PLACE ON A FOLDER NOBODY BORROWED IT FOR. The exchange is
		// not owned — it has no work/ of its own and litters nothing, because it
		// works in the project the sentence was about — so it takes the ordinary
		// borrowed shape, and [v3PointAt] fills in SessionFile from it. Every
		// sidecar a session keeps then lands inside the exchange's folder, which
		// is what makes the folder the whole record and a move of it a move of
		// everything (place.go, Decision 26).
		fresh, err := v3PointAt(fresh, v3PlaceFor(dir, ws, false))
		if err != nil {
			return nil, err
		}
		return v3OpenSession(fresh)
	}
}
