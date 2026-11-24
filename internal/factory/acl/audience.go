package acl

import (
	"slices"
	"time"
)

// Audience is who hears about a change to an item: the persons and teams that
// hold any access to it, the operator when it may see the item, and every
// member when the item is visible to the organization. Events are filtered by
// an Audience, so what it leaves out can never leak through the event stream.
type Audience struct {
	Persons  []string `json:"persons,omitempty"`
	Teams    []string `json:"teams,omitempty"`
	Operator bool     `json:"operator,omitempty"`
	Org      bool     `json:"org,omitempty"`
}

// include adds one subject to the audience.
func (a *Audience) include(s Subject) {
	switch s.Kind {
	case "person":
		a.Persons = append(a.Persons, s.ID)
	case "team":
		a.Teams = append(a.Teams, s.ID)
	case "org":
		a.Org = a.Org || s.ID == "*"
	}
}

// Audience computes who may view the given items at now, as the union over the
// items. It reads the same facts Verbs does, so an event is heard by exactly
// the people who could fetch what changed.
func (p Policy) Audience(now time.Time, items ...Item) Audience {
	var a Audience
	for _, it := range items {
		if it.OwnerKind == "person" || it.OwnerKind == "team" {
			a.include(Subject{Kind: it.OwnerKind, ID: it.OwnerID})
		}
		a.Operator = a.Operator || !it.Private || p.Operator.SeesPrivate
		if it.Private && !it.Closed {
			continue
		}
		if !it.Private {
			a.Org = a.Org || it.Visibility == Org
			for _, s := range it.Shares {
				if s.Kind == "person" || s.Kind == "team" {
					a.include(Subject{Kind: s.Kind, ID: s.ID})
				}
			}
		}
		for _, g := range it.Grants {
			if g.Live(now) {
				if _, exists := p.Role(g.Role); exists {
					a.include(g.Subject)
				}
			}
		}
	}
	return Union(a)
}

// Union merges audiences into one with sorted, unique persons and teams. A
// change event goes to the union of the audience before and after, so someone
// who loses access hears it go and someone who gains it hears it arrive.
func Union(audiences ...Audience) Audience {
	out := Audience{Persons: []string{}, Teams: []string{}}
	for _, a := range audiences {
		out.Persons = append(out.Persons, a.Persons...)
		out.Teams = append(out.Teams, a.Teams...)
		out.Operator = out.Operator || a.Operator
		out.Org = out.Org || a.Org
	}
	slices.Sort(out.Persons)
	slices.Sort(out.Teams)
	out.Persons = slices.Compact(out.Persons)
	out.Teams = slices.Compact(out.Teams)
	return out
}

// Equal reports whether two audiences hold the same people, teams and flags.
func (a Audience) Equal(b Audience) bool {
	return a.Operator == b.Operator && a.Org == b.Org &&
		slices.Equal(a.Persons, b.Persons) && slices.Equal(a.Teams, b.Teams)
}

// Hears reports whether the caller is in the audience.
func (a Audience) Hears(c Caller) bool {
	return c.Solo || c.Operator && a.Operator || c.Member && a.Org ||
		c.Handle != "" && slices.Contains(a.Persons, c.Handle) ||
		slices.ContainsFunc(a.Teams, func(team string) bool { return c.Teams[team] })
}
