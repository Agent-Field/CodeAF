package desktopbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// placesPolicyRoutes serves how codeaf offers places and the offers it made:
//
//	GET  /places/policy                    the settings rows, in page order
//	PUT  /places/policy/{key}              one setting; {value: null} resets it
//	GET  /places/proposals                 the open offers (NO MODEL CALL)
//	POST /places/proposals/organize        queue one background pass
//	POST /places/proposals/{id}/accept     {name?, chatIds?, offerVersion?}
//	POST /places/proposals/{id}/decline    "Not now"
//
// It reports whether the path was one of its own, and it is asked BEFORE the
// places door, which would otherwise read "policy" and "proposals" as place
// ids. The policy lives in the same profile the model roles do; the offers
// live in the Recommender's ledger beside the graph (places_advice.go).
func (b *Bridge) placesPolicyRoutes(w http.ResponseWriter, r *http.Request, path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] != "places" || (parts[1] != "policy" && parts[1] != "proposals") {
		return false
	}
	b.mu.Lock()
	models, advice, places := b.models, b.advice, b.places
	b.mu.Unlock()
	if parts[1] == "policy" {
		if models == nil {
			failPlaces(w, 404, "not_found", "This engine has no places settings.")
			return true
		}
		policyRoute(w, r, models.ProfileDir, parts[2:])
		return true
	}
	if advice == nil || places == nil {
		failPlaces(w, 404, "not_found", "This engine makes no place offers.")
		return true
	}
	advice.serve(w, r, places, parts[2:])
	return true
}

func policyRoute(w http.ResponseWriter, r *http.Request, profileDir string, rest []string) {
	switch len(rest) {
	case 0:
		if needGet(w, r) {
			write(w, map[string]any{"settings": config.DesktopPlacesSettings(profileDir)})
		}
	case 1:
		if r.Method != http.MethodPut {
			fail(w, 405, "PUT required")
			return
		}
		var ask struct {
			Value json.RawMessage `json:"value"`
		}
		if !readBody(w, r, &ask) {
			return
		}
		if err := config.WriteDesktopPlacesSetting(profileDir, rest[0], ask.Value); err != nil {
			if errors.Is(err, placegraph.ErrInvalid) {
				failPlaces(w, 400, "invalid", invalidSentence(err))
				return
			}
			failPlaces(w, 500, "store", "That setting couldn't be saved. Nothing was changed.")
			return
		}
		view, ok := config.DesktopPlacesSetting(profileDir, rest[0])
		if !ok {
			failPlaces(w, 404, "not_found", "There is no such setting.")
			return
		}
		write(w, view)
	default:
		fail(w, 404, "unknown engine action")
	}
}

// ProposalView is one open offer as the window reads it: the Recommender's
// record and an opaque version of its content, which an accept may echo so a
// window holding an older reading of the offer is refused rather than obeyed.
type ProposalView struct {
	placegraph.Proposal
	OfferVersion string `json:"offerVersion"`
}

// ProposalsView is what the Home page and the Place rail read.
type ProposalsView struct {
	Proposals []ProposalView `json:"proposals"`
	// Organizing is a background pass queued or running.
	Organizing bool `json:"organizing"`
	// Engine says whether an open conversation can carry a model question.
	// False is not an error: rules-only offers still appear.
	Engine EngineAsks `json:"engine"`
	// Recovered is the sentence for a damaged ledger that was set aside.
	Recovered string `json:"recovered,omitempty"`
	// LastAsk is the last model ask made for places, and which model answered.
	LastAsk *AskRecord `json:"lastAsk,omitempty"`
}

