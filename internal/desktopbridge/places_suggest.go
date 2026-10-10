package desktopbridge

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

func init() {
	registerPlacesRoute("GET /places/suggestions", func(p *Places, w http.ResponseWriter, r *http.Request, _ string) {
		if !p.hasSuggestions(w) {
			return
		}
		x, ok := p.open(w, false)
		if !ok {
			return
		}
		write(w, struct {
			Suggestions []placegraph.Suggestion `json:"suggestions"`
		}{Suggestions: placegraph.Suggestions(p.staleInput(x))})
	})
	registerPlacesRoute("POST /places/suggestions/snooze", func(p *Places, w http.ResponseWriter, r *http.Request, _ string) {
		if !p.hasSuggestions(w) {
			return
		}
		var ask struct {
			PlaceID string `json:"placeId"`
		}
		if !readBody(w, r, &ask) {
			return
		}
		if ask.PlaceID == "" {
			p.failStore(w, placegraph.ErrInvalid, "", nil)
			return
		}
		snap, err := p.Store.Snapshot()
		if err != nil {
			p.failStore(w, err, "", nil)
			return
		}
		if _, ok := snap.Place(ask.PlaceID); !ok {
			p.failStore(w, placegraph.ErrNotFound, "", nil)
			return
		}
		got, err := p.Stale.Snooze(ask.PlaceID, p.now())
		if err != nil {
			p.failStore(w, err, "", nil)
			return
		}
		write(w, staleSnoozed{OK: true, PlaceID: got.PlaceID, Until: got.Until})
	})
}

// hasSuggestions keeps the offer absent when the bridge cannot remember Not now.
func (p *Places) hasSuggestions(w http.ResponseWriter) bool {
	if p.Stale == nil {
		fail(w, 404, "unknown engine action")
		return false
	}
	return true
}
