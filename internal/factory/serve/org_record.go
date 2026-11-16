package serve

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// ── WHO CHANGED THE ORGANIZATION ───────────────────────────────────────────
//
// Every change to the organization itself (a team, a workstream, a go-ahead
// policy, the factory's rules, routes, spending defaults and settings) is
// written to the factory's record, the same append-only history a job's
// decisions go to, with the person the server resolved from the sign-in as
// its actor. NO CLIENT FIELD EVER NAMES THE ACTOR. A change is recorded only
// after it was saved, and a record that cannot be written is not a failure of
// the change: the change already happened, and refusing it now would only
// tell the person it did not.
//
// A change to one workstream (or the go-ahead policy of one) is part of that
// workstream: the audit list and its live push show it only to those who may
// see the workstream (orgRecordVisible), and GET /v1/workstreams/{id}/activity
// lists them.

// orgObject prefixes, so the visibility check and the activity route agree.
const (
	orgObjectTeam       = "team:"
	orgObjectWorkstream = "workstream:"
	orgObjectPolicy     = "policy:"
	orgObjectFactory    = "factory"
)

// recordOrg appends one organization change made by the caller. It is a
// no-op on a factory without a record and on a caller with no handle (the
// host's own token on a solo factory), whose change still stands.
func (h *Handler) recordOrg(r *http.Request, kind, verb, object, detail string) {
	if h.config.Record == nil {
		return
	}
	who := h.orgActor(r)
	if who == "" {
		return
	}
	_, _ = h.config.Record.Append(r.Context(), record.Event{Kind: kind, Verb: verb, Object: object,
		Handle: who, Detail: strings.TrimSpace(detail),
		Actor: record.Actor{Kind: "person", Name: "@" + who, Operator: operatorOnly[kind]}})
}

// operatorOnly are the kinds only the operator may make, so their actor is
// marked as acting as operator. A workstream or a go-ahead is changed through
// a person's own rights on it, whoever they are.
var operatorOnly = map[string]bool{record.TeamChanged: true, record.RulesChanged: true,
	record.RoutesChanged: true, record.SpendingChanged: true, record.SettingsChanged: true}

// orgActor is the handle the caller is, or "" when there is none. The
// operator's host token is the claimed operator's login.
func (h *Handler) orgActor(r *http.Request) string {
	who, err := h.callerHandle(r)
	if err == nil {
		if who = factory.NormalizeHandle(who); who != "" {
			return who
		}
	}
	if h.isOperator(r) {
		return factory.NormalizeHandle(h.operatorLogin(r))
	}
	return ""
}

// recordWorkstream is one change to workstream id.
func (h *Handler) recordWorkstream(r *http.Request, id, verb, detail string) {
	h.recordOrg(r, record.WorkstreamChanged, verb, orgObjectWorkstream+id, detail)
}

// recordTeam is one change to team id.
func (h *Handler) recordTeam(r *http.Request, id, verb, detail string) {
	h.recordOrg(r, record.TeamChanged, verb, orgObjectTeam+id, detail)
}

// changedFields names the fields a workstream patch sent, in a fixed order,
// for the record's detail; the content itself is never copied into the
// record, which every member of the organization can read.
func changedFields(names ...string) string {
	return strings.Join(names, ", ")
}

// orgRecordVisible filters out the changes to workstreams the caller may not
// see. Every other organization change (teams, the factory's own rules) is
// read by every member, the same as the things themselves.
func (h *Handler) orgRecordVisible(r *http.Request, events []record.Event) []record.Event {
	seen := map[string]bool{}
	out := events[:0:0]
	for _, e := range events {
		id := orgRecordWorkstream(e)
		if id == "" {
			out = append(out, e)
			continue
		}
		ok, cached := seen[id]
		if !cached {
			known, see, _, err := h.orgWorkstreamAccess(r, id)
			ok = err == nil && known && see
			// A workstream that is gone from the store (purged), or a caller
			// the workstream rules cannot place, is the operator's alone to
			// read about: the operator removed it, and still answers for it.
			if err != nil || !known {
				ok = h.isOperator(r)
			}
			seen[id] = ok
		}
		if ok {
			out = append(out, e)
		}
	}
	return out
}

// orgRecordWorkstream is the workstream an organization change belongs to,
// or "".
func orgRecordWorkstream(e record.Event) string {
	if e.Job != 0 {
		return ""
	}
	if id, ok := strings.CutPrefix(e.Object, orgObjectWorkstream); ok {
		return id
	}
	if id, ok := strings.CutPrefix(e.Object, orgObjectPolicy); ok && strings.HasPrefix(id, workstreamIDPrefix) {
		return id
	}
	return ""
}

// workstreamActivityMost is the most changes one activity read answers: the
// newest ones, in the order they happened.
const workstreamActivityMost = 500

// workstreamActivity answers GET /v1/workstreams/{id}/activity: every
// recorded change to the workstream and its go-aheads, oldest first, with the
// people who made them. A workstream the caller cannot see is 404, as its own
// route answers. ?since=<RFC3339> and ?limit=<n> narrow it.
func (h *Handler) workstreamActivity(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	var since time.Time
	if words := r.URL.Query().Get("since"); words != "" {
		var err error
		if since, err = time.Parse(time.RFC3339Nano, words); err != nil {
			h.fail(w, http.StatusUnprocessableEntity, "since must be an RFC3339 timestamp")
			return
		}
	}
	limit := workstreamActivityMost
	if words := r.URL.Query().Get("limit"); words != "" {
		n, err := strconv.Atoi(words)
		if err != nil || n < 1 {
			h.fail(w, http.StatusUnprocessableEntity, "limit must be a positive number")
			return
		}
		limit = min(n, workstreamActivityMost)
	}
	events := []record.Event{}
	if h.config.Record != nil {
		all, err := h.config.Record.Since(r.Context(), since)
		if h.failed(w, err) {
			return
		}
		for _, e := range all {
			if orgRecordWorkstream(e) == ws.ID {
				events = append(events, e)
			}
		}
	}
	if len(events) > limit {
		events = events[len(events)-limit:]
	}
	actors := make([]string, 0, len(events))
	for _, e := range events {
		actors = append(actors, e.Handle)
	}
	h.write(w, http.StatusOK, struct {
		ID     string                           `json:"id"`
		Events []record.Event                   `json:"events"`
		People map[string]factory.PersonSummary `json:"people,omitempty"`
	}{ws.ID, events, h.signedInPeople(actors)})
}
