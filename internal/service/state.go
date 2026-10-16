package service

import (
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// State is where a service is in its life. It is the engine's word for the row,
// and a surface draws it; the engine owns every transition.
type State string

const (
	// StateStarting is a service started and not yet seen passing a check.
	StateStarting State = "starting"
	// StateHealthy is a service whose last check passed.
	StateHealthy State = "healthy"
	// StateUnhealthy is a service still running that failed UnhealthyAfter
	// checks in a row and that its policy does not restart.
	StateUnhealthy State = "unhealthy"
	// StateRestarting is a service waiting out its backoff, or being started
	// again by its policy.
	StateRestarting State = "restarting"
	// StateGaveUp is a service whose policy wanted another restart after
	// RestartLimit in RestartWindow, and got none.
	StateGaveUp State = "gave_up"
	// StateNotRunning is a service this codeaf could not prove is running —
	// after codeaf or the machine restarted — or one that exited 0 under a
	// policy that does not restart a clean exit. Nothing restarts it on its own.
	StateNotRunning State = "not_running"
	// StateStopped is a service a person stopped. Nothing restarts it.
	StateStopped State = "stopped"
	// StateFailed is a service that exited non-zero under PolicyNever, or one
	// that could not be started at all.
	StateFailed State = "failed"
)

func (s State) valid() bool {
	switch s {
	case StateStarting, StateHealthy, StateUnhealthy, StateRestarting, StateGaveUp,
		StateNotRunning, StateStopped, StateFailed:
		return true
	}
	return false
}

// How a service's current process came to be.
const (
	StartedByAgent      = "agent"
	StartedByRestart    = "restart"
	StartedByPerson     = "person"
	StartedAfterRestart = "after_restart"
	ReAdopted           = "re_adopted"
)

// Provenance is where a service came from: the conversation's own reference to
// the call that asked, and the line that says it.
type Provenance struct {
	// Label is the started-by line, "Start the web dev server…".
	Label string `json:"label,omitempty"`
	// Turn and CallID name the turn and the tool call that asked.
	Turn   uint64    `json:"turn,omitempty"`
	CallID string    `json:"call_id,omitempty"`
	At     time.Time `json:"at"`
}

// Check is a service's newest check.
type Check struct {
	OK bool      `json:"ok"`
	At time.Time `json:"at"`
	// Fails is how many checks in a row have failed, 0 after a pass.
	Fails int `json:"fails"`
	// Detail is what the failing check found: "port 5173 not open".
	Detail string `json:"detail,omitempty"`
}

// record is one service as the file keeps it. It holds the exact command, so
// the file is the person's own and private; nothing that leaves the package
// carries it unredacted ([Row]).
type record struct {
	ID      string     `json:"id"`
	Owner   string     `json:"owner"`
	Spec    Spec       `json:"spec"`
	Policy  Policy     `json:"policy"`
	From    Provenance `json:"from"`
	Created time.Time  `json:"created"`

	State  State  `json:"state"`
	Reason string `json:"reason,omitempty"`

	Identity   *processgroup.Identity `json:"identity,omitempty"`
	StartedAt  time.Time              `json:"started_at,omitempty"`
	StartedHow string                 `json:"started_how,omitempty"`
	EndedAt    time.Time              `json:"ended_at,omitempty"`
	ExitCode   *int                   `json:"exit_code,omitempty"`

	Check        *Check      `json:"check,omitempty"`
	HealthySince time.Time   `json:"healthy_since,omitempty"`
	Restarts     []time.Time `json:"restarts,omitempty"`
	BackoffUntil time.Time   `json:"backoff_until,omitempty"`
	ReAdoptedAt  time.Time   `json:"readopted_at,omitempty"`
	PortConflict bool        `json:"port_conflict,omitempty"`

	Episode       int       `json:"episode,omitempty"`
	EpisodeOpen   bool      `json:"episode_open,omitempty"`
	EpisodeAt     time.Time `json:"episode_at,omitempty"`
	EpisodeEnded  time.Time `json:"episode_ended,omitempty"`
	EpisodeReason string    `json:"episode_reason,omitempty"`

	StoppedBy string    `json:"stopped_by,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
	Updated   time.Time `json:"updated"`
}

// Row is one service as a surface sees it. The command and the check command
// are redacted: a row travels in events and to other processes, and a token
// someone pasted into a command line must not ride with it.
type Row struct {
	ID          string     `json:"id"`
	Owner       string     `json:"owner"`
	Name        string     `json:"name"`
	Command     string     `json:"command"`
	Dir         string     `json:"dir"`
	Health      Health     `json:"health"`
	HealthWords string     `json:"health_words"`
	OpenURL     string     `json:"open_url,omitempty"`
	Policy      Policy     `json:"policy"`
	From        Provenance `json:"from"`

	State  State  `json:"state"`
	Reason string `json:"reason,omitempty"`
	// Running says a process this codeaf can prove is the service is up.
	Running    bool       `json:"running"`
	Stopping   bool       `json:"stopping,omitempty"`
	PID        int        `json:"pid,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	StartedHow string     `json:"started_how,omitempty"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	ExitCode   *int       `json:"exit_code,omitempty"`

	Check        *Check     `json:"check,omitempty"`
	Restarts     int        `json:"restarts"`
	RestartLimit int        `json:"restart_limit"`
	BackoffUntil *time.Time `json:"backoff_until,omitempty"`
	ReAdopted    bool       `json:"re_adopted,omitempty"`
	ReAdoptedAt  *time.Time `json:"re_adopted_at,omitempty"`
	PortConflict bool       `json:"port_conflict,omitempty"`

	// Episode names one run of trouble — from the first failure to being
	// healthy again — so a surface keeps one line per episode and updates it
	// in place. EpisodeOpen is false once it is over.
	//
	// THE EPISODE OUTLIVES ITS TROUBLE. A failure that recovered before a
	// listener heard it is still on the row that says healthy: its id, when it
	// began and what first went wrong stay until the next episode replaces
	// them, so a surface that only ever saw the recovered row can still write
	// the one line about it.
	Episode          string     `json:"episode,omitempty"`
	EpisodeOpen      bool       `json:"episode_open,omitempty"`
	EpisodeStartedAt *time.Time `json:"episode_started_at,omitempty"`
	// EpisodeResolvedAt is when the episode closed — healthy again, or the
	// person stopped it — set once and never moved by later checks.
	EpisodeResolvedAt *time.Time `json:"episode_resolved_at,omitempty"`
	EpisodeReason     string     `json:"episode_reason,omitempty"`

	StoppedBy       string    `json:"stopped_by,omitempty"`
	LogPath         string    `json:"log_path"`
	OutputTruncated bool      `json:"output_truncated,omitempty"`
	Updated         time.Time `json:"updated"`
}

// Event is one change to one service.
type Event struct {
	Row Row `json:"row"`
	// Change is what moved: "started", "check", "state", "policy", "stopped",
	// "removed", "re_adopted".
	Change string `json:"change"`
	// Removed is set when the record is gone; Row is its last state.
	Removed bool `json:"removed,omitempty"`
}

func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	t = t.UTC()
	return &t
}

