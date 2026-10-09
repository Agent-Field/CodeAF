package desktopbridge

// GET /places/{id}/effective-model — which model the FIRST message typed on that
// place's Home would run on, as far as the places can say.
//
// It is the engine's own rule, not a second one: [placegraph.Snapshot.StartPolicy]
// resolves the model field for a chat imagined as filed in {id} (nearest common
// ancestor, needs-a-pick and all), and [session.DecidePlaceSettings] turns that
// decision into what the engine will do with it when the new chat opens. So the
// caption can never name a model the engine would not apply, or miss one it would.
//
// READ-ONLY AND CHEAP. It reads the graph once, writes nothing, opens no session
// and calls no model. It does NOT say what the Conversation role is: when no place
// decides, the answer is "none" and the screen shows the saved role, which it
// already reads for itself.

import (
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Effective-model states. "applies" is the only one that overrides the role.
const (
	// EffectiveModelNone: no place in the context has a model opinion.
	EffectiveModelNone = "none"
	// EffectiveModelApplies: the new chat will start on Model, set by DecidedBy.
	EffectiveModelApplies = "applies"
	// EffectiveModelNeedsPick: places disagree and none above them decides, so
	// NOTHING is applied and the chat starts on the role's model and asks.
	EffectiveModelNeedsPick = "needsPick"
	// EffectiveModelUnavailable: a place has an opinion this engine will not apply.
	EffectiveModelUnavailable = "unavailable"
)

type effectiveModelPlace struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Model string `json:"model,omitempty"`
}

type effectiveModelAnswer struct {
	PlaceID  string `json:"placeId"`
	Revision uint64 `json:"revision"`
	State    string `json:"state"`
	// Model is the model the place decides, only when State is "applies".
	Model string `json:"model,omitempty"`
	// DecidedBy is the place whose value it is, only when State is "applies".
	DecidedBy *effectiveModelPlace `json:"decidedBy,omitempty"`
	// Outcome is the resolver's word: agreed or decided.
	Outcome string `json:"outcome,omitempty"`
	// Wanted lists the places with an opinion, nearest first, when a pick is needed.
	Wanted []effectiveModelPlace `json:"wanted,omitempty"`
	Reason string                `json:"reason,omitempty"`
}

// reservedPlaceWords are single-segment /places routes; an id spelled like one
// is never a place.
var reservedPlaceWords = map[string]bool{
	"status": true, "rail": true, "undo": true, "stale": true, "policy": true, "proposals": true,
	placegraph.RootID: true, placegraph.NowID: true,
}

func (p *Places) effectiveModel(w http.ResponseWriter, id string) {
	if reservedPlaceWords[id] {
		failPlaces(w, 400, "reserved", "All places and Now are built in; they have no model of their own.")
		return
	}
	snap, err := p.Store.Snapshot()
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	if _, ok := snap.Place(id); !ok {
		status, code, sentence := storeFailure(placegraph.ErrNotFound, "")
		failPlaces(w, status, code, sentence)
		return
	}
	out := effectiveModelAnswer{PlaceID: id, Revision: snap.Revision, State: EffectiveModelNone}
	decision, ok := snap.StartPolicy(id, placegraph.PolicyModel)
	if !ok {
		write(w, out)
		return
	}
	named := func(placeID, value string) effectiveModelPlace {
		name := placeName(snap, placeID)
		return effectiveModelPlace{ID: placeID, Name: strings.Trim(name, "“”"), Model: value}
	}
	p.mu.Lock()
	door := p.door
	p.mu.Unlock()
	settings := session.DecidePlaceSettings(&placegraph.Bundle{Policy: []placegraph.PolicyDecision{decision}},
		session.PlaceSettingFacts{Door: door != nil, Fresh: true})
	if len(settings) != 1 {
		write(w, out)
		return
	}
	s := settings[0]
	out.Reason = s.Reason
	switch s.State {
	case session.PlaceSettingPending, session.PlaceSettingApplied:
		out.State, out.Model, out.Outcome = EffectiveModelApplies, s.Value, string(decision.Outcome)
		by := named(s.DecidedBy, s.Value)
		out.DecidedBy = &by
		out.Reason = ""
	case session.PlaceSettingNeedsPick:
		out.State = EffectiveModelNeedsPick
		for _, want := range decision.Wanted {
			out.Wanted = append(out.Wanted, named(want.PlaceID, want.Value))
		}
	default:
		out.State = EffectiveModelUnavailable
	}
	write(w, out)
}
