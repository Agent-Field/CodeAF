package session

// WHAT A PLACE'S DEFAULT MODEL AND PERMISSIONS DO TO A CONVERSATION.
//
// A desktop place can say which model its conversations run on and what runs
// in them without asking (placegraph.Policy). placegraphcontext.go resolves
// those through the one resolver — agreed, decided by the nearest common
// ancestor, chosen by the person once, or waiting for that pick — and this file
// is the half that makes the answer HAPPEN, through the same doors a person's
// own pick goes through: the session's model swap ([Agent.setModelLocked], the
// body of [Agent.SetModel]) and the gate's builder ([ApprovalGate.Build], what
// [Agent.SetApprovalPosture] calls).
//
// ── THE RULES, EACH OF THEM A REFUSAL TO GUESS ──────────────────────────────
//
// A PLACE'S DEFAULTS ARE FOR NEW CONVERSATIONS. They are first applied at the
// opening of the first turn of a conversation the person has said nothing in
// yet. A conversation that was already running when it was filed keeps the
// model and posture it has; the Using list says so (notNew), and the person can
// take the place's value with one press — which is then their own pick.
//
// A CONVERSATION THAT FOLLOWS ITS PLACES KEEPS FOLLOWING THEM, at the NEXT turn's
// opening and never inside one: a place whose model changes while a turn runs
// moves the conversation when the person next speaks, and a step already on the
// wire finishes on the model it started with.
//
// THE PERSON'S OWN PICK WINS, AND STAYS WON. Choosing a model or a posture inside
// the conversation marks that field the person's (PlaceDefaults.ModelYours /
// PermissionsYours), persisted, and no place moves it again.
//
// A PLACE NEVER WIDENS THE GATE BY ITSELF. A posture that would let more run
// without asking than the conversation does right now (deny < ask < guardian <
// allow, `auto` read as what the settings rows stand at) is HELD, not applied —
// the Using list says it needs you, and only the person's own act opens it. The
// reason is that filing is not always the person's act: the desktop's AI offers
// to file conversations, and a filing must never be a way to switch approvals
// off. A narrower posture is applied, because asking more is never a surprise
// anybody pays for.
//
// A CONFLICT APPLIES NOTHING. needsPick leaves the conversation exactly where it
// is until the person picks a place; the pick is remembered for this
// conversation alone (placegraph.ChoiceBook) and applied at the next turn.
//
// A VALUE THAT CANNOT WORK IS NOT APPLIED. A posture word codeaf does not have,
// or a session with no dial onto its gate, is reported unavailable rather than
// pretended; a model is applied as named, because the bridge checked it against
// the model list when the person set it and the engine has no cheaper list.
//
// Every change this file makes posts one aside into the conversation, as the
// place lines do, so the person and the model both see why the model moved.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// PlaceChoicesFile is the remembered-picks file's name, beside the graph.
const PlaceChoicesFile = "place-choices.json"

// PlaceGraphDoorFor is the ONE way a door onto the place graph is built, by the
// engine for its conversations and by the desktop bridge for its Using list, so
// the two read the same graph, the same picks and the same source policy. The
// path must be absolute: a relative one would be resolved against wherever each
// process happens to stand, and two processes would read two files.
func PlaceGraphDoorFor(graphPath string) (*PlaceGraphDoor, error) {
	path := strings.TrimSpace(graphPath)
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("place graph %q: the path must be absolute", graphPath)
	}
	path = filepath.Clean(path)
	userHome, _ := os.UserHomeDir()
	return &PlaceGraphDoor{
		Path:        path,
		ChoicesPath: filepath.Join(filepath.Dir(path), PlaceChoicesFile),
		Sources:     placegraph.DefaultSourcePolicy(userHome, home.Dir()),
	}, nil
}

// PlaceDefaults is the record, kept in meta.json, of what this conversation's
// places set on it.
type PlaceDefaults struct {
	// Model is the model the places last set, and ModelBy the place that
	// decided it ("you" for a remembered pick between places).
	Model   string `json:"model,omitempty"`
	ModelBy string `json:"modelBy,omitempty"`
	// ModelFollows says this conversation takes its model from its places: it
	// was new when a place first had an opinion on it.
	ModelFollows bool `json:"modelFollows,omitempty"`
	// ModelYours says the person chose a model inside this conversation. It
	// wins over every place, for good.
	ModelYours bool `json:"modelYours,omitempty"`

	Permissions        string `json:"permissions,omitempty"`
	PermissionsBy      string `json:"permissionsBy,omitempty"`
	PermissionsFollows bool   `json:"permissionsFollows,omitempty"`
	PermissionsYours   bool   `json:"permissionsYours,omitempty"`
}