// row is a record as a surface sees it, at now.
func (r *record) row(now time.Time, running, stopping bool, logPath string) Row {
	health := r.Spec.Health
	health.Cmd = redact.Secrets(health.Cmd)
	health.URL = redact.Secrets(health.URL)
	out := Row{
		ID: r.ID, Owner: r.Owner, Name: r.Spec.Name,
		Command: redact.Secrets(r.Spec.Command), Dir: r.Spec.Dir,
		Health: health, HealthWords: health.Words(), OpenURL: r.Spec.Health.OpenURL(),
		Policy: r.Policy, From: r.From,
		State: r.State, Reason: r.Reason,
		Running: running, Stopping: stopping,
		StartedAt: timeOrNil(r.StartedAt), StartedHow: r.StartedHow,
		EndedAt:  timeOrNil(r.EndedAt),
		Restarts: len(restartsWithin(r.Restarts, now)), RestartLimit: RestartLimit,
		ReAdopted: r.StartedHow == ReAdopted, ReAdoptedAt: timeOrNil(r.ReAdoptedAt),
		PortConflict: r.PortConflict, EpisodeOpen: r.EpisodeOpen,
		StoppedBy: r.StoppedBy, LogPath: logPath, OutputTruncated: r.Truncated,
		Updated: r.Updated.UTC(),
	}
	if out.OpenURL != "" {
		out.OpenURL = redact.Secrets(out.OpenURL)
	}
	if running && r.Identity != nil {
		out.PID = r.Identity.PID
	}
	// Nothing in a row aliases the record: a caller that writes into what it
	// was handed changes its copy, never the supervisor's.
	if r.Check != nil {
		check := *r.Check
		out.Check = &check
	}
	if r.ExitCode != nil {
		code := *r.ExitCode
		out.ExitCode = &code
	}
	if r.State == StateRestarting {
		out.BackoffUntil = timeOrNil(r.BackoffUntil)
	}
	if r.Episode > 0 {
		out.Episode = r.ID + "." + strconv.Itoa(r.Episode)
		out.EpisodeStartedAt = timeOrNil(r.EpisodeAt)
		if !r.EpisodeOpen {
			out.EpisodeResolvedAt = timeOrNil(r.EpisodeEnded)
		}
		out.EpisodeReason = redact.Secrets(r.EpisodeReason)
	}
	return out
}

// restartsWithin is the automatic restarts still inside the sliding window.
func restartsWithin(restarts []time.Time, now time.Time) []time.Time {
	kept := restarts[:0:0]
	for _, at := range restarts {
		if now.Sub(at) < RestartWindow {
			kept = append(kept, at)
		}
	}
	return kept
}
