package serve

import (
	"errors"
	"math"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── MY SPACE: EACH PERSON'S OWN PLACE INSIDE THE ORGANIZATION ──────────────
//
// Inside an organization every person has one space of their own
// (org-space DESIGN §2.1, OWNER-DECISIONS §6): their pages, canvases and chats
// about the organization's work. It is a workstream marked Space, owned by the
// person and private, so its files, canvases and chats are the workstream
// machinery's, and the access core keeps it theirs alone. The rules:
//
//   - ONE PER PERSON, MADE ON FIRST READ. It has no setup, is never listed
//     with the workstreams, and is never shared, handed to someone else,
//     nested, archived or deleted.
//   - IT HOLDS NO TASKS. A task is a shared promise and belongs in a
//     workstream; what a person makes in their space moves into one.
//   - IT RUNS HERE, on the organization's key, whatever the operator chose
//     for Afi's pane (workstream_agent.go agentCannot), and everything it
//     spends is posted to it under the person.
//   - IT STOPS AT THE PERSON'S MONTHLY LIMIT, which the operator sets for
//     everyone (PUT /v1/myspace/limit). At the limit its chats take no more
//     messages and say so; work in workstreams keeps its own budget.
//   - THE OPERATOR SEES TOTALS ONLY (OWNER-DECISIONS §8): each person's spend
//     this month and how many things their space holds. Never a name, a
//     title or a word of what it says (GET /v1/myspace/totals).
//
// A solo factory has no spaces: there the person's own things are Personal's.

// spaceAtLimit is what a space's chats say once its person's month is spent.
const spaceAtLimit = "Paused: this month's limit for your space is reached. The organization's operator sets it in Settings › Organizations."

// errSpaceFixed is the refusal for anything that would share, hand on,
// nest, archive or delete a person's space.
var errSpaceFixed = errors.New("My space is yours alone: it cannot be shared, handed to someone, archived or deleted. Move what is in it into a workstream instead")

// errSpaceNoTasks is a task asked for in a person's space.
var errSpaceNoTasks = errors.New("My space holds no tasks: a task belongs in a workstream, where others can see it. Move this into a workstream, or make the task there")

func (h *Handler) mySpaceRoutes() {
	h.route("/v1/myspace", map[string]http.HandlerFunc{http.MethodGet: h.getMySpace})
	h.route("/v1/myspace/totals", map[string]http.HandlerFunc{http.MethodGet: h.mySpaceTotals})
	h.route("/v1/myspace/limit", map[string]http.HandlerFunc{http.MethodPut: h.putMySpaceLimit})
}

// spacesServed is whether this server keeps spaces: an organization's, with a
// store.
func (h *Handler) spacesServed() bool {
	return h.config.Store != nil && h.mode() != api.FactorySolo
}

// spaceLimit is the per-person monthly limit now.
func (h *Handler) spaceLimit() float64 {
	s, err := h.config.Store.MySpaceSettings()
	if err != nil || s.LimitUSD == nil {
		return api.MySpaceLimitDefaultUSD
	}
	return *s.LimitUSD
}

// monthStart is the first instant of now's calendar month, in UTC, as spend
// posts are written.
func monthStart(now time.Time) string {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
}

// spaceMonth is the limit and what space ws has spent this month.
func (h *Handler) spaceMonth(ws string) (limit, spent float64) {
	limit = h.spaceLimit()
	if by, err := h.config.Store.WorkstreamSpendSince(monthStart(h.clock())); err == nil {
		spent = by[ws]
	}
	return limit, spent
}

// spaceOf is person's space, if they have one.
func spaceOf(all factory.OrgWorkstreams, person string) (factory.OrgWorkstream, bool) {
	for _, ws := range all {
		if ws.Space && ws.Owner.Kind == api.OwnerPerson && factory.NormalizeHandle(ws.Owner.ID) == person {
			return ws, true
		}
	}
	return factory.OrgWorkstream{}, false
}

// getMySpace is GET /v1/myspace: the caller's own space, made the first time
// they ask for it.
func (h *Handler) getMySpace(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.spacesServed()) {
		return
	}
	v, err := h.wsViewer(r)
	if h.failed(w, err) {
		return
	}
	if v.Handle == "" {
		h.fail(w, http.StatusForbidden, "My space is someone's; sign in first")
		return
	}
	var space factory.OrgWorkstream
	if isBotCall(r) {
		// The bot reads its creator's existing space through the item evaluator.
		all, err := h.config.Store.Workstreams()
		if h.failed(w, err) {
			return
		}
		var found bool
		space, found = spaceOf(all, v.Handle)
		if !found || !v.canSee(space) {
			h.fail(w, http.StatusNotFound, errWorkstreamGone.Error())
			return
		}
	} else {
		space, err = h.ensureSpace(v.Handle)
		if h.failed(w, err) {
			return
		}
	}
	body, err := h.workstreamBody(space, v)
	if h.failed(w, err) {
		return
	}
	limit, spent := h.spaceMonth(space.ID)
	h.write(w, http.StatusOK, api.MySpace{Workstream: body, LimitUSD: limit, SpentUSD: roundCents(spent), AtLimit: spent >= limit})
}

