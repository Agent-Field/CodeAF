// Package claudecode is the program that hands a whole task to the person's own
// Claude Code: `codeaf claude <brief>` in a shell, `/claude <brief>` in the chat,
// and a worker a task can name in `via`.
//
// CLAUDE IS SEATED AS AN AGENT, NEVER AS A MODEL. codeaf reaches OpenRouter and
// a ChatGPT plan as model services: it sends each request itself, and its own
// loop decides what happens next. Anthropic does not offer a Claude plan that
// way to another program, so this package does not try to. It starts the
// `claude` program the person installed and signed in to themselves, in the
// folder codeaf cut for the run, and Claude Code runs its own loop with its own
// tools on its own account. codeaf holds no Claude credential, sends no request
// to Anthropic, and reads only what the program prints: its stream-json events,
// which become this run's steps and its ending.
//
// That is the same arrangement an editor makes when it drives an agent over the
// Agent Client Protocol, and it is why the program is a delegate (a child that
// streams records and ends with one terminal record) rather than a model
// source: the seam already existed for senior-dev, and Claude Code is one more
// program on the far side of it.
package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/env"
)

// Name is the program's one word: the slash command, the shell verb and the
// manual page.
const Name = "claude"

// EnvBinary names the Claude Code program to start in place of the first
// `claude` on PATH, for a machine whose `claude` is a wrapper or a shell
// function that a child process cannot reach.
const EnvBinary = "CODEAF_CLAUDE_BIN"

// stageWork is the one stage a run reports. Claude Code plans, edits and
// checks inside its own loop, so codeaf has no phases of its own to name.
const stageWork = "work"

// guide is what the conversation's model reads about the program on every
// turn, so it is held to delegate.GuideMax.
const guide = "Hands the task to the person's own Claude Code, which works in the folder with its own tools on " +
	"their Claude plan and leaves its changes on the run's branch. Use it when they ask for Claude, or name " +
	"Claude Code, to do the work. Its brief is the whole task in plain words."

// Program is the definition codeaf carries (internal/delegate/builtin).
var Program = delegate.Delegate{
	Name:    Name,
	Summary: "hand a task to your own signed-in Claude Code",
	Guide:   guide,
	Lands:   delegate.LandsTree,
	// A PERSON'S CREW SEAT IS HEARD WHEN IT IS A CLAUDE MODEL. A crew whose
	// working seat is `anthropic/claude-opus-…` asked for Opus, and Claude Code
	// answers to the family word; any other vendor's model is not something
	// Claude Code can run, so it is left to Claude Code's own default.
	CrewFlags:  crewFlags,
	StageWords: map[string]string{stageWork: "working in Claude Code"},
	Unattended: delegate.Ceilings{CostUSD: 10, Hours: 2},
	Default:    "run",
	Page:       Name,
	Commands:   []delegate.Command{{Name: "run", Usage: "[flags] -- <brief>", Summary: "run the brief in Claude Code", Bind: bindRun}},
}

// crewFlags turns the crew's working seat into Claude Code's model word.
func crewFlags(crew delegate.Crew) []string {
	seat := crew.Hands
	if len(crew.Asked) > 0 {
		seat = crew.Asked[0]
	}
	if family := Family(seat); family != "" {
		return []string{"--claude-model", family}
	}
	return nil
}

// Family answers the Claude Code model word (`opus`, `sonnet`, `haiku`) for a
// model id from any service's spelling of it, or "" for a model that is not
// Claude.
func Family(model string) string {
	lower := strings.ToLower(model)
	if !strings.Contains(lower, "claude") && !strings.HasPrefix(lower, "anthropic/") {
		return ""
	}
	for _, family := range []string{"opus", "sonnet", "haiku"} {
		if strings.Contains(lower, family) {
			return family
		}
	}
	return ""
}

func bindRun(fs *flag.FlagSet) delegate.Body {
	model := fs.String("claude-model", "", "Claude Code model: opus, sonnet, haiku or a full name")
	return func(ctx context.Context, host delegate.Host, args []string) error {
		brief := strings.TrimSpace(strings.Join(args, " "))
		host.Hello([]string{stageWork})
		if brief == "" {
			host.Terminal(delegate.Ending{Status: delegate.StatusFail, Message: "there was no brief to hand Claude Code"})
			return nil
		}
		binary, err := Binary()
		if err != nil {
			host.Terminal(delegate.Ending{Status: delegate.StatusFail, Message: err.Error()})
			return nil
		}
		ceilings := host.Ceilings()
		if ceilings.Hours > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Duration(ceilings.Hours*float64(time.Hour)))
			defer cancel()
		}
		cmd := exec.CommandContext(ctx, binary, Args(brief, *model, ceilings)...)
		cmd.Dir = host.Workspace()
		cmd.Stderr = os.Stderr
		out, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			host.Terminal(delegate.Ending{Status: delegate.StatusFail, Message: "Claude Code did not start: " + err.Error()})
			return nil
		}
		end := Translate(out, host)
		waitErr := cmd.Wait()
		if !end.seen {
			message := "Claude Code ended without saying how it went"
			status := delegate.StatusCrashed
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				message, status = "Claude Code was stopped at the time ceiling", delegate.StatusBudget
			} else if waitErr != nil {
				message += ": " + waitErr.Error()
			}
			host.Terminal(delegate.Ending{Status: status, Message: message})
			return nil
		}
		host.Terminal(end.Ending)
		return nil
	}
}

