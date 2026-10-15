package appwire

import (
	"strings"
	"time"
)

// Wave 7: closing, archiving, finding (PROTOCOL.md). A closed agent keeps its
// whole conversation and leaves the rail; an archived one is put away further
// and listed only when asked for. Search reaches agents, crews, tasks and the
// words of every conversation.

// StateArchived is an agent put away: closed, and listed only when asked.
const StateArchived = "archived"

// The listing states agents.list takes.
const (
	ListOpen     = "open"
	ListClosed   = "closed"
	ListArchived = "archived"
	ListAll      = "all"
)

// The kinds of a search group.
const (
	HitAgent   = "agent"
	HitCrew    = "crew"
	HitTask    = "task"
	HitMessage = "message"
)

// Hit is one search result: what it is, the agent it belongs to, its title,
// and for a message the words around the match.
type Hit struct {
	Kind    string     `json:"kind"`
	ID      string     `json:"id"`
	Agent   string     `json:"agent,omitempty"`
	Title   string     `json:"title"`
	Excerpt string     `json:"excerpt,omitempty"`
	At      *time.Time `json:"at,omitempty"`
}

// HitGroup is the hits of one kind.
type HitGroup struct {
	Kind string `json:"kind"`
	Hits []Hit  `json:"hits"`
}

// Tidier is an engine that closes, archives, reopens, renames and finds. It is
// asserted, as Crewer is, so an engine without it answers unknown_method.
type Tidier interface {
	ListAgentsIn(workspace, state, query string) ([]Agent, error)
	MoveToClosed(ids []string) error
	Archive(ids []string) error
	Reopen(id string) (Agent, error)
	Rename(id, title string) (Agent, error)
	IdleAgents(days int) ([]string, error)
	SearchAll(query string, perGroup int) ([]HitGroup, error)
}

func init() {
	for name, m := range tidyMethods {
		methods[name] = m
	}
}

func tidier(e Engine, name string) (Tidier, error) {
	t, ok := e.(Tidier)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return t, nil
}

// tidyCall decodes a method's params into P and answers what call answers.
func tidyCall[P, R any](name string, call func(Tidier, P) (*R, error)) method {
	return typed(func(e Engine, p P) (*R, error) {
		t, err := tidier(e, name)
		if err != nil {
			return nil, err
		}
		return call(t, p)
	})
}

type tidyIDs struct {
	IDs []string `json:"ids"`
}

func needIDs(name string, ids []string) error {
	if len(ids) == 0 {
		return Errorf(CodeBadParams, "%s needs ids", name)
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return Errorf(CodeBadParams, "%s has an empty id", name)
		}
	}
	return nil
}

func agentOf(a Agent, err error) (*agentResult, error) {
	if err != nil {
		return nil, err
	}
	return &agentResult{a}, nil
}

var tidyMethods = map[string]method{
	"agents.move_to_closed": tidyCall("agents.move_to_closed", func(t Tidier, p tidyIDs) (*Empty, error) {
		if err := needIDs("agents.move_to_closed", p.IDs); err != nil {
			return nil, err
		}
		return done(t.MoveToClosed(p.IDs))
	}),
	"agents.archive": tidyCall("agents.archive", func(t Tidier, p tidyIDs) (*Empty, error) {
		if err := needIDs("agents.archive", p.IDs); err != nil {
			return nil, err
		}
		return done(t.Archive(p.IDs))
	}),
	"agents.reopen": tidyCall("agents.reopen", func(t Tidier, p crewID) (*agentResult, error) {
		if err := needID("agents.reopen", p.ID); err != nil {
			return nil, err
		}
		return agentOf(t.Reopen(p.ID))
	}),
	"agents.rename": tidyCall("agents.rename", func(t Tidier, p struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}) (*agentResult, error) {
		if err := needID("agents.rename", p.ID); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Title) == "" {
			return nil, Errorf(CodeBadParams, "agents.rename needs a title")
		}
		return agentOf(t.Rename(p.ID, strings.TrimSpace(p.Title)))
	}),
	"agents.idle": tidyCall("agents.idle", func(t Tidier, p struct {
		Days int `json:"days"`
	}) (*IDsResult, error) {
		if p.Days < 0 {
			return nil, Errorf(CodeBadParams, "days cannot be negative")
		}
		ids, err := t.IdleAgents(p.Days)
		if err != nil {
			return nil, err
		}
		return &IDsResult{nonNil(ids)}, nil
	}),
	"search.all": tidyCall("search.all", func(t Tidier, p struct {
		Query    string `json:"query"`
		PerGroup int    `json:"limit_per_group"`
	}) (*GroupsResult2, error) {
		if strings.TrimSpace(p.Query) == "" {
			return nil, Errorf(CodeBadParams, "search.all needs a query")
		}
		if p.PerGroup <= 0 || p.PerGroup > 50 {
			p.PerGroup = 8
		}
		groups, err := t.SearchAll(strings.TrimSpace(p.Query), p.PerGroup)
		if err != nil {
			return nil, err
		}
		for i := range groups {
			groups[i].Hits = nonNil(groups[i].Hits)
		}
		return &GroupsResult2{nonNil(groups)}, nil
	}),
}

// The answers of this file's methods.
type (
	GroupsResult2 struct {
		Groups []HitGroup `json:"groups"`
	}
)
