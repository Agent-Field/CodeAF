package appwire

import (
	"strings"
	"time"
)

// The work log's detail (PROTOCOL.md, "Work log detail"): the facts of every
// tool call, from which the app groups, folds, pins and tails its rows, and
// the full output of one call by line.

// The kinds of tool call.
const (
	ToolRead      = "read"
	ToolSearch    = "search"
	ToolRun       = "run"
	ToolEdit      = "edit"
	ToolWeb       = "web"
	ToolConnector = "connector"
	ToolTask      = "task"
	ToolAsksFirst = "asks_first"
	ToolOther     = "other"
)

// EventToolOutput is a followed call's new output lines.
const EventToolOutput = "tool.output"

// ToolFacts are one call's facts, on turn.tool and on its tool entry.
type ToolFacts struct {
	Kind      string     `json:"tool_kind,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	// Batch is the same on every call the agent started together.
	Batch    string `json:"batch,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
	// RetriedOf is the earlier call of this turn that ran the same command.
	RetriedOf string       `json:"retried_of,omitempty"`
	Files     []FileChange `json:"files,omitempty"`
	Connector string       `json:"connector,omitempty"`
	Task      string       `json:"task,omitempty"`
	Ask       string       `json:"ask,omitempty"`
	Input     *ToolInput   `json:"input,omitempty"`
	// Tail is the last lines of the call's output: three while it runs, up
	// to twelve once it has ended. Lines is how many it wrote in all.
	Tail  []string `json:"tail,omitempty"`
	Lines *int     `json:"lines,omitempty"`
	// Handoff is what a team_post call delivered, read from the call's own
	// answer, and absent on every other call and one still running.
	Handoff *Handoff `json:"handoff,omitempty"`
}

// Handoff is one team_post as facts: the crew log it was written to, who it
// was for, and the log entry it became. Delivered false is a post the engine
// refused or could not write; Reason is then the engine's own words.
type Handoff struct {
	// CrewName is the name of the crew whose log holds the line.
	CrewName string `json:"crew_name,omitempty"`
	// To is "manager", "room" or a member's handle (no @).
	To string `json:"to,omitempty"`
	// Post is the log entry the line was written as and Answers the entry it
	// answers (LogLine ids); "" when the answer named none.
	Post      string `json:"post,omitempty"`
	Answers   string `json:"answers,omitempty"`
	Delivered bool   `json:"delivered"`
	Reason    string `json:"reason,omitempty"`
	// Up is a post to the manager of the crew above (the member's crew has
	// no manager of its own); FromCrew is the member's crew then.
	Up       bool   `json:"up,omitempty"`
	FromCrew string `json:"from_crew,omitempty"`
}

// ToolInput is what a call was asked to do: the command, query or path, and
// where and for how long a command may run.
type ToolInput struct {
	Cwd      string `json:"cwd,omitempty"`
	TimeoutS *int   `json:"timeout_s,omitempty"`
	Text     string `json:"text"`
}

// ToolOutputEvent is a followed call's new lines.
type ToolOutputEvent struct {
	Agent string   `json:"agent"`
	Call  string   `json:"call"`
	Lines []string `json:"lines"`
}

// ToolReader is the engine's half of the work log's output: one call's whole
// output by line, and following one call's output as it arrives.
type ToolReader interface {
	ToolOutput(agent, call string, from, limit int) (lines []string, next, total int, err error)
	FollowTool(agent, call string, follow bool) error
}

func init() {
	for name, m := range toolMethods {
		methods[name] = m
	}
}

// toolReader is the engine as a ToolReader, or a refusal.
func toolReader(e Engine, name string) (ToolReader, error) {
	r, ok := e.(ToolReader)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return r, nil
}

// toolCallParams names one call of one agent.
type toolCallParams struct {
	Agent    string `json:"agent"`
	Call     string `json:"call"`
	FromLine int    `json:"from_line"`
	Limit    int    `json:"limit"`
}

func checkToolCall(p toolCallParams, name string) (toolCallParams, error) {
	if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.Call) == "" {
		return p, Errorf(CodeBadParams, "%s needs an agent and a call", name)
	}
	if p.FromLine < 0 || p.Limit < 0 {
		return p, Errorf(CodeBadParams, "from_line and limit are zero or more")
	}
	return p, nil
}

// toolFollow is tools.follow or tools.unfollow.
func toolFollow(name string, follow bool) method {
	return typed(func(e Engine, p toolCallParams) (*Empty, error) {
		r, err := toolReader(e, name)
		if err != nil {
			return nil, err
		}
		p, err = checkToolCall(p, name)
		if err != nil {
			return nil, err
		}
		return done(r.FollowTool(p.Agent, p.Call, follow))
	})
}

var toolMethods = map[string]method{
	"tools.output": typed(func(e Engine, p toolCallParams) (*LinesNextLineTotalResult, error) {
		r, err := toolReader(e, "tools.output")
		if err != nil {
			return nil, err
		}
		p, err = checkToolCall(p, "tools.output")
		if err != nil {
			return nil, err
		}
		lines, next, total, err := r.ToolOutput(p.Agent, p.Call, p.FromLine, p.Limit)
		if err != nil {
			return nil, err
		}
		return &LinesNextLineTotalResult{nonNil(lines), next, total}, nil
	}),
	"tools.follow":   toolFollow("tools.follow", true),
	"tools.unfollow": toolFollow("tools.unfollow", false),
}

// The answers of this file's methods.
type (
	LinesNextLineTotalResult struct {
		Lines    []string `json:"lines"`
		NextLine int      `json:"next_line"`
		Total    int      `json:"total"`
	}
)
