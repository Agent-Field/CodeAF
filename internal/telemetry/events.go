package telemetry

import (
	"runtime"
	"time"
)

// Count buckets, exactly the strings the contract enumerates. A bucket is a
// band a person agreed to, not a number they did not.
const (
	BucketZero = "0"
	BucketOne  = "1"
	BucketTwo5 = "2-5"
	BucketSix  = "6-20"
	BucketTwo1 = "21-100"
	Bucket100  = "100+"
)

// Cost buckets in US dollars, from the contract.
const (
	CostZero    = "0"
	CostUnder1c = "<0.01"
	Cost1cTo10c = "0.01-0.1"
	Cost10cTo1  = "0.1-1"
	Cost1To10   = "1-10"
	Cost10Plus  = "10+"
)

// Duration buckets for a session, from the contract.
const (
	DurationUnder1m = "<1m"
	Duration1To5m   = "1-5m"
	Duration5To30m  = "5-30m"
	Duration30mTo2h = "30m-2h"
	Duration2hPlus  = "2h+"
)

// BucketCount folds a number into the contract's count band. Negative and
// unknown counts are nothing; there is no band a fault can inflate into.
func BucketCount(count int) string {
	switch {
	case count <= 0:
		return BucketZero
	case count == 1:
		return BucketOne
	case count <= 5:
		return BucketTwo5
	case count <= 20:
		return BucketSix
	case count <= 100:
		return BucketTwo1
	}
	return Bucket100
}

// BucketCost folds a dollar figure into the contract's cost band. Negative
// costs mean no cost: a refund is not a price.
func BucketCost(costUSD float64) string {
	switch {
	case costUSD <= 0:
		return CostZero
	case costUSD < 0.01:
		return CostUnder1c
	case costUSD < 0.1:
		return Cost1cTo10c
	case costUSD < 1:
		return Cost10cTo1
	case costUSD < 10:
		return Cost1To10
	}
	return Cost10Plus
}

// BucketDuration folds a session length into the contract's duration bands.
// A negative duration is not a length; it is a clock that disagrees with
// itself, and it answers as the empty band.
func BucketDuration(d time.Duration) string {
	switch {
	case d <= 0:
		return DurationUnder1m
	case d < 5*time.Minute:
		return Duration1To5m
	case d < 30*time.Minute:
		return Duration5To30m
	case d < 2*time.Hour:
		return Duration30mTo2h
	}
	return Duration2hPlus
}

// StopReason enumerates why a session ended, as the contract spells them.
const (
	StopDone        = "done"
	StopError       = "error"
	StopIncomplete  = "incomplete"
	StopBudget      = "budget"
	StopTurnCap     = "turn-cap"
	StopDeadline    = "deadline"
	StopPrice       = "price"
	StopQuestion    = "question"
	StopInterrupted = "interrupted"
	StopUnknown     = "unknown"
)

// ValidStopReason reports whether s is one of the contract's stop reasons, so
// a constructor can drop an unrecognised one rather than invent a key.
func ValidStopReason(s string) bool {
	switch s {
	case StopDone, StopError, StopIncomplete, StopBudget, StopTurnCap,
		StopDeadline, StopPrice, StopQuestion, StopInterrupted, StopUnknown:
		return true
	}
	return false
}

// Mode is how a session ran. The zero value is chat, which is what a bare
// `codeaf` opens.
type Mode string

// The contract's two session modes.
const (
	ModeChat Mode = "chat"
	ModeTask Mode = "task"
)

// Scope is where a fault happened.
const (
	ScopeMain      = "main"
	ScopeGoroutine = "goroutine"
	ScopeSurface   = "surface"
)

// Event is one wire row. Only the contract's keys exist; the allowlist lives
// in how each constructor fills Props, and the doc test holds the two
// together. MarshalJSON writes the envelope exactly as the contract spells it.
type Event struct {
	Name        string
	ID          string
	InstallHash string
	SessionHash string // empty on first_run
	Time        string
	Props       map[string]any
}

// jsonEvent is the wire shape of [Event]: fixed key order is not required, but
// the key NAMES are, and this is the one place they are spelled.
type jsonEvent struct {
	EventName     string         `json:"event_name"`
	EventID       string         `json:"event_id"`
	InstallIDHash string         `json:"install_id_hash"`
	SessionIDHash string         `json:"session_id_hash,omitempty"`
	EventTime     string         `json:"event_time"`
	Props         map[string]any `json:"props"`
}

// MarshalJSON renders the event under its wire names. The struct carries the
// typed fields; this decides the bytes.
func (e Event) MarshalJSON() ([]byte, error) {
	return jsonMarshal(jsonEvent{
		EventName:     e.Name,
		EventID:       e.ID,
		InstallIDHash: e.InstallHash,
		SessionIDHash: e.SessionHash,
		EventTime:     e.Time,
		Props:         e.Props,
	})
}

// base is what every constructor starts from: a fresh id, the install hash,
// the wall clock, and the common props. It never reads anything a person
// wrote — no flag, no path, no model answer.
func base(name, sessionID string, now time.Time) Event {
	props := commonProps()
	event := Event{
		Name:        name,
		ID:          randomHex(16),
		InstallHash: InstallIDHash(),
		Time:        roundTime(now),
		Props:       props,
	}
	if sessionID != "" {
		event.SessionHash = hashHex(sessionID)
	}
	return event
}

