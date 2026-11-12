package appwire

import (
	"context"
	"errors"
	"time"
)

// PROCESSES.PAGE AND PROCESSES.LOG: a conversation's whole process history,
// one bounded page at a time, and one process's output by byte offset.
//
// THEY ARE ADDITIVE. processes.list, processes.output and every process verb
// keep their shapes; an engine without these features answers unknown_method
// and a surface falls back to the list. A page is newest first; its first page
// leads with every process live right now. Next is absent ONLY when the walk
// reached the oldest record — any budget that runs out first (the limit, the
// reply's bytes, holes in the numbering, the deadline) answers a Next instead,
// never a false end. Incomplete says some history exists that could not be
// read and will be on no page.

// The hello features, one per method.
const (
	FeatureProcessesPage = "processes.page"
	FeatureProcessesLog  = "processes.log"
	// FeatureProcessesCrew says processes.page also serves the crew forms:
	// {crew} for one team's recorded subtree and {scope:"all"} for every
	// recorded team. An engine without it answers both as bad_params.
	FeatureProcessesCrew = "processes.crew"
)

// The bounds a request is checked against before anything is read.
const (
	ProcessPageLimitDefault = 50
	ProcessPageLimitMax     = 100
	ProcessPageReplyMax     = 4 << 20
	ProcessCursorMax        = 1024
	ProcessLogBytesDefault  = 64 << 10
	ProcessLogBytesMax      = 512 << 10
	ProcessReadTimeout      = 2 * time.Second
	// ProcessReadsInFlight is how many page and log reads one engine serves at
	// once; beyond it a read is answered busy rather than queued.
	ProcessReadsInFlight = 4
)

// The page's scopes. An agent's own history is the default; the crew forms are
// served only by an engine that names FeatureProcessesCrew, from the member set
// that engine owns, and are refused rather than approximated.
const (
	ProcessScopeAgent = "agent"
	ProcessScopeCrew  = "crew"
	ProcessScopeAll   = "all"
)

// ProcessRosterRecorded is the fixed caption of a crew page's roster: the
// membership as the team file records it at the first page, not who is
// working now and not who was there on any earlier day.
const ProcessRosterRecorded = "recorded"

// The reasons an owner's history could not be read in a crew walk.
const (
	ProcessOwnerMissing    = "missing"
	ProcessOwnerUnreadable = "unreadable"
	ProcessOwnerTimeout    = "timeout"
	ProcessOwnerStale      = "stale"
)

// ProcessPartialMax bounds how many owners one page names as partial; the rest
// are counted in ProcessOwners.More.
const ProcessPartialMax = 32

// Why a process's state is unknown.
const (
	ProcessUnknownNotHeld   = "not_held"
	ProcessUnknownOwnerLost = "owner_lost"
)

