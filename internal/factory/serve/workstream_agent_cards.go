package serve

// AFI'S TASK CARDS (Workstreams v2, screen 8; DELTA §4.1 L4).
//
// In a person's own conversation with the workstream's agent, the agent
// proposes tasks with ws_task_propose instead of making them. The proposal is
// kept as one line of that conversation (kind cards), which only that person
// reads. NOTHING SHARED IS WRITTEN AND NOBODY IS TOLD until the person presses
// Add: then the ticked cards, as they changed them, are made as tasks AS THAT
// PERSON, with their rights, and a note line records which tasks they became.
// A cards line some note took is done; it is added once, never twice.
//
// A task's own run never proposes (its door says so), because nobody sits in
// front of a run to press Add.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// cardsPlanAt is how many added cards make a plan: they share one group, and
// the group waits for one press of Start (session.WorkstreamBatchAt).
const cardsPlanAt = 5

// Proposes is whether this door keeps proposals: a person's conversation from
// an app that draws cards does; a task's run, which nobody is in front of,
// and an app that would show a card with no Add, do not.
func (d *agentDoor) Proposes() bool { return d.wrote == nil && d.cards }

// wsTaskPropose checks a proposal and keeps it as a cards line of the
// asker's conversation. It writes nothing shared.
func (d *agentDoor) wsTaskPropose(ctx context.Context, args json.RawMessage) (any, error) {
	if !d.Proposes() {
		return nil, errors.New("tasks cannot be proposed here: nobody can add them. Make what is needed with ws_task_create")
	}
	var a api.AgentCards
	if err := agentArgs(args, &a); err != nil {
		return nil, err
	}
	ws, err := d.Workstream(ctx)
	if err != nil {
		return nil, err
	}
	cards, err := d.h.checkCards(ws, d.asker, a)
	if err != nil {
		return nil, err
	}
	n := len(cards.Tasks)
	// A PLAN IS ONE PROPOSAL. Live on deepseek-v4.1-flash the plan arrived as
	// one card and then seven, two sets of cards to add, and the seven could
	// not wait on the one (a card waits only on cards in its own proposal).
	if cards.Group != "" && n < cardsPlanAt {
		return nil, fmt.Errorf("a plan is %d to %d tasks in ONE call: propose all of its tasks together, or leave out group for fewer", cardsPlanAt, api.AgentCardsMost)
	}
	d.h.addAgentEntry(agentSeat{ws: d.id, person: factory.NormalizeHandle(d.asker)}, api.AgentEntry{
		Kind: api.AgentEntryCards, Tool: "ws_task_propose", Cards: &cards, Text: cardsWords(cards),
	})
	said := fmt.Sprintf("Shown to them as %d card%s. Nothing is made until they press Add %d task%s", n, pluralS(n), n, pluralS(n))
	if n >= cardsPlanAt {
		said = fmt.Sprintf("Shown to them as a plan of %d tasks: it goes to their Inbox and nothing starts until they press Start there", n)
	}
	if told := d.h.cardsTold(cards, d.asker); told != "" {
		said += "; " + told + " " + isAre(told) + " told when it is added"
	}
	// YOUR WORDS OUTLIVE THE CARDS' STATE (sim v3 D5): a reply that said
	// "nothing exists until you press Add" still stood under the card after
	// Add, and under a plan already running. The card says added or started.
	return said + ". Say in one line what you proposed and who does each. The card shows whether it was added or started, and your reply stays on the page after that: never say whether the tasks exist, and never tell them to press Add or Start.", nil
}