// FirstRun builds the once-per-install event. It carries no session hash, by
// contract: an install has no run yet.
func FirstRun(now time.Time) Event {
	return base("first_run", "", now)
}

// SessionStarted announces one run. mode must be chat or task; anything else
// collapses to chat, the mode a bare `codeaf` opens, rather than becoming a
// value the contract does not list.
func SessionStarted(mode Mode, resumed bool, sessionID string, now time.Time) Event {
	event := base("session_started", sessionID, now)
	if mode != ModeTask {
		mode = ModeChat
	}
	event.Props["mode"] = string(mode)
	event.Props["resumed"] = resumed
	return event
}

// SessionStats is what a run produced. It is a struct of typed Go values so
// the caller cannot hand over a pre-bucketed string, a raw error, or anything
// else the allowlist would have to trust.
type SessionStats struct {
	Duration         time.Duration
	Turns            int
	ModelCalls       int
	ModelCallsFailed int
	ToolCalls        int
	ToolCallsFailed  int
	CostUSD          float64
	StopReason       string
	ExitCode         int
}

// SessionEnded closes one run. An unrecognised stop reason is recorded as
// unknown — the fact that the run ended survives, the vocabulary it ended in
// does not — and the exit code is clamped to the contract's 0..5.
func SessionEnded(mode Mode, stats SessionStats, sessionID string, now time.Time) Event {
	event := base("session_ended", sessionID, now)
	if mode != ModeTask {
		mode = ModeChat
	}
	stop := stats.StopReason
	if !ValidStopReason(stop) {
		stop = StopUnknown
	}
	exit := stats.ExitCode
	if exit < 0 {
		exit = 0
	}
	if exit > 5 {
		exit = 5
	}
	event.Props["mode"] = string(mode)
	event.Props["duration"] = BucketDuration(stats.Duration)
	event.Props["turns"] = BucketCount(stats.Turns)
	event.Props["model_calls"] = BucketCount(stats.ModelCalls)
	event.Props["model_calls_failed"] = BucketCount(stats.ModelCallsFailed)
	event.Props["tool_calls"] = BucketCount(stats.ToolCalls)
	event.Props["tool_calls_failed"] = BucketCount(stats.ToolCallsFailed)
	event.Props["cost_usd"] = BucketCost(stats.CostUSD)
	event.Props["stop_reason"] = stop
	event.Props["exit_code"] = exit
	return event
}

// Fault describes one recovered panic for the fault event.
type Fault struct {
	Mode  string // chat or task; anything else is sent as other
	Scope string // main, goroutine or surface
	Stack []byte
}

// Fault builds the fault event. The fingerprint is derived from the stack
// here, by fingerprint.go; the panic value itself is never carried.
func FaultEvent(details Fault, sessionID string, now time.Time) Event {
	event := base("fault", sessionID, now)
	mode := string(details.Mode)
	switch mode {
	case string(ModeChat), string(ModeTask):
	default:
		mode = "other"
	}
	scope := string(details.Scope)
	switch scope {
	case ScopeMain, ScopeGoroutine, ScopeSurface:
	default:
		scope = ScopeMain
	}
	event.Props["mode"] = mode
	event.Props["scope"] = scope
	event.Props["fingerprint"] = Fingerprint(details.Stack)
	return event
}

// allowedProps is the allowlist itself: the one table the doc test reads and
// the constructors are held to. Adding a prop means adding it here, adding it
// to the constructor that produces it, and adding it to docs/TELEMETRY.md in
// the same change.
var allowedProps = map[string]map[string]bool{
	"first_run":       set(commonPropNames),
	"session_started": merge(set(commonPropNames), set([]string{"mode", "resumed"})),
	"session_ended": merge(set(commonPropNames), set([]string{
		"mode", "duration", "turns", "model_calls", "model_calls_failed",
		"tool_calls", "tool_calls_failed", "cost_usd", "stop_reason", "exit_code",
	})),
	"fault": merge(set(commonPropNames), set([]string{"mode", "scope", "fingerprint"})),
}

// AllowlistedProps answers which key an event name accepts. It backs the doc
// drift test and the privacy law: the table above is the allowlist, this is
// its only reader outside this file's own tests.
func AllowlistedProps(eventName string) []string {
	var names []string
	for key := range allowedProps[eventName] {
		names = append(names, key)
	}
	return names
}

// AllowlistedEvents names the four events the contract defines, in a stable
// order for the doc.
func AllowlistedEvents() []string {
	return []string{"first_run", "session_started", "session_ended", "fault"}
}

// CommonPropNames names the six props every event carries, in contract order.
func CommonPropNames() []string { return commonPropNames }

var commonPropNames = []string{
	"codeaf_version", "channel", "os", "arch", "usage_context", "install_method",
}

func set(names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, name := range names {
		out[name] = true
	}
	return out
}

func merge(a, b map[string]bool) map[string]bool {
	out := make(map[string]bool, len(a)+len(b))
	for key := range a {
		out[key] = true
	}
	for key := range b {
		out[key] = true
	}
	return out
}

// buildOSArch re-answers the buckets for tests that pin them; it exists so a
// test can name the current values without reaching into runtime itself.
func buildOSArch() (string, string) { return osName(), archName() }

var _ = runtime.GOOS
