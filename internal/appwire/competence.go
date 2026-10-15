package appwire

import "time"

// V76: the competence map — what each model has proven, with evidence.
//
// A result is one finished task whose DECLARED checks ran with a real exit; a
// pass counts for, a failed check or a person's undo counts against. No model's
// judgement of work reaches these shapes, and nothing here changes which model
// runs: both methods are reads. The engine groups and counts; the app draws.

// The classes a scope is in, in the order a map draws them.
const (
	ClassStrong   = "strong"
	ClassFrontier = "frontier"
	ClassWeak     = "weak"
	ClassStale    = "stale"
)

// Competencer is an engine that reads the competence ledger. It is asserted,
// as Tidier is, so an engine without it answers unknown_method.
type Competencer interface {
	CompetenceMap(p CompetenceMapParams) (CompetenceMap, error)
	CompetenceEvidence(p CompetenceEvidenceParams) (CompetenceEvidence, error)
}

// CompetenceMapParams is competence.map's params: an exact model id (empty is
// the model with the newest evidence) and a filter, place or scope id (empty is
// every scope).
type CompetenceMapParams struct {
	Model string `json:"model,omitempty"`
	Scope string `json:"scope,omitempty"`
}

// CompetenceMap is one model's results, grouped by the engine.
type CompetenceMap struct {
	// Model is "" when nothing has been proven by any model.
	Model   string             `json:"model"`
	Models  []CompetenceModel  `json:"models"`
	Scope   string             `json:"scope"`
	Filters []CompetenceFilter `json:"filters"`
	// Groups is always the four classes, strong, frontier, weak, stale.
	Groups []CompetenceGroup `json:"groups"`
	Totals CompetenceTotals  `json:"totals"`
	Policy CompetencePolicy  `json:"policy"`
	ReadAt time.Time         `json:"read_at"`
	// Truncated is always false: a ledger longer than one read takes is the
	// engine error "checked-result history exceeds this bounded read;
	// competence unavailable" (code "engine"), never a map of its newest part,
	// and a ledger with an unreadable row (not JSON, over-long, an unknown
	// format, kind, source or outcome, or a check that is not a pass or a
	// failure with a command) is "checked-result history has an unreadable
	// row; competence unavailable". Omitted is how many scopes, filters and models this
	// answer left out for size; Totals still count every scope that matched.
	Truncated bool              `json:"truncated"`
	Omitted   CompetenceOmitted `json:"omitted"`
}

// CompetenceOmitted is what a map left out for size. A map carries at most
// 300 scopes, 200 filters and 50 models, newest evidence first.
type CompetenceOmitted struct {
	Scopes  int `json:"scopes"`
	Filters int `json:"filters"`
	Models  int `json:"models"`
}

// CompetenceModel is one model with at least one result.
type CompetenceModel struct {
	ID      string    `json:"id"`
	Results int       `json:"results"`
	LastAt  time.Time `json:"last_at"`
}

// CompetenceFilter is one entry of the Scope menu: a place, or a kind of work.
type CompetenceFilter struct {
	ID        string `json:"id"`
	Kind      string `json:"kind" enum:"place,work"`
	Path      string `json:"path,omitempty"`
	PlaceKind string `json:"place_kind,omitempty" enum:"repo,folder"`
	Removed   bool   `json:"removed,omitempty"`
	Words     string `json:"words,omitempty"`
}

// CompetenceGroup is one class's scopes, newest evidence first.
type CompetenceGroup struct {
	Class  string            `json:"class" enum:"strong,frontier,weak,stale"`
	Scopes []CompetenceScope `json:"scopes"`
}

// CompetenceScope is one place at one kind of work. Since is the start of the
// counted window, and absent on a stale scope, whose counts are all it has.
type CompetenceScope struct {
	ID      string          `json:"id"`
	Place   CompetencePlace `json:"place"`
	Work    CompetenceWork  `json:"work"`
	Class   string          `json:"class" enum:"strong,frontier,weak,stale"`
	Results int             `json:"results"`
	Passed  int             `json:"passed"`
	Failed  int             `json:"failed"`
	Undone  int             `json:"undone"`
	LastAt  time.Time       `json:"last_at"`
	Since   *time.Time      `json:"since,omitempty"`
}