// EngineAsks is whether the offers can use a model right now, and why not.
type EngineAsks struct {
	Asks bool `json:"asks"`
	// Via is "conversation" (an open window) or "saved" (a saved conversation
	// opened for reading only, when no window is open).
	Via    string `json:"via,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// offerVersion fingerprints everything accepting an offer would act on.
func offerVersion(p placegraph.Proposal) string {
	h := sha256.New()
	for _, part := range []string{p.ID, string(p.Kind), p.PlaceID, p.FromPlaceID, p.Name, p.ParentID,
		strings.Join(p.ChatIDs, ","), p.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z")} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// proposalIDPattern is the shape placegraph mints; anything else is refused
// before the ledger is opened.
var proposalIDPattern = regexp.MustCompile(`^prop_[0-9a-f]{16}$`)

func (a *PlaceAdvice) view(rec *placegraph.Recommender) (ProposalsView, error) {
	pending, err := rec.Pending()
	if err != nil {
		return ProposalsView{}, err
	}
	out := ProposalsView{Proposals: make([]ProposalView, 0, len(pending)), Organizing: a.organizing(), Recovered: a.Ledger.LastRecovery()}
	for _, p := range pending {
		out.Proposals = append(out.Proposals, ProposalView{Proposal: p, OfferVersion: offerVersion(p)})
	}
	switch {
	case a.b.askingConversation() != nil:
		out.Engine.Asks, out.Engine.Via = true, "conversation"
	case a.Detached != nil && a.backgroundFile() != "":
		if file := a.backgroundFile(); a.recentlyFailed(file) {
			out.Engine.Reason = "codeaf couldn't open a saved conversation to ask its model. It will try again in a few minutes; until then only folder matches are offered."
		} else {
			out.Engine.Asks, out.Engine.Via = true, "saved"
		}
	default:
		out.Engine.Reason = "There's no conversation yet for codeaf to ask its model through; only folder matches are offered."
	}
	a.mu.Lock()
	if a.lastAsk != nil {
		last := *a.lastAsk
		out.LastAsk = &last
	}
	a.mu.Unlock()
	return out, nil
}

func (a *PlaceAdvice) serve(w http.ResponseWriter, r *http.Request, places *Places, rest []string) {
	// Reading and deciding never ask a model, so their Recommender has no door.
	rec := a.recommender(nil)
	switch {
	case len(rest) == 0:
		if needGet(w, r) {
			a.writeView(w, 200, rec)
		}
	case len(rest) == 1 && rest[0] == "organize":
		if needPost(w, r) && readBody(w, r, &struct{}{}) {
			status := 200
			if a.requestOrganize() {
				status = 202
			}
			a.writeView(w, status, rec)
		}
	case len(rest) == 2 && (rest[1] == "accept" || rest[1] == "decline"):
		if !needPost(w, r) {
			return
		}
		id := rest[0]
		if !proposalIDPattern.MatchString(id) {
			failPlaces(w, 400, "invalid", "That isn't an offer.")
			return
		}
		if rest[1] == "decline" {
			if !readBody(w, r, &struct{}{}) {
				return
			}
			if err := rec.Decline(id); err != nil {
				failOffer(w, err)
				return
			}
			a.writeView(w, 200, rec)
			return
		}
		a.accept(w, r, places, rec, id)
	default:
		fail(w, 404, "unknown engine action")
	}
}

func (a *PlaceAdvice) accept(w http.ResponseWriter, r *http.Request, places *Places, rec *placegraph.Recommender, id string) {
	var ask struct {
		Name         string   `json:"name"`
		ChatIDs      []string `json:"chatIds"`
		OfferVersion string   `json:"offerVersion"`
	}
	if !readBody(w, r, &ask) {
		return
	}
	if ask.OfferVersion != "" {
		pending, err := rec.Pending()
		if err != nil {
			failOffer(w, err)
			return
		}
		found := false
		for _, p := range pending {
			if p.ID == id {
				found = true
				if offerVersion(p) != ask.OfferVersion {
					failPlaces(w, 409, "stale_offer", "That offer changed since you saw it. Look at it again.")
					return
				}
			}
		}
		if !found {
			failOffer(w, placegraph.ErrProposalGone)
			return
		}
	}
	// The places door serialises its own read-modify-write changes with this
	// lock; an accept is several store verbs and is held to the same order.
	places.mu.Lock()
	result, err := rec.Accept(id, placegraph.AcceptEdit{Name: strings.TrimSpace(ask.Name), ChatIDs: ask.ChatIDs})
	places.mu.Unlock()
	if err != nil {
		failOffer(w, err)
		return
	}
	view, err := a.view(rec)
	if err != nil {
		failOffer(w, err)
		return
	}
	write(w, map[string]any{"proposal": result.Proposal, "placeId": result.PlaceID, "receipts": result.Receipts, "view": view})
}

func (a *PlaceAdvice) writeView(w http.ResponseWriter, status int, rec *placegraph.Recommender) {
	view, err := a.view(rec)
	if err != nil {
		failOffer(w, err)
		return
	}
	writeStatus(w, status, view)
}

// failOffer turns a Recommender or store error into the window's sentence.
func failOffer(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, placegraph.ErrProposalGone):
		failPlaces(w, 409, "gone", "That offer is no longer open. Your places may have changed since it was made.")
	case errors.Is(err, placegraph.ErrPolicyLimit):
		failPlaces(w, 409, "policy_limit", "codeaf can't create another place there under your Places settings.")
	default:
		status, code, sentence := storeFailure(err, "")
		failPlaces(w, status, code, sentence)
	}
}
