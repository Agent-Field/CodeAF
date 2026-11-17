package appwire

import (
	"strings"
	"time"
)

// Kept-running services (V77): processes a person agreed to keep running
// after a turn, supervised by the engine. A service is listed with the
// agent's other processes — ONE Running/Ended list and one count — as a
// [Process] whose Service carries what only a service has. Its id is
// "<agent>:s<n>", which no ordinary process id can be ("<agent>:p<n>…").
//
// HOW A SERVICE ROW IS CLASSIFIED. Process.State is "running" while the engine
// owns a live process (Service.Running or Service.Stopping) or while the
// service is starting or restarting with no process yet; it is "stopped" for a
// stopped service, "failed" for failed and gave_up, and "ended" for
// not_running. A service whose stop failed still owns its process: Running is
// true and State is "running", never "ended".

// The lifecycle of a service.
const (
	ServiceStarting   = "starting"
	ServiceHealthy    = "healthy"
	ServiceUnhealthy  = "unhealthy"
	ServiceRestarting = "restarting"
	ServiceGaveUp     = "gave_up"
	ServiceNotRunning = "not_running"
	ServiceStopped    = "stopped"
	ServiceFailed     = "failed"
)

// The restart policies.
const (
	PolicyNever     = "never"
	PolicyOnFailure = "on_failure"
	PolicyAlways    = "always"
)

// The keep-running ask: its subkind and its three option ids.
const (
	SubkindKeepRunning = "keep_running"
	OptionAllow        = "allow"
	OptionAllowNever   = "allow_never"
)

// A service in trouble: its Inbox line is an Ask of subkind service_health
// about subject service, answered restart or stop; it resolves recovered or
// stopped; and its one notice in the chat is a footer of kind service_trouble.
const (
	SubkindServiceHealth = "service_health"
	SubjectService       = "service"
	OptionRestart        = "restart"
	ResolvedRecovered    = "recovered"
	ResolvedStopped      = "stopped"
	FooterServiceTrouble = "service_trouble"
)

// KeepRunning is what a keep-running ask proposes, read from the engine's own
// card and never from its words. The command is redacted.
type KeepRunning struct {
	Name    string        `json:"name"`
	Command string        `json:"command"`
	Dir     string        `json:"dir"`
	Health  ServiceHealth `json:"health"`
	Policy  string        `json:"policy" enum:"never,on_failure,always"`
	CallID  string        `json:"call_id,omitempty"`
	Timing  ServiceTiming `json:"timing"`
}

// ServiceTiming is the supervisor's fixed rules, the same for every service,
// sent so the app says the engine's figures rather than its own: a check
// every check_every_s, each given check_timeout_s; unhealthy after
// unhealthy_after failures in a row; automatic restarts after first_backoff_s,
// doubling, at most restart_limit in any restart_window_s, the count clearing
// after healthy_reset_s healthy; a stop's TERM given stop_grace_s before KILL.
type ServiceTiming struct {
	CheckEverySeconds    int `json:"check_every_s"`
	CheckTimeoutSeconds  int `json:"check_timeout_s"`
	UnhealthyAfter       int `json:"unhealthy_after"`
	FirstBackoffSeconds  int `json:"first_backoff_s"`
	RestartLimit         int `json:"restart_limit"`
	RestartWindowSeconds int `json:"restart_window_s"`
	HealthyResetSeconds  int `json:"healthy_reset_s"`
	StopGraceSeconds     int `json:"stop_grace_s"`
}

