package session

// The background half of the belt: bash with one extra argument, and the jobs
// tool that looks at what it started.
//
// This file is the WINDOW onto jobs.go's registry. The split is deliberate:
// jobs.go owns processes, logs, and the steering note; this file owns the wire
// — the schemas the model sees and the sentences it reads back. The watch tool
// (tools_watch.go) is the third thing in this family and it uses the same
// registry, the same list, and the same kill.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// ── bash, wrapped ───────────────────────────────────────────────────────────

// backgroundSentence is the one sentence the wrapper adds to pi's bash
// description. One sentence, not a paragraph: the tool is pi's, and a model
// that has read pi's description already knows what bash is.
const backgroundSentence = " Run long-lived commands (servers, watchers) with background:true and query them with the jobs tool."

// timeoutSentence states the v3 foreground law pi leaves unstated: a call the
// model did not bound is bounded by the harness, because one hung command
// otherwise wedges the whole turn until the person interrupts.
//
// AND WHAT REACHING THE BOUND ACTUALLY DOES, because that changed and a model
// reasoning from "it will be killed" reasons wrongly: it hedges, splits the
// command, or starts again from nothing when the answer was already running
// (promote.go).
const timeoutSentence = " Foreground calls are bounded at 120s unless you set timeout (max 600s); a foreground command that reaches its bound is NOT killed — it becomes a background job and the call answers 'still running as job N; log at <path>', so the work continues and its exit reaches you like any other job's. Background calls never time out."

// The v3 foreground timeout law. bare carries a model-settable timeout with no
// default and no sane cap (its maximum is int32 milliseconds — 24 days), which
// is pi's choice for a bare loop and the wrong default for a session: an
// unbounded foreground call is a turn that never ends. The defaults are Claude
// Code's proven shape — two minutes by default, ten at the most, anything
// longer belongs in the background where no clock runs at all. Exported so the
// surface can count down against the same numbers rather than restate them.
const (
	DefaultBashTimeoutSeconds = 120
	MaxBashTimeoutSeconds     = 600
)

// BashTimeoutSeconds is the bound one foreground bash call actually runs
// under: the model's own figure when it set a usable one, the default when it
// did not, the cap when it asked for more than the law allows.
//
// It is the ONE READ of the timeout argument. The wrapper applies it to the
// wire args ([withTimeoutLaw]) and the surface counts down against it
// (internal/tui3's toolLimit), so the number a person watches and the number
// the command dies on cannot drift apart.
//
// AN EXPLICIT NULL IS UNSET, and so is a zero, a negative, a string, a NaN, or
// anything else that does not read as a positive number of seconds. A weak
// model that spells every optional argument out — `"timeout": null` — must not
// be able to disarm the law by saying nothing in more words: that value used to
// take the "set" branch, decode as 0, pass the cap test untouched, and reach
// bare as a nil timeout, which arms no timer at all. An unbounded foreground
// call is a turn that never ends.
func BashTimeoutSeconds(args json.RawMessage) float64 {
	var fields struct {
		Timeout *float64 `json:"timeout"`
	}
	if err := json.Unmarshal(args, &fields); err != nil || fields.Timeout == nil {
		return DefaultBashTimeoutSeconds
	}
	seconds := *fields.Timeout
	switch {
	case math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0:
		return DefaultBashTimeoutSeconds
	case seconds > MaxBashTimeoutSeconds:
		return MaxBashTimeoutSeconds
	}
	return seconds
}

// withTimeoutLaw returns args with the v3 timeout law applied: the default
// written in when the model set none it can be held to, the cap clamped when it
// set too much. The bytes are re-marshalled only when something actually
// changed — a call that already sits inside the law rides through untouched,
// and args that do not decode belong to bare's own error wording, not this
// wrapper's.
func withTimeoutLaw(args json.RawMessage) json.RawMessage {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return args
	}
	seconds := BashTimeoutSeconds(args)
	if raw, set := fields["timeout"]; set {
		var asked float64
		if err := json.Unmarshal(raw, &asked); err == nil && asked == seconds {
			return args
		}
	}
	fields["timeout"] = json.RawMessage(strconv.FormatFloat(seconds, 'f', -1, 64))
	out, err := json.Marshal(fields)
	if err != nil {
		return args
	}
	return out
}

// backgroundProperty is the one property the wrapper adds to pi's bash schema.
const backgroundProperty = `{"type":"boolean","description":"Run the command in the background and return immediately with a job id instead of waiting for it to finish (default: false)"}`