// ensureSpace answers person's space, making it if they have none. Two first
// reads at once make one: the check and the write are one store edit.
func (h *Handler) ensureSpace(person string) (factory.OrgWorkstream, error) {
	all, err := h.config.Store.Workstreams()
	if err != nil {
		return factory.OrgWorkstream{}, err
	}
	if ws, ok := spaceOf(all, person); ok {
		return ws, nil
	}
	now := h.clock().UTC()
	id, err := cell.NewID(now)
	if err != nil {
		return factory.OrgWorkstream{}, err
	}
	ws := factory.OrgWorkstream{
		ID: workstreamIDPrefix + id, Name: "My space", Stage: api.WorkstreamOngoing,
		Owner: api.WorkstreamOwner{Kind: api.OwnerPerson, ID: person}, Private: true, Space: true,
		Pages: []factory.WorkstreamPage{}, Decisions: []factory.WorkstreamDecision{},
		Sources: []factory.WorkstreamSource{}, Links: []string{}, Shares: []factory.Share{},
		CreatedBy: person, CreatedAt: now, UpdatedAt: now, Steward: person,
	}
	var made factory.OrgWorkstream
	saved, err := h.config.Store.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		if cur, ok := spaceOf(all, person); ok {
			made = cur
			return nil, nil
		}
		all[ws.ID] = ws
		return []string{ws.ID}, nil
	})
	if err != nil {
		return factory.OrgWorkstream{}, err
	}
	if made.ID != "" {
		return made, nil
	}
	return saved[ws.ID], nil
}

// spaceItems counts what a space holds, for the operator's totals: its live
// files and documents (folders are not things), canvases filed in it that no
// file already stands for, and its person's chats. Their agents are added by
// the caller (org_agents.go).
func (h *Handler) spaceItems(ws factory.OrgWorkstream, canvases factory.OrgCanvases) int {
	n := 0
	filed := map[string]bool{}
	if files, err := h.config.Store.WorkstreamFiles(ws.ID); err == nil {
		for _, f := range files {
			if f.DeletedAt != nil || f.Kind == factory.FileFolder {
				continue
			}
			if f.CanvasID != "" {
				filed[f.CanvasID] = true
			}
			n++
		}
	}
	for id, c := range canvases {
		if c.Workstream == ws.ID && !filed[id] {
			n++
		}
	}
	if entries, err := os.ReadDir(h.chatsDir(ws.ID, factory.NormalizeHandle(ws.Owner.ID))); err == nil {
		for _, e := range entries {
			if e.IsDir() && chatIDPattern.MatchString(e.Name()) {
				n++
			}
		}
	}
	return n
}

// mySpaceTotals is GET /v1/myspace/totals, the operator's: every person's
// space as spend this month and a count. Nothing in it names a thing.
func (h *Handler) mySpaceTotals(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.spacesServed()) {
		return
	}
	if !h.isOperator(r) {
		h.fail(w, http.StatusForbidden, "only the organization's operator sees what each person's space spends")
		return
	}
	all, err := h.config.Store.Workstreams()
	if h.failed(w, err) {
		return
	}
	now := h.clock()
	spent, err := h.config.Store.WorkstreamSpendSince(monthStart(now))
	if h.failed(w, err) {
		return
	}
	canvases, _ := h.config.Store.Canvases()
	agents, _ := h.config.Store.OrgAgents()
	agentsOf := map[string]int{}
	for _, a := range agents {
		agentsOf[factory.NormalizeHandle(a.Owner)]++
	}
	limit := h.spaceLimit()
	nameOf := h.personNames()
	out := api.MySpaceTotals{Month: now.UTC().Format("2006-01"), LimitUSD: limit, People: []api.MySpacePerson{}}
	for _, ws := range all {
		if !ws.Space {
			continue
		}
		person := factory.NormalizeHandle(ws.Owner.ID)
		row := api.MySpacePerson{Person: person, SpentUSD: roundCents(spent[ws.ID]), Items: h.spaceItems(ws, canvases) + agentsOf[person], AtLimit: spent[ws.ID] >= limit}
		if name := nameOf(person); name != person {
			row.Name = name
		}
		out.People = append(out.People, row)
	}
	slices.SortFunc(out.People, func(a, b api.MySpacePerson) int {
		if a.SpentUSD != b.SpentUSD {
			if a.SpentUSD > b.SpentUSD {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Person, b.Person)
	})
	h.write(w, http.StatusOK, out)
}

// putMySpaceLimit is PUT /v1/myspace/limit, the operator's: the monthly limit
// for every person's space.
func (h *Handler) putMySpaceLimit(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.spacesServed()) {
		return
	}
	if !h.isOperator(r) {
		h.fail(w, http.StatusForbidden, "only the organization's operator sets the limit for everyone's space")
		return
	}
	var body api.MySpaceLimitPut
	if !h.decode(w, r, &body, false) {
		return
	}
	if math.IsNaN(body.LimitUSD) || body.LimitUSD < 0 || body.LimitUSD > api.MySpaceLimitMostUSD {
		h.fail(w, http.StatusUnprocessableEntity, "a space's monthly limit is 0 to 10000 US dollars")
		return
	}
	limit := roundCents(body.LimitUSD)
	if _, err := h.config.Store.ChangeMySpaceSettings(func(s *store.MySpaceSettings) error {
		s.LimitUSD = &limit
		return nil
	}); h.failed(w, err) {
		return
	}
	h.recordOrg(r, record.SpendingChanged, "set space limit", orgObjectFactory, strconv.FormatFloat(limit, 'f', 2, 64)+" USD")
	h.mySpaceTotals(w, r)
}

// roundCents keeps a dollar amount to the cent.
func roundCents(usd float64) float64 { return math.Round(usd*100) / 100 }

// spaceRefused writes the refusal for a change no space takes, answering
// whether it did.
func (h *Handler) spaceRefused(w http.ResponseWriter, ws factory.OrgWorkstream) bool {
	if !ws.Space {
		return false
	}
	h.fail(w, http.StatusUnprocessableEntity, errSpaceFixed.Error())
	return true
}
