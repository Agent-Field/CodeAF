package desktopbridge

// THE USING LIST, AND THE DOORS THAT MAKE A PLACE'S SETTINGS REAL.
//
// A conversation filed under places is given what they say (internal/session's
// placegraphcontext.go) and runs on the model and permissions they decide
// (placegraphpolicy.go). This file is the desktop's view of both, from the SAME
// resolver, the SAME picks file and the SAME rule the engine applies — so the
// list never says a place's setting is applied when the engine would not apply
// it, and never hides something the model was given.
//
// THE CONVERSATION IS NAMED BY ITS TRANSCRIPT, NEVER BY THE BRIDGE'S TOKEN. The
// {token} in /sessions/{token}/using is this process's handle on an open
// connection and means nothing to the graph; the chat a place holds is the
// folder its transcript lives in ([ChatIDFromSessionFile] of the welcome's
// session file), which is what the engine files and resolves under too.
//
// NOTHING HERE CHANGES A SETTING ON ITS OWN. A pick between disagreeing places
// is the person's, from the candidates that actually disagree for this
// conversation and nothing else; adopting a place's value is the person's, and
// goes through the conversation's own model and approval doors, after which it
// is their pick and no place moves it again.

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

// UseDoor hands the places door the engine's own door onto the graph — built
// by the caller with [session.PlaceGraphDoorFor] on the same path its Store was
// opened on — and opens the remembered-picks book beside it. The Using routes
// and the add-a-source route need it; the rest of /places does not.
func (p *Places) UseDoor(door *session.PlaceGraphDoor) error {
	if door == nil || door.Path == "" || door.ChoicesPath == "" {
		return errors.New("places: a door needs the graph and the picks file")
	}
	book, err := placegraph.OpenChoices(door.ChoicesPath)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.door, p.choices = door, book
	p.mu.Unlock()
	return nil
}

// sourcePolicy is the engine's source policy, or the default for this user's
// home when no door was handed in.
func (p *Places) sourcePolicy() placegraph.SourcePolicy {
	if p.door != nil {
		return p.door.Sources
	}
	userHome, _ := os.UserHomeDir()
	return placegraph.DefaultSourcePolicy(userHome, home.Dir())
}

// refusedSentence turns a source refusal into a sentence without the package
// prefix: "That can't be added: <reason>."
func refusedSentence(err error) string {
	reason := strings.TrimPrefix(err.Error(), placegraph.ErrSourceRefused.Error())
	reason = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(reason), ":"))
	if reason == "" {
		return "That can't be given to a chat."
	}
	return "That can't be given to a chat: " + strings.TrimSuffix(reason, ".") + "."
}

// allowsModel is the settings page's own check (Models.allows) when a models
// door is attached. With none, every model is allowed: there is no list to hold
// it against, and the engine applies the name as given.
func (b *Bridge) allowsModel(ctx context.Context, model string) bool {
	b.mu.Lock()
	models := b.models
	b.mu.Unlock()
	if models == nil {
		return true
	}
	return models.allows(ctx, model)
}

// checkPolicy refuses a place policy the engine could not apply: a permissions
// word that is not one of the session's postures, or a model that is not on
// the model list. It reports whether the request may proceed.
func (p *Places) checkPolicy(w http.ResponseWriter, ctx context.Context, model, permissions *string) bool {
	if permissions != nil {
		word := strings.TrimSpace(*permissions)
		known := word == ""
		for _, posture := range session.ApprovalPostures {
			known = known || word == posture
		}
		if !known || word != *permissions {
			failPlaces(w, 400, "invalid_policy", "Permissions must be one of "+strings.Join(session.ApprovalPostures, ", ")+".")
			return false
		}
	}
	if model != nil {
		name := strings.TrimSpace(*model)
		if name != *model {
			failPlaces(w, 400, "invalid_policy", "A model name has no spaces around it.")
			return false
		}
		if name != "" && p.allowsModel != nil && !p.allowsModel(ctx, name) {
			failPlaces(w, 400, "invalid_policy", "That model is not on the list.")
			return false
		}
	}
	return true
}

// UsingView is one conversation's Using list.
type UsingView struct {
	ChatID string `json:"chatId"`
	// Engine says whether this conversation's engine reads the place graph at
	// all. False means the bundle below is what the places SAY, and none of it
	// reached the model or the conversation's settings.
	Engine   UsingEngine            `json:"engine"`
	Revision uint64                 `json:"revision"`
	ReadAt   time.Time              `json:"readAt"`
	Bundle   *placegraph.Bundle     `json:"bundle"`
	Settings []session.PlaceSetting `json:"settings"`
}

// UsingEngine is the engine half of the answer.
type UsingEngine struct {
	Places bool   `json:"places"`
	Reason string `json:"reason,omitempty"`
}

