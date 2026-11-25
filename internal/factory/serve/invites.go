package serve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// ── INVITES: PEOPLE › INVITE SOMEONE (v2 design J4, J7) ───────────────────
//
// An invite is a one-person access link that also says who the person is (an
// email, a name or both), the team they join and the workstreams they join.
// It is THE ONE DOOR an invitation is made through: Settings › Organizations ›
// People calls the routes, and Afi's organization setup calls createInvite.
//
// THE INVITED PERSON EXISTS BEFORE THEY JOIN. They go by "invite:<id>"
// ([tokens.InviteRef]) as a share on each of their workstreams, so they can
// be given tasks at once; a task done by them waits until they join. Joining
// (by GitHub, password or passkey, whichever door) is settled afterwards by
// settleInvites, which is idempotent and is also run on every read of the
// invites and of the joiner's welcome, so a join is carried out even when the
// door that admitted the person never called it.
//
// MAIL IS ABSENT, NOT BROKEN. With no mailer configured (Config.SendMail),
// nothing is mailed and People offers "Copy an invite link" alone.

// inviteInvalid is a refusal of what was asked, answered 422.
type inviteInvalid string

func (e inviteInvalid) Error() string { return string(e) }

// errInviteGone is an invite that is not there, or was withdrawn.
var errInviteGone = errors.New("that invite is not there any more")

func (h *Handler) inviteRoutes() {
	h.route("/v1/invites", map[string]http.HandlerFunc{http.MethodGet: h.listInvites, http.MethodPost: h.postInvite})
	h.route("/v1/invites/{id}", map[string]http.HandlerFunc{http.MethodDelete: h.withdrawInvite})
	h.route("/v1/invites/{id}/resend", map[string]http.HandlerFunc{http.MethodPost: h.resendInvite})
	h.route("/v1/me/welcome", map[string]http.HandlerFunc{http.MethodGet: h.myWelcome})
	h.route("/v1/me/welcome/seen", map[string]http.HandlerFunc{http.MethodPost: h.seenWelcome})
}

func (h *Handler) inviteFailed(w http.ResponseWriter, err error) bool {
	var bad inviteInvalid
	switch {
	case err == nil:
		return false
	case errors.As(err, &bad):
		h.fail(w, http.StatusUnprocessableEntity, bad.Error())
	case errors.Is(err, errInviteGone), errors.Is(err, tokens.ErrNotFound):
		h.fail(w, http.StatusNotFound, errInviteGone.Error())
	default:
		h.failed(w, err)
	}
	return true
}

func (h *Handler) postInvite(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperator(w, r) || !h.requireVerb(w, h.config.TokensPath != "" && h.config.Store != nil) {
		return
	}
	var body api.InviteNew
	if !h.decode(w, r, &body, false) {
		return
	}
	clearPhase(w)
	inv, link, err := h.createInvite(r, body)
	if h.inviteFailed(w, err) {
		return
	}
	h.write(w, http.StatusCreated, api.InviteMade{Invite: inv, URL: link})
}

