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
	case d <= 0, d < time.Minute:
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
	// MarshalJSON carries only the contract's keys and the allowlisted props;
	// an unknown prop key in the map (a hand-written or older-build spool line)
	// is dropped here rather than sent.
	clean := make(map[string]any, len(e.Props))
	if allowed := allowedProps[e.Name]; len(allowed) > 0 {
		for key, value := range e.Props {
			if allowed[key] {
				clean[key] = value
			}
		}
	}
	row := jsonEvent{
		EventName:     e.Name,
		EventID:       e.ID,
		InstallIDHash: e.InstallHash,
		SessionIDHash: e.SessionHash,
		EventTime:     e.Time,
		Props:         clean,
	}
	if e.Name == "first_run" {
		row.SessionIDHash = ""
	}
	return jsonMarshal(row)
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
	TotalTokens      int
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
	event.Props["total_tokens"] = max(0, stats.TotalTokens)
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
		"tool_calls", "tool_calls_failed", "cost_usd", "total_tokens", "stop_reason", "exit_code",
	})),
	"fault": merge(set(commonPropNames), set([]string{"mode", "scope", "fingerprint"})),
}

// EveryEvent is the key under which [PropDoc] answers for the six props every
// event carries, and the exact words the doc's table spells in its first
// column for them.
const EveryEvent = "every event"

// propDocs is what each allowlisted prop IS, in a person's words: the third
// column of docs/TELEMETRY.md's table, held here so that `codeaf telemetry
// show` and the doc read from one table and the doc test can fail the build
// when the two drift. Every allowlisted prop has a line, and the test holds
// that too.
var propDocs = map[string]map[string]string{
	EveryEvent: {
		"codeaf_version": "the release tag this binary was built from, at most 64 characters",
		"channel":        "stable, rc, staging, dev, or unknown",
		"os":             "darwin, linux, windows, or other",
		"arch":           "amd64, arm64, or other",
		"usage_context":  "local (a person's machine), ci, or container",
		"install_method": "script, source, or unknown",
	},
	"session_started": {
		"mode":    "chat or task",
		"resumed": "whether the session continued an earlier one",
	},
	"session_ended": {
		"mode":               "chat or task",
		"duration":           "a band: under 1m, 1-5m, 5-30m, 30m-2h, 2h or more",
		"turns":              "a count band",
		"model_calls":        "a count band",
		"model_calls_failed": "a count band",
		"tool_calls":         "a count band",
		"tool_calls_failed":  "a count band",
		"cost_usd":           "a dollar band",
		"total_tokens":       "total provider-reported input and output tokens in this session",
		"stop_reason":        "done, error, incomplete, budget, turn-cap, deadline, price, question, interrupted, or unknown",
		"exit_code":          "0 to 5",
	},
	"fault": {
		"mode":        "chat, task, or other",
		"scope":       "main, goroutine, or surface",
		"fingerprint": "16 hex characters hashed from codeaf function names in the stack",
	},
}

// PropDoc answers what one prop is, for the event named — [EveryEvent] for
// the six every event carries — or "" for a prop the table does not hold.
func PropDoc(event, prop string) string { return propDocs[event][prop] }

// EventPropNames answers the props an event adds beyond the every-event six,
// in the doc's order, so a listing reads the way the doc reads.
func EventPropNames(event string) []string {
	switch event {
	case "session_started":
		return []string{"mode", "resumed"}
	case "session_ended":
		return []string{"mode", "duration", "turns", "model_calls", "model_calls_failed",
			"tool_calls", "tool_calls_failed", "cost_usd", "total_tokens", "stop_reason", "exit_code"}
	case "fault":
		return []string{"mode", "scope", "fingerprint"}
	}
	return nil
}

// The identity and envelope fields every event carries beside its props, and
// what each is, for a listing that shows a person the whole row.
const (
	InstallHashDoc = "sha256 of a random id kept on this machine; the id itself never leaves"
	SessionHashDoc = "sha256 of the run id, one per session; absent on first_run"
	EventIDDoc     = "16 random bytes as hex, one per event"
	EventTimeDoc   = "when the event happened, UTC, to the second"
)

// CountBandsDoc and CostBandsDoc spell the bands, as the doc spells them.
const (
	CountBandsDoc = "count bands are 0, 1, 2-5, 6-20, 21-100 and 100+"
	CostBandsDoc  = "dollar bands are 0, under 0.01, 0.01-0.1, 0.1-1, 1-10 and 10+"
)

// StopReasons lists every stop reason, in the contract's order.
func StopReasons() []string {
	return []string{StopDone, StopError, StopIncomplete, StopBudget, StopTurnCap,
		StopDeadline, StopPrice, StopQuestion, StopInterrupted, StopUnknown}
}

// exampleProps is one plausible value per event prop, spelled from the
// contract's own constants wherever the contract has one, so an example row
// can never show a value a real row could not carry. `codeaf telemetry show`
// prints one row per event from this table so a person sees the shape of
// what leaves before anything has. The fingerprint is the one invented value:
// sixteen hex characters, which is all a real one is.
var exampleProps = map[string]map[string]string{
	"session_started": {
		"mode":    string(ModeChat),
		"resumed": "false",
	},
	"session_ended": {
		"mode":               string(ModeChat),
		"duration":           Duration5To30m,
		"turns":              BucketSix,
		"model_calls":        BucketTwo1,
		"model_calls_failed": BucketZero,
		"tool_calls":         BucketSix,
		"tool_calls_failed":  BucketZero,
		"cost_usd":           Cost10cTo1,
		"total_tokens":       "12500",
		"stop_reason":        StopDone,
		"exit_code":          "0",
	},
	"fault": {
		"mode":        string(ModeChat),
		"scope":       ScopeMain,
		"fingerprint": "3fa9c1e2b7d04e85",
	},
}

// ExampleProp answers the example value for one event prop, or "" for a prop
// the table does not hold; a test holds the table to the allowlist.
func ExampleProp(event, prop string) string { return exampleProps[event][prop] }

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