// ProcessPageAsk is processes.page's params.
type ProcessPageAsk struct {
	Agent  string `json:"agent,omitempty"`
	Crew   string `json:"crew,omitempty"`
	Scope  string `json:"scope,omitempty"`
	Limit  *int   `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// ProcessPageResult is processes.page's answer.
type ProcessPageResult struct {
	Processes  []Process `json:"processes"`
	Next       string    `json:"next,omitempty"`
	Incomplete bool      `json:"incomplete,omitempty"`
	Revision   string    `json:"revision,omitempty"`
	// Roster and Owners are answered by the crew forms only. Roster is always
	// ProcessRosterRecorded. Owners says how much of the roster has been read
	// and which owners could not be: a crew walk is complete only when Next is
	// absent and Owners.Partial is empty throughout.
	Roster string         `json:"roster,omitempty"`
	Owners *ProcessOwners `json:"owners,omitempty"`
}

// ProcessOwners is a crew walk's progress. Done counts owners read to their end
// or named partial; Total is every owner the roster holds. Partial names the
// owners that became partial on THIS page (at most ProcessPartialMax), More
// counts those not named, and Unread counts every owner partial so far. A
// partial owner is skipped for the rest of the walk, never silently dropped.
type ProcessOwners struct {
	Total   int                   `json:"total"`
	Done    int                   `json:"done"`
	Unread  int                   `json:"unread,omitempty"`
	Partial []ProcessOwnerPartial `json:"partial,omitempty"`
	More    int                   `json:"more,omitempty"`
}

// ProcessOwnerPartial names one owner a crew walk could not read: its agent
// id, or the member key when no conversation could be named, and why.
type ProcessOwnerPartial struct {
	Agent  string `json:"agent,omitempty"`
	Key    string `json:"key,omitempty"`
	Reason string `json:"reason"`
}

// ProcessLogAsk is processes.log's params. Offset absent reads the tail.
type ProcessLogAsk struct {
	ID         string `json:"id"`
	Offset     *int64 `json:"offset,omitempty"`
	LimitBytes *int   `json:"limit_bytes,omitempty"`
	Revision   string `json:"revision,omitempty"`
}

// ProcessLogResult is processes.log's answer.
type ProcessLogResult struct {
	Text            string `json:"text"`
	Offset          int64  `json:"offset"`
	Next            int64  `json:"next"`
	Size            int64  `json:"size"`
	EOF             bool   `json:"eof"`
	Partial         bool   `json:"partial,omitempty"`
	MoreBefore      bool   `json:"more_before,omitempty"`
	OutputTruncated bool   `json:"output_truncated,omitempty"`
	LogGone         bool   `json:"log_gone,omitempty"`
	Revision        string `json:"revision,omitempty"`
}

// ProcessPager and ProcessLogReader are the optional engine halves. Both
// honour ctx.
type ProcessPager interface {
	PageProcesses(ctx context.Context, ask ProcessPageAsk) (*ProcessPageResult, error)
}

// ProcessCrewPager is the optional crew half of ProcessPager. An engine that
// has it names FeatureProcessesCrew.
type ProcessCrewPager interface {
	PageCrewProcesses(ctx context.Context, ask ProcessPageAsk) (*ProcessPageResult, error)
}

type ProcessLogReader interface {
	ReadProcessLog(ctx context.Context, ask ProcessLogAsk) (*ProcessLogResult, error)
}

func withProcessFeatures(hello Hello, e Engine) Hello {
	if _, ok := e.(ProcessPager); ok {
		hello = withFeature(hello, FeatureProcessesPage)
		if _, ok := e.(ProcessCrewPager); ok {
			hello = withFeature(hello, FeatureProcessesCrew)
		}
	}
	if _, ok := e.(ProcessLogReader); ok {
		hello = withFeature(hello, FeatureProcessesLog)
	}
	return hello
}

// CheckProcessPageAsk is the effective limit, or bad_params.
func CheckProcessPageAsk(p ProcessPageAsk) (int, error) {
	if ProcessCrewForm(p) {
		return 0, Errorf(CodeBadParams, "crew process history is not served here; page each agent's own")
	}
	if p.Agent == "" || !plainText(p.Agent, 128) {
		return 0, Errorf(CodeBadParams, "processes.page needs an agent")
	}
	limit := ProcessPageLimitDefault
	if p.Limit != nil {
		limit = *p.Limit
		if limit < 1 || limit > ProcessPageLimitMax {
			return 0, Errorf(CodeBadParams, "limit is 1 to %d", ProcessPageLimitMax)
		}
	}
	if len(p.Cursor) > ProcessCursorMax {
		return 0, Errorf(CodeBadParams, "cursor is one a previous page gave")
	}
	return limit, nil
}

// ProcessCrewForm says the ask is not an agent's own: it names a crew, or a
// scope other than the agent's.
func ProcessCrewForm(p ProcessPageAsk) bool {
	return p.Crew != "" || (p.Scope != "" && p.Scope != ProcessScopeAgent)
}

// CheckProcessCrewAsk is the effective limit of a crew form, or bad_params.
// The forms are exactly {crew} (scope absent or "crew") and {scope:"all"};
// any agent beside them, a scope that is not one of those, or both a crew and
// "all" is refused.
func CheckProcessCrewAsk(p ProcessPageAsk) (int, error) {
	bad := func() (int, error) {
		return 0, Errorf(CodeBadParams, `crew process history takes a crew, or scope "all", and no agent`)
	}
	switch {
	case p.Agent != "":
		return bad()
	case p.Crew != "" && (p.Scope != "" && p.Scope != ProcessScopeCrew):
		return bad()
	case p.Crew == "" && p.Scope != ProcessScopeAll:
		return bad()
	case p.Crew != "" && !plainText(p.Crew, 128):
		return bad()
	}
	limit := ProcessPageLimitDefault
	if p.Limit != nil {
		limit = *p.Limit
		if limit < 1 || limit > ProcessPageLimitMax {
			return 0, Errorf(CodeBadParams, "limit is 1 to %d", ProcessPageLimitMax)
		}
	}
	if len(p.Cursor) > ProcessCursorMax {
		return 0, Errorf(CodeBadParams, "cursor is one a previous page gave")
	}
	return limit, nil
}

// CheckProcessLogAsk is the effective byte limit, or bad_params.
func CheckProcessLogAsk(p ProcessLogAsk) (int, error) {
	if p.ID == "" || !plainText(p.ID, 256) {
		return 0, Errorf(CodeBadParams, "processes.log needs a process id")
	}
	limit := ProcessLogBytesDefault
	if p.LimitBytes != nil {
		limit = *p.LimitBytes
		if limit < 1 || limit > ProcessLogBytesMax {
			return 0, Errorf(CodeBadParams, "limit_bytes is 1 to %d", ProcessLogBytesMax)
		}
	}
	if p.Offset != nil && *p.Offset < 0 {
		return 0, Errorf(CodeBadParams, "offset is zero or more")
	}
	if len(p.Revision) > 128 {
		return 0, Errorf(CodeBadParams, "revision is one a previous read gave")
	}
	return limit, nil
}

// processReads admits page and log reads.
var processReads = make(chan struct{}, ProcessReadsInFlight)

func admitProcessRead() (func(), error) {
	select {
	case processReads <- struct{}{}:
		return func() { <-processReads }, nil
	default:
		return nil, Errorf(CodeBusy, "too many process reads at once; try again")
	}
}

func processReadError(method string, err error) error {
	var wire *Error
	switch {
	case errors.As(err, &wire):
		return err
	case errors.Is(err, context.DeadlineExceeded):
		return Errorf(CodeEngine, "%s took longer than %s", method, ProcessReadTimeout)
	case errors.Is(err, context.Canceled):
		return Errorf(CodeEngine, "%s was cancelled", method)
	}
	return Errorf(CodeEngine, "the process history could not be read")
}

func init() {
	methods["processes.page"] = typed(func(e Engine, p ProcessPageAsk) (*ProcessPageResult, error) {
		pager, ok := e.(ProcessPager)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve processes.page")
		}
		var serve func(context.Context, ProcessPageAsk) (*ProcessPageResult, error) = pager.PageProcesses
		if ProcessCrewForm(p) {
			crew, ok := e.(ProcessCrewPager)
			if !ok {
				return nil, Errorf(CodeBadParams, "this engine does not serve crew process history; page each agent's own")
			}
			if _, err := CheckProcessCrewAsk(p); err != nil {
				return nil, err
			}
			serve = crew.PageCrewProcesses
		} else if _, err := CheckProcessPageAsk(p); err != nil {
			return nil, err
		}
		release, err := admitProcessRead()
		if err != nil {
			return nil, err
		}
		defer release()
		ctx, cancel := context.WithTimeout(context.Background(), ProcessReadTimeout)
		defer cancel()
		res, err := serve(ctx, p)
		if err != nil {
			return nil, processReadError("processes.page", err)
		}
		if res.Processes == nil {
			res.Processes = []Process{}
		}
		return res, nil
	})
	methods["processes.log"] = typed(func(e Engine, p ProcessLogAsk) (*ProcessLogResult, error) {
		reader, ok := e.(ProcessLogReader)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve processes.log")
		}
		if _, err := CheckProcessLogAsk(p); err != nil {
			return nil, err
		}
		release, err := admitProcessRead()
		if err != nil {
			return nil, err
		}
		defer release()
		ctx, cancel := context.WithTimeout(context.Background(), ProcessReadTimeout)
		defer cancel()
		res, err := reader.ReadProcessLog(ctx, p)
		if err != nil {
			return nil, processReadError("processes.log", err)
		}
		return res, nil
	})
}
