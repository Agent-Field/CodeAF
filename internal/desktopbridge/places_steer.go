package desktopbridge

import (
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

func postSteer(p *Places, w http.ResponseWriter, r *http.Request, _ string) {
	if !needPost(w, r) {
		return
	}
	org := p.organizer()
	if org == nil || org.steer == nil {
		fail(w, 404, "unknown engine action")
		return
	}
	var ask struct {
		Scope    string `json:"scope"`
		Sentence string `json:"sentence"`
	}
	if !readBody(w, r, &ask) {
		return
	}
	if strings.TrimSpace(ask.Scope) == "" {
		failPlaces(w, 400, "invalid", "Say which place to organize.")
		return
	}
	if running, id := org.runningNow(); running {
		writeStatus(w, 409, map[string]any{
			"error": "Places are already being organized.",
			"code":  "running",
			"run":   id,
		})
		return
	}
	tree, err := p.advice.existingTree()
	if err != nil {
		failPlaces(w, 500, "store", "Places could not be read.")
		return
	}
	if _, ok := tree.Node(ask.Scope); !ok {
		failPlaces(w, 404, "unknown_place", "That place isn't in the list.")
		return
	}
	parsed, _, err := org.steer.Parse(r.Context(), tree, ask.Sentence, p.advice.judge())
	if err != nil {
		failPlaces(w, 500, "store", "That place could not be organized.")
		return
	}
	// The model cannot choose the place. Scope is the one the person is on.
	parsed.Scope = ask.Scope
	reply := steerWire{Steer: parsed, Understood: parsed.Action != "" && parsed.Action != placeorg.SteerNone}
	if !reply.Understood {
		write(w, reply)
		return
	}
	ops, err := org.steer.Render(r.Context(), tree, p.advice.corpus(), parsed, p.advice.judge())
	if err != nil {
		failPlaces(w, 500, "store", "That place could not be organized.")
		return
	}
	snap, _ := p.advice.snapshot()
	var rows []heldRow
	for _, op := range ops {
		if op.Kind == placeorg.OpLens || (op.Kind == placeorg.OpAxis && !op.Proposed) {
			org.remember(op)
			continue
		}
		if row, ok := expandOp(op, snap); ok {
			rows = append(rows, row)
		}
	}
	if org.State != nil && org.statePath != "" {
		_ = org.State.Save(org.statePath)
	}
	if len(rows) == 0 {
		write(w, reply)
		return
	}
	org.mu.Lock()
	if org.held == nil {
		org.held = &heldPlan{ID: newPlanID(), Summary: rows[0].Wire.Why, Rows: rows}
	} else {
		org.held.Rows = append(org.held.Rows, rows...)
	}
	reply.Plan = org.held.wire()
	org.saveReview()
	org.mu.Unlock()
	write(w, reply)
}

// steerWire is the architecture's reply: the steer, whether it was understood,
// and a plan when the sentence proposed a change to the tree. understood sits
// on this object because the desktop reads it there.
type steerWire struct {
	Steer      placeorg.Steer `json:"steer"`
	Understood bool           `json:"understood"`
	Plan       *planWire      `json:"plan,omitempty"`
}
