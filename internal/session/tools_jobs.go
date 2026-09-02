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
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// ── bash, wrapped ───────────────────────────────────────────────────────────

// backgroundSentence is the one sentence the wrapper adds to pi's bash
// description. One sentence, not a paragraph: the tool is pi's, and a model
// that has read pi's description already knows what bash is.
// AND WHAT THE PERSON GETS OUT OF IT, because that is the fact that decides
// whether this door is the right one. A job leaves a log and an exit code; a
// task leaves a room, a row that says what the work is, and a report. A model
// that only knows background:true exists will reach for it for any long thing,
// which is how a multi-part research sweep became a shell command nobody could
// watch.
const backgroundSentence = " Run long-lived commands (servers, watchers, long builds) with background:true and query them with the jobs tool. A background job gives the person a quiet row on the right while it runs and leaves them its log and an exit code, and nothing else — work whose outcome is a deliverable somebody reads belongs to propose_task instead, which leaves them a room to watch and a report."

// timeoutSentence states the v3 foreground law pi leaves unstated: a call the
// model did not bound is bounded by the harness, because one hung command
// otherwise wedges the whole turn until the person interrupts.
//
// AND WHAT REACHING THE BOUND ACTUALLY DOES, because that changed and a model
// reasoning from "it will be killed" reasons wrongly: it hedges, splits the
// command, or starts again from nothing when the answer was already running
// (promote.go).
//
// The figure is interpolated from [BashCeilingSeconds] rather than typed, on
// this codebase's one-source-of-truth law: a number in a description is read by
// the model as a fact about the machine, and a stale one is a lie it reasons
// from.
func timeoutSentence(backgroundAfter int) string {
	if backgroundAfter <= 0 {
		return " bash WAITS for the command. A foreground call runs for as long as your own timeout argument says, up to " +
			strconv.Itoa(BashCeilingSeconds) + "s, and is never turned into a job before then. A call that outlives even that is NOT killed — it becomes a background job, and the call answers with the output so far and 'still running as job N; log at <path>'. Its exit and closing output then arrive on their own, in this conversation, with no call from you: do not poll for them. Background calls never time out."
	}
	return " bash WAITS for the command. A foreground call runs for up to " + strconv.Itoa(backgroundAfter) +
		" seconds and is then kept running as a background job while you get its output so far and the job id. Its own timeout can move that handoff sooner and is capped at " +
		strconv.Itoa(BashCeilingSeconds) + " seconds. The job's exit and closing output then arrive on their own, in this conversation, with no call from you: do not poll for them. Background calls never time out."
}

// BashCeilingSeconds is THE bound on a foreground bash call — one number, read
// everywhere, typed once.
//
// ── WHY THE CEILING IS THE DEFAULT TOO ──
//
// There used to be two numbers: a 120-second default and a 600-second cap. The
// gap between them was measured and it was expensive. A scoring script that took
// three to five minutes hit the 120-second default on every call, was adopted as
// a job, and answered `still running as job N` — so a model that had asked for
// nothing of the kind was handed a background job it then had to chase, and the
// chasing (sleep, tail, sleep, tail) ate 68% of a ten-hour worker's wall clock
// while a competitor's harness, which simply waited, saw the same score
// thirty-eight times to our two.
//
// THE MODEL'S FIGURE IS HONOURED UP TO THIS CEILING. A session may hand the
// still-running command to the job registry sooner through its independently
// configured background-after clock; setting that clock to zero restores the
// timeout-only posture. Either handoff preserves the same process.
//
// The number itself lives with the bare tool (bare.BashCeilingSeconds), which
// now applies it as its own default too — a headless worker nobody is watching
// used to run `find /` unbounded — so the session, the bare loop and the surface
// that counts down all read one figure. Exported here so the surface need not
// know where it is kept.
const BashCeilingSeconds = bare.BashCeilingSeconds

// BashTimeoutSeconds is the bound one foreground bash call actually runs
// under: the model's own figure when it set a usable one, and
// [BashCeilingSeconds] when it did not or when it asked for more.
//
// It is the ONE READ of the timeout argument. The wrapper applies it to the
// wire args ([withTimeoutLaw]) and the surface counts down against it
// (internal/tui3's toolLimit), so the number a person watches and the number
// the command is bounded by cannot drift apart.
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
	if err := decodeToolArguments(args, &fields); err != nil || fields.Timeout == nil {
		return BashCeilingSeconds
	}
	seconds := *fields.Timeout
	switch {
	case math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0:
		return BashCeilingSeconds
	case seconds > BashCeilingSeconds:
		return BashCeilingSeconds
	}
	return seconds
}

