package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory/aisearch"
)

// ── THE JOB SEARCH BAR'S AI SEARCH ──────────────────────────────────────────
//
// A person's question in their own words, answered with the jobs and issues it
// is about, each with one sentence why (internal/factory/aisearch: a cheap
// screen over every item in batches, then the routing rule's own yes/no judge
// over the shortlist).
//
// IT IS NEVER STARTED BY TYPING. factory.ai_search_estimate is free and asks
// no model; factory.ai_search is the person's press of Ask AI, and it starts
// one run in the background and answers its id at once. The surface reads the
// run's progress and, at the end, its matches with factory.ai_search_status
// (a poll, so a reconnect or a second window reads the same run), and
// factory.ai_search_stop ends it. Every model call is on the spend ledger
// under `factory search`.
//
// JOBS SCOPES THE SEARCH to the rows the bar lists (the field's repo:, state:
// and other keys applied); omitted, it reads every issue job the caller can
// see that is not dropped. Every answer is kept per factory, so the same
// question again (a saved view reopened) asks the model only about items that
// arrived or changed since.

// FeatureFactoryAISearch is announced in hello when the engine serves the four
// factory.ai_search methods.
const FeatureFactoryAISearch = "factory.ai_search"

// AISearchRunsMost is how many runs one engine keeps readable at once; the
// oldest finished one is forgotten first.
const AISearchRunsMost = 16

// FactoryAISearchParams is a question about a factory's jobs.
type FactoryAISearchParams struct {
	Factory  string `json:"factory"`
	Question string `json:"question"`
	// Jobs are the job ids to read, in the order the bar lists them; omitted
	// is every issue job the caller can see.
	Jobs []int `json:"jobs,omitempty"`
}

// FactoryAISearchEstimate is what a search would cost before it runs: the
// model it would ask, and aisearch.Estimate's counts and dollars (absent
// without the model's price).
type FactoryAISearchEstimate struct {
	Factory  string `json:"factory"`
	Question string `json:"question"`
	Model    string `json:"model,omitempty"`
	aisearch.Estimate
}

// FactoryAISearchStarted is the run a press of Ask AI started.
type FactoryAISearchStarted struct {
	Run string `json:"run"`
}

// FactoryAISearchRunRef names one run.
type FactoryAISearchRunRef struct {
	Run string `json:"run"`
}

// FactoryAISearchRun is one run as far as it has gone: its question and
// aisearch.Status (phase, counts, cost, and the matches so far).
type FactoryAISearchRun struct {
	Run      string `json:"run"`
	Factory  string `json:"factory"`
	Question string `json:"question"`
	// Words is the one progress sentence a surface shows
	// ("Reading 80 issues… 12 look related").
	Words string `json:"words,omitempty"`
	aisearch.Status
}

// FactoryAISearcher is the engine's half of the four methods.
type FactoryAISearcher interface {
	FactoryAISearchEstimate(ctx context.Context, p FactoryAISearchParams) (FactoryAISearchEstimate, error)
	FactoryAISearchStart(ctx context.Context, p FactoryAISearchParams) (FactoryAISearchStarted, error)
	FactoryAISearchStatus(run string) (FactoryAISearchRun, error)
	FactoryAISearchStop(run string) (FactoryAISearchRun, error)
}

// CheckAISearch bounds a search before anyone is asked: a factory, a question
// of at most aisearch.QuestionMost characters once cleaned, and at most
// aisearch.ItemsMost distinct positive job ids.
func CheckAISearch(p FactoryAISearchParams) (FactoryAISearchParams, error) {
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return p, err
	}
	if utf8.RuneCountInString(strings.TrimSpace(p.Question)) > aisearch.QuestionMost+1 {
		return p, Errorf(CodeBadParams, "question is at most %d characters", aisearch.QuestionMost)
	}
	if p.Question = aisearch.Clean(p.Question); p.Question == "" {
		return p, Errorf(CodeBadParams, "question is required")
	}
	if len(p.Jobs) > aisearch.ItemsMost {
		return p, Errorf(CodeBadParams, "jobs is at most %d ids", aisearch.ItemsMost)
	}
	seen := map[int]bool{}
	jobs := make([]int, 0, len(p.Jobs))
	for _, id := range p.Jobs {
		if id <= 0 {
			return p, Errorf(CodeBadParams, "jobs are positive job ids")
		}
		if !seen[id] {
			seen[id] = true
			jobs = append(jobs, id)
		}
	}
	if p.Jobs != nil {
		p.Jobs = jobs
	}
	return p, nil
}

func checkAISearchRun(p FactoryAISearchRunRef) (string, error) {
	run := strings.TrimSpace(p.Run)
	if run == "" || len(run) > 64 {
		return "", Errorf(CodeBadParams, "run is required")
	}
	return run, nil
}

func withFactoryAISearchFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryAISearcher); ok {
		hello = withFeature(hello, FeatureFactoryAISearch)
	}
	return hello
}

func aiSearcher(e Engine, name string) (FactoryAISearcher, error) {
	s, ok := e.(FactoryAISearcher)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return s, nil
}

func init() {
	const estimate = "factory.ai_search_estimate"
	methods[estimate] = shaped[FactoryAISearchParams, FactoryAISearchEstimate](func(e Engine, raw json.RawMessage) (*FactoryAISearchEstimate, error) {
		s, err := aiSearcher(e, estimate)
		if err != nil {
			return nil, err
		}
		var p FactoryAISearchParams
		if err := decodeStrict(estimate, raw, &p); err != nil {
			return nil, err
		}
		if p, err = CheckAISearch(p); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := s.FactoryAISearchEstimate(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
	const start = "factory.ai_search"
	methods[start] = shaped[FactoryAISearchParams, FactoryAISearchStarted](func(e Engine, raw json.RawMessage) (*FactoryAISearchStarted, error) {
		s, err := aiSearcher(e, start)
		if err != nil {
			return nil, err
		}
		var p FactoryAISearchParams
		if err := decodeStrict(start, raw, &p); err != nil {
			return nil, err
		}
		if p, err = CheckAISearch(p); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := s.FactoryAISearchStart(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
	const status = "factory.ai_search_status"
	methods[status] = shaped[FactoryAISearchRunRef, FactoryAISearchRun](func(e Engine, raw json.RawMessage) (*FactoryAISearchRun, error) {
		s, err := aiSearcher(e, status)
		if err != nil {
			return nil, err
		}
		var p FactoryAISearchRunRef
		if err := decodeStrict(status, raw, &p); err != nil {
			return nil, err
		}
		run, err := checkAISearchRun(p)
		if err != nil {
			return nil, err
		}
		out, err := s.FactoryAISearchStatus(run)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
	const stop = "factory.ai_search_stop"
	methods[stop] = shaped[FactoryAISearchRunRef, FactoryAISearchRun](func(e Engine, raw json.RawMessage) (*FactoryAISearchRun, error) {
		s, err := aiSearcher(e, stop)
		if err != nil {
			return nil, err
		}
		var p FactoryAISearchRunRef
		if err := decodeStrict(stop, raw, &p); err != nil {
			return nil, err
		}
		run, err := checkAISearchRun(p)
		if err != nil {
			return nil, err
		}
		out, err := s.FactoryAISearchStop(run)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
