package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── STARTING AN ORGANIZATION, AND AFI SETTING IT UP (v2 designs J2, J3) ────
//
// An organization is a server somebody runs (`codeaf factory serve`); whoever
// claims it with the operator link is its operator. "Start your
// organization" is then two fields, a name and what it does in a line, kept
// with PUT /v1/org; Afi reads the line. After it, the operator lands on a
// conversation with Afi in the main column of the empty organization, and
// Afi answers what is on their plate with ONE card (session's
// org_propose_setup): workstreams with what each is for and who is in it,
// the workstreams inside them, the teams, and the people to invite.
//
//   - NOTHING IS MADE AND NOBODY IS INVITED UNTIL THE PRESS. Proposing keeps
//     the card for the person who asked. POST /v1/org/setup/apply makes it in
//     one fixed order: teams, then workstreams (parents first), then the
//     workstreams inside them, then the invites with their workstreams and
//     team, each through the route a person's client uses.
//
//   - A FAILURE NAMES ITS PART, AND A PRESS AGAIN DOES ONLY WHAT IS LEFT.
//     Every part made is written down by the card's own name before the next
//     is tried, so a press after a failure (or a crash) skips what exists.
//
//   - THE CONVERSATION IS THE OPERATOR'S, AND PRIVATE. It is a seat like a
//     workstream agent's (workstream_agent.go), under a name no workstream
//     can have, and its events reach that one person.
//
//   - "SET UP BY HAND" IS A CHOICE, NOT A FAILURE. It marks the organization
//     set up with nothing made, and the app opens an empty workstream.

// orgSetupSeat is the seat name the set-up conversation sits under. A
// workstream's id always starts with workstreamIDPrefix, so no workstream
// can be called this.
const orgSetupSeat = "org-setup"

// orgNameMost is the most characters an organization's name may have; it is
// factory.NormalizeName's limit.
const orgNameMost = 40

func (h *Handler) orgSetupRoutes() {
	h.route("/v1/org", map[string]http.HandlerFunc{http.MethodGet: h.getOrgProfile, http.MethodPut: h.putOrgProfile})
	h.route("/v1/org/setup", map[string]http.HandlerFunc{http.MethodGet: h.getOrgSetup})
	h.route("/v1/org/setup/messages", map[string]http.HandlerFunc{http.MethodPost: h.postOrgSetupMessage})
	h.route("/v1/org/setup/stop", map[string]http.HandlerFunc{http.MethodPost: h.stopOrgSetup})
	h.route("/v1/org/setup/apply", map[string]http.HandlerFunc{http.MethodPost: h.applyOrgSetup})
	h.route("/v1/org/setup/by-hand", map[string]http.HandlerFunc{http.MethodPost: h.orgSetupByHand})
}

// factoryNow is the organization as the server answers it now: the name a
// person gave it with "Start your organization" wins over the one it was
// started with.
func (h *Handler) factoryNow() api.Factory {
	f := h.factory
	if name := h.orgName.Load(); name != nil && *name != "" {
		f.Name = *name
	}
	return f
}

// loadOrgName reads the kept name once, at start.
func (h *Handler) loadOrgName() {
	if h.config.Store == nil {
		return
	}
	if p, err := h.config.Store.OrgProfile(); err == nil && p.Name != "" {
		name := p.Name
		h.orgName.Store(&name)
	}
}

// orgProfile is the organization's own words as the wire says them.
func (h *Handler) orgProfile() (api.OrgProfile, error) {
	out := api.OrgProfile{Name: h.factoryNow().Name, Relay: h.config.RelayURL}
	if h.config.Store == nil {
		return out, nil
	}
	p, err := h.config.Store.OrgProfile()
	if err != nil {
		return out, err
	}
	out.Description, out.SetUp, out.ByHand = p.Description, p.SetUpAt != nil, p.ByHand
	// AN ORGANIZATION THAT ALREADY HAS WORK IS SET UP, whoever set it up
	// and however: a server from before this page never lands its operator
	// on Afi's first conversation.
	if !out.SetUp {
		if all, err := h.config.Store.Workstreams(); err == nil {
			for _, ws := range all {
				if ws.ArchivedAt == nil {
					out.SetUp = true
					break
				}
			}
		}
	}
	return out, nil
}