// checkCards is a proposal held to the rules, its names made handles: one to
// AgentCardsMost cards, each a title and a request, done by Afi or by a person
// in the workstream, owned by someone in it, waiting only on earlier cards.
func (h *Handler) checkCards(ws factory.OrgWorkstream, asker string, a api.AgentCards) (api.AgentCards, error) {
	if len(a.Tasks) == 0 || len(a.Tasks) > api.AgentCardsMost {
		return a, fmt.Errorf("invalid arguments: propose 1 to %d tasks at once", api.AgentCardsMost)
	}
	out := api.AgentCards{Tasks: make([]api.AgentCard, 0, len(a.Tasks))}
	if g := strings.TrimSpace(a.Group); g != "" {
		var err error
		if out.Group, err = agentWords(g, "group", 200); err != nil {
			return a, err
		}
	}
	me := factory.NormalizeHandle(asker)
	for i, c := range a.Tasks {
		n := i + 1
		title, err := agentWords(c.Title, "title", 200)
		if err != nil {
			return a, fmt.Errorf("card %d: %w", n, err)
		}
		request, err := agentWords(c.Request, "request", 4000)
		if err != nil {
			return a, fmt.Errorf("card %d: %w", n, err)
		}
		card := api.AgentCard{Title: title, Request: request, DoneBy: "afi"}
		switch strings.TrimSpace(c.DoneBy) {
		case "", "afi":
		case "person":
			card.DoneBy = "person"
			if card.Person, err = h.cardPerson(ws, me, c.Person); err != nil {
				return a, fmt.Errorf("card %d: %w", n, err)
			}
		default:
			return a, fmt.Errorf("invalid arguments: card %d's done_by is afi or person, not %q", n, c.DoneBy)
		}
		if card.Owner, err = h.cardPerson(ws, me, c.Owner); err != nil {
			return a, fmt.Errorf("card %d: %w", n, err)
		}
		if due := strings.TrimSpace(c.Due); due != "" {
			if _, err := time.Parse(time.DateOnly, due); err != nil {
				return a, fmt.Errorf("invalid arguments: card %d's due is YYYY-MM-DD", n)
			}
			card.Due = due
		}
		for _, w := range c.WaitsOn {
			if w < 1 || w >= n {
				return a, fmt.Errorf("invalid arguments: card %d waits on %d; a card waits only on earlier cards (1 to %d)", n, w, n-1)
			}
			if !containsInt(card.WaitsOn, w) {
				card.WaitsOn = append(card.WaitsOn, w)
			}
		}
		if len(c.Checklist) > factory.TaskChecklistMost {
			return a, fmt.Errorf("invalid arguments: card %d's checklist holds at most %d steps", n, factory.TaskChecklistMost)
		}
		for _, line := range c.Checklist {
			step, err := agentWords(line, "checklist step", factory.TaskChecklistLineMost)
			if err != nil {
				return a, fmt.Errorf("card %d: %w", n, err)
			}
			card.Checklist = append(card.Checklist, step)
		}
		out.Tasks = append(out.Tasks, card)
	}
	return out, nil
}

// cardPerson is word as a handle of someone who can edit ws; "" (and "me")
// is the person adding the cards, kept as "".
func (h *Handler) cardPerson(ws factory.OrgWorkstream, me, word string) (string, error) {
	word = strings.TrimPrefix(strings.TrimSpace(word), "@")
	switch strings.ToLower(word) {
	case "", "me", "you":
		return "", nil
	}
	handle := factory.NormalizeHandle(word)
	if handle == me {
		return "", nil
	}
	v := h.viewerOf(handle)
	if handle == "" || !v.canWrite(ws) {
		return "", fmt.Errorf("%q is not someone in this workstream: name a person's id from ws_tasks, or leave it empty for the person you are talking to", word)
	}
	return handle, nil
}

// cardsTold is who adding cards tells, by name ("Lena", "Lena and Marco"):
// everyone a card names who is not the person adding them.
func (h *Handler) cardsTold(cards api.AgentCards, me string) string {
	me = factory.NormalizeHandle(me)
	name := h.personNames()
	var told []string
	seen := map[string]bool{}
	for _, c := range cards.Tasks {
		for _, who := range []string{c.Person, c.Owner} {
			if who == "" || who == me || seen[who] {
				continue
			}
			seen[who] = true
			told = append(told, name(who))
		}
	}
	switch len(told) {
	case 0:
		return ""
	case 1:
		return told[0]
	}
	return strings.Join(told[:len(told)-1], ", ") + " and " + told[len(told)-1]
}

