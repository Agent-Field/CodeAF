package appwire

import (
	"context"
	"errors"
	"time"
)

// PROCESSES.ACTIONS, PROCESSES.STOP_ALL AND PROCESSES.LOG_TARGET: what may be
// done with a process, said by the engine that holds it.
//
// A ROW NEVER SAYS WHAT A BUTTON MAY DO. A row is drawn from a page that may be
// seconds old and may show its command cut; processes.actions answers each verb
// by id from the holding registry and the record, and processes.rerun and
// processes.stop ask the same question again at the moment they act. A surface
// without this feature draws no Run again, no Stop all and no Open full log.
//
// processes.stop_all is a person's word only — never the model's, never an
// automation's, never a poll — over ONE agent's running jobs and kept-running
// services, under one shared deadline from the moment it is asked, and it
// answers a receipt that says what it could not stop or could not see.
//
// processes.log_target answers the facts a native opener needs — the managed
// file, as it was opened and identified just now — and NO GRANT: the path is
// never accepted back by anything, and the opener opens it again its own way.

// FeatureProcessesActions is the one hello feature for the three methods.
const FeatureProcessesActions = "processes.actions"

// The process kinds an action answer names.
const (
	ProcessKindJob     = "job"
	ProcessKindService = "service"
)

// Why a verb is not available, beyond the engine's own words
// (internal/session's ProcessStop* and ProcessRerun* constants, which travel as
// they are).
const (
	ProcessActionUnsupported = "unsupported"
	ProcessActionNotHeld     = "not_held"
	ProcessRerunRemoved      = "removed"
	ProcessLogNoLog          = "no_log"
	ProcessLogGone           = "gone"
	ProcessLogUnmanaged      = "unmanaged"
)

// The parts of a stop-all that may be unsupported.
const (
	ProcessStopAllJobs     = "jobs"
	ProcessStopAllServices = "services"
)

// The stop-all's bounds. PERF.md states them.
const (
	// ProcessStopAllCeiling is the ONE deadline a stop-all has, from the moment
	// it is asked: its jobs (internal/session's ProcessStopAllWait, five
	// seconds, inside it) and its services run at once beneath it.
	ProcessStopAllCeiling = 10 * time.Second
	// ProcessStopAllIDs bounds each id list a receipt carries; the counts stay
	// whole when a list is cut.
	ProcessStopAllIDs = 256
)

// ProcessAction is one verb's availability; Reason only when it is not.
type ProcessAction struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// ProcessActionsResult is processes.actions' answer. UnknownReason is present
// only when State is unknown.
type ProcessActionsResult struct {
	ID            string        `json:"id"`
	Kind          string        `json:"kind"`
	State         string        `json:"state"`
	UnknownReason string        `json:"unknown_reason,omitempty"`
	Stop          ProcessAction `json:"stop"`
	Rerun         ProcessAction `json:"rerun"`
	Log           ProcessAction `json:"log"`
}

// ProcessStopAllAsk is processes.stop_all's params. Only one agent's scope is
// served; a crew's is refused rather than approximated.
type ProcessStopAllAsk struct {
	Agent string `json:"agent"`
	Scope string `json:"scope,omitempty"`
	Crew  string `json:"crew,omitempty"`
}

// ProcessStopAllResult is processes.stop_all's receipt. Every count and list is
// present, zero or empty included: it is a receipt, not telemetry.
type ProcessStopAllResult struct {
	Agent              string   `json:"agent"`
	Scope              string   `json:"scope"`
	Eligible           int      `json:"eligible"`
	Stopped            int      `json:"stopped"`
	AlreadyEnded       int      `json:"already_ended"`
	NotStopped         []string `json:"not_stopped"`
	ServicesEligible   int      `json:"services_eligible"`
	ServicesStopped    int      `json:"services_stopped"`
	ServicesNotStopped []string `json:"services_not_stopped"`
	// Partial says the receipt is not the whole agent: jobs left beyond the
	// cap, a scan that stopped short, a part unsupported, or the ceiling
	// reached before a part answered.
	Partial          bool     `json:"partial"`
	Unenumerated     int      `json:"unenumerated"`
	RemainingUnknown bool     `json:"remaining_unknown"`
	Unsupported      []string `json:"unsupported"`
	// The deadlines that applied, so a surface can say how long it waited.
	JobsDeadlineMS     int `json:"jobs_deadline_ms"`
	ServicesDeadlineMS int `json:"services_deadline_ms"`
}

