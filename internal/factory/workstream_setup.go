package factory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ── A WORKSTREAM'S SETUP CARD (owner decision 2026-11-17 §9) ───────────────
//
// Afi proposes how a new workstream is set up with session's
// ws_propose_setup; the card is kept here, one per person who asked, until
// that person accepts or drops it in the app. KEEPING A CARD WRITES NOTHING
// SHARED: no task, file, rule or person changes until Accept, which the app
// makes through the ordinary routes as the person.

// WorkstreamSetupCard is one proposed setup, as the tool's arguments say it.
// By is the handle it was proposed for and At when, both stamped by the
// server.
type WorkstreamSetupCard struct {
	Name                  string                `json:"name"`
	Goal                  string                `json:"goal"`
	Brief                 string                `json:"brief,omitempty"`
	People                []string              `json:"people,omitempty"`
	Teams                 []string              `json:"teams,omitempty"`
	IncomingOwner         string                `json:"incoming_owner,omitempty"`
	Decider               string                `json:"decider,omitempty"`
	Fallback              string                `json:"fallback,omitempty"`
	StartWithoutAskingUSD *float64              `json:"start_without_asking_usd,omitempty"`
	Rules                 []string              `json:"rules,omitempty"`
	Tasks                 []WorkstreamSetupTask `json:"tasks"`
	Inside                string                `json:"inside,omitempty"`
	By                    string                `json:"by,omitempty"`
	At                    string                `json:"at,omitempty"`
}

// WorkstreamSetupTask is one task of a card's first plan. WaitsOn are the
// 1-based positions of other tasks in the same plan.
type WorkstreamSetupTask struct {
	Title   string `json:"title"`
	Request string `json:"request,omitempty"`
	Doer    string `json:"doer,omitempty"`
	Person  string `json:"person,omitempty"`
	WaitsOn []int  `json:"waits_on,omitempty"`
	Due     string `json:"due,omitempty"`
}

// The limits a card is held to; the tool's schema states the same numbers.
const (
	SetupMostTasks  = 8
	SetupMostRules  = 5
	SetupMostPeople = 20
	SetupMostTeams  = 10
	SetupMostSpend  = 1000
)

var setupDay = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// ParseWorkstreamSetupCard reads the tool's arguments into a card, or says in
// words what is wrong with them. Words are trimmed; nothing is invented.
func ParseWorkstreamSetupCard(args json.RawMessage) (WorkstreamSetupCard, error) {
	var c WorkstreamSetupCard
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
		n := utf8.RuneCountInString(*s)
		switch {
		case n < least:
			err = fmt.Errorf("invalid arguments: %s needs words", what)
		case n > most:
			err = fmt.Errorf("invalid arguments: %s is longer than %d characters", what, most)
		}
	}
	list := func(xs []string, what string, most, each int) []string {
		if err != nil {
			return xs
		}
		if len(xs) > most {
			err = fmt.Errorf("invalid arguments: at most %d %s", most, what)
			return xs
		}
		out := make([]string, 0, len(xs))
		for _, x := range xs {
			words(&x, what, 1, each)
			if err == nil && !containsFold(out, x) {
				out = append(out, x)
			}
		}
		return out
	}
	words(&c.Name, "name", 1, 80)
	words(&c.Goal, "goal", 1, 400)
	if err == nil && utf8.RuneCountInString(strings.TrimSpace(c.Brief)) > 8000 {
		err = fmt.Errorf("invalid arguments: brief is longer than 8000 characters")
	}
	c.Brief = strings.TrimSpace(c.Brief)
	c.People = list(c.People, "people", SetupMostPeople, 80)
	c.Teams = list(c.Teams, "teams", SetupMostTeams, 80)
	c.Rules = list(c.Rules, "rules", SetupMostRules, 200)
	words(&c.IncomingOwner, "incoming_owner", 0, 80)
	words(&c.Decider, "decider", 0, 80)
	words(&c.Fallback, "fallback", 0, 80)
	words(&c.Inside, "inside", 0, 80)
	if err != nil {
		return c, err
	}
	if s := c.StartWithoutAskingUSD; s != nil && (*s < 0 || *s > SetupMostSpend) {
		return c, fmt.Errorf("invalid arguments: start_without_asking_usd is between 0 and %d", SetupMostSpend)
	}
	if len(c.Tasks) < 1 || len(c.Tasks) > SetupMostTasks {
		return c, fmt.Errorf("invalid arguments: a first plan has 1 to %d tasks; make each a deliverable, not a step", SetupMostTasks)
	}
	for i := range c.Tasks {
		t := &c.Tasks[i]
		at := fmt.Sprintf("task %d", i+1)
		words(&t.Title, at+"'s title", 1, 120)
		words(&t.Person, at+"'s person", 0, 80)
		if err != nil {
			return c, err
		}
		t.Request = strings.TrimSpace(t.Request)
		if utf8.RuneCountInString(t.Request) > 1000 {
			return c, fmt.Errorf("invalid arguments: %s's request is longer than 1000 characters", at)
		}
		switch t.Doer {
		case "", "afi":
			t.Doer, t.Person = "afi", ""
		case "person":
			if t.Person == "" {
				t.Person = "me"
			}
		default:
			return c, fmt.Errorf("invalid arguments: %s's doer is afi or person", at)
		}
		if t.Due != "" && !setupDay.MatchString(t.Due) {
			return c, fmt.Errorf("invalid arguments: %s's due is YYYY-MM-DD", at)
		}
		seen := map[int]bool{}
		waits := t.WaitsOn[:0]
		for _, w := range t.WaitsOn {
			if w < 1 || w > len(c.Tasks) || w == i+1 {
				return c, fmt.Errorf("invalid arguments: %s waits on %d, which is not another task in this plan", at, w)
			}
			if !seen[w] {
				seen[w] = true
				waits = append(waits, w)
			}
		}
		t.WaitsOn = waits
	}
	if cyclic(c.Tasks) {
		return c, fmt.Errorf("invalid arguments: the tasks wait on each other in a circle")
	}
	return c, nil
}

func containsFold(xs []string, x string) bool {
	for _, y := range xs {
		if strings.EqualFold(x, y) {
			return true
		}
	}
	return false
}

// cyclic says whether the plan's waits loop back on themselves.
func cyclic(tasks []WorkstreamSetupTask) bool {
	state := make([]int, len(tasks))
	var visit func(i int) bool
	visit = func(i int) bool {
		if state[i] == 1 {
			return true
		}
		if state[i] == 2 {
			return false
		}
		state[i] = 1
		for _, w := range tasks[i].WaitsOn {
			if visit(w - 1) {
				return true
			}
		}
		state[i] = 2
		return false
	}
	for i := range tasks {
		if visit(i) {
			return true
		}
	}
	return false
}