// orgAbout is the line every Afi on this server is told about its
// organization: its name and what it does, empty when nobody wrote the line.
func (h *Handler) orgAbout() string {
	if h.config.Store == nil {
		return ""
	}
	p, err := h.config.Store.OrgProfile()
	if err != nil || p.Description == "" {
		return ""
	}
	return "The organization is " + h.factoryNow().Name + ": " + p.Description
}

// getOrgProfile is GET /v1/org, for anyone signed in.
func (h *Handler) getOrgProfile(w http.ResponseWriter, r *http.Request) {
	p, err := h.orgProfile()
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, p)
}

// putOrgProfile is PUT /v1/org: the operator names the organization and says
// what it does in a line.
func (h *Handler) putOrgProfile(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) || !h.requireOperator(w, r) {
		return
	}
	var body api.OrgProfilePut
	if !h.decode(w, r, &body, false) {
		return
	}
	var name, line string
	if body.Name != nil {
		n, err := factory.NormalizeName(*body.Name)
		if err != nil || n == "" {
			h.fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("a name is 1 to %d characters", orgNameMost))
			return
		}
		name = n
	}
	if body.Description != nil {
		line = strings.Join(strings.Fields(*body.Description), " ")
		if utf8.RuneCountInString(line) > api.OrgDescriptionMost {
			h.fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("the line is at most %d characters", api.OrgDescriptionMost))
			return
		}
	}
	saved, err := h.config.Store.ChangeOrgProfile(func(p *store.OrgProfile) error {
		if body.Name != nil {
			p.Name = name
		}
		if body.Description != nil {
			p.Description = line
		}
		p.UpdatedAt = h.clock().UTC()
		return nil
	})
	if h.failed(w, err) {
		return
	}
	if body.Name != nil {
		n := saved.Name
		h.orgName.Store(&n)
		h.recordOrg(r, record.SettingsChanged, "named the organization", orgObjectFactory, n)
	}
	if body.Description != nil {
		h.recordOrg(r, record.SettingsChanged, "described the organization", orgObjectFactory, saved.Description)
	}
	p, err := h.orgProfile()
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, p)
}

// orgSetupCannot is whether the operator may talk to Afi about the set-up
// now, and the sentence that says why not.
func (h *Handler) orgSetupCannot() (bool, string) {
	switch {
	case h.config.WorkstreamAgent == nil:
		return false, "This organization's server does not run Afi. Set it up by hand."
	case h.orgModelKey() == "":
		return false, "Afi needs the organization's model key first. Add one in Settings › Organizations, or set it up by hand."
	}
	return true, ""
}

// orgSetupView is the operator's own set-up conversation, card and parts.
func (h *Handler) orgSetupView(person string, since int) (api.OrgSetupView, error) {
	out := api.OrgSetupView{Agent: api.WorkstreamAgent{ID: orgSetupSeat, AgentState: api.AgentState{State: api.AgentIdle}, Entries: []api.AgentEntry{}, Private: true}, Made: []api.OrgSetupMadePart{}}
	h.agentDesk.mu.Lock()
	a := h.agentOfLocked(agentSeat{ws: orgSetupSeat, person: person})
	out.Agent.AgentState, out.Agent.Seq = a.state, a.seq
	from := len(a.entries)
	for from > 0 && a.entries[from-1].Seq > since {
		from--
	}
	if len(a.entries)-from > api.AgentEntriesMost {
		from = len(a.entries) - api.AgentEntriesMost
	}
	out.Agent.Entries = append(out.Agent.Entries, a.entries[from:]...)
	h.agentDesk.mu.Unlock()
	out.Agent.CanTalk, out.Agent.Cannot = h.orgSetupCannot()
	st, err := h.config.Store.OrgSetup()
	if err != nil {
		return out, err
	}
	if card, ok := st.Cards[person]; ok {
		out.Card = &card
	}
	out.Made = h.madeParts(st.Made[person], out.Card)
	p, err := h.orgProfile()
	out.SetUp = p.SetUp
	return out, err
}

