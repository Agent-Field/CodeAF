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
//      folder a place holds is that place's; chats sharing a folder are a group
//      named after the folder; two sibling places whose names fold to the same
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
// WHY NOT A PLACE PER CONVERSATION, OR PER MESSAGE. Most chats are quick and
// belong nowhere (design: "quick chats stay unplaced"). A place made for each
// one would bury the dozen that matter under hundreds that do not, and a model
// reading one message has no way to know whether it is the first of fifty or
// the only one. So a new place is offered only for a GROUP — MinClusterChats
// chats that already belong together — and the group is found by rules over
// what those chats share, not by a model's impression of one of them.
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
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/roles"
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
}

// AcceptEdit is what the person changed on an offer before saying yes.
type AcceptEdit struct {
	// Name renames a new place before it is created.
	Name string `json:"name,omitempty"`
	// ChatIDs narrows a group to the chats the person kept.
	ChatIDs []string `json:"chatIds,omitempty"`
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

func clusterFingerprint(ids []string) string { return "cluster|" + strings.Join(ids, "|") }

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
	folder, said := cleanFolder(chat.Workspace), chatWords(chat)
	if folder == "" && len(said) < minChatWords {
		return nil, nil
	}
	exclude := map[string]bool{}
	for _, m := range snap.PlacesOf(chat.ChatID) {
		exclude[m.PlaceID] = true
	}
	lib := libraryIndex(library)
	cands := rankCandidates(snap, folder, said, exclude, lib)
	if len(cands) > pol.MaxCandidates {
		cands = cands[:pol.MaxCandidates]
	}
	pick, conf, basis := -1, 0, ""
	switch {
	case len(cands) == 0:
		// Nothing to choose from: weighed, and nothing to say.
	case cands[0].basis == BasisFolder:
		pick, conf, basis = 0, cands[0].confidence, BasisFolder
	case r.Ask == nil:
		// Words alone never reach an offer; without a model there is no more
		// to learn, and the chat is left to be weighed if one is connected.
		return nil, nil
	default:
		reserved, err := r.reserveCall(string(roles.RolePlaceFile), pol.FilingCallsPerDay, 0, func(st *ledgerState) bool {
			if _, done := st.Considered[chat.ChatID]; done {
				return false
			}
			st.Considered[chat.ChatID] = r.now()
			return true
		})
		if err != nil || !reserved {
			return nil, err
		}
		answer, err := r.Ask(ctx, fileQuestion(snap, chat, cands))
		if err != nil {
			// The engine could not answer: un-weigh the chat so a later reply
			// may try again. The call is still counted; it may have cost.
			_ = r.Ledger.update(r.now(), func(st *ledgerState) error { delete(st.Considered, chat.ChatID); return nil })
			return nil, err
		}
		i, c, err := readFileAnswer(answer, len(cands))
		if err != nil {
			return nil, err
		}
		pick, conf, basis = i, c, BasisModel
	}
	var prop *Proposal
	if pick >= 0 && conf >= pol.MinConfidence {
		place := cands[pick].place
		reason := "Looks like the work in " + place.Name
		if basis == BasisFolder {
			reason = "Same folder as " + place.Name
		}
		prop = &Proposal{ID: id, Kind: ProposalFile, Status: StatusPending, ChatIDs: []string{chat.ChatID}, PlaceID: place.ID,
			Confidence: conf, Basis: basis, Reason: reason, Revision: snap.Revision, CreatedAt: r.now()}
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
	var errs []error
	if pol.MergeOffers {
		for _, p := range r.mergeOffers(snap, st, pol) {
			if room <= 0 {
				break
			}
			fresh = append(fresh, p)
			room--
		}
	}
	if pol.ClusterOffers && room > 0 {
		lib := libraryIndex(library)
		var ids []string
		for _, c := range library {
			if c.Replies >= 1 {
				ids = append(ids, c.ChatID)
			}
		}
		var loose []ChatEvidence
		for _, id := range snap.Unplaced(ids) {
			c := lib[id]
			if cleanFolder(c.Workspace) != "" || len(chatWords(c)) >= minChatWords {
				loose = append(loose, c)
			}
		}
		if len(loose) >= pol.MinClusterChats {
			for n, cl := range findClusters(loose, pol.MinClusterChats) {
				if room <= 0 || n >= maxClustersPass {
					break
				}
				if suppressedCluster(st, cl.ids(), r.now(), pol) || coveredBy(fresh, cl.ids()) {
					continue
				}
				p, err := r.clusterOffer(ctx, snap, st, pol, cl, lib)
				if err != nil {
					errs = append(errs, err)
				}
				if p != nil {
					fresh = append(fresh, *p)
					if p.Status == StatusPending {
						room--
					}
				}
			}
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
	return open, errors.Join(errs...)
}

// suppressedCluster reports whether a group mostly overlaps one already open,
// accepted, or declined or dropped within the snooze.
func suppressedCluster(st *ledgerState, ids []string, now time.Time, pol RecommendPolicy) bool {
	snooze := time.Duration(pol.DeclineSnoozeDays) * 24 * time.Hour
	for _, p := range st.Proposals {
		if p.Kind != ProposalMove && p.Kind != ProposalCreate {
			continue
		}
		switch p.Status {
		case StatusDeclined, StatusDropped:
			if now.Sub(p.DecidedAt) >= snooze {
				continue
			}
		case StatusStale:
			continue
		}
		if overlapsHalf(p.ChatIDs, ids) {
			return true
		}
	}
	return false
}

func coveredBy(ps []Proposal, ids []string) bool {
	for _, p := range ps {
		if overlapsHalf(p.ChatIDs, ids) {
			return true
		}
	}
	return false
}

func overlapsHalf(a, b []string) bool {
	in := map[string]bool{}
	for _, x := range a {
		in[x] = true
	}
	n := 0
	for _, x := range b {
		if in[x] {
			n++
		}
	}
	return len(b) > 0 && n*2 >= len(b)
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

// clusterOffer decides what to offer for one group: an existing place, a new
// place named by rules, a new place named by the model, or nothing.
func (r *Recommender) clusterOffer(ctx context.Context, snap *Snapshot, st *ledgerState, pol RecommendPolicy, cl cluster, lib map[string]ChatEvidence) (*Proposal, error) {
	ids := cl.ids()
	id := proposalID(clusterFingerprint(ids))
	cands := rankCandidates(snap, cl.folder, cl.core, nil, lib)
	if len(cands) > pol.MaxCandidates {
		cands = cands[:pol.MaxCandidates]
	}
	moveTo := func(p Place, conf int, basis string, chats []string) *Proposal {
		return &Proposal{ID: proposalID(clusterFingerprint(chats)), Kind: ProposalMove, Status: StatusPending, ChatIDs: chats, PlaceID: p.ID,
			Confidence: conf, Basis: basis, Reason: fmt.Sprintf("%d chats look like the work in %s", len(chats), p.Name),
			Revision: snap.Revision, CreatedAt: r.now()}
	}
	// Reuse first.
	if len(cands) > 0 && cands[0].confidence >= pol.MinConfidence {
		return moveTo(cands[0].place, cands[0].confidence, cands[0].basis, ids), nil
	}
	counts := countAI(snap, st.AIPlaces)
	rootOK, _ := canCreateUnder(snap, pol, counts, "")
	create := func(name, parent string, conf int, basis string, chats []string) *Proposal {
		var parents []string
		if parent != "" {
			parents = []string{parent}
		}
		for _, sib := range snap.Places {
			if !sib.Archived && strings.EqualFold(sib.Name, name) && parentKey(sib.Parents) == parentKey(parents) {
				return moveTo(sib, conf, basis, chats) // the place already exists: reuse it
			}
		}
		reason := fmt.Sprintf("%d chats about the same work", len(chats))
		if basis == BasisFolder {
			reason = fmt.Sprintf("%d chats in the %s folder", len(chats), name)
		}
		return &Proposal{ID: proposalID(clusterFingerprint(chats)), Kind: ProposalCreate, Status: StatusPending, ChatIDs: chats, Name: name, ParentID: parent,
			Confidence: conf, Basis: basis, Reason: reason, Revision: snap.Revision, CreatedAt: r.now()}
	}
	// A shared folder names itself, at the top level, with no call.
	if cl.folder != "" {
		if !rootOK {
			return nil, nil
		}
		name, err := cleanSuggestedName(filepath.Base(cl.folder))
		if err != nil {
			return nil, nil
		}
		return create(name, "", 90, BasisFolder, ids), nil
	}
	if r.Ask == nil {
		return nil, nil
	}
	var parents []Place
	for _, c := range cands {
		if ok, _ := canCreateUnder(snap, pol, counts, c.place.ID); ok {
			parents = append(parents, c.place)
		}
	}
	if len(cands) == 0 && len(parents) == 0 && !rootOK {
		return nil, nil // every answer the model could give is one the caps refuse
	}
	shown := append([]ChatEvidence(nil), cl.chats...)
	sort.SliceStable(shown, func(i, j int) bool {
		if !shown[i].UpdatedAt.Equal(shown[j].UpdatedAt) {
			return shown[i].UpdatedAt.After(shown[j].UpdatedAt)
		}
		return shown[i].ChatID < shown[j].ChatID
	})
	if len(shown) > maxClusterShown {
		shown = shown[:maxClusterShown]
	}
	q := suggestQuestion(snap, shown, cands, parents)
	reserved, err := r.reserveCall(string(q.Role), pol.ClusterCallsPerDay, time.Duration(pol.OrganizeEveryMinutes)*time.Minute, nil)
	if err != nil || !reserved {
		return nil, err
	}
	dropped := &Proposal{ID: id, Kind: ProposalCreate, Status: StatusDropped, ChatIDs: ids, Basis: BasisModel, Revision: snap.Revision, CreatedAt: r.now(), DecidedAt: r.now()}
	answer, err := r.Ask(ctx, q)
	if err != nil {
		return nil, err
	}
	sg, err := readSuggestAnswer(answer, len(shown), len(cands), len(parents))
	if err != nil {
		return dropped, err
	}
	if sg == nil || len(sg.chats) < pol.MinClusterChats || sg.confidence < pol.MinConfidence {
		return dropped, nil
	}
	var chats []string
	for _, i := range sg.chats {
		chats = append(chats, shown[i].ChatID)
	}
	sort.Strings(chats)
	if sg.use >= 0 {
		return moveTo(cands[sg.use].place, sg.confidence, BasisModel, chats), nil
	}
	parent := ""
	if sg.under >= 0 {
		parent = parents[sg.under].ID
	} else if !rootOK {
		return dropped, nil
	}
	return create(sg.name, parent, sg.confidence, BasisModel, chats), nil
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
