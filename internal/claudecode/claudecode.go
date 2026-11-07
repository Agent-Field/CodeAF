// Package claudecode is the program that hands a whole task to the person's own
// Claude Code: `codeaf claude <brief>` in a shell, `/claude <brief>` in the chat,
// and a worker a task can name in `via`.
//
// CLAUDE IS SEATED AS AN AGENT, NEVER AS A MODEL. codeaf reaches OpenRouter and
// a ChatGPT plan as model services: it sends each request itself, and its own
// loop decides what happens next. Anthropic does not offer a Claude plan that
// way to another program, so this package does not try to. It holds a live
// session with the `claude` program the person installed and signed in to
// themselves (internal/claudeagent, the same control protocol the Agent SDK
// and T3 Code speak), in the folder codeaf cut for the run. Claude Code runs
// its own loop with its own tools on its own account; codeaf holds no Claude
// credential and sends no request to Anthropic.
//
// IT IS A WORKER LIKE ANY OTHER. Its tool calls are the task's steps, the
// person's words on the task's page reach it while it works, stopping the task
// interrupts its turn, and every tool it is not already allowed is asked of
// codeaf first ([policy]).
package claudecode

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/claudeagent"
	"github.com/Agent-Field/codeaf/internal/delegate"
)

// Name is the program's one word: the slash command, the shell verb and the
// manual page.
const Name = "claude"

// EnvBinary names the Claude Code program to start (internal/claudeagent).
const EnvBinary = claudeagent.EnvBinary

// stageWork is the one stage a run reports. Claude Code plans, edits and
// checks inside its own loop, so codeaf has no phases of its own to name.
const stageWork = "work"

// idleGrace is how long a finished turn waits for one more message from the
// person before the run ends. A message that arrives later starts a new task.
var idleGrace = 8 * time.Second

// guide is what the conversation's model reads about the program on every
// turn, so it is held to delegate.GuideMax.
const guide = "Hands the task to the person's own Claude Code, which works in the folder with its own tools on " +
	"their Claude plan and leaves its changes on the run's branch. Use it when they ask for Claude, or name " +
	"Claude Code, to do the work. Its brief is the whole task in plain words."

// unattended is the system prompt added to Claude Code's own.
const unattended = "You are running as a codeaf task. The person may send you messages while you work; " +
	"take them into account. Nobody answers questions mid-task: decide, do the work, check it, and end with a " +
	"short account of what you changed and how you checked it. Pushing, publishing and opening pull requests " +
	"are the person's to do; codeaf lands your work on this branch."