// cardsWords is a cards line's own words, for a reader that cannot draw the
// cards: "Proposed 2 tasks: Draft the post; Book the venue".
func cardsWords(cards api.AgentCards) string {
	titles := make([]string, len(cards.Tasks))
	for i, c := range cards.Tasks {
		titles[i] = c.Title
	}
	n := len(cards.Tasks)
	return fmt.Sprintf("Proposed %d task%s: %s", n, pluralS(n), strings.Join(titles, "; "))
}

// pluralS is the plural ending for n of something.
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func isAre(told string) string {
	if strings.Contains(told, " and ") {
		return "are"
	}
	return "is"
}

func containsInt(xs []int, x int) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// postAgentCards is POST /v1/workstreams/{id}/agent/cards/{seq}: the caller
// adds the ticked cards of one cards line in their own conversation. They are
// made as the caller, in order, and a note line records the tasks they became.
func (h *Handler) postAgentCards(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	if !v.canWrite(ws) {
		h.fail(w, http.StatusForbidden, "only its owner and those it is shared with for edit can add tasks to this workstream")
		return
	}
	person := h.orgActor(r)
	if person == "" {
		h.fail(w, http.StatusForbidden, "adding tasks needs a signed-in person")
		return
	}
	seq, err := strconv.Atoi(r.PathValue("seq"))
	if err != nil || seq < 1 {
		h.fail(w, http.StatusNotFound, "no such cards")
		return
	}
	var body api.AgentCardsAdd
	if !h.decode(w, r, &body, false) {
		return
	}
	seat := agentSeat{ws: ws.ID, person: factory.NormalizeHandle(person)}
	if status, msg := h.claimCards(seat, seq); status != 0 {
		h.fail(w, status, msg)
		return
	}
	defer h.releaseCards(seat, seq)
	cards, err := h.checkCards(ws, person, api.AgentCards{Group: body.Group, Tasks: body.Tasks})
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, strings.TrimPrefix(err.Error(), "invalid arguments: "))
		return
	}
	made, err := h.addCards(r, ws, person, cards)
	n := len(cards.Tasks)
	note := api.AgentEntry{Kind: api.AgentEntryNote, Took: seq, Tasks: made}
	switch {
	case err == nil:
		note.Text = fmt.Sprintf("Added %d task%s", n, pluralS(n))
		if told := h.cardsTold(cards, person); told != "" {
			note.Text += " · " + told + " " + isAre(told) + " told"
		}
	case len(made) == 0:
		h.fail(w, http.StatusUnprocessableEntity, "nothing was added: "+err.Error())
		return
	default:
		// Some were made and the rest were refused. The cards are taken all
		// the same, so pressing Add again never makes the first ones twice;
		// the note says what is left to add by hand.
		note.Text = fmt.Sprintf("Added %d of %d tasks; the rest were not added: %s", len(made), n, err.Error())
		note.Failed = true
	}
	h.addAgentEntry(seat, note)
	h.write(w, http.StatusOK, h.agentWire(ws, v, seat.person, "", 0))
}

// claimCards marks seq of seat's conversation as being added, or says why it
// cannot be: it is not a cards line kept here, or it was already added.
func (h *Handler) claimCards(seat agentSeat, seq int) (int, string) {
	h.agentDesk.mu.Lock()
	defer h.agentDesk.mu.Unlock()
	a := h.agentOfLocked(seat)
	found := false
	for _, e := range a.entries {
		if e.Seq == seq && e.Kind == api.AgentEntryCards {
			found = true
		}
		if e.Took == seq {
			return http.StatusConflict, "these tasks were already added"
		}
	}
	if !found {
		return http.StatusNotFound, "those cards are no longer here; ask Afi again"
	}
	if h.agentDesk.adding == nil {
		h.agentDesk.adding = map[cardsKey]bool{}
	}
	k := cardsKey{seat, seq}
	if h.agentDesk.adding[k] {
		return http.StatusConflict, "these tasks are being added"
	}
	h.agentDesk.adding[k] = true
	return 0, ""
}