// Binary finds the Claude Code program: [EnvBinary] when it is set, else the
// first `claude` on PATH. The refusal says how to make it findable, because a
// person who has Claude Code installed and reads "not found" needs to know
// which of the two codeaf looked at.
func Binary() (string, error) {
	if named := strings.TrimSpace(env.Get(EnvBinary)); named != "" {
		if _, err := os.Stat(named); err != nil {
			return "", fmt.Errorf("%s names %s, which is not there", EnvBinary, named)
		}
		return named, nil
	}
	path, err := exec.LookPath("claude")
	if err != nil {
		return "", fmt.Errorf("Claude Code is not installed here: install it, run `claude` once to sign in, or set %s to its path", EnvBinary)
	}
	return path, nil
}

// Args is the command line Claude Code is started with. It is a pure function
// so a test can hold the flags without starting anything.
//
// THE RUN IS UNATTENDED, SO NOTHING MAY WAIT ON A PERMISSION PROMPT. codeaf
// starts a tree program on a branch it cut for the run, which is the same trust
// senior-dev's own shell gets; acceptEdits plus the named tools lets Claude Code
// edit and run commands without stopping to ask a person who is not there.
func Args(brief, model string, ceilings delegate.Ceilings) []string {
	args := []string{
		"-p", brief,
		"--output-format", "stream-json", "--verbose",
		"--permission-mode", "acceptEdits",
		"--allowedTools", "Read Edit Write Glob Grep Bash TodoWrite",
		"--append-system-prompt", "You are running unattended for codeaf. Nobody can answer a question: " +
			"decide, do the work, check it, and end with a short account of what you changed and how you checked it.",
	}
	if model = strings.TrimSpace(model); model != "" {
		args = append(args, "--model", model)
	}
	if ceilings.CostUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", ceilings.CostUSD))
	}
	return args
}

// ending is what the stream said at its end, and whether it said it at all.
type ending struct {
	delegate.Ending
	seen bool
}

// event is the part of a Claude Code stream-json line this package reads.
type event struct {
	Type         string  `json:"type"`
	Subtype      string  `json:"subtype"`
	Model        string  `json:"model"`
	APIKeySource string  `json:"apiKeySource"`
	IsError      bool    `json:"is_error"`
	Result       string  `json:"result"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	NumTurns     int     `json:"num_turns"`
	Message      struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// block is one item of a message's content.
type block struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// Translate reads Claude Code's stream-json from r and reports it through host:
// a stage when the session starts, a step for every tool call once its result
// arrives, and the ending the result line says. It never writes the terminal
// record itself; the caller does, once, whatever the stream said.
func Translate(r io.Reader, host delegate.Host) ending {
	var end ending
	pending := map[string]block{}
	subscription := false
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	for scanner.Scan() {
		var ev event
		if json.Unmarshal(scanner.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "system":
			if ev.Subtype != "init" {
				continue
			}
			// A SESSION SIGNED IN TO A CLAUDE PLAN SPENDS NO MONEY OF CODEAF'S
			// LEDGER. Claude Code reports what the run WOULD have cost at API
			// prices either way; on a plan that figure is not a charge, so it
			// is kept as a fact and never booked as spend.
			subscription = ev.APIKeySource == "" || ev.APIKeySource == "none"
			data, _ := json.Marshal(map[string]any{"models": []string{ev.Model}, "doing": "working"})
			host.Stage(delegate.StageRecord{Stage: stageWork, Status: "running", Data: data})
		case "assistant":
			for _, b := range blocks(ev.Message.Content) {
				if b.Type == "tool_use" {
					pending[b.ID] = b
				}
			}
		case "user":
			for _, b := range blocks(ev.Message.Content) {
				if b.Type != "tool_result" {
					continue
				}
				call, ok := pending[b.ToolUseID]
				if !ok {
					continue
				}
				delete(pending, b.ToolUseID)
				step := delegate.StepRecord{Tool: call.Name, Step: stageWork, Command: describe(call), Observation: resultText(b.Content)}
				if call.Name == "Bash" {
					exit := 0
					if b.IsError {
						exit = 1
					}
					step.Exit = &exit
				}
				host.Step(step)
			}
		case "result":
			end.seen = true
			end.Deliverable = strings.TrimSpace(ev.Result)
			end.Claim = end.Deliverable
			end.Extra = map[string]any{"claude_turns": ev.NumTurns, "claude_list_price_usd": ev.TotalCostUSD}
			if !subscription {
				end.CostUSD = ev.TotalCostUSD
			}
			switch {
			case ev.Subtype == "error_max_budget_usd":
				end.Status, end.Message = delegate.StatusBudget, "Claude Code stopped at the cost ceiling"
			case ev.IsError || ev.Subtype != "success":
				end.Status, end.Message = delegate.StatusFail, "Claude Code ended with "+strings.ReplaceAll(ev.Subtype, "_", " ")
			default:
				end.Status, end.Message = delegate.StatusPass, "Claude Code finished"
			}
		}
	}
	return end
}

func blocks(raw json.RawMessage) []block {
	var out []block
	_ = json.Unmarshal(raw, &out)
	return out
}

// describe is the one line a step shows for a tool call: the command for a
// shell call, the file for a file tool, the pattern for a search.
func describe(call block) string {
	var in map[string]any
	_ = json.Unmarshal(call.Input, &in)
	for _, key := range []string{"command", "file_path", "pattern", "path", "url"} {
		if value, ok := in[key].(string); ok && value != "" {
			return value
		}
	}
	return call.Name
}

// resultText flattens a tool result, which is a string or a list of blocks.
func resultText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &parts)
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(part.Text)
	}
	return b.String()
}