// PlaceSettingState is what a place's decided value amounts to for one
// conversation right now. Persisted nowhere; the wire carries the words.
type PlaceSettingState string

const (
	// PlaceSettingApplied: the conversation runs on the places' value.
	PlaceSettingApplied PlaceSettingState = "applied"
	// PlaceSettingPending: it will at the opening of the next turn.
	PlaceSettingPending PlaceSettingState = "pending"
	// PlaceSettingNeedsPick: places disagree and none above them decides.
	// Nothing is applied.
	PlaceSettingNeedsPick PlaceSettingState = "needsPick"
	// PlaceSettingNeedsYou: a posture wider than the conversation's own, held
	// until the person takes it themselves.
	PlaceSettingNeedsYou PlaceSettingState = "needsYou"
	// PlaceSettingYours: the person's own pick in this conversation stands.
	PlaceSettingYours PlaceSettingState = "yours"
	// PlaceSettingNotNew: the conversation was running before a place had an
	// opinion on this field, and keeps what it has.
	PlaceSettingNotNew PlaceSettingState = "notNew"
	// PlaceSettingUnavailable: nothing here can apply it (the reason says why).
	PlaceSettingUnavailable PlaceSettingState = "unavailable"
)

// PlaceSetting is one field's effective state.
type PlaceSetting struct {
	Field placegraph.PolicyField `json:"field"`
	State PlaceSettingState      `json:"state"`
	// Value is the places' decided value; "" when a pick is needed.
	Value string `json:"value,omitempty"`
	// Current is what the conversation runs on now: its model, or the posture
	// its gate stands at.
	Current string `json:"current,omitempty"`
	// DecidedBy is the place whose value it is (a pick's place for a
	// remembered pick), "" when nobody decided.
	DecidedBy string `json:"decidedBy,omitempty"`
	// Reason is a sentence a person can read.
	Reason string `json:"reason,omitempty"`
}

// PlaceSettingFacts is what [DecidePlaceSettings] needs to know about the
// conversation besides its places. The engine fills it from the live agent;
// the bridge fills it from the same meta.json and the wire's own reads.
type PlaceSettingFacts struct {
	// Door says this conversation's engine reads the place graph at all.
	Door bool
	// DoorReason is the sentence to give when Door is false.
	DoorReason string
	// Fresh says the person has not said anything in the conversation yet.
	Fresh  bool
	Record PlaceDefaults
	// Model is the model the conversation runs on now.
	Model string
	// Posture is the conversation's own stored posture word, "" for none.
	Posture string
	// Resolved is the posture its gate stands at now, whichever scope decided.
	Resolved string
	// Standing is what `auto` stands at: the settings rows as they are.
	Standing string
	// Dial says the conversation can move its own gate.
	Dial bool
}

// postureRank orders the postures by how much runs without asking. `auto` is
// not on it: it is read as what the settings rows stand at. -1 is unknown.
func postureRank(word string) int {
	switch word {
	case PostureDeny:
		return 0
	case PostureAsk:
		return 1
	case PostureGuardian:
		return 2
	case PostureAllow:
		return 3
	}
	return -1
}

// DecidePlaceSettings is the ONE rule for what a place's decided values do to a
// conversation: the engine applies the pending ones at a turn's opening, and
// the bridge's Using list shows all of them, so the list never says a value is
// applied that the engine would not apply.
func DecidePlaceSettings(b *placegraph.Bundle, f PlaceSettingFacts) []PlaceSetting {
	out := []PlaceSetting{}
	if b == nil {
		return out
	}
	for _, d := range b.Policy {
		s := PlaceSetting{Field: d.Field, Value: d.Value, DecidedBy: decider(d)}
		switch d.Field {
		case placegraph.PolicyModel:
			s.Current = f.Model
		case placegraph.PolicyPermissions:
			s.Current = f.Resolved
		}
		switch {
		case !f.Door:
			s.State, s.Reason = PlaceSettingUnavailable, f.DoorReason
			if s.Reason == "" {
				s.Reason = "This conversation's engine does not read your places, so their settings are not applied to it."
			}
		case d.Outcome == placegraph.PolicyNeedsPick:
			s.State = PlaceSettingNeedsPick
			s.Reason = "These places disagree and no place above them decides. Pick one; until then neither is applied."
		case d.Field == placegraph.PolicyModel:
			decideModel(&s, f)
		case d.Field == placegraph.PolicyPermissions:
			decidePermissions(&s, f)
		default:
			s.State, s.Reason = PlaceSettingUnavailable, "codeaf does not apply this setting."
		}
		out = append(out, s)
	}
	return out
}