// usingRoutes serves /sessions/{token}/using[/choice|/apply]. It reports
// whether the path was one of its own.
func (b *Bridge) usingRoutes(w http.ResponseWriter, r *http.Request, s *conversation, parts []string) bool {
	if len(parts) < 3 || parts[2] != "using" || len(parts) > 4 {
		return false
	}
	b.mu.Lock()
	p := b.places
	b.mu.Unlock()
	if p == nil || p.door == nil || p.choices == nil {
		failPlaces(w, 501, "unsupported", "This desktop has no places to use.")
		return true
	}
	chatID := ChatIDFromSessionFile(s.conn.Welcome.SessionFile)
	if chatID == "" || chatID == "." {
		failPlaces(w, 409, "unsaved", "This conversation isn't saved yet, so no place can hold it.")
		return true
	}
	switch {
	case len(parts) == 3:
		if needGet(w, r) {
			p.writeUsing(w, s, chatID)
		}
	case parts[3] == "choice":
		if needPost(w, r) {
			p.choose(w, r, s, chatID)
		}
	case parts[3] == "apply":
		if needPost(w, r) {
			p.apply(w, r, s, chatID)
		}
	default:
		fail(w, 404, "unknown engine action")
	}
	return true
}

// using resolves the conversation's view. A graph or picks file that cannot be
// read is an error, never an empty list.
func (p *Places) using(s *conversation, chatID string) (*UsingView, error) {
	snap, err := p.Store.Snapshot()
	if err != nil {
		return nil, err
	}
	picks, err := p.choices.For(chatID)
	if err != nil {
		return nil, err
	}
	bundle := session.PlaceGraphUsing(snap, chatID, picks, p.door.Sources)
	facts := p.facts(s)
	return &UsingView{
		ChatID:   chatID,
		Engine:   UsingEngine{Places: facts.Door, Reason: facts.DoorReason},
		Revision: snap.Revision,
		ReadAt:   p.now().UTC(),
		Bundle:   bundle,
		Settings: session.DecidePlaceSettings(bundle, facts),
	}, nil
}

// facts reads what [session.DecidePlaceSettings] needs about this conversation
// from the same places the engine keeps it: the welcome's launch shape (does
// the engine read this graph), the conversation's meta.json (what places set
// and what is the person's own), and the wire's own reads of the model and the
// gate.
func (p *Places) facts(s *conversation) session.PlaceSettingFacts {
	f := session.PlaceSettingFacts{Model: strings.TrimSpace(s.conn.Agent.Model())}
	launch := s.conn.Welcome.Launch
	switch {
	case !s.conn.Local:
		f.DoorReason = "This conversation runs on another machine, which does not read the places on this one."
	case launch == nil || launch.PlaceGraph == "":
		f.DoorReason = "This conversation was opened without your places (in the terminal, or by an older codeaf), so its engine does not read them. Open it again from the desktop."
	case filepath.Clean(launch.PlaceGraph) != p.door.Path:
		f.DoorReason = "This conversation's engine reads places from another file, so these settings are not applied to it."
	default:
		f.Door = true
	}
	if door, ok := s.conn.Agent.(interface {
		ApprovalDial() bool
		ResolvedApprovalPosture() string
		StandingApprovalPosture() string
	}); ok {
		f.Dial = door.ApprovalDial()
		f.Resolved = door.ResolvedApprovalPosture()
		f.Standing = door.StandingApprovalPosture()
	}
	if s.conn.Local && s.conn.Welcome.SessionFile != "" {
		if meta, err := session.LoadMeta(filepath.Dir(s.conn.Welcome.SessionFile)); err == nil {
			f.Fresh = meta.LastUserAt.IsZero()
			f.Posture = meta.Approval
			if meta.PlaceDefaults != nil {
				f.Record = *meta.PlaceDefaults
			}
		}
	}
	return f
}

func (p *Places) writeUsing(w http.ResponseWriter, s *conversation, chatID string) {
	view, err := p.using(s, chatID)
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	write(w, view)
}

type usingAsk struct {
	Field   placegraph.PolicyField `json:"field"`
	PlaceID string                 `json:"placeId"`
}

// decision finds the conversation's decision for one field.
func decision(b *placegraph.Bundle, field placegraph.PolicyField) (placegraph.PolicyDecision, bool) {
	for _, d := range b.Policy {
		if d.Field == field {
			return d, true
		}
	}
	return placegraph.PolicyDecision{}, false
}