// BashBoundSeconds is the first clock a foreground row will meet. With the
// background-after clock off it is the command's timeout law unchanged; with
// it on, the earlier of the two hands the process to the job registry.
func BashBoundSeconds(args json.RawMessage, backgroundAfter int) float64 {
	timeout := BashTimeoutSeconds(args)
	if backgroundAfter <= 0 || timeout <= float64(backgroundAfter) {
		return timeout
	}
	return float64(backgroundAfter)
}

// withTimeoutLaw returns args with the v3 timeout law applied: the default
// written in when the model set none it can be held to, the cap clamped when it
// set too much. The bytes are re-marshalled only when something actually
// changed — a call that already sits inside the law rides through untouched,
// and args that do not decode belong to bare's own error wording, not this
// wrapper's.
func withTimeoutLaw(args json.RawMessage) json.RawMessage {
	var fields map[string]json.RawMessage
	if err := decodeToolArguments(args, &fields); err != nil {
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
//
// IT SPELLS OUT THE SPAWN SEMANTICS, because that is the whole difference
// between the two doors and the model has to be able to choose between them: a
// foreground call WAITS and hands back the output, a background one FORKS and
// hands back an id, and either way the ending reaches the conversation without
// being asked for.
const backgroundProperty = `{"type":"boolean","description":"Spawn the command instead of waiting for it: the call returns immediately with a job id, and the job's exit and closing output arrive in the conversation on their own when it ends (default: false — the call waits and returns the output)"}`

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
		Description: inner.Description + backgroundSentence + timeoutSentence(a.config.BashBackgroundAfterSeconds),
		Schema:      schemaWithBackground(inner.Schema),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Command    string `json:"command"`
				Background bool   `json:"background"`
			}
			// A call whose arguments do not parse belongs to bare: it owns the
			// wording of every other bash error, and a second parser reporting
			// the same fault in different words helps nobody.
			if err := decodeToolArguments(args, &parsed); err != nil || !parsed.Background {
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

// WRITTEN FOR DENSITY, BECAUSE THIS STRING IS BILLED ON EVERY REQUEST OF EVERY
// TURN. The belt's schemas ride in front of every request the model makes, so
// each word here is paid dozens of times in one task and the prose around it is
// paid never. Every rule the longer version stated is still stated, once.
//
// AND EVERY FIGURE IS INTERPOLATED. The ring's size, the kill grace and the
// tail's bounds are all enforced somewhere else in this package ([jobRingBytes],
// [jobTermGrace], [jobsDefaultTail], [jobsMaxTail]); a digit typed here would be
// the second copy, and the second copy is the one that goes stale.
// AND IT DOES NOT OFFER POLLING AS A WAY TO WAIT. `output` is still here for an
// intermediate look at a job somebody asked about, but the sentence that used to
// invite a poll loop is gone and replaced by the fact that makes polling
// pointless: A FINISHED JOB REPORTS ITSELF. Every outstanding job's state also
// rides at the foot of every tool result (jobfooter.go), so "is it still going,
// and what did it last say" is answered without a call at all. The measured cost
// of the old wording was a model that answered `sleep 30 && tail` nine times to
// an empty log and then killed the work.
var jobsDescription = "Background work: bash background:true commands and watches. Completions come to you: when a job ends, its exit code and closing output arrive in the conversation on their own, and every running job's elapsed time and last line ride at the foot of every tool result — so never sleep, tail or poll to wait for one. list: this session's jobs (id, kind, command, status, elapsed). output: the tail of one job's last " +
	strconv.Itoa(jobRingBytes>>10) + "KB (a watch's is its accumulated ticks), for an intermediate look and not for waiting. kill: SIGTERM the process group, SIGKILL " +
	strconv.Itoa(int(jobTermGrace/time.Second)) + "s later; stops watches. Each job's whole log is a file on disk, named when it started; read it when the tail is short. A running job also shows on their screen."

var jobsSchemaJSON = `{"type":"object","properties":{"action":{"type":"string","description":"The op.","enum":["list","output","kill"]},"id":{"type":"integer","description":"Job id (output and kill need one)"},"tail":{"type":"integer","description":"Lines returned (default: ` +
	strconv.Itoa(jobsDefaultTail) + `, max: ` + strconv.Itoa(jobsMaxTail) + `)"}},"required":["action"],"additionalProperties":false}`

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
				// THE CALL'S OWN CONTEXT RIDES INTO THE GRACES, so a kill
				// caught inside a turn somebody stopped does not spend four
				// seconds waiting for an exit nobody is waiting for (jobs.go's
				// [waitDoneUnder]). The signals are sent either way.
				text, isError := a.jobs.kill(ctx, *parsed.ID)
				return text, isError, nil
			case "":
				return "Invalid arguments: action is required (list, output, or kill)", true, nil
			default:
				return fmt.Sprintf("Unknown action: %s. Use list, output, or kill.", parsed.Action), true, nil
			}
		},
	}
}