// createInvite makes an invite, or refreshes the open one already made for
// the same email (else the same name): its team and workstreams are added to
// and its link is made again for seven more days, so pressing Send invite
// twice, or Afi's setup running twice, invites a person once. It answers the
// invite and its link, which is shown this once.
func (h *Handler) createInvite(r *http.Request, in api.InviteNew) (api.Invite, string, error) {
	in.Email = strings.TrimSpace(in.Email)
	in.Name = strings.TrimSpace(in.Name)
	if in.Email != "" {
		addr, err := mail.ParseAddress(in.Email)
		if err != nil || addr.Address != in.Email || !strings.Contains(in.Email, "@") {
			return api.Invite{}, "", inviteInvalid("that is not an email address")
		}
		in.Email = strings.ToLower(in.Email)
	}
	if in.Email == "" && in.Name == "" {
		return api.Invite{}, "", inviteInvalid("an invite needs an email, or at least a name")
	}
	if len(in.Name) > 120 {
		return api.Invite{}, "", inviteInvalid("a name is at most 120 characters")
	}
	if in.Team != "" {
		teams, err := h.config.Store.Teams()
		if err != nil {
			return api.Invite{}, "", err
		}
		if !slices.ContainsFunc(teams, func(t store.Team) bool { return t.ID == in.Team }) {
			return api.Invite{}, "", inviteInvalid("there is no team " + in.Team)
		}
	}
	wsIDs, err := h.inviteWorkstreams(in.Workstreams)
	if err != nil {
		return api.Invite{}, "", err
	}
	now := h.now()
	by := h.orgActor(r)
	secret, err := tokens.Mint()
	if err != nil {
		return api.Invite{}, "", err
	}
	var row tokens.Token
	err = tokens.Edit(h.config.TokensPath, func(rows []tokens.Token) ([]tokens.Token, error) {
		for i := range rows {
			if !sameInvitee(rows[i], in, now) {
				continue
			}
			t := &rows[i]
			t.Invite.Workstreams = mergeIDs(t.Invite.Workstreams, wsIDs)
			if in.Team != "" {
				t.Invite.Team = in.Team
			}
			if in.Name != "" {
				t.Invite.Name, t.InvitedName = in.Name, in.Name
			}
			if in.Email != "" {
				t.Invite.Email = in.Email
			}
			t.Token, t.TokenHash, t.Expires = secret.Token, "", now.Add(tokens.HumanLinkTTL)
			row = *t
			return rows, nil
		}
		row = secret
		row.Created, row.Expires, row.InvitedBy = now, now.Add(tokens.HumanLinkTTL), by
		row.AccessLink, row.MaxUses, row.InvitedName = true, 1, in.Name
		row.Invite = &tokens.Invite{Email: in.Email, Name: in.Name, Team: in.Team, Workstreams: wsIDs}
		return append(rows, row), nil
	})
	if err != nil {
		return api.Invite{}, "", err
	}
	if err := h.shareWithInvitee(row, by); err != nil {
		return api.Invite{}, "", err
	}
	link := accessLinkURL(h.factory.Address, row)
	row = h.mailInvite(r.Context(), row, link, by)
	_ = h.operatorEvent(r, record.LinkCreated, "link:"+row.ID, "Invited "+inviteeWords(row))
	return h.inviteWire(row, now), link, nil
}

// sameInvitee is whether old is the open invite in would refresh.
func sameInvitee(old tokens.Token, in api.InviteNew, now time.Time) bool {
	if old.Invite == nil || old.Revoked || !old.Invite.Settled.IsZero() || old.Used() > 0 {
		return false
	}
	if in.Email != "" {
		return strings.EqualFold(old.Invite.Email, in.Email)
	}
	return old.Invite.Email == "" && strings.EqualFold(old.Invite.Name, in.Name)
}

