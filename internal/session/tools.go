package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// belt is the session's tool inventory: the seven pi tools — read, bash, edit,
// write, grep, find, ls — with their verbatim pi schemas and descriptions.
//
// The hands are bare's, not a second copy: the session works the way a
// subharness leaf works, and a divergence between them would be a divergence
// between what the person watched happen and what the workforce would do with
// the same instruction. The workforce verbs (task/change/stop) are not here
// yet; when the tasker attaches they append to this slice and nothing else
// changes.
//
// There is deliberately no todo tool (docs/CHAT-V3.md Decision 11): a plan
// the person can read beats state only the model can see, and work big enough
// to decompose belongs to the workforce, not to a session-local list.
//
// Two entries are the session's own rather than bare's: bash is WRAPPED (not
// replaced) so it can start a background job, and jobs is added beside it to
// look at what was started. bare is untouched — a subharness leaf gets pi's
// bash exactly as before, and the session gets pi's bash plus one argument.
func (a *Agent) belt() []bare.Tool {
	tools := bare.AllTools(a.config.Workspace)
	for index, tool := range tools {
		if tool.Name == "bash" {
			tools[index] = a.backgroundBash(tool)
		}
	}
	return append(tools, a.jobsTool())
}

// ── bash, wrapped ───────────────────────────────────────────────────────────

// backgroundSentence is the one sentence the wrapper adds to pi's bash
// description. One sentence, not a paragraph: the tool is pi's, and a model
// that has read pi's description already knows what bash is.
const backgroundSentence = " Run long-lived commands (servers, watchers) with background:true and query them with the jobs tool."

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
		Description: inner.Description + backgroundSentence,
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
				return inner.Execute(ctx, args)
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

const jobsDescription = "Inspect background commands started with bash background:true. Actions: 'list' — every job this session started, with id, command, status (running, exited(N), killed) and elapsed time; 'output' — the last lines of one job's output (default 50, maximum 200) from an in-memory buffer of its last 64KB; 'kill' — SIGTERM the job's process group, then SIGKILL after 2 seconds. The complete log of every job is a file on disk, named when the job started: read it with the read tool when the tail is not enough."

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

// toolDefinitions builds the wire form of the belt, carrying each tool's
// schema and description verbatim.
//
// A schema that does not parse fails here, at construction. Dropped instead, it
// would ride the wire as Parameters:nil — a tool the model is told takes no
// arguments — and every call it then made would fail as if the model had
// written it wrong. The belt is built from literals in this binary, so a
// malformed schema is a bug in the build, and the build is where it belongs.
func toolDefinitions(tools []bare.Tool) ([]ai.ToolDefinition, error) {
	definitions := make([]ai.ToolDefinition, len(tools))
	for index, tool := range tools {
		var parameters map[string]interface{}
		if err := json.Unmarshal(tool.Schema, &parameters); err != nil {
			return nil, fmt.Errorf("session: tool %q has a malformed schema: %w", tool.Name, err)
		}
		definitions[index] = ai.ToolDefinition{
			Type: "function",
			Function: ai.ToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  parameters,
			},
		}
	}
	return definitions, nil
}