// choose remembers the person's pick between places that disagree. The pick
// must name one of the places that actually want a value for this field in
// this conversation; a place outside that set, or a field nobody disagrees
// about, is refused rather than written down as a rule nobody asked for.
func (p *Places) choose(w http.ResponseWriter, r *http.Request, s *conversation, chatID string) {
	var ask usingAsk
	if !readBody(w, r, &ask) {
		return
	}
	if !ask.Field.Valid() || strings.TrimSpace(ask.PlaceID) == "" {
		failPlaces(w, 400, "invalid", "Say which setting and which place.")
		return
	}
	view, err := p.using(s, chatID)
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	d, found := decision(view.Bundle, ask.Field)
	if !found || (d.Outcome != placegraph.PolicyNeedsPick && d.Outcome != placegraph.PolicyChosen) {
		failPlaces(w, 409, "no_conflict", "The places this conversation uses don't disagree about that, so there is nothing to pick.")
		return
	}
	candidate := false
	for _, want := range d.Wanted {
		candidate = candidate || want.PlaceID == ask.PlaceID
	}
	if !candidate {
		failPlaces(w, 422, "not_a_candidate", "Pick one of the places that disagree about this conversation's "+string(ask.Field)+".")
		return
	}
	if _, err := p.choices.Set(chatID, ask.Field, ask.PlaceID); err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	p.writeUsing(w, s, chatID)
}

// apply is the person taking the places' decided value as this conversation's
// own, through the conversation's own doors. It is how a held posture is
// opened and how a conversation that was running before it was filed takes the
// place's model — and either way it is then the person's pick, which no place
// changes afterwards.
func (p *Places) apply(w http.ResponseWriter, r *http.Request, s *conversation, chatID string) {
	var ask usingAsk
	if !readBody(w, r, &ask) {
		return
	}
	if !ask.Field.Valid() || ask.PlaceID != "" {
		failPlaces(w, 400, "invalid", "Say which setting to use.")
		return
	}
	view, err := p.using(s, chatID)
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	d, found := decision(view.Bundle, ask.Field)
	switch {
	case !found || (d.Value == "" && d.Outcome != placegraph.PolicyNeedsPick):
		failPlaces(w, 409, "nothing_to_apply", "No place this conversation uses sets that.")
		return
	case d.Outcome == placegraph.PolicyNeedsPick:
		failPlaces(w, 409, "needs_pick", "These places disagree. Pick one first.")
		return
	}
	switch ask.Field {
	case placegraph.PolicyModel:
		door, ok := s.conn.Agent.(interface{ SetModel(string) })
		if !ok {
			failPlaces(w, 501, "unsupported", "This conversation's engine can't change its model from here.")
			return
		}
		door.SetModel(d.Value)
	case placegraph.PolicyPermissions:
		door, ok := s.conn.Agent.(interface {
			ApprovalDial() bool
			SetApprovalPosture(string) error
		})
		if !ok || !door.ApprovalDial() {
			failPlaces(w, 501, "unsupported", "This conversation has no control over what runs without asking.")
			return
		}
		if err := door.SetApprovalPosture(d.Value); err != nil {
			failPlaces(w, 409, "refused", err.Error())
			return
		}
	}
	s.publishSnapshot()
	p.writeUsing(w, s, chatID)
}

// placeHoldsModel says a conversation's model is its places' to set right now
// (applied, or waiting for the next turn), so a change to the settings page's
// default must not move it: the place is the nearer default.
func (b *Bridge) placeHoldsModel(s *conversation) bool {
	b.mu.Lock()
	p := b.places
	b.mu.Unlock()
	if p == nil || p.door == nil || p.choices == nil {
		return false
	}
	chatID := ChatIDFromSessionFile(s.conn.Welcome.SessionFile)
	if chatID == "" {
		return false
	}
	view, err := p.using(s, chatID)
	if err != nil {
		return false
	}
	for _, setting := range view.Settings {
		if setting.Field == placegraph.PolicyModel && (setting.State == session.PlaceSettingApplied || setting.State == session.PlaceSettingPending) {
			return true
		}
	}
	return false
}

// newChatPlace checks a place named for a conversation about to be opened. It
// answers the status and sentence to refuse with, or 0.
func (b *Bridge) newChatPlace(placeID string) (*Places, int, string, string) {
	b.mu.Lock()
	p := b.places
	b.mu.Unlock()
	if p == nil {
		return nil, 409, "unsupported", "This desktop has no places to start a chat in."
	}
	if placeID == placegraph.RootID || placeID == placegraph.NowID {
		return nil, 400, "reserved", "A new chat goes into a place, not into All places or Now."
	}
	snap, err := p.Store.Snapshot()
	if err != nil {
		status, code, sentence := storeFailure(err, "")
		return nil, status, code, sentence
	}
	pl, found := snap.Place(placeID)
	switch {
	case !found:
		return nil, 404, "not_found", "That place doesn't exist any more."
	case pl.Archived:
		return nil, 409, "archived", "That place is archived. Restore it first."
	}
	return p, 0, "", ""
}

// fileNewChat files a just-opened conversation under the place the person
// started it in, before its first turn, so the engine's first turn is the one
// that applies the place's defaults.
func (p *Places) fileNewChat(conn Connection, placeID string) error {
	chatID := ChatIDFromSessionFile(conn.Welcome.SessionFile)
	if chatID == "" || chatID == "." {
		return errors.New("this conversation has no saved folder to file")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _, err := p.Store.AddChat(chatID, placeID, placegraph.AddedByYou)
	return err
}