// CompetencePlace is a folder results were earned in; Removed is a folder no
// codeaf conversation works in any more.
type CompetencePlace struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	PlaceKind string `json:"place_kind" enum:"repo,folder"`
	Removed   bool   `json:"removed"`
}

// CompetenceWork is the engine's kind of work, pooled.
type CompetenceWork struct {
	ID    string `json:"id"`
	Words string `json:"words"`
}

// CompetenceTotals is what the groups shown hold.
type CompetenceTotals struct {
	Scopes  int `json:"scopes"`
	Results int `json:"results"`
}

// CompetencePolicy is the engine's grouping, echoed so the app never has its
// own copy of a threshold.
type CompetencePolicy struct {
	StaleAfterDays int     `json:"stale_after_days"`
	StrongAtLeast  float64 `json:"strong_at_least"`
	WeakAtMost     float64 `json:"weak_at_most"`
	MinResults     int     `json:"min_results"`
}

// CompetenceEvidenceParams is competence.evidence's params. Limit is 7 when
// absent and at most 100.
type CompetenceEvidenceParams struct {
	Model   string `json:"model"`
	Scope   string `json:"scope"`
	Outcome string `json:"outcome,omitempty" enum:"passed,failed,undone"`
	Cursor  string `json:"cursor,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// CompetenceEvidence is one page of a scope's results, newest first.
type CompetenceEvidence struct {
	Model      string           `json:"model"`
	Scope      string           `json:"scope"`
	Counts     CompetenceCounts `json:"counts"`
	Items      []CompetenceItem `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// CompetenceCounts is how the scope's results came out, over the same window
// the scope row counts.
type CompetenceCounts struct {
	All    int `json:"all"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
	Undone int `json:"undone"`
}

// CompetenceItem is one result. SourceMissing names what was deleted — the
// task or the whole conversation — and the result still counts.
type CompetenceItem struct {
	ID            string            `json:"id"`
	Title         string            `json:"title"`
	At            time.Time         `json:"at"`
	Outcome       string            `json:"outcome" enum:"passed,failed,undone"`
	Source        CompetenceSource  `json:"source"`
	SourceMissing string            `json:"source_missing" enum:",task,conversation"`
	Check         *CompetenceCheck  `json:"check,omitempty"`
	Checks        []CompetenceCheck `json:"checks"`
	UndoneAt      *time.Time        `json:"undone_at,omitempty"`
}

// CompetenceSource is where a result came from. ID is the task's wire id for a
// task, and the agent's for a conversation; Title is the owning conversation's
// title as it stands now.
type CompetenceSource struct {
	Kind  string `json:"kind" enum:"task,conversation"`
	ID    string `json:"id"`
	Agent string `json:"agent"`
	Turn  int    `json:"turn,omitempty"`
	Title string `json:"title,omitempty"`
}

// CompetenceCheck is one declared check and how its exit came out.
type CompetenceCheck struct {
	Command string `json:"command"`
	Outcome string `json:"outcome" enum:"passed,failed"`
}

func init() {
	methods["competence.map"] = typed(func(e Engine, p CompetenceMapParams) (*CompetenceMap, error) {
		c, err := competencer(e, "competence.map")
		if err != nil {
			return nil, err
		}
		r, err := c.CompetenceMap(p)
		return &r, err
	})
	methods["competence.evidence"] = typed(func(e Engine, p CompetenceEvidenceParams) (*CompetenceEvidence, error) {
		c, err := competencer(e, "competence.evidence")
		if err != nil {
			return nil, err
		}
		if _, err := required("model", p.Model); err != nil {
			return nil, err
		}
		if _, err := required("scope", p.Scope); err != nil {
			return nil, err
		}
		switch p.Outcome {
		case "", "passed", "failed", "undone":
		default:
			return nil, Errorf(CodeBadParams, "outcome is passed, failed or undone, not %q", p.Outcome)
		}
		r, err := c.CompetenceEvidence(p)
		return &r, err
	})
}

func competencer(e Engine, name string) (Competencer, error) {
	c, ok := e.(Competencer)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return c, nil
}
