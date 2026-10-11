package placegraph

// Recommendations: codeaf offering where a chat belongs, offering a new place
// when enough chats in no place belong together, and offering to merge two
// places that say the same thing. docs/AI-ROLES-AND-PLACES-POLICY.md is the
// account of this file in a person's terms; this comment is the engineer's.
//
// THE ORDER OF EVERY DECISION IS: REUSE, RULES, THEN A BOUNDED MODEL CALL.
//   1. A place that already exists is always preferred to a new one. A chat is
//      only ever offered existing places (design 6e), and a group of chats is
//      offered an existing place before a new one is considered.
//   2. Rules answer whatever they can without a call: a chat running in a
//      folder a place holds is that place's; two sibling places whose names fold to the same
//      words are a merge. Rules also decide when NOT to call: nothing to choose
//      from, too little said, budget spent, an answer that could not be used
//      because every cap is full.
//   3. Only then is the engine's role door asked, once, with a handful of
//      labelled candidates, and its answer is checked against the contract
//      before anything is offered (recommend_model.go).
//
// NOTHING IS REORGANISED WITHOUT A YES. Every result is a Proposal a person
// accepts or declines. The single exception is filing a chat into a place that
// already exists, when the person turned that on (RecommendPolicy.AutoFile, off
// by default) — it adds one membership, marked as the AI's, and can be undone.
// Creating, moving a group and merging always wait for a person.
//
// A NEW PLACE IS NOT OFFERED FROM HERE. Grouping chats into a place is the
// organiser's run. This file files one chat by the folder rule, or by a
// Decide the organiser supplies, and it still offers to merge two sibling
// places whose names say the same thing.
//
// A FAILURE KEEPS THE ORGANISATION EXACTLY AS IT WAS. A model that errs, times
// out, answers malformed JSON, names a label it was not shown or a cyclic or
// over-deep parent produces no offer and changes nothing. An accept that fails
// part-way takes back the steps it already made.
//
// NOTHING A MODEL WRITES BECOMES AN ID, A PATH OR A PERMISSION. A new place
// created from an offer has a name, a parent and members; its context sources
// and policy are empty, so accepting an offer can never widen what any chat is
// allowed to read or do.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ChatEvidence is what the caller (the engine's conversation list) knows about
// one chat. None of it comes from a model answer.
type ChatEvidence struct {
	ChatID       string `json:"chatId"`
	Title        string `json:"title"`
	Summary      string `json:"summary,omitempty"`
	FirstMessage string `json:"firstMessage,omitempty"`
	// Workspace is the chat's canonical project folder. It is compared with
	// place folder sources for equality and never opened or written.
	Workspace string `json:"workspace,omitempty"`
	// Replies counts settled assistant replies; an offer waits for the first.
	Replies   int       `json:"replies"`
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
}

// ProposalKind is what accepting would do.
type ProposalKind string

const (
	// ProposalFile adds one chat to one existing place.
	ProposalFile ProposalKind = "file"
	// ProposalMove puts a group of chats in no place into one existing place.
	ProposalMove ProposalKind = "move"
	// ProposalCreate makes a new place and puts a group of chats in it.
	ProposalCreate ProposalKind = "create"
	// ProposalMerge merges FromPlaceID into PlaceID (Store.MergePlaces).
	ProposalMerge ProposalKind = "merge"
	// ProposalPlan applies an organising run as one undoable commit.
	ProposalPlan ProposalKind = "plan"
)

// ProposalStatus is where an offer stands.
type ProposalStatus string

const (
	StatusPending  ProposalStatus = "pending"
	StatusAccepted ProposalStatus = "accepted"
	StatusDeclined ProposalStatus = "declined"
	// StatusStale is an offer the graph moved past: its place went, its chats
	// were filed by hand, or a cap filled. It is never shown again.
	StatusStale ProposalStatus = "stale"
	// StatusDropped is a group the model was asked about and said does not
	// belong together, or answered unusably. It is kept so the same group is
	// not asked about again until DeclineSnoozeDays pass.
	StatusDropped ProposalStatus = "dropped"
)