// orgSetupPerson is the operator asking, or a refusal written to w.
func (h *Handler) orgSetupPerson(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !h.requireVerb(w, h.config.Store != nil) || !h.requireOperator(w, r) {
		return "", false
	}
	who := factory.NormalizeHandle(h.orgActor(r))
	if who == "" {
		h.fail(w, http.StatusForbidden, "setting the organization up needs a signed-in person")
		return "", false
	}
	return who, true
}

// getOrgSetup is GET /v1/org/setup?since=.
func (h *Handler) getOrgSetup(w http.ResponseWriter, r *http.Request) {
	who, ok := h.orgSetupPerson(w, r)
	if !ok {
		return
	}
	since := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			h.fail(w, http.StatusBadRequest, "since is the last seq you have")
			return
		}
		since = n
	}
	view, err := h.orgSetupView(who, since)
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, view)
}

// postOrgSetupMessage is POST /v1/org/setup/messages: the operator tells
// Afi what is on their plate (or what to change), answered in the
// background.
func (h *Handler) postOrgSetupMessage(w http.ResponseWriter, r *http.Request) {
	who, ok := h.orgSetupPerson(w, r)
	if !ok {
		return
	}
	if can, cannot := h.orgSetupCannot(); !can {
		h.fail(w, http.StatusConflict, cannot)
		return
	}
	var body api.AgentMessageNew
	if !h.decode(w, r, &body, false) {
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" || len(text) > api.AgentMessageMost {
		h.fail(w, http.StatusUnprocessableEntity, "a message is 1 to 16384 bytes of words")
		return
	}
	err := h.startOrgSetupTurn(who, text, strings.TrimSpace(body.TZ))
	if errors.Is(err, errAgentBusy) {
		h.write(w, http.StatusConflict, map[string]string{"error": "Afi is still answering your last message; wait for it, or stop it", "reason": "busy", "by": who})
		return
	}
	if h.failed(w, err) {
		return
	}
	view, err := h.orgSetupView(who, 0)
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusAccepted, view)
}

// startOrgSetupTurn is startAgentTurn for the set-up seat.
func (h *Handler) startOrgSetupTurn(who, text, tz string) error {
	now := h.clock().UTC()
	tid, err := cell.NewID(now)
	if err != nil {
		return err
	}
	turn := "turn_" + tid
	seat := agentSeat{ws: orgSetupSeat, person: who}
	d := &h.agentDesk
	d.mu.Lock()
	if d.ctx == nil {
		d.ctx, d.stop = context.WithCancel(context.Background())
	}
	if d.ctx.Err() != nil {
		d.mu.Unlock()
		return errors.New("this organization's server is closing")
	}
	a := h.agentOfLocked(seat)
	if a.state.State != api.AgentIdle {
		d.mu.Unlock()
		return errAgentBusy
	}
	ctx, cancel := context.WithCancel(d.ctx)
	a.cancel = cancel
	a.state = api.AgentState{State: api.AgentWorking, Turn: turn, For: who, Since: now.Format(time.RFC3339Nano)}
	state := a.state
	d.wg.Add(1)
	d.mu.Unlock()

	h.addAgentEntry(seat, api.AgentEntry{Kind: api.AgentEntryPerson, By: who, Text: text, Turn: turn})
	h.publishAgent(seat, api.AgentEvent{AgentState: &state})
	profile, _ := h.orgProfile()
	run := AgentTurn{Workstream: orgSetupSeat, Name: profile.Name, Turn: turn, Asker: who, Text: text, Now: now, TZ: tz,
		Key: h.orgModelKey(), Dir: filepath.Join(h.agentDir(seat), "agent"), About: h.orgAbout(),
		Org: &AgentOrg{Name: profile.Name, Description: profile.Description, Setup: &orgSetupDoor{h: h, asker: who}},
		Step: func(kind, text, tool string, failed bool) {
			switch kind {
			case api.AgentEntryStep, api.AgentEntryAgent, api.AgentEntryNote:
			default:
				return
			}
			h.addAgentEntry(seat, api.AgentEntry{Kind: kind, By: who, Text: text, Tool: tool, Failed: failed, Turn: turn})
		}}
	guard.Go("organization set-up agent", func() {
		defer d.wg.Done()
		defer cancel()
		h.runAgentTurn(ctx, run)
	})
	return nil
}