// ProcessLogTargetResult is processes.log_target's answer: facts about the
// managed file, never a grant. Local says the file was opened on the machine
// this app runs on. Path, Size and Revision are absent when Gone.
type ProcessLogTargetResult struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Local    bool   `json:"local"`
	Path     string `json:"path,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Revision string `json:"revision,omitempty"`
	Gone     bool   `json:"gone,omitempty"`
	// CapBytes is the size the producer holds the file under: a job's log is
	// compacted at it; a service's is cut at it while a codeaf runs.
	CapBytes int64 `json:"cap_bytes"`
}

// ProcessActor is the engine's half. An engine that is not one refuses the
// three methods as unknown.
type ProcessActor interface {
	ProcessActions(ctx context.Context, id string) (*ProcessActionsResult, error)
	StopAllProcesses(ctx context.Context, ask ProcessStopAllAsk) (*ProcessStopAllResult, error)
	ProcessLogTarget(ctx context.Context, id string) (*ProcessLogTargetResult, error)
}

// CheckProcessStopAllAsk is bad_params for anything but one agent.
func CheckProcessStopAllAsk(p ProcessStopAllAsk) error {
	if p.Crew != "" || (p.Scope != "" && p.Scope != ProcessScopeAgent) {
		return Errorf(CodeBadParams, "only one agent's processes can be stopped at once")
	}
	if p.Agent == "" || !plainText(p.Agent, 128) {
		return Errorf(CodeBadParams, "processes.stop_all needs an agent")
	}
	return nil
}

func checkProcessID(method, id string) error {
	if id == "" || !plainText(id, 256) {
		return Errorf(CodeBadParams, "%s needs a process id", method)
	}
	return nil
}

func processActError(method string, err error) error {
	var wire *Error
	switch {
	case errors.As(err, &wire):
		return err
	case errors.Is(err, context.DeadlineExceeded):
		return Errorf(CodeEngine, "%s ran out of time", method)
	case errors.Is(err, context.Canceled):
		return Errorf(CodeEngine, "%s was cancelled", method)
	}
	return Errorf(CodeEngine, "%s could not be answered", method)
}

func withProcessActionFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(ProcessActor); ok {
		hello = withFeature(hello, FeatureProcessesActions)
	}
	return hello
}

func init() {
	methods["processes.actions"] = typed(func(e Engine, p IDParams) (*ProcessActionsResult, error) {
		actor, ok := e.(ProcessActor)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve processes.actions")
		}
		if err := checkProcessID("processes.actions", p.ID); err != nil {
			return nil, err
		}
		release, err := admitProcessRead()
		if err != nil {
			return nil, err
		}
		defer release()
		ctx, cancel := context.WithTimeout(context.Background(), ProcessReadTimeout)
		defer cancel()
		res, err := actor.ProcessActions(ctx, p.ID)
		if err != nil {
			return nil, processActError("processes.actions", err)
		}
		return res, nil
	})
	methods["processes.log_target"] = typed(func(e Engine, p IDParams) (*ProcessLogTargetResult, error) {
		actor, ok := e.(ProcessActor)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve processes.log_target")
		}
		if err := checkProcessID("processes.log_target", p.ID); err != nil {
			return nil, err
		}
		release, err := admitProcessRead()
		if err != nil {
			return nil, err
		}
		defer release()
		ctx, cancel := context.WithTimeout(context.Background(), ProcessReadTimeout)
		defer cancel()
		res, err := actor.ProcessLogTarget(ctx, p.ID)
		if err != nil {
			return nil, processActError("processes.log_target", err)
		}
		return res, nil
	})
	methods["processes.stop_all"] = typed(func(e Engine, p ProcessStopAllAsk) (*ProcessStopAllResult, error) {
		actor, ok := e.(ProcessActor)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve processes.stop_all")
		}
		if err := CheckProcessStopAllAsk(p); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), ProcessStopAllCeiling)
		defer cancel()
		res, err := actor.StopAllProcesses(ctx, p)
		if err != nil {
			return nil, processActError("processes.stop_all", err)
		}
		return res, nil
	})
}