// Proposal is one offer.
type Proposal struct {
	// ID is derived from what the offer is ABOUT (the chat, the group of chats,
	// the pair of places), so asking twice yields the same offer, never two.
	ID          string         `json:"id"`
	Ops         []PlanOp       `json:"ops,omitempty"`
	Kind        ProposalKind   `json:"kind"`
	Status      ProposalStatus `json:"status"`
	ChatIDs     []string       `json:"chatIds,omitempty"`
	PlaceID     string         `json:"placeId,omitempty"`
	FromPlaceID string         `json:"fromPlaceId,omitempty"`
	Name        string         `json:"name,omitempty"`
	ParentID    string         `json:"parentId,omitempty"`
	Confidence  int            `json:"confidence"`
	Basis       string         `json:"basis"`
	// Reason is one short line in a person's words, written by rules from the
	// basis and real place names; never model text.
	Reason    string    `json:"reason"`
	Revision  uint64    `json:"revision"`
	CreatedAt time.Time `json:"createdAt"`
	DecidedAt time.Time `json:"decidedAt,omitzero"`
	// Auto marks a filing made without asking (RecommendPolicy.AutoFile).
	Auto bool `json:"auto,omitempty"`
}

// Recommender ties the graph, the ledger, the policy and the engine's model
// door together. Store, Ledger and Policy are required; Ask may be nil.
type Recommender struct {
	Store  *Store
	Ledger *Ledger
	// Policy is read on every call, so a change in Settings applies at once.
	Policy func() RecommendPolicy
	Ask    Asker
	Now    func() time.Time
	// Decide, when set, is the organiser's filing. Nil keeps the folder rule
	// only: a shared word is no longer an offer, and this type does not ask a
	// model where a chat goes. The organiser's filer is that question now.
	Decide func(context.Context, ChatEvidence, *Snapshot) (FilingChoice, error)
}

// FilingChoice is a place the organiser has already chosen for one chat.
// An empty PlaceID means the chat was weighed and there is nothing to offer.
type FilingChoice struct {
	PlaceID    string
	Confidence int
	Reason     string
	Basis      string
}

// AcceptEdit is what the person changed on an offer before saying yes.
type AcceptEdit struct {
	// Name renames a new place before it is created.
	Name string `json:"name,omitempty"`
	// ChatIDs narrows a group to the chats the person kept.
	ChatIDs []string `json:"chatIds,omitempty"`
	// Skip contains operation indexes the person unchecked, counted from zero.
	Skip []int `json:"skip,omitempty"`
}

// AcceptResult is what accepting did. Receipts are in the order they were
// made; undoing them in reverse takes the whole accept back.
type AcceptResult struct {
	Proposal Proposal  `json:"proposal"`
	PlaceID  string    `json:"placeId"`
	Receipts []Receipt `json:"receipts"`
}

var (
	// ErrProposalGone is an accept or decline of an offer that is not pending.
	ErrProposalGone = errors.New("placegraph: that offer is no longer open")
	// ErrPolicyLimit is an accept the AI caps no longer allow.
	ErrPolicyLimit = errors.New("placegraph: codeaf may not create another place there")
)

func (r *Recommender) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *Recommender) policy() RecommendPolicy {
	if r.Policy == nil {
		return DefaultRecommendPolicy()
	}
	return r.Policy().Normalized()
}

func proposalID(fingerprint string) string {
	sum := sha256.Sum256([]byte(fingerprint))
	return "prop_" + hex.EncodeToString(sum[:8])
}

func libraryIndex(library []ChatEvidence) map[string]ChatEvidence {
	m := make(map[string]ChatEvidence, len(library))
	for _, c := range library {
		if c.ChatID != "" {
			m[c.ChatID] = c
		}
	}
	return m
}