// stopOrgSetup is POST /v1/org/setup/stop.
func (h *Handler) stopOrgSetup(w http.ResponseWriter, r *http.Request) {
	who, ok := h.orgSetupPerson(w, r)
	if !ok {
		return
	}
	seat := agentSeat{ws: orgSetupSeat, person: who}
	h.agentDesk.mu.Lock()
	a := h.agentOfLocked(seat)
	var state *api.AgentState
	if a.state.State == api.AgentWorking && a.cancel != nil {
		a.state.State, a.state.For = api.AgentStopping, who
		a.cancel()
		s := a.state
		state = &s
	}
	h.agentDesk.mu.Unlock()
	if state != nil {
		h.publishAgent(seat, api.AgentEvent{AgentState: state})
	}
	view, err := h.orgSetupView(who, 0)
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, view)
}

// orgSetupByHand is POST /v1/org/setup/by-hand: the operator skips Afi.
func (h *Handler) orgSetupByHand(w http.ResponseWriter, r *http.Request) {
	who, ok := h.orgSetupPerson(w, r)
	if !ok {
		return
	}
	if _, err := h.markOrgSetUp(who, true); h.failed(w, err) {
		return
	}
	h.recordOrg(r, record.SettingsChanged, "set the organization up by hand", orgObjectFactory, "")
	p, err := h.orgProfile()
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, p)
}

// markOrgSetUp writes down that the organization is set up, once.
func (h *Handler) markOrgSetUp(who string, byHand bool) (store.OrgProfile, error) {
	return h.config.Store.ChangeOrgProfile(func(p *store.OrgProfile) error {
		if p.SetUpAt == nil {
			now := h.clock().UTC()
			p.SetUpAt, p.SetUpBy, p.ByHand, p.UpdatedAt = &now, who, byHand, now
		}
		return nil
	})
}

// ── THE DOOR AFI PROPOSES THROUGH ──

// orgSetupDoor keeps Afi's card for the person it answers.
type orgSetupDoor struct {
	h     *Handler
	asker string
}

var _ session.OrgSetupDoor = (*orgSetupDoor)(nil)

// ProposeOrgSetup keeps the card and says what the person will see.
func (d *orgSetupDoor) ProposeOrgSetup(_ context.Context, args json.RawMessage) (string, error) {
	card, err := factory.ParseOrgSetupCard(args)
	if err != nil {
		return "", err
	}
	who := factory.NormalizeHandle(d.asker)
	card.By, card.At = who, d.h.clock().UTC().Format(time.RFC3339)
	if _, err := d.h.config.Store.ChangeOrgSetup(func(st *store.OrgSetupState) error {
		if st.Cards == nil {
			st.Cards = map[string]factory.OrgSetupCard{}
		}
		st.Cards[who] = card
		return nil
	}); err != nil {
		return "", err
	}
	words := fmt.Sprintf("Shown to %s as a card in the main column: %s, %s to invite. Nothing is made and nobody is invited until they press %q.",
		d.h.personNames()(who), setupWorkstreamsWords(card), plural(len(card.Invites), "person", "people"), OrgSetupButton(card))
	var noEmail []string
	for _, p := range card.Invites {
		if p.Email == "" {
			noEmail = append(noEmail, p.Name)
		}
	}
	if len(noEmail) > 0 {
		words += " They add an email on the card for " + strings.Join(noEmail, ", ") + "; without one the invite is a link to copy."
	}
	return words, nil
}

// OrgSetupButton is the card's one button: "Create 5 workstreams and invite
// 3 people". The app spells it the same way from the same card.
func OrgSetupButton(c factory.OrgSetupCard) string {
	n, m := len(c.Workstreams), len(c.Invites)
	switch {
	case n > 0 && m > 0:
		return "Create " + plural(n, "workstream", "workstreams") + " and invite " + plural(m, "person", "people")
	case n > 0:
		return "Create " + plural(n, "workstream", "workstreams")
	default:
		return "Invite " + plural(m, "person", "people")
	}
}