func decider(d placegraph.PolicyDecision) string {
	if d.Outcome == placegraph.PolicyChosen {
		return d.Chosen
	}
	return d.DecidedBy
}

func decideModel(s *PlaceSetting, f PlaceSettingFacts) {
	r := f.Record
	following := f.Fresh || (r.ModelFollows && (r.Model == "" || f.Model == r.Model))
	switch {
	case r.ModelYours:
		s.State, s.Reason = PlaceSettingYours, "You chose this conversation's model yourself, so places do not change it."
	case f.Model == s.Value:
		s.State = PlaceSettingApplied
	case !following && r.ModelFollows:
		// It followed its places and somebody moved it by another road since.
		s.State, s.Reason = PlaceSettingYours, "This conversation's model was changed here, so places do not change it."
	case !following:
		s.State, s.Reason = PlaceSettingNotNew, "This conversation was already running when a place gave it a model, so it keeps the one it has."
	default:
		s.State, s.Reason = PlaceSettingPending, "Applies when the next turn starts."
	}
}

func decidePermissions(s *PlaceSetting, f PlaceSettingFacts) {
	r := f.Record
	want := s.Value
	effective := want
	if want == PostureAuto {
		effective = f.Standing
	}
	following := f.Fresh || (r.PermissionsFollows && (r.Permissions == "" || f.Posture == r.Permissions))
	switch {
	case !knownPosture(want):
		s.State, s.Reason = PlaceSettingUnavailable, fmt.Sprintf("%q is not a permissions setting codeaf has, so it is not applied.", want)
	case !f.Dial:
		s.State, s.Reason = PlaceSettingUnavailable, "This conversation has no control over what runs without asking, so the place's setting cannot be applied."
	case r.PermissionsYours:
		s.State, s.Reason = PlaceSettingYours, "You set what runs without asking in this conversation yourself, so places do not change it."
	case f.Posture == want:
		s.State = PlaceSettingApplied
	case !following && r.PermissionsFollows:
		s.State, s.Reason = PlaceSettingYours, "What runs without asking was changed in this conversation, so places do not change it."
	case !following:
		s.State, s.Reason = PlaceSettingNotNew, "This conversation was already running when a place set its permissions, so it keeps what it has."
	case postureRank(effective) < 0 || postureRank(f.Resolved) < 0 || postureRank(effective) > postureRank(f.Resolved):
		// WIDER, OR NOT KNOWABLE AS NARROWER: held. A posture whose reach
		// cannot be compared is treated as wider, never as safe.
		s.State, s.Reason = PlaceSettingNeedsYou, "This would let more run without asking than this conversation does now, so it waits for you to choose it."
	default:
		s.State, s.Reason = PlaceSettingPending, "Applies when the next turn starts."
	}
}

// loadPlaceDefaultsLocked reads the record back off meta.json once per agent.
// A conversation with no folder, or no meta.json yet, starts with an empty
// record and is fresh exactly when the person has said nothing in it.
func (a *Agent) loadPlaceDefaultsLocked() {
	if a.placeDefaultsRead {
		return
	}
	a.placeDefaultsRead = true
	// FRESH IS ASKED OF THE TRANSCRIPT FIRST: a person's message in it is a
	// conversation that already started, whatever a missing or older
	// meta.json says. The meta's own stamp is the second witness.
	a.placeFresh = true
	for _, m := range a.messages {
		if m.Role == "user" {
			a.placeFresh = false
			break
		}
	}
	dir := strings.TrimSpace(a.config.Place.Dir)
	if dir == "" {
		return
	}
	meta, err := LoadMeta(dir)
	if err != nil {
		// UNREADABLE IS NOT NEW: a conversation whose record cannot be read is
		// never treated as one the places may set up from scratch.
		a.placeFresh = false
		return
	}
	if meta.PlaceDefaults != nil {
		a.placeDefaults = *meta.PlaceDefaults
	}
	a.placeFresh = a.placeFresh && meta.LastUserAt.IsZero()
}

// placeFactsLocked is [PlaceSettingFacts] for this live agent.
func (a *Agent) placeFactsLocked(needStanding bool) PlaceSettingFacts {
	f := PlaceSettingFacts{
		Door:    true,
		Fresh:   a.placeFresh,
		Record:  a.placeDefaults,
		Model:   strings.TrimSpace(a.model),
		Posture: a.approvalPosture,
		Dial:    a.config.ApprovalGate != nil,
	}
	if needStanding && a.config.ApprovalGate != nil {
		f.Standing = a.config.ApprovalGate.Standing()
	}
	switch stored := a.approvalPosture; {
	case stored != "" && stored != PostureAuto:
		f.Resolved = stored
	case stored == "" && a.config.ApprovalPosture != "":
		f.Resolved = a.config.ApprovalPosture
	default:
		f.Resolved = f.Standing
	}
	return f
}

