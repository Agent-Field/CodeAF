package serve

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── AFI'S SETUP CARD, KEPT ON THE ORGANIZATION'S SERVER ────────────────────
//
// session's ws_propose_setup (tools_workstream_setup.go) is answered here for
// an organization's workstream: the card is kept on the workstream for the
// person whose message Afi answered, and only that person reads it back
// (GET's setup_card). Accept is theirs, through the ordinary routes, and it
// clears the card with PATCH's drop_setup_card.
//
//   - KEEPING A CARD MOVES NO REV. Nobody guards a card with a rev, so the
//     save is a mirror save: a person editing the workstream's rules while
//     Afi drafts is never refused for it.
//
//   - A TASK'S RUN NEVER PROPOSES. A card is for the person who is talking to
//     Afi; a run that works one task has nobody to accept it.

// KeepsSetupCards says the server's door answers ws_propose_setup.
func (d *agentDoor) KeepsSetupCards() bool { return true }

var errSetupInTaskRun = errors.New("a task's run cannot propose a workstream's setup; only a conversation with a person can")

// wsProposeSetup keeps the card for the asker and answers what the person
// will see, with the names it could not match to anyone in the organization.
func (d *agentDoor) wsProposeSetup(_ context.Context, args json.RawMessage) (any, error) {
	if d.wrote != nil {
		return nil, errSetupInTaskRun
	}
	card, err := factory.ParseWorkstreamSetupCard(args)
	if err != nil {
		return nil, err
	}
	who := factory.NormalizeHandle(d.asker)
	card.By, card.At = who, d.h.clock().UTC().Format("2006-01-02T15:04:05Z")
	var before factory.OrgWorkstream
	saved, err := d.h.config.Store.MirrorWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		cur, ok := all[d.id]
		if !ok {
			return nil, errWorkstreamGone
		}
		if cur.ArchivedAt != nil {
			return nil, errWorkstreamArchived
		}
		before = cur
		cards := make(map[string]factory.WorkstreamSetupCard, len(cur.SetupCards)+1)
		for k, v := range cur.SetupCards {
			cards[k] = v
		}
		cards[who] = card
		cur.SetupCards = cards
		all[cur.ID] = cur
		return []string{cur.ID}, nil
	})
	if err != nil {
		return nil, err
	}
	d.h.publishWorkstreams(api.EventWorkstreamChanged, who, &before, saved[d.id])
	words := "Shown to " + d.h.personNames()(who) + " as a card beside this conversation, to change and accept. Nothing is set up until they press Accept."
	if missing := d.h.setupUnknown(card); missing != "" {
		words += " " + missing
	}
	return words, nil
}

// setupUnknown names the people and teams on card that nobody in the
// organization answers to, and who is there, so Afi's next card uses real
// names. Empty when every name matched.
func (h *Handler) setupUnknown(card factory.WorkstreamSetupCard) string {
	var people, handles []string
	for _, m := range h.memberList() {
		people = append(people, strings.TrimSpace(m.Name))
		handles = append(handles, factory.NormalizeHandle(m.Handle))
	}
	var teams []string
	if all, err := h.config.Store.Teams(); err == nil {
		for _, t := range all {
			teams = append(teams, t.Name)
		}
	}
	matches := func(name string, in ...[]string) bool {
		for _, xs := range in {
			for _, x := range xs {
				if x != "" && (strings.EqualFold(x, name) || strings.EqualFold(strings.Fields(x + " ")[0], name)) {
					return true
				}
			}
		}
		return false
	}
	var unknown []string
	for _, p := range append(append([]string{card.IncomingOwner, card.Decider, card.Fallback}, card.People...), taskPeople(card)...) {
		if p != "" && !strings.EqualFold(p, "me") && !matches(p, people, handles) && !containsName(unknown, p) {
			unknown = append(unknown, p)
		}
	}
	for _, t := range card.Teams {
		if !matches(t, teams) && !containsName(unknown, t) {
			unknown = append(unknown, t)
		}
	}
	if len(unknown) == 0 {
		return ""
	}
	sort.Strings(people)
	line := "Nobody in the organization is called " + strings.Join(unknown, ", ") + "; the card leaves them out. People here: " + strings.Join(nonEmpty(people), ", ") + "."
	if len(teams) > 0 {
		sort.Strings(teams)
		line += " Teams: " + strings.Join(teams, ", ") + "."
	}
	return line
}

func taskPeople(card factory.WorkstreamSetupCard) []string {
	var out []string
	for _, t := range card.Tasks {
		if t.Doer == "person" {
			out = append(out, t.Person)
		}
	}
	return out
}

func containsName(xs []string, x string) bool {
	for _, y := range xs {
		if strings.EqualFold(x, y) {
			return true
		}
	}
	return false
}

func nonEmpty(xs []string) []string {
	out := xs[:0:0]
	for _, x := range xs {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

// setupCardFor is the card ws keeps for the viewer, nil when there is none.
// A CARD IS ITS PERSON'S ALONE: everyone else reads none.
func setupCardFor(ws factory.OrgWorkstream, v Viewer) *api.WorkstreamSetupCard {
	card, ok := ws.SetupCards[factory.NormalizeHandle(v.Handle)]
	if !ok {
		return nil
	}
	return &card
}

// dropSetupCard clears the card ws keeps for handle.
func dropSetupCard(ws *factory.OrgWorkstream, handle string) {
	who := factory.NormalizeHandle(handle)
	if _, ok := ws.SetupCards[who]; !ok {
		return
	}
	cards := make(map[string]factory.WorkstreamSetupCard, len(ws.SetupCards))
	for k, v := range ws.SetupCards {
		if k != who {
			cards[k] = v
		}
	}
	if len(cards) == 0 {
		cards = nil
	}
	ws.SetupCards = cards
}