func (h *Handler) releaseCards(seat agentSeat, seq int) {
	h.agentDesk.mu.Lock()
	delete(h.agentDesk.adding, cardsKey{seat, seq})
	h.agentDesk.mu.Unlock()
}

// cardsKey is one cards line of one conversation.
type cardsKey struct {
	seat agentSeat
	seq  int
}

// addCards makes cards' tasks in order, as the person r is from, and answers
// the ids made. A plan (cardsPlanAt or more) shares one group, so it waits
// for one press of Start.
func (h *Handler) addCards(r *http.Request, ws factory.OrgWorkstream, person string, cards api.AgentCards) ([]string, error) {
	var group *factory.TaskGroup
	if len(cards.Tasks) >= cardsPlanAt || cards.Group != "" {
		// A plan the proposal did not name is named for its first task, so it
		// never joins an earlier plan that happens to share a stock name.
		name := cards.Group
		if name == "" {
			name = cards.Tasks[0].Title
		}
		group = &factory.TaskGroup{Name: name, Kind: "batch"}
	}
	name := h.personNames()
	me := factory.NormalizeHandle(person)
	made := make([]string, 0, len(cards.Tasks))
	for _, c := range cards.Tasks {
		in := factory.TaskCreate{Title: c.Title, Request: c.Request, Due: c.Due, Checklist: c.Checklist,
			CameFrom: &factory.TaskCameFrom{Kind: factory.TaskByChat, ChatID: "afi", Name: "Afi"},
			DoneBy:   factory.TaskDoneBy{Kind: factory.TaskByAfi, Name: "Afi"},
			// Afi wrote the card's words, though the person adds it.
			WordsBy: factory.TaskByAfi}
		if c.DoneBy == "person" {
			who := c.Person
			if who == "" {
				who = me
			}
			in.DoneBy = factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: who, Name: name(who)}
		}
		if c.Owner != "" {
			in.Owner = &factory.TaskOwner{Kind: factory.TaskOwnerPerson, ID: c.Owner, Name: name(c.Owner)}
		}
		for _, wait := range c.WaitsOn {
			in.WaitingOn = append(in.WaitingOn, made[wait-1])
		}
		if group != nil {
			// The first task's group comes back with its id; the rest join it.
			in.Group = group
		}
		t, err := h.createAs(r, ws.ID, in)
		if err != nil {
			return made, fmt.Errorf("%q: %w", c.Title, err)
		}
		if group != nil && t.Group != nil {
			group = t.Group
		}
		made = append(made, t.ID)
	}
	return made, nil
}

// createAs makes one task through the route a person's own Add task takes,
// as the person r is from: their rights, their name on it.
func (h *Handler) createAs(r *http.Request, ws string, in factory.TaskCreate) (factory.WorkstreamTask, error) {
	var t factory.WorkstreamTask
	raw, err := json.Marshal(in)
	if err != nil {
		return t, err
	}
	rr := r.Clone(r.Context())
	rr.Method = http.MethodPost
	rr.Body = io.NopCloser(bytes.NewReader(raw))
	rr.ContentLength = int64(len(raw))
	rr.Header.Set("Content-Type", "application/json")
	rr.Header.Del("If-Match")
	rr.SetPathValue("id", ws)
	w := httptest.NewRecorder()
	h.createWorkstreamTask(w, rr)
	if w.Code >= 300 {
		var refused struct {
			Error string `json:"error"`
		}
		msg := strings.TrimSpace(w.Body.String())
		if json.Unmarshal(w.Body.Bytes(), &refused) == nil && refused.Error != "" {
			msg = refused.Error
		}
		return t, errors.New(msg)
	}
	return t, json.Unmarshal(w.Body.Bytes(), &t)
}
