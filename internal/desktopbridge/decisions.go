package desktopbridge

import (
	"encoding/base64"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/decide"
	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// DecisionDoor is what the bridge needs from the engine to serve the decision
// routes. Open returns one place's ledger, the same file the engine's gate
// appends to. Undo and Dependents are the engine's own doors for reversing a
// decision and finding the work that used it. The bridge guesses neither: with
// either missing, overturn answers 501 rather than pretending to succeed.
type DecisionDoor struct {
	Open       func(placeID string) (*decide.Store, error)
	Undo       decide.UndoAction
	Dependents decide.ReadDependents
}

const (
	// decisionPage is the rows a list answers when the caller names no limit.
	decisionPage = 50
	// decisionPageMax is the P-8 cap: All never lists more than this at once.
	decisionPageMax = 200
	// statusWeek is the window the status line counts agreements over.
	statusWeek = 7 * 24 * time.Hour
)

func init() {
	registerSeamRoute(http.MethodGet, "/places/{id}/decisions", decisionsList)
	registerSeamRoute(http.MethodGet, "/places/{id}/decide-status", decideStatus)
	registerSeamRoute(http.MethodGet, "/decisions/{id}", decisionGet)
	registerSeamRoute(http.MethodPost, "/decisions/{id}/overturn", decisionOverturn)
	registerSeamRoute(http.MethodPut, "/places/{id}/decide", decideSettings)
}

// DecisionRow is one ledger row as the desktop reads it. The undo token is
// engine-private and never leaves; ByName is what a person reads for By.
type DecisionRow struct {
	ID           string             `json:"id"`
	PlaceID      string             `json:"placeId"`
	QuestionRef  decide.QuestionRef `json:"questionRef"`
	AskKind      string             `json:"askKind"`
	Action       string             `json:"action"`
	By           string             `json:"by"`
	ByName       string             `json:"byName,omitempty"`
	Because      string             `json:"because,omitempty"`
	Percent      int                `json:"percent,omitempty"`
	Stakes       string             `json:"stakes,omitempty"`
	Reversible   bool               `json:"reversible"`
	At           time.Time          `json:"at"`
	OverturnedAt *time.Time         `json:"overturnedAt,omitempty"`
}

type decisionPageBody struct {
	Decisions []DecisionRow `json:"decisions"`
	// Next is the cursor for the following page, absent on the last one.
	Next string `json:"next,omitempty"`
}

// DecideStatusBody is the body the Home status line reads.
type DecideStatusBody struct {
	Mode       string          `json:"mode"`
	AgreedWeek *int            `json:"agreedWeek,omitempty"`
	TotalWeek  *int            `json:"totalWeek,omitempty"`
	Learning   *learningStatus `json:"learning,omitempty"`
}

type learningStatus struct {
	Kind   string `json:"kind,omitempty"`
	Agreed int    `json:"agreed"`
	Of     int    `json:"of"`
}

// decisionsPlaces returns the bridge's places once the decision door is set.
func (b *Bridge) decisionsPlaces() *Places {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.places
}

func (p *Places) row(snap *placegraph.Snapshot, d decide.Decision) DecisionRow {
	row := DecisionRow{ID: d.ID, PlaceID: d.PlaceID, QuestionRef: d.QuestionRef, AskKind: d.AskKind,
		Action: d.Action, By: d.By, Because: d.Because, Percent: d.Percent, Stakes: d.Stakes,
		Reversible: d.Reversible, At: d.At, OverturnedAt: d.OverturnedAt}
	if by, ok := snap.Place(d.By); ok {
		row.ByName = by.Name
	}
	return row
}

// newestFirst orders the ledger by time, then id, so a cursor is a stable key
// even when two decisions share a clock tick.
func newestFirst(ds []decide.Decision) {
	sort.SliceStable(ds, func(i, j int) bool {
		if !ds[i].At.Equal(ds[j].At) {
			return ds[i].At.After(ds[j].At)
		}
		return ds[i].ID > ds[j].ID
	})
}

func encodeCursor(d decide.Decision) string {
	return base64.RawURLEncoding.EncodeToString([]byte(d.At.UTC().Format(time.RFC3339Nano) + "|" + d.ID))
}

func decodeCursor(c string) (time.Time, string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, "", false
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return time.Time{}, "", false
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	return t, id, err == nil
}

// ledger opens one place's ledger or answers why it cannot. A place the graph
// does not know is 404; a door that was never set is the same empty slot as
// before, 501.
func (p *Places) ledger(w http.ResponseWriter, id string) (*placegraph.Snapshot, *decide.Store, bool) {
	if p == nil || p.Decisions == nil || p.Decisions.Open == nil {
		fail(w, 501, seamNotImplemented)
		return nil, nil, false
	}
	snap, err := p.Store.Snapshot()
	if err != nil {
		p.failStore(w, err, "", nil)
		return nil, nil, false
	}
	if _, found := snap.Place(id); !found {
		p.failStore(w, placegraph.ErrNotFound, "", nil)
		return nil, nil, false
	}
	store, err := p.Decisions.Open(id)
	if err != nil || store == nil {
		fail(w, 500, "The decision log couldn't be read.")
		return nil, nil, false
	}
	return snap, store, true
}

func decisionsList(b *Bridge, w http.ResponseWriter, r *http.Request, ids map[string]string) {
	p := b.decisionsPlaces()
	snap, store, ok := p.ledger(w, ids["id"])
	if !ok {
		return
	}
	limit := decisionPage
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > decisionPageMax {
			fail(w, 400, "limit must be a whole number from 1 to "+strconv.Itoa(decisionPageMax))
			return
		}
		limit = n
	}
	var cutAt time.Time
	var cutID string
	cursor := r.URL.Query().Get("cursor")
	if cursor != "" {
		var valid bool
		if cutAt, cutID, valid = decodeCursor(cursor); !valid {
			fail(w, 400, "That cursor isn't one this log gave out.")
			return
		}
	}
	all, err := store.List()
	if err != nil {
		fail(w, 500, "The decision log couldn't be read.")
		return
	}
	newestFirst(all)
	out := decisionPageBody{Decisions: []DecisionRow{}}
	for _, d := range all {
		if cursor != "" && !(d.At.Before(cutAt) || d.At.Equal(cutAt) && d.ID < cutID) {
			continue
		}
		if len(out.Decisions) == limit {
			out.Next = encodeCursor(out.Decisions[limit-1].asDecision())
			break
		}
		out.Decisions = append(out.Decisions, p.row(snap, d))
	}
	write(w, out)
}