// Service is what a kept-running process carries beyond an ordinary one.
type Service struct {
	Name    string        `json:"name"`
	Dir     string        `json:"dir"`
	Health  ServiceHealth `json:"health"`
	Timing  ServiceTiming `json:"timing"`
	OpenURL string        `json:"open_url,omitempty"`
	Policy  string        `json:"policy" enum:"never,on_failure,always"`
	// Lifecycle is the engine's state of the service; Running and Stopping
	// are whether it owns a live process right now, which a failed stop can
	// make true in any lifecycle.
	Lifecycle  string        `json:"lifecycle" enum:"starting,healthy,unhealthy,restarting,gave_up,not_running,stopped,failed"`
	Reason     string        `json:"reason,omitempty"`
	Running    bool          `json:"running"`
	Stopping   bool          `json:"stopping,omitempty"`
	PID        int           `json:"pid,omitempty"`
	StartedHow string        `json:"started_how,omitempty" enum:"agent,restart,person,after_restart,re_adopted"`
	StartedBy  ServiceOrigin `json:"started_by"`
	Check      *ServiceCheck `json:"check,omitempty"`
	// Restarts is the automatic restarts in the last ten minutes, of
	// RestartLimit; a person's restart is not one of them.
	Restarts     int        `json:"restarts"`
	RestartLimit int        `json:"restart_limit"`
	BackoffUntil *time.Time `json:"backoff_until,omitempty"`
	ReAdopted    bool       `json:"re_adopted,omitempty"`
	ReAdoptedAt  *time.Time `json:"re_adopted_at,omitempty"`
	PortConflict bool       `json:"port_conflict,omitempty"`
	// Episode names one run of trouble, from its first failure to being
	// healthy again or stopped: one Inbox line, updated in place. Its facts
	// stay on the row after it closes.
	Episode           string     `json:"episode,omitempty"`
	EpisodeOpen       bool       `json:"episode_open,omitempty"`
	EpisodeReason     string     `json:"episode_reason,omitempty"`
	EpisodeStartedAt  *time.Time `json:"episode_started_at,omitempty"`
	EpisodeResolvedAt *time.Time `json:"episode_resolved_at,omitempty"`
	StoppedBy         string     `json:"stopped_by,omitempty"`
	OutputTruncated   bool       `json:"output_truncated,omitempty"`
	// Removed is a service taken off the list whose output is still kept.
	Removed bool `json:"removed,omitempty"`
}

// ServiceHealth is a service's one check.
type ServiceHealth struct {
	Kind  string `json:"kind" enum:"port,url,cmd"`
	Port  int    `json:"port,omitempty"`
	URL   string `json:"url,omitempty"`
	Cmd   string `json:"cmd,omitempty"`
	Words string `json:"words"`
}

// ServiceOrigin is the request that started a service: the person's words,
// the turn and the call.
type ServiceOrigin struct {
	Label  string    `json:"label,omitempty"`
	Turn   uint64    `json:"turn,omitempty"`
	CallID string    `json:"call_id,omitempty"`
	At     time.Time `json:"at"`
}

// ServiceCheck is a service's newest check.
type ServiceCheck struct {
	OK     bool      `json:"ok"`
	At     time.Time `json:"at"`
	Fails  int       `json:"fails"`
	Detail string    `json:"detail,omitempty"`
}

// OutputPage is one bounded read of a service's output.
type OutputPage struct {
	Lines      []string `json:"lines"`
	FirstLine  int      `json:"first_line"`
	NextLine   int      `json:"next_line"`
	TotalLines int      `json:"total_lines"`
	Truncated  bool     `json:"truncated,omitempty"`
	// Partial: the page ended at a line or byte cap with more stream after
	// next_line. Nothing was lost; it is not truncated.
	Partial bool `json:"partial,omitempty"`
}

// StopAllResult is services.stop_all's answer.
type StopAllResult struct {
	Stopped int `json:"stopped"`
}

// Servicer is the engine's half of kept-running services. An [Engine] that is
// not one refuses these methods as unknown, which is how an engine with no
// supervisor says the capability is absent.
type Servicer interface {
	// ListServices is one agent's services, or every agent's for "".
	ListServices(agent string) ([]Process, error)
	GetService(id string) (Process, error)
	SetServicePolicy(id, policy string) (Process, error)
	RestartService(id string) (Process, error)
	StartService(id string) (Process, error)
	StopService(id string) (Process, error)
	RemoveService(id string) error
	// StopAllServices stops every agent's services and waits for their
	// groups to go, for a quit that was asked to stop them.
	StopAllServices() (int, error)
	ServiceOutput(id string, from, max int) (OutputPage, error)
	ServiceTail(id string, lines int) (OutputPage, error)
}

