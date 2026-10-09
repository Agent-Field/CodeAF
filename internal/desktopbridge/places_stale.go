package desktopbridge

// The untouched-place suggestion's two routes (design 6d "Too many places"):
//
//	GET  /places/stale             the places to offer for merging or archiving
//	POST /places/{id}/stale-snooze "Not now": hide that place's offer for 30 days
//
// The rule is placegraph.StalePlaces and it is date arithmetic on this bridge's
// clock; no model is called. A snooze is written to the shared snooze file, so
// every window sees it. Archiving and merging are NOT here: the offer's buttons
// use the existing archive and merge routes, with their receipts and Undo.
//
// WITHOUT A StaleBook THE ROUTES ARE ABSENT (404), not failing: a bridge that
// cannot remember a snooze must not offer a line it cannot honour.

import (
	"net/http"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

type staleAnswer struct {
	Revision   uint64                  `json:"revision"`
	ReadAt     time.Time               `json:"readAt"`
	AfterDays  int                     `json:"afterDays"`
	SnoozeDays int                     `json:"snoozeDays"`
	Places     []placegraph.StalePlace `json:"places"`
}

// staleList answers with every place due for a suggestion, longest untouched
// first. The screens draw the first and fetch again after acting on it.
func (p *Places) staleList(w http.ResponseWriter) {
	x, ok := p.open(w, false)
	if !ok {
		return
	}
	activity := map[string]time.Time{}
	for id := range x.incl {
		for _, chat := range x.incl[id] {
			if r := x.rows[chat]; r != nil && r.At.After(activity[id]) {
				activity[id] = r.At
			}
		}
	}
	list := placegraph.StalePlaces(placegraph.StaleInput{
		Snap: x.snap, Now: x.now, Activity: activity, Snoozed: p.Stale.Active(x.now),
		Busy: func(id string) bool {
			s := x.rollup(x.incl[id])
			return s.Running > 0 || s.NeedsYou > 0
		},
	})
	write(w, staleAnswer{Revision: x.snap.Revision, ReadAt: x.now, AfterDays: placegraph.StaleAfterDays, SnoozeDays: placegraph.StaleSnoozeDays, Places: list})
}

type staleSnoozed struct {
	OK      bool      `json:"ok"`
	PlaceID string    `json:"placeId"`
	Until   time.Time `json:"until"`
}

func (p *Places) staleSnooze(w http.ResponseWriter, r *http.Request, id string) {
	var ask plainAsk
	if !readBody(w, r, &ask) {
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
	got, err := p.Stale.Snooze(id, p.now())
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	write(w, staleSnoozed{OK: true, PlaceID: got.PlaceID, Until: got.Until})
}
