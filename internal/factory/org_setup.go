package factory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"
)

// ── AN ORGANIZATION'S SETUP CARD (v2 designs J3, DELTA.md §2.11) ───────────
//
// A new organization is empty. Afi asks the person who started it what is on
// their plate, and answers with ONE card, proposed through session's
// org_propose_setup: the workstreams it would make (each with what it is for,
// who is in it, and the workstreams inside it), the teams, and the people to
// invite with their email and team. The person unticks, renames and edits the
// card and presses one button, which makes all of it on the server in a fixed
// order (serve/org_setup.go).
//
//   - PROPOSING WRITES NOTHING SHARED. The card is kept for the person who
//     asked, and nobody is invited and nothing is made until the press.
//
//   - TOP LEVEL OR INSIDE IS DECIDED BY WHO THE PEOPLE ARE. A workstream with
//     the same people as its parent is one piece of work by them, so it goes
//     inside; different people make a top-level workstream. Afi is told the
//     rule, and a child carries no people of its own: it has its parent's.

// OrgSetupCard is one proposed organization setup, as the tool's arguments
// say it. By and At are stamped by the server.
type OrgSetupCard struct {
	Workstreams []OrgSetupWorkstream `json:"workstreams"`
	Teams       []OrgSetupTeam       `json:"teams,omitempty"`
	Invites     []OrgSetupInvite     `json:"invites,omitempty"`
	By          string               `json:"by,omitempty"`
	At          string               `json:"at,omitempty"`
}

// OrgSetupWorkstream is one top-level workstream on a card. People are names
// as the person said them; "me" is the person who asked. Inside are the
// workstreams that are one piece of its work, by the same people.
type OrgSetupWorkstream struct {
	Name    string          `json:"name"`
	Purpose string          `json:"purpose,omitempty"`
	People  []string        `json:"people,omitempty"`
	Inside  []OrgSetupChild `json:"inside,omitempty"`
}

// OrgSetupChild is a workstream inside a top-level one.
type OrgSetupChild struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose,omitempty"`
}

// OrgSetupTeam is a team on a card: a group of people added to a workstream
// in one step. Its people are invitees' names.
type OrgSetupTeam struct {
	Name   string   `json:"name"`
	People []string `json:"people,omitempty"`
}

// OrgSetupInvite is a person to invite. Email may be empty ("sam@… add an
// email"): the person adds it on the card, or the invite is a link to copy.
type OrgSetupInvite struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	Team  string `json:"team,omitempty"`
}

// The limits a card is held to; the tool's schema states the same numbers.
const (
	OrgSetupMostWorkstreams = 12
	OrgSetupMostInside      = 6
	OrgSetupMostTeams       = 6
	OrgSetupMostInvites     = 30
	OrgSetupMostPeople      = 30
	OrgSetupNameMost        = 80
	OrgSetupPurposeMost     = 300
)