// reserveCall spends one model call from the role's daily budget, and for the
// suggest role also from the organizing interval. It answers false, spending
// nothing, when the budget is gone. The reservation is written BEFORE the call
// so two windows cannot both spend the last one.
func (r *Recommender) reserveCall(role string, perDay int, every time.Duration, also func(*ledgerState) bool) (bool, error) {
	now := r.now()
	ok := false
	err := r.Ledger.update(now, func(st *ledgerState) error {
		if perDay <= 0 || st.callsSince(role, now.Add(-24*time.Hour)) >= perDay {
			return nil
		}
		if every > 0 && !st.LastSuggestCall.IsZero() && now.Sub(st.LastSuggestCall) < every {
			return nil
		}
		if also != nil && !also(st) {
			return nil
		}
		st.Calls = append(st.Calls, callMark{Role: role, At: now})
		if every > 0 {
			st.LastSuggestCall = now
		}
		ok = true
		return nil
	})
	return ok, err
}

// ---- filing one chat ---------------------------------------------------------

// FileChat considers ONE chat for ONE offer of a place it is not already in.
// It is meant to be called after each settled reply and is idempotent: a chat
// is weighed once, and asking again returns the same open offer or nothing.
// (nil, nil) means there is nothing to offer, which is the common answer.
// library is the caller's list of chats, used to read what places hold.
func (r *Recommender) FileChat(ctx context.Context, chat ChatEvidence, library []ChatEvidence) (*Proposal, error) {
	pol := r.policy()
	if !pol.FilingOffers || chat.Replies < 1 || validChatID(chat.ChatID) != nil {
		return nil, nil
	}
	snap, err := r.Store.Snapshot()
	if err != nil {
		return nil, err
	}
	id := proposalID("file|" + chat.ChatID)
	var open *Proposal
	considered := false
	if err := r.Ledger.view(r.now(), func(st *ledgerState) error {
		if _, done := st.Considered[chat.ChatID]; done {
			considered = true
			if i := st.find(id); i >= 0 && st.Proposals[i].Status == StatusPending {
				p := st.Proposals[i]
				open = &p
			}
		}
		return nil
	}); err != nil || considered {
		return open, err
	}
	exclude := map[string]bool{}
	for _, m := range snap.PlacesOf(chat.ChatID) {
		exclude[m.PlaceID] = true
	}
	var prop *Proposal
	if r.Decide != nil {
		choice, err := r.Decide(ctx, chat, snap)
		if err != nil {
			return nil, err
		}
		if choice.PlaceID != "" && !exclude[choice.PlaceID] && choice.Confidence >= pol.MinConfidence {
			if place, ok := snap.Place(choice.PlaceID); ok && !place.Archived {
				basis := choice.Basis
				if basis == "" {
					basis = BasisOrganizer
				}
				reason := choice.Reason
				if reason == "" {
					reason = "Looks like the work in " + place.Name
				}
				prop = &Proposal{ID: id, Kind: ProposalFile, Status: StatusPending, ChatIDs: []string{chat.ChatID}, PlaceID: place.ID,
					Confidence: choice.Confidence, Basis: basis, Reason: reason, Revision: snap.Revision, CreatedAt: r.now()}
			}
		}
	} else {
		folder, said := cleanFolder(chat.Workspace), chatWords(chat)
		if folder == "" && len(said) < minChatWords {
			return nil, nil
		}
		lib := libraryIndex(library)
		if place, conf, ok := folderMatch(snap, folder, exclude, lib); ok && conf >= pol.MinConfidence {
			prop = &Proposal{ID: id, Kind: ProposalFile, Status: StatusPending, ChatIDs: []string{chat.ChatID}, PlaceID: place.ID,
				Confidence: conf, Basis: BasisFolder, Reason: "Same folder as " + place.Name, Revision: snap.Revision, CreatedAt: r.now()}
		}
	}
	if err := r.Ledger.update(r.now(), func(st *ledgerState) error {
		st.Considered[chat.ChatID] = r.now()
		if prop != nil && st.find(prop.ID) < 0 {
			st.Proposals = append(st.Proposals, *prop)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if prop != nil && pol.AutoFile && prop.Confidence >= pol.AutoFileConfidence {
		res, err := r.accept(prop.ID, AcceptEdit{}, true)
		if err != nil {
			return prop, nil // the offer stands; the person can still say yes
		}
		return &res.Proposal, nil
	}
	return prop, nil
}

// ---- organizing chats in no place --------------------------------------------

// Organize looks over the chats in no place and over the places themselves,
// and returns every open offer afterwards (new and old), oldest first. Errors
// from the model are returned joined after the offers; they never change the
// graph. library is the caller's list of chats.
func (r *Recommender) Organize(ctx context.Context, library []ChatEvidence) ([]Proposal, error) {
	pol := r.policy()
	snap, err := r.Store.Snapshot()
	if err != nil {
		return nil, err
	}
	var st *ledgerState
	if err := r.Ledger.view(r.now(), func(s *ledgerState) error { st = s; return nil }); err != nil {
		return nil, err
	}
	room := pol.MaxPending
	for _, p := range st.Proposals {
		if p.Status == StatusPending && p.Kind != ProposalFile {
			room--
		}
	}
	var fresh []Proposal
	if pol.MergeOffers {
		for _, p := range r.mergeOffers(snap, st, pol) {
			if room <= 0 {
				break
			}
			fresh = append(fresh, p)
			room--
		}
	}
	if len(fresh) > 0 {
		if err := r.Ledger.update(r.now(), func(st *ledgerState) error {
			// The same offer made again after a snooze, or after it went stale,
			// takes its old record's place; an open or accepted one is kept.
			for _, p := range fresh {
				switch i := st.find(p.ID); {
				case i < 0:
					st.Proposals = append(st.Proposals, p)
				case st.Proposals[i].Status != StatusPending && st.Proposals[i].Status != StatusAccepted:
					st.Proposals[i] = p
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	open, err := r.Pending()
	if err != nil {
		return nil, err
	}
	return open, nil
}

// mergeOffers finds pairs of active sibling places whose names say the same
// thing. The older place, or the person's own over the AI's, is kept.
func (r *Recommender) mergeOffers(snap *Snapshot, st *ledgerState, pol RecommendPolicy) []Proposal {
	ai := map[string]bool{}
	for _, id := range st.AIPlaces {
		ai[id] = true
	}
	groups := map[string][]Place{}
	var keys []string
	for _, p := range snap.Places {
		if p.Archived {
			continue
		}
		k := parentKey(p.Parents) + "\x01" + sameNameKey(p.Name)
		if sameNameKey(p.Name) == "" {
			continue
		}
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], p)
	}
	sort.Strings(keys)
	snooze := time.Duration(pol.DeclineSnoozeDays) * 24 * time.Hour
	var out []Proposal
	for _, k := range keys {
		g := groups[k]
		if len(g) < 2 {
			continue
		}
		sort.SliceStable(g, func(i, j int) bool {
			if ai[g[i].ID] != ai[g[j].ID] {
				return !ai[g[i].ID]
			}
			if !g[i].CreatedAt.Equal(g[j].CreatedAt) {
				return g[i].CreatedAt.Before(g[j].CreatedAt)
			}
			return g[i].ID < g[j].ID
		})
		into, from := g[0], g[1]
		id := proposalID("merge|" + from.ID + "|" + into.ID)
		if i := st.find(id); i >= 0 {
			p := st.Proposals[i]
			if p.Status == StatusPending || ((p.Status == StatusDeclined || p.Status == StatusDropped) && r.now().Sub(p.DecidedAt) < snooze) {
				continue
			}
		}
		out = append(out, Proposal{ID: id, Kind: ProposalMerge, Status: StatusPending, PlaceID: into.ID, FromPlaceID: from.ID,
			Confidence: sameNameMergeConf, Basis: BasisSameName, Reason: fmt.Sprintf("“%s” and “%s” look like the same place", from.Name, into.Name),
			Revision: snap.Revision, CreatedAt: r.now()})
	}
	return out
}

// ---- reading, accepting and declining ------------------------------------------

// Pending lists the open offers, oldest first, after setting aside any the
// graph has moved past.
func (r *Recommender) Pending() ([]Proposal, error) {
	snap, err := r.Store.Snapshot()
	if err != nil {
		return nil, err
	}
	pol := r.policy()
	now := r.now()
	var out []Proposal
	err = r.Ledger.update(now, func(st *ledgerState) error {
		changed := false
		for i := range st.Proposals {
			p := &st.Proposals[i]
			if p.Status != StatusPending {
				continue
			}
			if _, _, err := check(snap, pol, st, *p, AcceptEdit{}); err != nil {
				p.Status, p.DecidedAt, changed = StatusStale, now, true
				continue
			}
			out = append(out, *p)
		}
		if !changed {
			return errUnchanged // a read that closed nothing writes nothing
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, err
}

// Decline is the person's "Not now": the offer closes, and the same offer is
// not made again for DeclineSnoozeDays.
func (r *Recommender) Decline(id string) error {
	now := r.now()
	return r.Ledger.update(now, func(st *ledgerState) error {
		i := st.find(id)
		if i < 0 || st.Proposals[i].Status != StatusPending {
			return ErrProposalGone
		}
		st.Proposals[i].Status, st.Proposals[i].DecidedAt = StatusDeclined, now
		return nil
	})
}

// Accept applies an open offer, with the person's edits, after checking it
// against the graph as it is NOW. An offer the graph has moved past is closed
// and ErrProposalGone returned; nothing is changed.
func (r *Recommender) Accept(id string, edit AcceptEdit) (AcceptResult, error) {
	return r.accept(id, edit, false)
}

func (r *Recommender) accept(id string, edit AcceptEdit, auto bool) (AcceptResult, error) {
	now := r.now()
	pol := r.policy()
	var res AcceptResult
	var outcome error
	err := r.Ledger.update(now, func(st *ledgerState) error {
		i := st.find(id)
		if i < 0 || st.Proposals[i].Status != StatusPending {
			outcome = ErrProposalGone
			return nil
		}
		p := st.Proposals[i]
		snap, err := r.Store.Snapshot()
		if err != nil {
			return err
		}

		if p.Kind == ProposalPlan {
			ops, err := selectedPlan(p.Ops, edit)
			if err != nil {
				outcome = err
				return nil
			}
			rc, err := r.Store.applyPlan(ops, &p.Revision, func(work *State) error { return planPolicy(work, ops, st.AIPlaces, pol) })
			if err != nil {
				outcome = err
				return nil
			}
			st.Proposals[i].Status, st.Proposals[i].DecidedAt = StatusAccepted, now
			for _, op := range ops {
				if op.Kind == PlanCreate && !containsString(st.AIPlaces, op.PlaceID) {
					st.AIPlaces = append(st.AIPlaces, op.PlaceID)
				}
			}
			receipts := []Receipt{}
			if !rc.Noop() {
				receipts = append(receipts, rc)
			}
			res = AcceptResult{Proposal: st.Proposals[i], Receipts: receipts}
			return nil
		}
		chats, name, err := check(snap, pol, st, p, edit)
		if err != nil {
			// An offer the graph moved past, or one the caps now refuse, is
			// closed so it is not shown again; any other refusal (a name the
			// person typed that is taken) leaves it open to try again.
			if errors.Is(err, errStale) || errors.Is(err, ErrPolicyLimit) {
				st.Proposals[i].Status, st.Proposals[i].DecidedAt = StatusStale, now
				outcome = ErrProposalGone
				if errors.Is(err, ErrPolicyLimit) {
					outcome = ErrPolicyLimit
				}
				return nil
			}
			outcome = err
			return nil
		}
		receipts, placeID, err := r.apply(p, chats, name)
		if err != nil {
			outcome = err
			return nil
		}
		st.Proposals[i].Status, st.Proposals[i].DecidedAt, st.Proposals[i].Auto = StatusAccepted, now, auto
		if p.Kind == ProposalCreate {
			st.AIPlaces = append(st.AIPlaces, placeID)
		}
		res = AcceptResult{Proposal: st.Proposals[i], PlaceID: placeID, Receipts: receipts}
		return nil
	})
	if err != nil {
		return AcceptResult{}, err
	}
	return res, outcome
}

var errStale = errors.New("stale")

// check validates an offer against the graph now, with the person's edits, and
// returns the chats to act on and the name to use. errStale means the offer no
// longer applies at all; any other error means this accept is refused.
func check(snap *Snapshot, pol RecommendPolicy, st *ledgerState, p Proposal, edit AcceptEdit) ([]string, string, error) {
	active := func(id string) bool { pl, ok := snap.Place(id); return ok && !pl.Archived }
	chats := p.ChatIDs
	if len(edit.ChatIDs) > 0 {
		in := map[string]bool{}
		for _, c := range p.ChatIDs {
			in[c] = true
		}
		chats = nil
		for _, c := range edit.ChatIDs {
			if !in[c] {
				return nil, "", fmt.Errorf("%w: chat %q is not part of this offer", ErrInvalid, c)
			}
			chats = append(chats, c)
		}
	}
	switch p.Kind {
	case ProposalPlan:
		if p.Revision != snap.Revision {
			return nil, "", errStale
		}
		return nil, "", nil

	case ProposalFile:
		if !active(p.PlaceID) || len(chats) != 1 {
			return nil, "", errStale
		}
		for _, m := range snap.PlacesOf(chats[0]) {
			if m.PlaceID == p.PlaceID {
				return nil, "", errStale
			}
		}
		return chats, "", nil
	case ProposalMove, ProposalCreate:
		chats = snap.Unplaced(chats)
		if len(chats) == 0 {
			return nil, "", errStale
		}
		if p.Kind == ProposalMove {
			if !active(p.PlaceID) {
				return nil, "", errStale
			}
			return chats, "", nil
		}
		name := p.Name
		if strings.TrimSpace(edit.Name) != "" {
			n, err := cleanName(edit.Name)
			if err != nil {
				return nil, "", err
			}
			name = n
		}
		if p.ParentID != "" && !active(p.ParentID) {
			return nil, "", errStale
		}
		if ok, _ := canCreateUnder(snap, pol, countAI(snap, st.AIPlaces), p.ParentID); !ok {
			return nil, "", ErrPolicyLimit
		}
		var parents []string
		if p.ParentID != "" {
			parents = []string{p.ParentID}
		}
		if !snap.NameAvailable(name, parents, "") {
			return nil, "", ErrNameTaken
		}
		return chats, name, nil
	case ProposalMerge:
		if !active(p.PlaceID) || !active(p.FromPlaceID) || p.PlaceID == p.FromPlaceID {
			return nil, "", errStale
		}
		return nil, "", nil
	}
	return nil, "", fmt.Errorf("%w: unknown offer kind %q", ErrInvalid, p.Kind)
}

// apply makes the change, and on a failure part-way takes back what it made.
func (r *Recommender) apply(p Proposal, chats []string, name string) ([]Receipt, string, error) {
	var receipts []Receipt
	keep := func(rc Receipt) {
		if !rc.Noop() {
			receipts = append(receipts, rc)
		}
	}
	rollback := func(err error) ([]Receipt, string, error) {
		for i := len(receipts) - 1; i >= 0; i-- {
			if _, uerr := r.Store.Undo(receipts[i].ID); uerr != nil {
				return nil, "", errors.Join(err, uerr)
			}
		}
		return nil, "", err
	}
	placeID := p.PlaceID
	switch p.Kind {
	case ProposalMerge:
		_, rc, err := r.Store.MergePlaces(p.FromPlaceID, p.PlaceID)
		if err != nil {
			return nil, "", err
		}
		keep(rc)
		return receipts, placeID, nil
	case ProposalCreate:
		var parents []string
		if p.ParentID != "" {
			parents = []string{p.ParentID}
		}
		// Name and parent only. No sources and no policy: an offer can never
		// widen what a chat may read or do.
		pl, rc, err := r.Store.CreatePlace(NewPlace{Name: name, Parents: parents})
		if err != nil {
			return nil, "", err
		}
		keep(rc)
		placeID = pl.ID
	}
	for _, c := range chats {
		_, rc, err := r.Store.AddChat(c, placeID, AddedByAI)
		if err != nil {
			return rollback(err)
		}
		keep(rc)
	}
	return receipts, placeID, nil
}

// selectedPlan only narrows an offer; a caller cannot supply replacement operations.
func selectedPlan(ops []PlanOp, edit AcceptEdit) ([]PlanOp, error) {
	if edit.Name != "" || len(edit.ChatIDs) != 0 {
		return nil, fmt.Errorf("%w: plan edits take skip indexes", ErrInvalid)
	}
	skip := map[int]bool{}
	for _, i := range edit.Skip {
		if i < 0 || i >= len(ops) || skip[i] {
			return nil, fmt.Errorf("%w: invalid skip index", ErrInvalid)
		}
		skip[i] = true
	}
	selected := make([]PlanOp, 0, len(ops))
	for i, op := range ops {
		if !skip[i] {
			selected = append(selected, op)
		}
	}
	return selected, nil
}

// planPolicy counts every surviving plan-created place against the existing AI
// limits under the graph lock. Places the person made never consume these caps.
func planPolicy(work *State, ops []PlanOp, aiPlaces []string, pol RecommendPolicy) error {
	ai := append([]string(nil), aiPlaces...)
	for _, op := range ops {
		if op.Kind == PlanCreate && !containsString(ai, op.PlaceID) {
			ai = append(ai, op.PlaceID)
		}
	}
	for _, op := range ops {
		if op.Kind != PlanCreate {
			continue
		}
		p := work.find(op.PlaceID)
		if p == nil || p.Archived {
			continue
		}
		// canCreateUnder evaluates capacity before a birth, so exclude this birth.
		others := work.clone()
		kept := others.Places[:0:0]
		for _, row := range others.Places {
			if row.ID != p.ID {
				kept = append(kept, row)
			}
		}
		others.Places = kept
		snapshot := newSnapshot(others)
		parents := p.Parents
		if len(parents) == 0 {
			parents = []string{""}
		}
		for _, parent := range parents {
			if ok, _ := canCreateUnder(snapshot, pol, countAI(snapshot, ai), parent); !ok {
				return ErrPolicyLimit
			}
		}
	}
	return nil
}

// ProposePlan records a completed organiser's offer without changing the graph.
// A run id is supplied by the caller, not a model; repeating that run keeps the
// same decision and operations. Acceptance rechecks the captured revision.
func (r *Recommender) ProposePlan(runID string, ops []PlanOp, summary string) (Proposal, error) {
	if err := validID(runID); err != nil {
		return Proposal{}, err
	}
	snapshot, err := r.Store.Snapshot()
	if err != nil {
		return Proposal{}, err
	}
	now := r.now()
	proposal := Proposal{ID: proposalID("plan|" + runID), Kind: ProposalPlan, Status: StatusPending, Revision: snapshot.Revision, CreatedAt: now, Reason: summary}
	proposal.Ops = append([]PlanOp(nil), ops...)
	for i := range proposal.Ops {
		proposal.Ops[i].Parents = append([]string(nil), ops[i].Parents...)
	}
	err = r.Ledger.update(now, func(st *ledgerState) error {
		if i := st.find(proposal.ID); i >= 0 {
			proposal = st.Proposals[i]
			return errUnchanged
		}
		st.Proposals = append(st.Proposals, proposal)
		return nil
	})
	return proposal, err
}
