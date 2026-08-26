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
func v3Errand(cfg session.Config, workspace, profileDir string, yolo bool) func(tui3.ErrandOrders) (tui3.Agent, error) {
	return func(orders tui3.ErrandOrders) (tui3.Agent, error) {
		dir := orders.Dir
		if strings.TrimSpace(dir) == "" {
			return nil, fmt.Errorf("an errand needs a folder to write into")
		}
		ws := orders.Workspace
		if strings.TrimSpace(ws) == "" {
			ws = workspace
		}
		fresh := v3CurrentGate(cfg, ws, profileDir, yolo)
		fresh.Workspace = ws
		// ── THE TWO FACTS THE COMPOSER LAYER SETTLED, HONOURED HERE ──
		//
		// The model is the EXECUTION slot's — what the work this errand hands out
		// runs on (config's ModelSlotFor("work"), whose prefs field is task_model)
		// — so it is Config.TaskModel and not Config.Model: the errand still talks
		// on the launch's own model, and only the work it starts moves. Empty
		// keeps the launch's binding, which is what every door but the layer sends.
		if model := strings.TrimSpace(orders.Model); model != "" {
			fresh.TaskModel = model
		}
		// And the cap is the session's own spend rail (internal/session's rail.go):
		// it stops the errand's next turn once its journaled spend reaches the
		// figure, and it holds every adaptive run the errand starts to a tank no
		// bigger than itself ([Agent.railCap]). Zero is the launch's own rail,
		// untouched.
		if orders.CapUSD > 0 {
			fresh.SpendRailUSD = orders.CapUSD
		}
		// ── lane time ── AND IT SAYS WHAT IT IS. An exchange is a pane that
		// closes with home, not a room somebody sits in, so it is never a
		// steering target for a standing item that fires (internal/session's
		// standing_run.go). Its own card still ratifies: that goes through the
		// agent the surface is holding and not through the live registry.
		fresh.Errand = true
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