// backgroundBash wraps bare's bash: the same tool, with one optional argument.
//
// Without background, the call is handed to bare verbatim — same timeout, same
// truncation, same wire text. bare's parser ignores fields it does not know, so
// background:false rides through harmlessly rather than needing to be stripped.
// With background, the process is started by the registry and the call returns
// in the time it takes to fork.
func (a *Agent) backgroundBash(inner bare.Tool) bare.Tool {
	return bare.Tool{
		Name:        inner.Name,
		Description: inner.Description + backgroundSentence + timeoutSentence,
		Schema:      schemaWithBackground(inner.Schema),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Command    string `json:"command"`
				Background bool   `json:"background"`
			}
			// A call whose arguments do not parse belongs to bare: it owns the
			// wording of every other bash error, and a second parser reporting
			// the same fault in different words helps nobody.
			if err := json.Unmarshal(args, &parsed); err != nil || !parsed.Background {
				// THE PROMOTION DOOR IS FITTED HERE AND ONLY HERE (promote.go).
				// This is the foreground branch, so a call that asked for
				// background:true can never carry it — it left this function on
				// the other side of the branch and was a job from the first
				// instant, with nothing to promote and no timeout to promote at.
				return inner.Execute(a.promotable(ctx), withTimeoutLaw(args))
			}
			if strings.TrimSpace(parsed.Command) == "" {
				return "Invalid arguments: command is required", true, nil
			}
			started, err := a.jobs.start(parsed.Command)
			if err != nil {
				return "Could not start the background job: " + err.Error(), true, nil
			}
			// The id and the path, and nothing else. There is no output yet by
			// construction, and the two things the model needs next — how to ask
			// about it, where to read it — are both here.
			return fmt.Sprintf("job %d started; log at %s", started.id, started.logPath), false, nil
		},
	}
}

// schemaWithBackground returns pi's bash schema with one boolean property
// added.
//
// It DERIVES rather than restating the schema as a literal: bare's bytes are
// pinned to pi's source and may move with it, and a copied literal here would
// drift silently into a session bash whose wire schema is a version behind the
// tool it wraps. A schema that will not decode falls back to the original —
// background is then unsupported on the wire, which is a smaller failure than
// a malformed schema that fails the whole belt at construction.
func schemaWithBackground(schema json.RawMessage) json.RawMessage {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(schema, &root); err != nil {
		return schema
	}
	var properties map[string]json.RawMessage
	if err := json.Unmarshal(root["properties"], &properties); err != nil {
		return schema
	}
	properties["background"] = json.RawMessage(backgroundProperty)
	encodedProperties, err := json.Marshal(properties)
	if err != nil {
		return schema
	}
	root["properties"] = encodedProperties
	encoded, err := json.Marshal(root)
	if err != nil {
		return schema
	}
	return encoded
}

// ── jobs ────────────────────────────────────────────────────────────────────

// jobsDefaultTail and jobsMaxTail bound one output call. The default is a
// screenful; the cap is what keeps "show me the log" from being a way to put a
// megabyte in the context window when the file is right there to read.
const (
	jobsDefaultTail = 50
	jobsMaxTail     = 200
)

const jobsDescription = "Inspect background work: commands started with bash background:true, and watches started with the watch tool. Actions: 'list' — every job this session started, with id, kind, command, status (running, exited(N), killed, stopped) and elapsed time; 'output' — the last lines of one job's output (default 50, maximum 200) from an in-memory buffer of its last 64KB, which for a watch is the accumulated output of its ticks; 'kill' — SIGTERM the job's process group, then SIGKILL after 2 seconds, or stop a watch. The complete log of every job is a file on disk, named when the job started: read it with the read tool when the tail is not enough."

const jobsSchemaJSON = `{"type":"object","properties":{"action":{"type":"string","description":"What to do: list, output, or kill","enum":["list","output","kill"]},"id":{"type":"number","description":"Job id (required for output and kill)"},"tail":{"type":"number","description":"Number of trailing output lines to return (default: 50, maximum: 200)"}},"required":["action"],"additionalProperties":false}`

// jobsTool is the window onto the registry. It is a belt tool like any other —
// same Tool shape, same wire discipline — and it is deliberately the ONLY way
// the model reaches a job: the registry is not addressable from the prompt, so
// there is one vocabulary for background work and it is this one.
func (a *Agent) jobsTool() bare.Tool {
	return bare.Tool{
		Name:        "jobs",
		Description: jobsDescription,
		Schema:      json.RawMessage(jobsSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			parsed, err := parseJobsArguments(args)
			if err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			switch parsed.Action {
			case "list":
				return a.jobs.list(), false, nil
			case "output":
				if parsed.ID == nil {
					return "Invalid arguments: id is required for output", true, nil
				}
				lines := jobsDefaultTail
				if parsed.Tail != nil {
					lines = *parsed.Tail
				}
				if lines > jobsMaxTail {
					lines = jobsMaxTail
				}
				if lines < 1 {
					lines = 1
				}
				text, isError := a.jobs.output(*parsed.ID, lines)
				return text, isError, nil
			case "kill":
				if parsed.ID == nil {
					return "Invalid arguments: id is required for kill", true, nil
				}
				text, isError := a.jobs.kill(*parsed.ID)
				return text, isError, nil
			case "":
				return "Invalid arguments: action is required (list, output, or kill)", true, nil
			default:
				return fmt.Sprintf("Unknown action: %s. Use list, output, or kill.", parsed.Action), true, nil
			}
		},
	}
}