// asDecision is just enough of a row to build a cursor from it.
func (r DecisionRow) asDecision() decide.Decision { return decide.Decision{ID: r.ID, At: r.At} }

// decideStatus summarises one place for the Home status line. The mode is the
// place's own: always-ask when its effective setting says so, deciding when any
// kind has graduated, learning when any kind has history, none otherwise, and
// none draws no line (the emptiness law).
func decideStatus(b *Bridge, w http.ResponseWriter, _ *http.Request, ids map[string]string) {
	p := b.decisionsPlaces()
	snap, store, ok := p.ledger(w, ids["id"])
	if !ok {
		return
	}
	all, err := store.List()
	if err != nil {
		fail(w, 500, "The decision log couldn't be read.")
		return
	}
	out := DecideStatusBody{Mode: "none"}
	since := p.now().Add(-statusWeek)
	total, overturned := 0, 0
	for _, d := range all {
		if d.At.Before(since) {
			continue
		}
		total++
		if d.OverturnedAt != nil {
			overturned++
		}
	}
	if total > 0 {
		agreed := total - overturned
		out.TotalWeek, out.AgreedWeek = &total, &agreed
	}
	states, err := store.Modes()
	if err != nil {
		fail(w, 500, "The decision log couldn't be read.")
		return
	}
	deciding, learning, best := false, false, learningStatus{Of: decide.RingSize}
	for key, st := range states {
		switch st.Mode {
		case decide.ModeDeciding:
			deciding = true
		case decide.ModeLearning:
			if len(st.Recent) > 0 {
				learning = true
			}
			// The ring nearest graduation is the one worth a count; ties go to
			// the first key so the answer does not flicker between reads.
			if n := decide.Agreements(st); n > best.Agreed || n == best.Agreed && n > 0 && key < best.Kind {
				best.Agreed, best.Kind = n, key
			}
		}
	}
	switch settings, _ := snap.EffectiveDecide(ids["id"]); {
	case settings.AlwaysAsk:
		out.Mode = "always-ask"
	case deciding:
		out.Mode = "deciding"
	case learning:
		out.Mode = "learning"
	}
	if out.Mode == "learning" && best.Agreed > 0 {
		out.Learning = &best
	}
	write(w, out)
}