// applyPlacePolicyLocked is the turn-opening application. Called with a.mu held
// from [Agent.startTurnLocked], after the place block is refreshed and BEFORE
// the turn binds its client, so a model a place sets is the model this turn
// talks to.
func (a *Agent) applyPlacePolicyLocked() {
	if a.config.PlaceGraph == nil || a.placeGraphBundle == nil {
		return
	}
	a.loadPlaceDefaultsLocked()
	fresh := a.placeFresh
	// Only the first turn is the new conversation's; after it, a field
	// follows its places only if it already did.
	a.placeFresh = false
	if len(a.placeGraphBundle.Policy) == 0 {
		return
	}
	needStanding := false
	for _, d := range a.placeGraphBundle.Policy {
		needStanding = needStanding || d.Field == placegraph.PolicyPermissions
	}
	facts := a.placeFactsLocked(needStanding)
	facts.Fresh = fresh
	record := a.placeDefaults
	changed := false
	names := map[string]string{}
	for _, p := range a.placeGraphBundle.Places {
		names[p.ID] = p.Name
	}
	for _, s := range DecidePlaceSettings(a.placeGraphBundle, facts) {
		// A NEW CONVERSATION FOLLOWS EVERY FIELD A PLACE HAD AN OPINION ON
		// WHEN IT STARTED, including one waiting for a pick or for the person:
		// the pick, when it comes, is applied to it at the next turn.
		if fresh {
			switch s.Field {
			case placegraph.PolicyModel:
				changed = changed || !record.ModelFollows
				record.ModelFollows = true
			case placegraph.PolicyPermissions:
				changed = changed || !record.PermissionsFollows
				record.PermissionsFollows = true
			}
		}
		if s.State != PlaceSettingPending {
			continue
		}
		by := names[s.DecidedBy]
		if by == "" {
			by = "A place"
		}
		switch s.Field {
		case placegraph.PolicyModel:
			a.setModelLocked(s.Value)
			record.Model, record.ModelBy = s.Value, s.DecidedBy
			changed = true
			model := s.Value
			// The two hand-offs [Agent.setModel] makes after unlocking. The
			// lane beat takes a.mu, so it runs beside this turn, not under it.
			guard.Go("places-model-beat", func() { a.noteLaneModel(model) })
			a.queuePlaceNoteLocked(fmt.Sprintf("%s set this conversation's model to %s.", by, s.Value))
		case placegraph.PolicyPermissions:
			build := s.Value
			if build == PostureAuto {
				build = ""
			}
			policy, guardian, err := a.config.ApprovalGate.Build(build)
			if err != nil || policy == nil {
				// THE GATE ALREADY STANDING STAYS STANDING, approvalposture.go's
				// law; the Using list keeps saying pending, and the next turn
				// tries again.
				continue
			}
			a.approvalPolicy = policy
			a.approvalPosture = s.Value
			a.guardianOverride = &guardian
			record.Permissions, record.PermissionsBy = s.Value, s.DecidedBy
			changed = true
			a.queuePlaceNoteLocked(fmt.Sprintf("%s set what runs without asking in this conversation to %s.", by, s.Value))
		}
	}
	if changed {
		a.placeDefaults = record
		// Off the path: the stamp reads the agent under a.mu, which is held.
		a.stampModel()
	}
}

// markPlaceYours records that the person chose this field's value inside the
// conversation, so its places stop setting it. A pick equal to what the
// conversation already runs on, or to what the places themselves set, is no
// override: re-choosing a value is confirming it, and a restore that sets the
// saved posture back is not a person's act at all.
func (a *Agent) markPlaceYours(field placegraph.PolicyField, value string) {
	if a == nil || a.config.PlaceGraph == nil {
		return
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	a.mu.Lock()
	a.loadPlaceDefaultsLocked()
	record := a.placeDefaults
	switch field {
	case placegraph.PolicyModel:
		if record.ModelYours || value == strings.TrimSpace(a.model) || (record.Model != "" && value == record.Model) {
			a.mu.Unlock()
			return
		}
		record.ModelYours = true
	case placegraph.PolicyPermissions:
		if record.PermissionsYours || value == a.approvalPosture || (record.Permissions != "" && value == record.Permissions) {
			a.mu.Unlock()
			return
		}
		record.PermissionsYours = true
	default:
		a.mu.Unlock()
		return
	}
	a.placeDefaults = record
	a.mu.Unlock()
	a.stampModel()
}