// Program is the definition codeaf carries (internal/delegate/builtin).
var Program = delegate.Delegate{
	Name:    Name,
	Summary: "hand a task to your own signed-in Claude Code",
	Guide:   guide,
	Lands:   delegate.LandsTree,
	Listens: true,
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

// Family answers Claude Code's model word for a model id (internal/claudeagent).
func Family(model string) string { return claudeagent.Family(model) }

func bindRun(fs *flag.FlagSet) delegate.Body {
	model := fs.String("claude-model", "", "Claude Code model: opus, sonnet, haiku or a full name")
	resume := fs.String("resume", "", "continue the Claude Code session with this id")
	return func(ctx context.Context, host delegate.Host, args []string) error {
		run(ctx, host, strings.TrimSpace(strings.Join(args, " ")), *model, *resume)
		return nil
	}
}

func run(ctx context.Context, host delegate.Host, brief, model, resume string) {
	host.Hello([]string{stageWork})
	if brief == "" {
		host.Terminal(delegate.Ending{Status: delegate.StatusFail, Message: "there was no brief to hand Claude Code"})
		return
	}
	binary, err := Binary()
	if err != nil {
		host.Terminal(delegate.Ending{Status: delegate.StatusFail, Message: err.Error()})
		return
	}
	ceilings := host.Ceilings()
	if ceilings.Hours > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(ceilings.Hours*float64(time.Hour)))
		defer cancel()
	}
	tr := newTranslator(host)
	session, err := claudeagent.Start(ctx, claudeagent.Options{
		Binary: binary, Dir: host.Workspace(), Model: model, Resume: resume,
		PermissionMode: "acceptEdits", AppendSystemPrompt: unattended, MaxBudgetUSD: ceilings.CostUSD,
		Disallowed: outwardRules,
		CanUseTool: func(_ context.Context, req claudeagent.PermissionRequest) claudeagent.Decision {
			decision := policy(req)
			if !decision.Allow {
				host.Step(delegate.StepRecord{Tool: req.ToolName, Step: stageWork, Command: "refused: " + describeInput(req.Input, req.ToolName), Observation: decision.Message})
			}
			return decision
		},
		OnEvent: tr.handle, Stderr: os.Stderr,
	})
	if err != nil {
		host.Terminal(delegate.Ending{Status: delegate.StatusFail, Message: err.Error()})
		return
	}
	if err := session.Send(brief); err != nil {
		_ = session.Close(5 * time.Second)
		host.Terminal(delegate.Ending{Status: delegate.StatusCrashed, Message: "Claude Code would not take the brief: " + err.Error()})
		return
	}
	listener, _ := host.(delegate.Listener)
	lastSend := time.Now()
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			// STOPPING A TASK INTERRUPTS ITS TURN FIRST, so what Claude Code was
			// doing ends at a tool boundary rather than mid-write, and only then
			// is the process told the conversation is over.
			stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = session.Interrupt(stopCtx)
			cancel()
			_ = session.Close(5 * time.Second)
			end := tr.ending(session.SessionID())
			end.Status, end.Message = delegate.StatusBudget, "Claude Code was stopped"
			if ctx.Err() == context.DeadlineExceeded {
				end.Message = "Claude Code was stopped at the time ceiling"
			}
			closeInbox(listener, "the task ended")
			host.Terminal(end)
			return
		case <-session.Done():
			closeInbox(listener, "Claude Code ended")
			end := tr.ending(session.SessionID())
			if !tr.seen() {
				end.Status, end.Message = delegate.StatusCrashed, "Claude Code ended without saying how it went"
			}
			host.Terminal(end)
			return
		case <-tick.C:
			if listener != nil {
				if messages := listener.Messages(); len(messages) > 0 {
					ids := make([]string, 0, len(messages))
					for _, m := range messages {
						if session.Send(m.Text) == nil {
							ids = append(ids, m.ID)
						}
					}
					listener.Heard(ids)
					lastSend = time.Now()
				}
			}
			// THE RUN IS OVER WHEN CLAUDE CODE HAS ANSWERED EVERYTHING IT WAS
			// SENT AND GONE QUIET. A message that arrives mid-turn is sometimes
			// folded into that turn, which then ends with ONE result for two
			// messages, and sometimes runs as a turn of its own after it, so
			// results are never counted against messages: the last thing heard
			// must be a result newer than the last message, and nothing may
			// have stirred since for the grace.
			if tr.settledSince(lastSend, idleGrace) {
				closeInbox(listener, "the task finished")
				_ = session.Close(10 * time.Second)
				host.Terminal(tr.ending(session.SessionID()))
				return
			}
		}
	}
}

func closeInbox(listener delegate.Listener, reason string) {
	if listener != nil {
		listener.CloseInbox(reason)
	}
}

// outward is a shell command that reaches past the run's branch: a push, a
// pull request, a release or a publish. Those are the person's to make.
var outward = regexp.MustCompile(`\bgit\s+push\b|\bgh\s+(pr\s+(create|merge)|release)\b|\b(npm|pnpm|yarn|cargo)\s+publish\b`)

// outwardRules are the same refusals as Claude Code deny rules. The person's
// own Claude Code hooks can answer "allow" before codeaf is ever asked (a hook
// that rewrites shell commands does), and a deny rule holds even then, so the
// boundary is drawn twice: here for the hooks, in [policy] for everything else.
var outwardRules = []string{
	"Bash(git push:*)", "Bash(rtk git push:*)", "Bash(gh pr create:*)", "Bash(gh pr merge:*)",
	"Bash(gh release:*)", "Bash(npm publish:*)", "Bash(pnpm publish:*)", "Bash(cargo publish:*)",
}

// policy answers Claude Code's permission questions for an unattended task.
//
// THE RUN'S BRANCH IS THE BOUNDARY. codeaf cut it for this run, so anything
// Claude Code does inside it is as reversible as senior-dev's own work, and is
// allowed; what would reach somebody else — a push, a pull request, a publish —
// is refused with a sentence Claude Code reads, so it ends its turn with the
// work on the branch instead of asking again.
func policy(req claudeagent.PermissionRequest) claudeagent.Decision {
	if req.ToolName == "Bash" && outward.MatchString(string(req.Input)) {
		return claudeagent.Decision{Message: "Pushing, publishing and opening pull requests are the person's to do; leave the work on this branch."}
	}
	return claudeagent.Decision{Allow: true}
}