func setupWorkstreamsWords(c factory.OrgSetupCard) string {
	s := plural(len(c.Workstreams), "workstream", "workstreams")
	if in := c.InsideCount(); in > 0 {
		s += fmt.Sprintf(" with %d inside them", in)
	}
	return s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// ── THE PRESS ──

// applyOrgSetup is POST /v1/org/setup/apply.
func (h *Handler) applyOrgSetup(w http.ResponseWriter, r *http.Request) {
	who, ok := h.orgSetupPerson(w, r)
	if !ok {
		return
	}
	var raw struct {
		Card json.RawMessage `json:"card"`
	}
	if !h.decode(w, r, &raw, false) {
		return
	}
	card, err := factory.ParseOrgSetupCard(raw.Card)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	out, err := h.applySetup(r, who, card)
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, out)
}

// setupKey is a part's name in what a press made.
func setupKey(kind, name string) string { return kind + ":" + strings.ToLower(name) }

// applySetup makes card in its fixed order, skipping what an earlier press
// made, and stops at the first part that fails.
func (h *Handler) applySetup(r *http.Request, who string, card factory.OrgSetupCard) (api.OrgSetupApplied, error) {
	st, err := h.config.Store.OrgSetup()
	if err != nil {
		return api.OrgSetupApplied{}, err
	}
	made := store.OrgSetupMade{}
	for k, v := range st.Made[who] {
		made[k] = v
	}
	keep := func(key, id string) error {
		made[key] = id
		_, err := h.config.Store.ChangeOrgSetup(func(s *store.OrgSetupState) error {
			if s.Made == nil {
				s.Made = map[string]store.OrgSetupMade{}
			}
			m := s.Made[who]
			if m == nil {
				m = store.OrgSetupMade{}
			}
			m[key] = id
			s.Made[who] = m
			return nil
		})
		return err
	}
	var urls = map[string]string{}
	result := func(failed string) api.OrgSetupApplied {
		out := api.OrgSetupApplied{Made: h.madeParts(made, &card), Failed: failed}
		for i := range out.Made {
			if u := urls[out.Made[i].ID]; u != "" {
				out.Made[i].URL = u
			}
		}
		return out
	}
	fault := func(key string) error {
		if h.setupFault != nil {
			return h.setupFault(key)
		}
		return nil
	}
	fail := func(part string, err error) (api.OrgSetupApplied, error) {
		return result(part + " could not be made: " + strings.TrimSuffix(oneLineErr(err), ".") + ". Press again to make what is left."), nil
	}

	// 1. Teams.
	teams, err := h.config.Store.Teams()
	if err != nil {
		return api.OrgSetupApplied{}, err
	}
	teamID := map[string]string{}
	for _, t := range card.Teams {
		key := setupKey("team", t.Name)
		if id := made[key]; id != "" && teamExists(teams, id) {
			teamID[strings.ToLower(t.Name)] = id
			continue
		}
		id, err := h.setupTeam(r, t.Name)
		if err == nil {
			err = fault(key)
		}
		if err != nil {
			return fail(t.Name, err)
		}
		teamID[strings.ToLower(t.Name)] = id
		if err := keep(key, id); err != nil {
			return api.OrgSetupApplied{}, err
		}
	}

	// 2. Workstreams, parents first, each shared with the teams whose
	// people are all in it.
	all, err := h.config.Store.Workstreams()
	if err != nil {
		return api.OrgSetupApplied{}, err
	}
	live := func(id string) bool { ws, ok := all[id]; return ok && ws.ArchivedAt == nil }
	wsID := map[string]string{}
	for _, ws := range card.Workstreams {
		key := setupKey("workstream", ws.Name)
		if id := made[key]; id != "" && live(id) {
			wsID[strings.ToLower(ws.Name)] = id
			continue
		}
		if err := fault(key); err != nil {
			return fail(ws.Name, err)
		}
		id, err := h.setupWorkstream(r, api.WorkstreamNew{Name: ws.Name, Purpose: ws.Purpose})
		if err != nil {
			return fail(ws.Name, err)
		}
		wsID[strings.ToLower(ws.Name)] = id
		if err := keep(key, id); err != nil {
			return api.OrgSetupApplied{}, err
		}
		var shareTeams []string
		for _, t := range card.Teams {
			if len(t.People) > 0 && allIn(t.People, ws.People) {
				shareTeams = append(shareTeams, teamID[strings.ToLower(t.Name)])
			}
		}
		if len(shareTeams) > 0 {
			if err := h.setupShareTeams(r, id, shareTeams); err != nil {
				return fail(ws.Name+"'s teams", err)
			}
		}
	}

	// 3. The workstreams inside them.
	for _, ws := range card.Workstreams {
		parent := wsID[strings.ToLower(ws.Name)]
		for _, ch := range ws.Inside {
			key := setupKey("inside", ws.Name+"/"+ch.Name)
			if id := made[key]; id != "" && live(id) {
				continue
			}
			if err := fault(key); err != nil {
				return fail(ch.Name, err)
			}
			id, err := h.setupWorkstream(r, api.WorkstreamNew{Name: ch.Name, Purpose: ch.Purpose, Parent: parent})
			if err != nil {
				return fail(ch.Name, err)
			}
			if err := keep(key, id); err != nil {
				return api.OrgSetupApplied{}, err
			}
		}
	}

	// 4. The invites, each with its team and the workstreams it names them in.
	for _, p := range card.Invites {
		key := setupKey("invite", p.Name)
		if made[key] != "" {
			continue
		}
		var in []string
		for _, ws := range card.Workstreams {
			if namedIn(p.Name, ws.People) {
				in = append(in, wsID[strings.ToLower(ws.Name)])
			}
		}
		if err := fault(key); err != nil {
			return fail("The invite for "+p.Name, err)
		}
		id, url, err := h.setupInvite(r, p.Name, p.Email, teamID[strings.ToLower(p.Team)], in)
		if err != nil {
			return fail("The invite for "+p.Name, err)
		}
		urls[id] = url
		if err := keep(key, id); err != nil {
			return api.OrgSetupApplied{}, err
		}
	}

	// Every part is made: the organization is set up, and the card is gone.
	if _, err := h.markOrgSetUp(who, false); err != nil {
		return api.OrgSetupApplied{}, err
	}
	if _, err := h.config.Store.ChangeOrgSetup(func(s *store.OrgSetupState) error {
		delete(s.Cards, who)
		return nil
	}); err != nil {
		return api.OrgSetupApplied{}, err
	}
	h.recordOrg(r, record.SettingsChanged, "set the organization up", orgObjectFactory,
		fmt.Sprintf("%s, %s, %s invited", setupWorkstreamsWords(card), plural(len(card.Teams), "team", "teams"), plural(len(card.Invites), "person", "people")))
	out := result("")
	out.Done = true
	return out, nil
}

