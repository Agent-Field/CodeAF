package serve

import (
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

// ── A WORKSTREAM'S STRUCTURE ON THE SERVER (Workstreams v5, SPEC §2.1) ─────
//
// Nesting, the steward, people and rules are kept on the workstream itself
// (factory/workstream_structure.go). This file is the server's half:
//
//   - the people list a workstream answers, folded from its owner and shares;
//   - who may change what: ONLY THE STEWARD CHANGES THE RULES OR HANDS THE
//     WORKSTREAM ON, and the steward governs its people beside those who
//     always could (its owner, its owning team, the operator);
//   - a child's people are its parent's and are refused on the child;
//   - the rule "start without asking" written into the go-ahead policy kept
//     under the workstream's id, so the Factory honours it by itself;
//   - what a child could leave in its parent when it is archived, and folding
//     a child back into a heading in its parent.
//
//	GET  /v1/workstreams/{id}/archive/keep -> WorkstreamArchiveKeep
//	POST /v1/workstreams/{id}/fold         -> WorkstreamFolded

func (h *Handler) workstreamStructureRoutes() {
	h.route("/v1/workstreams/{id}/archive/keep", map[string]http.HandlerFunc{http.MethodGet: h.workstreamArchiveKeep})
	h.route("/v1/workstreams/{id}/fold", map[string]http.HandlerFunc{http.MethodPost: h.foldWorkstream})
}

var (
	errNotSteward  = errors.New("only the person who looks after this workstream can change its rules or hand it on")
	errChildPeople = workstreamInvalid(factory.ErrChildPeople.Error())
)

// stewards is whether v looks after ws. On a one-person factory everyone who
// can reach it is its steward, as everyone is its owner.
func (v Viewer) stewards(ws factory.OrgWorkstream) bool {
	return v.Solo || (v.Handle != "" && v.Handle == ws.StewardHandle())
}

// withStructure adds parent and steward to a summary for v.
func withStructure(out *api.WorkstreamSummary, ws factory.OrgWorkstream, v Viewer) {
	out.Parent = ws.Parent
	if s := ws.StewardHandle(); s != "" && out.MyRole != "" {
		out.Steward = &api.RulesPerson{ID: s, Name: s}
	}
	out.MySteward = out.MyRole != "" && v.stewards(ws)
}

// workstreamPeople is ws's owner and shares as one list, owner first, then
// teams, then persons, each by name. A team carries its members as the
// organization keeps them now.
func (h *Handler) workstreamPeople(ws factory.OrgWorkstream, nameOf func(string) string) []api.WorkstreamMember {
	steward := ws.StewardHandle()
	book := h.Membership()
	member := func(kind, id, role string) api.WorkstreamMember {
		m := api.WorkstreamMember{Kind: kind, ID: id, Name: id, Role: role, AddedAt: ws.PeopleAdded[kind+":"+id], AddedBy: ws.PeopleAddedBy[kind+":"+id]}
		if kind == api.MemberTeam {
			m.Name = h.wsTeamName(id)
			m.Members = book.Members(id)
		} else {
			m.Name = nameOf(id)
			m.Steward = factory.NormalizeHandle(id) == steward
			m.Invited = factory.InvitedPerson(id)
		}
		return m
	}
	out := []api.WorkstreamMember{}
	if ws.Owner.ID != "" {
		out = append(out, member(ws.Owner.Kind, ws.Owner.ID, factory.RoleFull))
	}
	shares := slices.Clone(ws.Shares)
	sort.SliceStable(shares, func(i, j int) bool {
		if shares[i].Kind != shares[j].Kind {
			return shares[i].Kind == factory.ShareToTeam
		}
		return shares[i].ID < shares[j].ID
	})
	for _, s := range shares {
		if s.Kind == ws.Owner.Kind && s.ID == ws.Owner.ID {
			continue
		}
		out = append(out, member(s.Kind, s.ID, s.Role))
	}
	// THE STEWARD IS ALWAYS IN IT. One who reaches it only through a team is
	// still named, so the sheet can say who looks after it.
	if steward != "" && !slices.ContainsFunc(out, func(m api.WorkstreamMember) bool { return m.Steward }) {
		out = append(out, api.WorkstreamMember{Kind: api.MemberPerson, ID: steward, Name: nameOf(steward), Role: factory.RoleFull, Steward: true})
	}
	return out
}

// personNames answers a person's name as they gave it when they joined
// ("Santosh Kumar"), or their handle when the organization knows none. The
// member roster is read once per call of personNames, not once per handle,
// because on a team host it is a file read. A workstream's people, steward
// and rules name people this way, so a desk offers "Hand to Abir Das", never
// a bare handle beside full names elsewhere on the same screen.
func (h *Handler) personNames() func(string) string {
	var names map[string]string
	return func(handle string) string {
		if names == nil {
			names = map[string]string{}
			for _, m := range h.memberList() {
				if n := strings.TrimSpace(m.Name); n != "" {
					names[factory.NormalizeHandle(m.Handle)] = n
				}
			}
			// Someone invited and not yet joined goes by the name or email
			// they were invited with.
			for ref, n := range h.inviteNames() {
				names[ref] = n
			}
		}
		if n := names[factory.NormalizeHandle(handle)]; n != "" {
			return n
		}
		return handle
	}
}

// wsTeamName is a team's name, or its id when the team is gone.
func (h *Handler) wsTeamName(id string) string {
	teams, err := h.config.Store.Teams()
	if err == nil {
		for _, t := range teams {
			if t.ID == id && t.Name != "" {
				return t.Name
			}
		}
	}
	return id
}

// rulesFor is ws's rules with names filled in, for the wire.
func rulesFor(ws factory.OrgWorkstream, nameOf func(string) string) api.WorkstreamRules {
	r := ws.RulesOrDefault()
	if r.DefaultOwner.Kind == factory.RulesOwnerPerson {
		r.DefaultOwner.Name = nameOf(r.DefaultOwner.ID)
	}
	if r.Fallback != nil {
		r.Fallback.Name = nameOf(r.Fallback.ID)
	}
	return r
}

// stampPeopleAdded writes when each person or team new to next was added.
func stampPeopleAdded(next *factory.OrgWorkstream, before factory.OrgWorkstream, now time.Time, by string) {
	had := map[string]bool{before.Owner.Kind + ":" + before.Owner.ID: true}
	for _, s := range before.Shares {
		had[s.Kind+":"+s.ID] = true
	}
	added, addedBy := map[string]string{}, map[string]string{}
	for k, v := range next.PeopleAdded {
		added[k] = v
	}
	for k, v := range next.PeopleAddedBy {
		addedBy[k] = v
	}
	keys := []string{next.Owner.Kind + ":" + next.Owner.ID}
	for _, s := range next.Shares {
		keys = append(keys, s.Kind+":"+s.ID)
	}
	for _, k := range keys {
		if !had[k] {
			added[k] = now.UTC().Format(time.RFC3339)
			if by != "" {
				addedBy[k] = by
			} else {
				delete(addedBy, k)
			}
		}
	}
	for k := range added {
		if !slices.Contains(keys, k) {
			delete(added, k)
		}
	}
	for k := range addedBy {
		if !slices.Contains(keys, k) {
			delete(addedBy, k)
		}
	}
	next.PeopleAdded = added
	next.PeopleAddedBy = addedBy
	if len(addedBy) == 0 {
		next.PeopleAddedBy = nil
	}
}

// peopleOf is the handles of everyone ws reaches by name or by team now.
func (h *Handler) peopleOf(ws factory.OrgWorkstream) map[string]bool {
	out := map[string]bool{}
	book := h.Membership()
	add := func(kind, id string) {
		switch kind {
		case factory.ShareToPerson:
			out[factory.NormalizeHandle(id)] = true
		case factory.ShareToTeam:
			for _, m := range book.Members(id) {
				out[factory.NormalizeHandle(m)] = true
			}
		}
	}
	add(ws.Owner.Kind, ws.Owner.ID)
	for _, s := range ws.Shares {
		add(s.Kind, s.ID)
	}
	return out
}

// WorkstreamPeopleRemoved is told, after a save, who left a workstream (and
// every workstream inside it) and who takes their open work: the person who
// removed them, else the rules' fallback, else the steward. The task list sets it to hand their open
// tasks on; nil hands nothing.
var WorkstreamPeopleRemoved func(h *Handler, r *http.Request, ws []string, removed []string, to string)

// peopleChanged tells [WorkstreamPeopleRemoved] who left ws.
func (h *Handler) peopleChanged(r *http.Request, before, after factory.OrgWorkstream, all factory.OrgWorkstreams) {
	if WorkstreamPeopleRemoved == nil {
		return
	}
	had, has := h.peopleOf(before), h.peopleOf(after)
	var removed []string
	for p := range had {
		if !has[p] {
			removed = append(removed, p)
		}
	}
	if len(removed) == 0 {
		return
	}
	sort.Strings(removed)
	// REMOVING SOMEONE OFFERS THEIR TASKS TO THE REMOVER (build spec v2,
	// screen 16): whoever took them out is the one who knows where their work
	// goes next, and it arrives in their Inbox to hand on. A remover no longer
	// in it (they took themselves out) leaves it to the fallback, else the
	// steward, as before.
	to := after.StewardHandle()
	if rules := after.RulesOrDefault(); rules.Fallback != nil && rules.Fallback.ID != "" {
		to = rules.Fallback.ID
	}
	if who := factory.NormalizeHandle(h.orgActor(r)); who != "" && has[who] {
		to = who
	}
	ids := append([]string{after.ID}, factory.ChildrenOf(all, after.ID)...)
	WorkstreamPeopleRemoved(h, r, ids, removed, to)
}

// ── create and patch ──

// nestNew puts a new workstream inside its parent, under the store's lock:
// the parent must be top-level, open and writable by v, and the child takes
// its people (the save mirrors them) and a copy of its rules unless the body
// sent its own. The maker looks after it and owns its incoming work.
func nestNew(all factory.OrgWorkstreams, ws *factory.OrgWorkstream, v Viewer, sentRules bool) error {
	if ws.Steward == "" {
		ws.Steward = factory.NormalizeHandle(ws.CreatedBy)
	}
	if ws.Parent == "" {
		return nil
	}
	if err := factory.CheckParent(all, ws.ID, ws.Parent); err != nil {
		return workstreamInvalid(err.Error())
	}
	p := all[ws.Parent]
	if !v.canWrite(p) {
		return errWorkstreamNoWrite
	}
	ws.Owner, ws.Private, ws.Shares = p.Owner, p.Private, slices.Clone(p.Shares)
	if !sentRules {
		r := p.RulesOrDefault()
		if ws.Steward != "" {
			r.DefaultOwner = factory.RulesOwner{Kind: factory.RulesOwnerPerson, ID: ws.Steward}
		}
		ws.Rules = &r
	}
	return nil
}

// cleanNewRules checks rules sent with a new workstream.
func cleanNewRules(r *api.WorkstreamRules, now time.Time) (*factory.WorkstreamRules, error) {
	if r == nil {
		return nil, nil
	}
	out := *r
	if err := factory.CleanRules(&out); err != nil {
		return nil, workstreamInvalid(err.Error())
	}
	factory.StampFactorySince(factory.WorkstreamRules{}, &out, now)
	return &out, nil
}

// patchStructure lays a patch's rules, steward and parent over cur, under the
// store's lock. It answers whether the rules changed.
func patchStructure(all factory.OrgWorkstreams, cur *factory.OrgWorkstream, body api.WorkstreamPatch, v Viewer, now time.Time) (bool, error) {
	if body.Rules == nil && body.Steward == nil && body.Parent == nil {
		return false, nil
	}
	if !v.stewards(*cur) {
		return false, errNotSteward
	}
	rulesChanged := false
	if body.Rules != nil {
		next := *body.Rules
		before := cur.RulesOrDefault()
		keepUnsentRules(before, &next, v.Handle, now)
		if err := factory.CleanRules(&next); err != nil {
			return false, workstreamInvalid(err.Error())
		}
		factory.StampFactorySince(before, &next, now)
		cur.Rules = &next
		rulesChanged = !sameWorkstreamRules(before, next)
	}
	if body.Steward != nil {
		who := factory.NormalizeHandle(*body.Steward)
		if who == "" {
			return false, invalidf("hand it on to a person in it")
		}
		cur.Steward = who
	}
	if body.Parent != nil && *body.Parent != cur.Parent {
		parent := strings.TrimSpace(*body.Parent)
		if err := factory.CheckParent(all, cur.ID, parent); err != nil {
			return false, workstreamInvalid(err.Error())
		}
		if parent != "" && !v.canWrite(all[parent]) {
			return false, errWorkstreamNoWrite
		}
		cur.Parent = parent
	}
	return rulesChanged, nil
}

// keepUnsentRules carries over what a rules write left unsaid. RULES ARE
// WRITTEN WHOLE, so a writer that predates a field would otherwise wipe it: an
// absent exceptions or not-now list keeps the kept one (an empty list clears
// it), an empty ask rule or wait keeps the kept setting ("outward" and 24 say
// the defaults on purpose). The server stamps who added each new exception and
// when; what a writer says about that is replaced.
func keepUnsentRules(before factory.WorkstreamRules, next *factory.WorkstreamRules, who string, now time.Time) {
	if next.Exceptions == nil {
		next.Exceptions = slices.Clone(before.Exceptions)
	}
	if next.NotNow == nil {
		next.NotNow = slices.Clone(before.NotNow)
	}
	// A client that never heard of rules in words sends none: it keeps them.
	if next.Said == nil {
		next.Said = slices.Clone(before.Said)
	}
	if next.AsksBefore == "" {
		next.AsksBefore = before.AsksBefore
	}
	if next.FallbackAfterHours == 0 {
		next.FallbackAfterHours = before.FallbackAfterHours
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	for i, e := range next.Exceptions {
		if kept, ok := factory.ExceptionFor(before, e.Kind); ok {
			next.Exceptions[i].AddedBy, next.Exceptions[i].AddedAt = kept.AddedBy, kept.AddedAt
			continue
		}
		next.Exceptions[i].AddedBy = &factory.PersonRef{ID: factory.NormalizeHandle(who)}
		next.Exceptions[i].AddedAt = stamp
	}
	for i, n := range next.NotNow {
		if _, err := time.Parse(time.RFC3339Nano, n.At); err != nil {
			next.NotNow[i].At = stamp
		}
	}
}

func sameWorkstreamRules(a, b factory.WorkstreamRules) bool {
	fa, fb := "", ""
	if a.Fallback != nil {
		fa = a.Fallback.ID
	}
	if b.Fallback != nil {
		fb = b.Fallback.ID
	}
	return a.DefaultOwner.Kind == b.DefaultOwner.Kind && a.DefaultOwner.ID == b.DefaultOwner.ID && fa == fb &&
		a.StartWithoutAskingUSD == b.StartWithoutAskingUSD && a.KeepsRunning.Factory == b.KeepsRunning.Factory &&
		slices.Equal(a.KeepsRunning.Orders, b.KeepsRunning.Orders) && a.PublishesWithoutAsking == b.PublishesWithoutAsking &&
		a.AsksBefore == b.AsksBefore && a.FallbackAfterHours == b.FallbackAfterHours && slices.Equal(a.Said, b.Said) &&
		slices.EqualFunc(a.Exceptions, b.Exceptions, func(x, y factory.AskException) bool { return x.Kind == y.Kind }) &&
		slices.EqualFunc(a.NotNow, b.NotNow, func(x, y factory.AskNotNow) bool { return x.Kind == y.Kind && x.At == y.At })
}

// patchedStructure names the structure fields a patch sent, for the record.
func patchedStructure(body api.WorkstreamPatch) []string {
	var out []string
	if body.Rules != nil {
		out = append(out, "rules")
	}
	if body.Steward != nil {
		out = append(out, "steward")
	}
	if body.Parent != nil {
		out = append(out, "parent")
	}
	return out
}

// ── start without asking ──

// syncStartWithoutAsking writes each workstream's "start without asking" rule
// into the go-ahead policy kept under its id. It is called after every save
// that changes a workstream's rules or its jobs. A workstream that never set
// a rule and has no policy is left without one.
func (h *Handler) syncStartWithoutAsking(ids ...string) {
	if h.config.Store == nil {
		return
	}
	for _, id := range ids {
		ws, ok, err := h.config.Store.Workstream(id)
		if err != nil || !ok {
			continue
		}
		rules := ws.RulesOrDefault()
		_, kept, err := h.config.Store.Policy(id)
		if err != nil || (!kept && rules.StartWithoutAskingUSD <= 0) {
			continue
		}
		owner := ""
		if rules.DefaultOwner.Kind == factory.RulesOwnerPerson {
			owner = rules.DefaultOwner.ID
		}
		usd := rules.StartWithoutAskingUSD
		if ws.ArchivedAt != nil {
			usd = 0
		}
		now := h.clock()
		changed := false
		_, _ = h.config.Store.ChangePolicy(id, func(p *factory.GroupPolicy) error {
			changed = factory.ApplyStartWithoutAsking(p, ws.Jobs, usd, owner, now)
			if !changed {
				return errNoPolicyChange
			}
			p.UpdatedAt = now.UTC()
			return nil
		})
	}
}

// errNoPolicyChange ends a policy change that would write what is kept.
var errNoPolicyChange = errors.New("no change")

// ── archive and fold ──

// workstreamArchiveKeep answers what a child could leave in its parent when
// it is archived, for whoever can read it.
func (h *Handler) workstreamArchiveKeep(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	h.write(w, http.StatusOK, api.WorkstreamArchiveKeep{Parent: ws.Parent, Keep: factory.ArchiveKeep(ws)})
}

// foldWorkstream retains the older parent-only door, using the same complete
// move and Undo receipt as the destination picker.
func (h *Handler) foldWorkstream(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	if ws.Parent == "" {
		h.fail(w, http.StatusUnprocessableEntity, "choose a destination workstream")
		return
	}
	h.foldWorkstreamPatch(w, r, ws, v, ws.Rev, api.WorkstreamPatch{FoldInto: ws.Parent})
}

// publishChildren says workstream.changed for each workstream inside id, whose
// people the save just mirrored: their audience moved with their parent's.
func (h *Handler) publishChildren(r *http.Request, saved factory.OrgWorkstreams, id string) {
	for _, cid := range factory.ChildrenOf(saved, id) {
		h.publishWorkstreams(api.EventWorkstreamChanged, h.orgActor(r), nil, saved[cid])
	}
}

// publishWorkstreamRules says workstream.changed with parts ["rules"], so a
// sheet open on another screen reads the rules again.
func (h *Handler) publishWorkstreamRules(ws factory.OrgWorkstream, by string) {
	e := workstreamLive{
		WorkstreamEvent: api.WorkstreamEvent{Kind: api.EventWorkstreamChanged, ID: ws.ID, By: factory.NormalizeHandle(by), Rev: ws.Rev, Parts: []string{"rules"}},
		itemAudience:    h.audienceOf(workstreamItem(ws)),
	}
	_ = live.Append(h.eventRoot(), live.New(api.EventWorkstreamChanged, 0, e))
}