// Binary finds the Claude Code program (internal/claudeagent).
func Binary() (string, error) { return claudeagent.Binary() }

// translator turns Claude Code's events into the task's stage, steps and
// ending.
type translator struct {
	host         delegate.Host
	mu           sync.Mutex
	pending      map[string]block
	subscription bool
	turns        int
	last         event
	gotResult    bool
	lastEvent    time.Time
	lastResult   time.Time
	listPrice    float64
}

func newTranslator(host delegate.Host) *translator {
	return &translator{host: host, pending: map[string]block{}}
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

func (t *translator) handle(raw claudeagent.Event) {
	var ev event
	if json.Unmarshal(raw.Raw, &ev) != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastEvent = time.Now()
	switch ev.Type {
	case "system":
		if ev.Subtype != "init" {
			return
		}
		// A SESSION SIGNED IN TO A CLAUDE PLAN SPENDS NO MONEY OF CODEAF'S
		// LEDGER. Claude Code reports what the run WOULD have cost at API
		// prices either way; on a plan that figure is not a charge, so it is
		// kept as a fact and never booked as spend.
		t.subscription = ev.APIKeySource == "" || ev.APIKeySource == "none"
		data, _ := json.Marshal(map[string]any{"models": []string{ev.Model}, "doing": "working"})
		t.host.Stage(delegate.StageRecord{Stage: stageWork, Status: "running", Data: data})
	case "assistant":
		for _, b := range blocks(ev.Message.Content) {
			if b.Type == "tool_use" {
				t.pending[b.ID] = b
			}
		}
	case "user":
		for _, b := range blocks(ev.Message.Content) {
			if b.Type != "tool_result" {
				continue
			}
			call, ok := t.pending[b.ToolUseID]
			if !ok {
				continue
			}
			delete(t.pending, b.ToolUseID)
			step := delegate.StepRecord{Tool: call.Name, Step: stageWork, Command: describeInput(call.Input, call.Name), Observation: resultText(b.Content)}
			if call.Name == "Bash" {
				exit := 0
				if b.IsError {
					exit = 1
				}
				step.Exit = &exit
			}
			t.host.Step(step)
		}
	case "result":
		t.turns++
		t.gotResult = true
		t.last = ev
		t.lastResult = t.lastEvent
		// Claude Code's figure is the session's running total, not the turn's.
		t.listPrice = ev.TotalCostUSD
	}
}

// settledSince says the last thing Claude Code printed was a result, that the
// result came after the message sent at sent, and that grace has passed since.
func (t *translator) settledSince(sent time.Time, grace time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.lastResult.IsZero() && t.lastResult.After(sent) && !t.lastEvent.After(t.lastResult) && time.Since(t.lastResult) >= grace
}

func (t *translator) seen() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.gotResult
}

// ending is what the last result said, with the session id a later run resumes
// by.
func (t *translator) ending(session string) delegate.Ending {
	t.mu.Lock()
	defer t.mu.Unlock()
	ev := t.last
	end := delegate.Ending{Deliverable: strings.TrimSpace(ev.Result)}
	end.Claim = end.Deliverable
	end.Extra = map[string]any{"claude_session": session, "claude_turns": t.turns, "claude_list_price_usd": t.listPrice}
	if !t.subscription {
		end.CostUSD = t.listPrice
	}
	switch {
	case ev.Subtype == "error_max_budget_usd":
		end.Status, end.Message = delegate.StatusBudget, "Claude Code stopped at the cost ceiling"
	case ev.IsError || ev.Subtype != "success":
		end.Status, end.Message = delegate.StatusFail, "Claude Code ended with "+strings.ReplaceAll(ev.Subtype, "_", " ")
	default:
		end.Status, end.Message = delegate.StatusPass, "Claude Code finished"
	}
	return end
}

func blocks(raw json.RawMessage) []block {
	var out []block
	_ = json.Unmarshal(raw, &out)
	return out
}

// describeInput is the one line a step shows for a tool call: the command for
// a shell call, the file for a file tool, the pattern for a search.
func describeInput(input json.RawMessage, tool string) string {
	var in map[string]any
	_ = json.Unmarshal(input, &in)
	for _, key := range []string{"command", "file_path", "pattern", "path", "url"} {
		if value, ok := in[key].(string); ok && value != "" {
			return value
		}
	}
	return tool
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