// madeParts is made in card order (a card that is gone lists what made has).
func (h *Handler) madeParts(made store.OrgSetupMade, card *factory.OrgSetupCard) []api.OrgSetupMadePart {
	out := []api.OrgSetupMadePart{}
	if card == nil {
		return out
	}
	add := func(kind, key, name, parent string) {
		if id := made[key]; id != "" {
			out = append(out, api.OrgSetupMadePart{Kind: kind, Name: name, ID: id, Parent: parent})
		}
	}
	for _, t := range card.Teams {
		add(api.OrgSetupPartTeam, setupKey("team", t.Name), t.Name, "")
	}
	for _, ws := range card.Workstreams {
		add(api.OrgSetupPartWorkstream, setupKey("workstream", ws.Name), ws.Name, "")
	}
	for _, ws := range card.Workstreams {
		for _, ch := range ws.Inside {
			add(api.OrgSetupPartWorkstream, setupKey("inside", ws.Name+"/"+ch.Name), ch.Name, made[setupKey("workstream", ws.Name)])
		}
	}
	for _, p := range card.Invites {
		add(api.OrgSetupPartInvite, setupKey("invite", p.Name), p.Name, "")
	}
	return out
}

// setupTeam makes a team called name, or finds the one already called that.
func (h *Handler) setupTeam(r *http.Request, name string) (string, error) {
	now := h.clock().UTC()
	made := store.Team{ID: newTeamID(now), Name: name, Members: []string{}, CreatedAt: now, UpdatedAt: now}
	id := made.ID
	err := h.config.Store.ChangeTeams(func(all *[]store.Team) error {
		for _, t := range *all {
			if strings.EqualFold(t.Name, name) {
				id = t.ID
				return nil
			}
		}
		*all = append(*all, made)
		return nil
	})
	if err != nil {
		return "", err
	}
	if id == made.ID {
		h.recordTeam(r, id, "created team", name)
		h.teamChanged(r, id)
	}
	return id, nil
}