func mergeIDs(a, b []string) []string {
	out := slices.Clone(a)
	for _, id := range b {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func inviteeWords(t tokens.Token) string {
	if t.Invite == nil {
		return t.InvitedName
	}
	if t.Invite.Name != "" && t.Invite.Email != "" {
		return t.Invite.Name + " <" + t.Invite.Email + ">"
	}
	if t.Invite.Email != "" {
		return t.Invite.Email
	}
	return t.Invite.Name
}

// inviteWorkstreams checks each id names a workstream and answers the ones
// people are added to: a workstream inside another takes its parent's people,
// so the parent is the one shared.
func (h *Handler) inviteWorkstreams(ids []string) ([]string, error) {
	out := []string{}
	for _, id := range ids {
		ws, found, err := h.config.Store.Workstream(id)
		if err != nil {
			return nil, err
		}
		if !found || ws.ArchivedAt != nil {
			return nil, inviteInvalid("there is no workstream " + id)
		}
		if ws.Private {
			return nil, inviteInvalid(ws.Name + " is private to its owner")
		}
		if ws.Parent != "" {
			id = ws.Parent
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// shareWithInvitee puts the invited person on each of their workstreams with
// edit, under the name they go by until they join.
func (h *Handler) shareWithInvitee(row tokens.Token, by string) error {
	if row.Invite == nil || len(row.Invite.Workstreams) == 0 {
		return nil
	}
	ref := tokens.InviteRef(row.ID)
	now := h.clock().UTC()
	befores := map[string]factory.OrgWorkstream{}
	saved, err := h.config.Store.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		var changed []string
		for _, id := range row.Invite.Workstreams {
			ws, ok := all[id]
			if !ok || slices.ContainsFunc(ws.Shares, func(s factory.Share) bool { return s.Kind == factory.ShareToPerson && s.ID == ref }) {
				continue
			}
			befores[id] = ws
			ws.Shares = append(slices.Clone(ws.Shares), factory.Share{Kind: factory.ShareToPerson, ID: ref, Role: factory.RoleEdit})
			ws.PeopleAdded = maps1(ws.PeopleAdded)
			ws.PeopleAdded[factory.ShareToPerson+":"+ref] = now.Format(time.RFC3339)
			ws.UpdatedAt = now
			all[id] = ws
			changed = append(changed, id)
		}
		return changed, nil
	})
	if err != nil {
		return err
	}
	for id, before := range befores {
		before := before
		h.publishWorkstreams(api.EventWorkstreamChanged, by, &before, saved[id])
	}
	return nil
}

func maps1(m map[string]string) map[string]string {
	out := make(map[string]string, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// mailInvite sends the link when the server has a mailer and the invite an
// address, and keeps when it went. A failed send is logged and the invite
// stands: the link can still be copied.
func (h *Handler) mailInvite(ctx context.Context, row tokens.Token, link, by string) tokens.Token {
	if h.config.SendMail == nil || row.Invite == nil || row.Invite.Email == "" {
		return row
	}
	inviter := h.personNames()(by)
	org := h.factory.Name
	if org == "" {
		org = "your organization"
	}
	subject := fmt.Sprintf("%s added you to %s", inviter, org)
	body := fmt.Sprintf("%s added you to %s on codeaf.\n\nOpen this link on a computer with codeaf to join. It works for 7 days:\n\n%s\n", inviter, org, link)
	if err := h.config.SendMail(ctx, row.Invite.Email, subject, body); err != nil {
		log.Printf("factory: invite %s: the email was not sent: %v", row.ID, err)
		return row
	}
	now := h.now()
	_ = tokens.Change(h.config.TokensPath, row.ID, func(t *tokens.Token) error {
		if t.Invite != nil {
			t.Invite.Emailed = now
			row = *t
		}
		return nil
	})
	return row
}

// inviteWire is the People row for row.
func (h *Handler) inviteWire(row tokens.Token, now time.Time) api.Invite {
	inv := row.Invite
	if inv == nil {
		inv = &tokens.Invite{}
	}
	out := api.Invite{ID: row.ID, Ref: tokens.InviteRef(row.ID), Email: inv.Email, Name: inv.Name, Team: inv.Team,
		Workstreams: slices.Clone(inv.Workstreams), WorkstreamNames: []string{}, InvitedBy: row.InvitedBy,
		CreatedAt: row.Created, ExpiresAt: row.Expires, State: api.InviteInvited, Handle: inv.Handle,
		Emailed: !inv.Emailed.IsZero() && !inv.Emailed.Before(row.Expires.Add(-tokens.HumanLinkTTL))}
	if out.Workstreams == nil {
		out.Workstreams = []string{}
	}
	if inv.Team != "" {
		out.TeamName = h.wsTeamName(inv.Team)
	}
	for _, id := range out.Workstreams {
		name := id
		if ws, ok, err := h.config.Store.Workstream(id); err == nil && ok {
			name = ws.Name
		}
		out.WorkstreamNames = append(out.WorkstreamNames, name)
	}
	if members := row.Members(); len(members) > 0 {
		at := members[0].Joined
		out.State, out.JoinedAt, out.Handle = api.InviteJoined, &at, members[0].Profile.Handle
	} else if row.Reason(now) == "expired" {
		out.State = api.InviteExpired
	}
	return out
}

func (h *Handler) listInvites(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperator(w, r) || !h.requireVerb(w, h.config.TokensPath != "" && h.config.Store != nil) {
		return
	}
	h.settleInvites(r)
	rows, err := tokens.Load(h.config.TokensPath)
	if h.failed(w, err) {
		return
	}
	now := h.now()
	out := api.Invites{Invites: []api.Invite{}, Mailer: h.config.SendMail != nil}
	for _, row := range rows {
		if row.Invite == nil || row.Revoked {
			continue
		}
		out.Invites = append(out.Invites, h.inviteWire(row, now))
	}
	sort.SliceStable(out.Invites, func(i, j int) bool { return out.Invites[i].CreatedAt.Before(out.Invites[j].CreatedAt) })
	h.write(w, http.StatusOK, out)
}

// resendInvite is Send again: the same invite with a new link for seven more
// days, mailed again when there is a mailer. The old link stops working, so a
// forwarded copy of it cannot be used by someone else afterwards.
func (h *Handler) resendInvite(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperator(w, r) || !h.requireVerb(w, h.config.TokensPath != "" && h.config.Store != nil) {
		return
	}
	id := r.PathValue("id")
	secret, err := tokens.Mint()
	if h.failed(w, err) {
		return
	}
	now := h.now()
	var row tokens.Token
	clearPhase(w)
	err = tokens.Change(h.config.TokensPath, id, func(t *tokens.Token) error {
		switch {
		case t.Invite == nil || t.Revoked:
			return errInviteGone
		case t.Used() > 0:
			return inviteInvalid("they have already joined")
		}
		t.Token, t.TokenHash, t.Expires = secret.Token, "", now.Add(tokens.HumanLinkTTL)
		row = *t
		return nil
	})
	if h.inviteFailed(w, err) {
		return
	}
	link := accessLinkURL(h.factory.Address, row)
	row = h.mailInvite(r.Context(), row, link, h.orgActor(r))
	_ = h.operatorEvent(r, record.LinkCreated, "link:"+row.ID, "Sent the invite again to "+inviteeWords(row))
	h.write(w, http.StatusOK, api.InviteMade{Invite: h.inviteWire(row, now), URL: link})
}

// withdrawInvite ends an invite nobody has used: the link stops working, the
// person leaves the workstreams they were put on, and the tasks they were
// given go back to whoever withdrew it.
func (h *Handler) withdrawInvite(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperator(w, r) || !h.requireVerb(w, h.config.TokensPath != "" && h.config.Store != nil) {
		return
	}
	id := r.PathValue("id")
	var row tokens.Token
	clearPhase(w)
	now := h.now()
	err := tokens.Change(h.config.TokensPath, id, func(t *tokens.Token) error {
		if t.Invite == nil || t.Revoked {
			return errInviteGone
		}
		if t.Used() > 0 {
			return inviteInvalid("they have already joined; remove them from People instead")
		}
		row = *t
		// The same tombstone a revoked access link becomes.
		t.TokenHash, t.Token, t.Revoked, t.RevokedAt = tokenHash(t.Token), "", true, now.UTC()
		return nil
	})
	if h.inviteFailed(w, err) {
		return
	}
	by := h.orgActor(r)
	if err := h.rebindPerson(r, tokens.InviteRef(row.ID), by, by, false); h.failed(w, err) {
		return
	}
	h.finish(w, h.operatorEvent(r, record.LinkRevoked, "link:"+id, "Withdrew the invite to "+inviteeWords(row)))
}

// settleInvites carries out every join not yet carried out: the person who
// used an invite is put in its team, takes its place on its workstreams, and
// is given the tasks it was given. It is safe to run any number of times, and
// a part that fails is tried again the next time.
func (h *Handler) settleInvites(r *http.Request) {
	if h.config.TokensPath == "" || h.config.Store == nil {
		return
	}
	for _, row := range h.tokenRows() {
		if row.Invite == nil || !row.Invite.Settled.IsZero() || row.Used() == 0 {
			continue
		}
		handle := factory.NormalizeHandle(row.Members()[0].Profile.Handle)
		if handle == "" {
			continue
		}
		if err := h.settleInvite(r, row, handle); err != nil {
			log.Printf("factory: invite %s: joining %s is not finished yet: %v", row.ID, handle, err)
			continue
		}
		now := h.now()
		_ = tokens.Change(h.config.TokensPath, row.ID, func(t *tokens.Token) error {
			if t.Invite != nil && t.Invite.Settled.IsZero() {
				t.Invite.Settled, t.Invite.Handle = now, handle
			}
			return nil
		})
	}
}

func (h *Handler) settleInvite(r *http.Request, row tokens.Token, handle string) error {
	if team := row.Invite.Team; team != "" {
		added := false
		now := h.clock().UTC()
		err := h.config.Store.ChangeTeams(func(all *[]store.Team) error {
			added = false
			for i := range *all {
				t := &(*all)[i]
				if t.ID == team && !slices.Contains(t.Members, handle) {
					t.Members = append(t.Members, handle)
					t.UpdatedAt, added = now, true
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if added {
			h.recordTeam(r, team, "added to team", "@"+handle)
			h.teamChanged(r, team)
		}
	}
	return h.rebindPerson(r, tokens.InviteRef(row.ID), handle, row.InvitedBy, true)
}

// rebindPerson moves everything that names the person `from` to `to`: their
// place on every workstream and every open task they own or do. When keep is
// false `from` simply leaves the workstreams (a withdrawn invite) and `to`
// is not added; their tasks still go to `to`.
func (h *Handler) rebindPerson(r *http.Request, from, to, by string, keep bool) error {
	now := h.clock().UTC()
	befores := map[string]factory.OrgWorkstream{}
	saved, err := h.config.Store.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		var changed []string
		for id, ws := range all {
			at := slices.IndexFunc(ws.Shares, func(s factory.Share) bool { return s.Kind == factory.ShareToPerson && s.ID == from })
			if at < 0 {
				continue
			}
			befores[id] = ws
			role := ws.Shares[at].Role
			shares := slices.Delete(slices.Clone(ws.Shares), at, at+1)
			if keep {
				if j := slices.IndexFunc(shares, func(s factory.Share) bool { return s.Kind == factory.ShareToPerson && s.ID == to }); j >= 0 {
					shares[j].Role = factory.HigherRole(shares[j].Role, role)
				} else if !(ws.Owner.Kind == api.OwnerPerson && ws.Owner.ID == to) {
					shares = append(shares, factory.Share{Kind: factory.ShareToPerson, ID: to, Role: role})
				}
			}
			ws.Shares = shares
			ws.PeopleAdded = maps1(ws.PeopleAdded)
			if added, ok := ws.PeopleAdded[factory.ShareToPerson+":"+from]; ok {
				delete(ws.PeopleAdded, factory.ShareToPerson+":"+from)
				if keep {
					ws.PeopleAdded[factory.ShareToPerson+":"+to] = added
				}
			}
			ws.UpdatedAt = now
			all[id] = ws
			changed = append(changed, id)
		}
		return changed, nil
	})
	if err != nil {
		return err
	}
	for id, before := range befores {
		before := before
		h.publishWorkstreams(api.EventWorkstreamChanged, by, &before, saved[id])
	}
	return h.rebindTasks(r, from, to, by, nil)
}

// rebindTasks gives every open task `from` owns or does to `to`, in every
// workstream. A joiner's tasks keep who handed them over, so they arrive as
// "Handed to you".
//
// handedBy, when set, is who the tasks read as handed over by: a person
// removed from the organization hands theirs to the admin who removed them,
// so they arrive in that admin's "Handed to you" to hand on.
func (h *Handler) rebindTasks(r *http.Request, from, to, by string, handedBy *factory.PersonRef) error {
	all, err := h.config.Store.Workstreams()
	if err != nil {
		return err
	}
	name := h.personNames()(to)
	who := factory.TaskEventBy{Kind: factory.TaskByPerson, ID: by, Name: h.personNames()(by)}
	for id, ws := range all {
		var moved []factory.WorkstreamTask
		err := h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
			kept, err := tw.Tasks(id)
			if err != nil {
				return err
			}
			for _, t := range kept {
				next, changed := factory.RebindPerson(t, from, to, name, h.clock())
				if !changed {
					continue
				}
				if next.Owner.Kind == factory.TaskOwnerPerson && next.Owner.ID == to {
					switch {
					case handedBy != nil:
						next.HandedBy = handedBy
					case next.HandedBy == nil && by != to:
						next.HandedBy = &factory.PersonRef{ID: by, Name: who.Name}
					}
				}
				if err := tw.Put(next, t.Rev); err != nil {
					return err
				}
				if err := tw.Append(id, factory.WorkstreamTaskEvent{ID: h.newTaskID("te_")(), Task: t.ID, At: next.UpdatedAt, By: who, Kind: factory.TaskEventHanded, Text: "Handed to " + name}); err != nil {
					return err
				}
				moved = append(moved, next)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if len(moved) > 0 {
			h.publishTasks(ws, by, moved...)
		}
	}
	return nil
}

// inviteNames names every invited person who has not joined by the id they
// go by, for personNames.
func (h *Handler) inviteNames() map[string]string {
	out := map[string]string{}
	for _, row := range h.tokenRows() {
		if row.Invite == nil || row.Revoked {
			continue
		}
		name := row.Invite.Name
		if name == "" {
			name = row.Invite.Email
		}
		out[tokens.InviteRef(row.ID)] = name
	}
	return out
}

// welcomeRow is the invite the caller joined through, if any.
func (h *Handler) welcomeRow(handle string) (tokens.Token, bool) {
	for _, row := range h.tokenRows() {
		if row.Invite != nil && row.Invite.Handle == handle && !row.Invite.Settled.IsZero() {
			return row, true
		}
	}
	return tokens.Token{}, false
}

// myWelcome is GET /v1/me/welcome: "Priya added you to AgentField · You are in
// 2 workstreams and the Engineering team", until Got it.
func (h *Handler) myWelcome(w http.ResponseWriter, r *http.Request) {
	out := api.Welcome{Workstreams: []string{}, WorkstreamNames: []string{}}
	handle, err := h.callerHandle(r)
	handle = factory.NormalizeHandle(handle)
	if err != nil || handle == "" || h.config.TokensPath == "" || h.config.Store == nil {
		h.write(w, http.StatusOK, out)
		return
	}
	h.settleInvites(r)
	row, ok := h.welcomeRow(handle)
	if !ok || !row.Invite.WelcomeSeen.IsZero() {
		h.write(w, http.StatusOK, out)
		return
	}
	wire := h.inviteWire(row, h.now())
	out.Show, out.InvitedBy, out.InvitedByName = true, row.InvitedBy, h.personNames()(row.InvitedBy)
	out.Workstreams, out.WorkstreamNames, out.Team, out.TeamName = wire.Workstreams, wire.WorkstreamNames, wire.Team, wire.TeamName
	h.write(w, http.StatusOK, out)
}

// seenWelcome is Got it: the welcome line is never shown again.
func (h *Handler) seenWelcome(w http.ResponseWriter, r *http.Request) {
	handle, err := h.callerHandle(r)
	handle = factory.NormalizeHandle(handle)
	if err != nil || handle == "" || h.config.TokensPath == "" {
		h.finish(w, nil)
		return
	}
	row, ok := h.welcomeRow(handle)
	if !ok {
		h.finish(w, nil)
		return
	}
	now := h.now()
	clearPhase(w)
	h.finish(w, tokens.Change(h.config.TokensPath, row.ID, func(t *tokens.Token) error {
		if t.Invite != nil && t.Invite.WelcomeSeen.IsZero() {
			t.Invite.WelcomeSeen = now
		}
		return nil
	}))
}

// tokenHash is what a revoked row keeps in place of its secret, as
// tokens.RemoveAt keeps it.
func tokenHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// closeInvitesOf ends the standing of a removed person on every link that admitted them, in one save. A named
// invite carries the handle it admitted and is revoked, which the list skips. An anonymous access link carries
// none, so it cannot be found by handle and cannot be revoked (that would turn away the others who joined by it):
// the person's own use is marked removed instead. Either way the use stays spent, so no link reopens.
func closeInvitesOf(path, handle string, now time.Time) error {
	if path == "" {
		return nil
	}
	return tokens.Edit(path, func(rows []tokens.Token) ([]tokens.Token, error) {
		for i := range rows {
			t := &rows[i]
			if t.Invite != nil && !t.Revoked && t.Invite.Handle != "" && factory.NormalizeHandle(t.Invite.Handle) == handle {
				t.TokenHash, t.Token, t.Revoked, t.RevokedAt = tokenHash(t.Token), "", true, now.UTC()
			}
			t.EndMember(handle, now)
		}
		return rows, nil
	})
}