// ParseOrgSetupCard reads the tool's arguments (or the app's edited card)
// into a card, or says in words what is wrong. Words are trimmed; nothing is
// invented. Two workstreams, teams or invites with one name are refused,
// because a retry finds what it already made by name.
func ParseOrgSetupCard(args json.RawMessage) (OrgSetupCard, error) {
	var c OrgSetupCard
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("invalid arguments: %v", err)
	}
	c.By, c.At = "", ""
	var err error
	words := func(s *string, what string, least, most int) {
		if err != nil {
			return
		}
		*s = strings.Join(strings.Fields(*s), " ")
		switch n := utf8.RuneCountInString(*s); {
		case n < least:
			err = fmt.Errorf("%s is empty", what)
		case n > most:
			err = fmt.Errorf("%s is longer than %d characters", what, most)
		}
	}
	names := func(xs []string, what string) []string {
		out := xs[:0:0]
		for _, x := range xs {
			x = strings.Join(strings.Fields(x), " ")
			if x != "" && !containsFold(out, x) {
				out = append(out, x)
			}
		}
		if len(out) > OrgSetupMostPeople && err == nil {
			err = fmt.Errorf("%s names more than %d people", what, OrgSetupMostPeople)
		}
		return out
	}
	switch {
	case len(c.Workstreams) == 0 && len(c.Invites) == 0:
		return c, fmt.Errorf("the card has no workstreams and nobody to invite")
	case len(c.Workstreams) > OrgSetupMostWorkstreams:
		return c, fmt.Errorf("a card holds at most %d workstreams", OrgSetupMostWorkstreams)
	case len(c.Teams) > OrgSetupMostTeams:
		return c, fmt.Errorf("a card holds at most %d teams", OrgSetupMostTeams)
	case len(c.Invites) > OrgSetupMostInvites:
		return c, fmt.Errorf("a card invites at most %d people", OrgSetupMostInvites)
	}
	var seen []string
	for i := range c.Workstreams {
		w := &c.Workstreams[i]
		words(&w.Name, fmt.Sprintf("workstream %d's name", i+1), 1, OrgSetupNameMost)
		words(&w.Purpose, w.Name+"'s purpose", 0, OrgSetupPurposeMost)
		w.People = names(w.People, w.Name)
		if err != nil {
			return c, err
		}
		if containsFold(seen, w.Name) {
			return c, fmt.Errorf("two workstreams are called %s", w.Name)
		}
		seen = append(seen, w.Name)
		if len(w.Inside) > OrgSetupMostInside {
			return c, fmt.Errorf("%s holds at most %d workstreams inside it", w.Name, OrgSetupMostInside)
		}
		var inside []string
		for j := range w.Inside {
			ch := &w.Inside[j]
			words(&ch.Name, fmt.Sprintf("the name of workstream %d inside %s", j+1, w.Name), 1, OrgSetupNameMost)
			words(&ch.Purpose, ch.Name+"'s purpose", 0, OrgSetupPurposeMost)
			if err != nil {
				return c, err
			}
			if containsFold(inside, ch.Name) {
				return c, fmt.Errorf("two workstreams inside %s are called %s", w.Name, ch.Name)
			}
			inside = append(inside, ch.Name)
		}
	}
	var teams []string
	for i := range c.Teams {
		t := &c.Teams[i]
		words(&t.Name, fmt.Sprintf("team %d's name", i+1), 1, OrgSetupNameMost)
		t.People = names(t.People, t.Name)
		if err != nil {
			return c, err
		}
		if containsFold(teams, t.Name) {
			return c, fmt.Errorf("two teams are called %s", t.Name)
		}
		teams = append(teams, t.Name)
	}
	var invited []string
	for i := range c.Invites {
		p := &c.Invites[i]
		words(&p.Name, fmt.Sprintf("invite %d's name", i+1), 1, OrgSetupNameMost)
		words(&p.Email, p.Name+"'s email", 0, 254)
		words(&p.Team, p.Name+"'s team", 0, OrgSetupNameMost)
		if err != nil {
			return c, err
		}
		if strings.EqualFold(p.Name, "me") {
			return c, fmt.Errorf("the person you are talking to is not invited")
		}
		if containsFold(invited, p.Name) {
			return c, fmt.Errorf("%s is invited twice", p.Name)
		}
		invited = append(invited, p.Name)
		if p.Email != "" {
			if a, perr := mail.ParseAddress(p.Email); perr != nil || a.Address != p.Email {
				return c, fmt.Errorf("%s is not an email address", p.Email)
			}
		}
		if p.Team != "" && !containsFold(teams, p.Team) {
			// A team named only on an invite is a team on the card.
			c.Teams = append(c.Teams, OrgSetupTeam{Name: p.Team})
			teams = append(teams, p.Team)
		}
	}
	// A team's people are invitees; the invite carries the team.
	for _, t := range c.Teams {
		for _, who := range t.People {
			for i := range c.Invites {
				if strings.EqualFold(c.Invites[i].Name, who) && c.Invites[i].Team == "" {
					c.Invites[i].Team = t.Name
				}
			}
		}
	}
	if len(c.Teams) > OrgSetupMostTeams {
		return c, fmt.Errorf("a card holds at most %d teams", OrgSetupMostTeams)
	}
	return c, nil
}

// InsideCount is how many workstreams the card puts inside others.
func (c OrgSetupCard) InsideCount() int {
	n := 0
	for _, w := range c.Workstreams {
		n += len(w.Inside)
	}
	return n
}

func containsFold(xs []string, x string) bool {
	for _, y := range xs {
		if strings.EqualFold(x, y) {
			return true
		}
	}
	return false
}