// setupWorkstream makes one workstream through POST /v1/workstreams, as the
// person pressing.
func (h *Handler) setupWorkstream(r *http.Request, body api.WorkstreamNew) (string, error) {
	raw, err := h.callAs(r, h.createWorkstream, http.MethodPost, "/v1/workstreams", nil, body)
	if err != nil {
		return "", err
	}
	var ws api.Workstream
	if err := json.Unmarshal(raw, &ws); err != nil || ws.ID == "" {
		return "", errors.New("the server answered no workstream")
	}
	return ws.ID, nil
}

// setupShareTeams shares workstream id with teams for edit.
func (h *Handler) setupShareTeams(r *http.Request, id string, teams []string) error {
	ws, ok, err := h.config.Store.Workstream(id)
	if err != nil || !ok {
		return errors.New("the workstream is gone")
	}
	shares := append([]factory.Share(nil), ws.Shares...)
	for _, t := range teams {
		shares = append(shares, factory.Share{Kind: factory.ShareToTeam, ID: t, Role: factory.RoleEdit})
	}
	rev := ws.Rev
	_, err = h.callAs(r, h.setWorkstreamShares, http.MethodPut, "/v1/workstreams/"+id+"/shares", map[string]string{"id": id},
		api.WorkstreamSharesPut{Shares: &shares, Teams: []string{}, Rev: &rev})
	return err
}

// callAs answers one request through h's own handler for it, as the person
// who made r: the same rules, records and events as their client's call.
func (h *Handler) callAs(r *http.Request, handler http.HandlerFunc, method, path string, values map[string]string, body any) ([]byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	in := r.Clone(r.Context())
	in.Method, in.Body, in.ContentLength = method, nopCloser{bytes.NewReader(raw)}, int64(len(raw))
	in.URL.Path, in.RequestURI = path, path
	in.Header.Set("Content-Type", "application/json")
	in.Header.Del("If-Match")
	in.Header.Del("Idempotency-Key")
	for k, v := range values {
		in.SetPathValue(k, v)
	}
	w := httptest.NewRecorder()
	handler(w, in)
	if w.Code >= 300 {
		var refused struct {
			Error string `json:"error"`
		}
		msg := strings.TrimSpace(w.Body.String())
		if json.Unmarshal(w.Body.Bytes(), &refused) == nil && refused.Error != "" {
			msg = refused.Error
		}
		return nil, errors.New(msg)
	}
	return w.Body.Bytes(), nil
}

type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }

func teamExists(all []store.Team, id string) bool {
	for _, t := range all {
		if t.ID == id {
			return true
		}
	}
	return false
}

// namedIn says whether name is one of names: the whole name, or its first
// word ("Marco" is "Marco Rossi").
func namedIn(name string, names []string) bool {
	first := strings.Fields(name + " ")[0]
	for _, x := range names {
		if strings.EqualFold(x, name) || strings.EqualFold(x, first) || strings.EqualFold(strings.Fields(x + " ")[0], name) {
			return true
		}
	}
	return false
}

func allIn(people, in []string) bool {
	for _, p := range people {
		if !namedIn(p, in) {
			return false
		}
	}
	return true
}

func oneLineErr(err error) string {
	return strings.Join(strings.Fields(err.Error()), " ")
}