func init() {
	for name, m := range serviceMethods {
		methods[name] = m
	}
}

func servicer(e Engine, name string) (Servicer, error) {
	s, ok := e.(Servicer)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return s, nil
}

// serviceVerb is one service verb that takes only an id and answers the row.
func serviceVerb(name string, act func(Servicer, string) (Process, error)) method {
	return typed(func(e Engine, p IDParams) (*ProcessResult, error) {
		s, err := servicer(e, name)
		if err != nil {
			return nil, err
		}
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		process, err := act(s, id)
		if err != nil {
			return nil, err
		}
		return &ProcessResult{process}, nil
	})
}

var serviceMethods = map[string]method{
	"services.list": typed(func(e Engine, p struct {
		Agent string `json:"agent,omitempty"`
	}) (*ProcessesResult, error) {
		s, err := servicer(e, "services.list")
		if err != nil {
			return nil, err
		}
		list, err := s.ListServices(strings.TrimSpace(p.Agent))
		if err != nil {
			return nil, err
		}
		return &ProcessesResult{nonNil(list)}, nil
	}),
	"services.get":     serviceVerb("services.get", Servicer.GetService),
	"services.restart": serviceVerb("services.restart", Servicer.RestartService),
	"services.start":   serviceVerb("services.start", Servicer.StartService),
	"services.stop":    serviceVerb("services.stop", Servicer.StopService),
	"services.policy": typed(func(e Engine, p struct {
		ID     string `json:"id"`
		Policy string `json:"policy" enum:"never,on_failure,always"`
	}) (*ProcessResult, error) {
		s, err := servicer(e, "services.policy")
		if err != nil {
			return nil, err
		}
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		switch p.Policy {
		case PolicyNever, PolicyOnFailure, PolicyAlways:
		default:
			return nil, Errorf(CodeBadParams, "policy is never, on_failure or always")
		}
		process, err := s.SetServicePolicy(id, p.Policy)
		if err != nil {
			return nil, err
		}
		return &ProcessResult{process}, nil
	}),
	"services.remove": typed(func(e Engine, p IDParams) (*Empty, error) {
		s, err := servicer(e, "services.remove")
		if err != nil {
			return nil, err
		}
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		return done(s.RemoveService(id))
	}),
	"services.stop_all": typed(func(e Engine, _ struct{}) (*StopAllResult, error) {
		s, err := servicer(e, "services.stop_all")
		if err != nil {
			return nil, err
		}
		n, err := s.StopAllServices()
		if err != nil {
			return nil, err
		}
		return &StopAllResult{Stopped: n}, nil
	}),
	"services.output": typed(func(e Engine, p struct {
		ID       string `json:"id"`
		FromLine int    `json:"from_line"`
		MaxLines int    `json:"max_lines,omitempty"`
	}) (*OutputPage, error) {
		s, err := servicer(e, "services.output")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.ID) == "" || p.FromLine < 0 || p.MaxLines < 0 {
			return nil, Errorf(CodeBadParams, "services.output needs an id, a from_line of zero or more and max_lines of zero or more")
		}
		page, err := s.ServiceOutput(p.ID, p.FromLine, p.MaxLines)
		if err != nil {
			return nil, err
		}
		page.Lines = nonNil(page.Lines)
		return &page, nil
	}),
	"services.tail": typed(func(e Engine, p struct {
		ID    string `json:"id"`
		Lines int    `json:"lines,omitempty"`
	}) (*OutputPage, error) {
		s, err := servicer(e, "services.tail")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.ID) == "" || p.Lines < 0 {
			return nil, Errorf(CodeBadParams, "services.tail needs an id and lines of zero or more")
		}
		page, err := s.ServiceTail(p.ID, p.Lines)
		if err != nil {
			return nil, err
		}
		page.Lines = nonNil(page.Lines)
		return &page, nil
	}),
}