// findDecision resolves a decision id to its place's ledger. The id is
// "<place>:<token>" and a place id may itself hold a colon, so every prefix that
// names a known place is tried.
func (p *Places) findDecision(w http.ResponseWriter, id string) (*placegraph.Snapshot, *decide.Store, decide.Decision, bool) {
	if p == nil || p.Decisions == nil || p.Decisions.Open == nil {
		fail(w, 501, seamNotImplemented)
		return nil, nil, decide.Decision{}, false
	}
	snap, err := p.Store.Snapshot()
	if err != nil {
		p.failStore(w, err, "", nil)
		return nil, nil, decide.Decision{}, false
	}
	for i, c := range id {
		if c != ':' {
			continue
		}
		if _, known := snap.Place(id[:i]); !known {
			continue
		}
		store, err := p.Decisions.Open(id[:i])
		if err != nil || store == nil {
			continue
		}
		all, err := store.List()
		if err != nil {
			continue
		}
		for _, d := range all {
			if d.ID == id {
				return snap, store, d, true
			}
		}
	}
	fail(w, 404, "That decision isn't in the log any more.")
	return nil, nil, decide.Decision{}, false
}

func decisionGet(b *Bridge, w http.ResponseWriter, _ *http.Request, ids map[string]string) {
	p := b.decisionsPlaces()
	if snap, _, d, ok := p.findDecision(w, ids["id"]); ok {
		write(w, p.row(snap, d))
	}
}

type overturnAsk struct {
	Next string `json:"next"`
}

// OverturnBody is what a reversal answers: the engine's own result, so the
// refusal sentence, the dependents and the offers are the engine's words.
type OverturnBody = decide.OverturnResult

func decisionOverturn(b *Bridge, w http.ResponseWriter, r *http.Request, ids map[string]string) {
	p := b.decisionsPlaces()
	var ask overturnAsk
	if !decode(w, r, &ask) {
		return
	}
	if ask.Next != string(decide.NextAsk) && ask.Next != string(decide.NextKeep) {
		fail(w, 400, `next must be "ask" or "keep"`)
		return
	}
	_, store, _, ok := p.findDecision(w, ids["id"])
	if !ok {
		return
	}
	if p.Decisions.Undo == nil || p.Decisions.Dependents == nil {
		fail(w, 501, seamNotImplemented)
		return
	}
	turner, err := decide.NewOverturner(store, p.Decisions.Undo, p.Decisions.Dependents)
	if err != nil {
		fail(w, 501, seamNotImplemented)
		return
	}
	result, err := turner.Overturn(r.Context(), ids["id"], decide.NextTime(ask.Next))
	switch {
	case errors.Is(err, decide.ErrNotFound):
		fail(w, 404, "That decision isn't in the log any more.")
	case errors.Is(err, decide.ErrInvalid):
		fail(w, 400, invalidSentence(err))
	case err != nil:
		fail(w, 500, "That decision couldn't be reversed. Nothing was changed.")
	case result.Refusal != "":
		fail(w, 409, result.Refusal)
	default:
		write(w, result)
	}
}

type decideAsk struct {
	IfRevision *uint64 `json:"ifRevision"`
	AlwaysAsk  bool    `json:"alwaysAsk"`
	// Threshold is omitted to keep the effective figure; Inherit drops the
	// place's own choice so it follows its ancestors again.
	Threshold *int `json:"threshold"`
	Inherit   bool `json:"inherit"`
}

type decideMutation struct {
	Mutation
	Decide placegraph.Decide `json:"decide"`
	// From is the place whose value answers, empty when it is the default.
	From string `json:"from,omitempty"`
}

func decideSettings(b *Bridge, w http.ResponseWriter, r *http.Request, ids map[string]string) {
	p := b.decisionsPlaces()
	if p == nil {
		fail(w, 501, seamNotImplemented)
		return
	}
	var ask decideAsk
	if !readBody(w, r, &ask) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.staleRevision(w, ask.IfRevision) {
		return
	}
	snap, err := p.Store.Snapshot()
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	var rc placegraph.Receipt
	if ask.Inherit {
		rc, err = p.Store.ClearDecide(ids["id"])
	} else {
		threshold := 0
		if ask.Threshold != nil {
			threshold = *ask.Threshold
		} else {
			current, _ := snap.EffectiveDecide(ids["id"])
			threshold = current.Threshold
		}
		rc, err = p.Store.SetDecide(ids["id"], placegraph.Decide{AlwaysAsk: ask.AlwaysAsk, Threshold: threshold})
	}
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	revision, err := p.Store.Revision()
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	out := decideMutation{Mutation: Mutation{Revision: revision, Generation: revision, Receipts: []placegraph.Receipt{}, Undo: []string{}, Noop: rc.Noop()}}
	if !rc.Noop() {
		out.Receipts = append(out.Receipts, rc)
		out.Undo = append(out.Undo, rc.ID)
		out.Receipt = &rc
	}
	if after, err := p.Store.Snapshot(); err == nil {
		out.Decide, out.From = after.EffectiveDecide(ids["id"])
	}
	write(w, out)
	if !rc.Noop() {
		p.publishPlaces(nil)
	}
}
