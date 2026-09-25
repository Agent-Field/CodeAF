package session

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// The experiment belt: one honest shell and the few hands that cannot be a
// shell command (docs/design/bash-task-loop/DESIGN.md, Decision 1).
//
// THE EXPERIMENT IS A SECOND BELT, NOT AN ENGINE CHANGE. A task worker's belt
// is composed by the same [Agent.belt] as the conversation's, differentiated by
// predicates on its Config; this file adds one more composition, reached
// through one more predicate ([Config.mayBashBelt]). The six pi tools — read,
// edit, write, grep, find, ls — come OFF, because each of them is a shell
// command wearing a schema: the branch worker spells what it did in bash, and
// the doctrine page it reads ([bashworkerPage]) teaches the idioms in their
// place. What stays is exactly what a shell cannot be: read_document (the
// billed document parse), jobs (the other end of bash's background argument,
// Decision 6), manual (the packed corpus), the task graph's verbs on their
// existing gates, the memory and state families, the web pair, the services
// pair, and the media family riding the shelf exactly as today. `ask` is NOT
// among them: the loop reaches the person through the plan CLI, not a consent
// gate, so the belt carries no verb for a question it already answers by
// re-planning ([Config.mayAsk] says the same where the page is composed).
//
// internal/exec/bare IS UNTOUCHED. The hands stay bare's; the experiment is a
// wire change — which tools the model can name — and the conversation belt and
// every subharness leaf keep pi's tools byte for byte. Turn CODEAF_TASK_BELT
// off, and not one byte of a worker is where it was on the older road:
// [Agent.belt] delegates only when the predicate holds, which is what lets both
// roads run from one binary.

// bashBeltSourceCaps is the cap the branch bash hand is BUILT with. bare cuts
// a bash result tail-only inside its own accumulator, from the caps the tool
// was composed with, and a cap that wide never binds — which is the point: the
// full output has to reach [Agent.cutBashResult] for the head to exist, so the
// tail-only cut must not fire first. THE WIDE CAP IS NOT THE BRANCH BOUND and
// must never reach the wire: the hand's description is rewritten below to name
// the real cut, and [Agent.cutBashResult] applies the window's own pair.
//
// THE COST OF SEEING THE WHOLE OUTPUT IS MEMORY, and it is bounded the same
// way pi's is — by how long the command runs, not by a buffer — because the
// accumulator that holds the bytes is bare's and the ceiling that bounds the
// run is bare's ([BashCeilingSeconds]). A command that pours output for ten
// minutes pours it here; the branch's answer to that is the cut at the end,
// not a smaller window in the middle.
func bashBeltSourceCaps() bare.Caps {
	return bare.Caps{MaxLines: math.MaxInt32, MaxBytes: math.MaxInt32}
}

// bashBelt composes the experiment belt: the one bash tool and the kept hands,
// in the order [Agent.belt] composes today's node belt so a worker sees the
// same list minus what came off.
func (a *Agent) bashBelt() []bare.Tool {
	tools := []bare.Tool{a.branchBash()}
	tools = append(tools, a.documentTool(), a.jobsTool(), a.manualTool())
	// ASK IS OFF THIS BELT. A bash-belt worker coordinates through the plan CLI
	// and reports through `plandb done`; there is no consent gate to reach the
	// person with, and the belt is not the place for a question the loop already
	// answers by re-planning. [Config.mayAsk] says the same thing to the page, so
	// the belt and the prompt cannot disagree about whether this verb is here.
	// The kept families ride the SAME GATES as today's belt, named at their own
	// sources: a gate that moves there moves here, because these are the same
	// calls. The pi block above them is the one thing this composition leaves
	// out, and [Agent.branchBash] is what replaces it.
	if a.config.mayWatch() {
		tools = append(tools, a.watchTool())
	}
	// THE GRAPH VERBS ARE OFF THIS BELT, and their absence IS the experiment:
	// the worker's coordination goes through the plan CLI (`plandb add`,
	// `plandb split`, the page teaches the rest), and a belt that carried both
	// would teach the model two ways to say one thing. The families are
	// propose_task and the tasks window, quick_task with its items door,
	// and divide_work. revise_assignment stays: it is the person's
	// redirection lane, not coordination, and on a plan-born node it writes
	// through to the store task so the store stays the record
	// (plandb_plan.go's [TaskGraph.planReviseThrough]).
	tools = append(tools, a.assignmentTools()...)
	tools = append(tools, a.standingTools()...)
	tools = append(tools, a.harnessTools()...)
	tools = append(tools, a.subharnessTools()...)
	tools = append(tools, a.memoryTools()...)
	tools = append(tools, a.anchorWorkspaceTools()...)
	tools = append(tools, a.workspaceTools()...)
	tools = append(tools, a.conversationTools()...)
	tools = append(tools, a.stateTools()...)
	tools = append(tools, a.searchTools()...)
	tools = append(tools, a.settingsTools()...)
	tools = append(tools, a.connectTools()...)
	// The media family travels whole, exactly as [Agent.belt] appends it: one
	// family, five verbs, shelved or carried by the same machinery as every
	// other belt's — a model with no media models still has no media group.
	tools = append(tools, a.imageTools()...)
	tools = append(tools, a.speakTools()...)
	tools = append(tools, a.musicTools()...)
	tools = append(tools, a.videoTools()...)
	tools = append(tools, a.viewTools()...)
	tools = append(tools, a.videoEditTools()...)
	return a.shelveDeferred(tools)
}

// branchBash is the experiment's one shell tool: bare's bash hand, composed at
// [bashBeltSourceCaps] so its own tail-only cut never binds, wrapped with the
// branch's head+tail+path cut, and then with the session's background argument
// and promotion door (Decision 6: background and jobs stay, because a worker
// nobody watches must not hold its node on a clock).
func (a *Agent) branchBash() bare.Tool {
	hand := bare.Tool{}
	for _, tool := range bare.AllToolsCapped(a.config.Workspace, bashBeltSourceCaps()) {
		if tool.Name == "bash" {
			hand = tool
			break
		}
	}
	// THE DESCRIPTION IS THE BRANCH'S OWN, because the cut it names is: the
	// model reads a description that promises the last N lines and plans
	// around a tool that does something else. The schema is bare's verbatim —
	// the schema is pi's, and pi's bytes are the ones the experiment measures
	// against — and [Agent.backgroundBash] adds its one boolean on top.
	hand.Description = branchBashDescription(a.resultCaps())
	return a.backgroundBash(a.truncatingBash(hand))
}

// truncatingBash is bare's bash hand with the branch cut fitted where its own
// accumulator's cut was: the full output reaches this wrapper, and what the
// model reads is the head, the marker naming the file the whole output is in,
// and the tail. A transport error belongs to bare — there is nothing here to
// cut.
func (a *Agent) truncatingBash(inner bare.Tool) bare.Tool {
	return bare.Tool{
		Name:        inner.Name,
		Description: inner.Description,
		Schema:      inner.Schema,
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			text, isError, err := inner.Execute(ctx, a.planCommandArgs(args))
			if err != nil {
				return text, isError, err
			}
			// THE PLAN'S FIRST PULSE POINT: a bash-belt worker's bash call may
			// have just run `plandb add` or `plandb split`, and every ready
			// task that created is a worker waiting to start. The pulse is
			// nil-safe for every agent outside the experiment (plandb_plan.go).
			if a.config.taskID != 0 {
				if g := a.graph(); g != nil {
					g.planPulse()
				}
			}
			return a.cutBashResult(text), isError, nil
		},
	}
}

// planCommandArgs returns a bash call's arguments with the plan shim's
// directory first on the PATH the COMMAND sees and the run's store bound beside
// it, or the arguments unchanged when this worker has no armed shim to reach.
// bare's hand takes no environment of its own — the schema carries a command and
// a timeout and nothing else — so the binding travels as an `export` prefix on
// the command string, the one spelling that reaches the whole command line in
// the one shell process this call runs while leaving the process environment
// (plandb_plan.go's [planBashPrefix]) alone.
//
// THE GATE IS THE WORKER'S OWN BELT, not the run's switch: the graph is the
// conversation's, shared with every node it admits, so a plan armed for a
// bash-belt worker is visible from the conversation's own bash hand — and a
// conversation that never asked for the experiment must not have its PATH
// rewritten by one. [Config.mayBashBelt] is the belt fact this file's belt was
// composed by, and it is the gate here for the same reason.
func (a *Agent) planCommandArgs(args json.RawMessage) json.RawMessage {
	var parsed struct {
		Command string `json:"command"`
	}
	if err := decodeToolArguments(args, &parsed); err != nil || strings.TrimSpace(parsed.Command) == "" {
		return args
	}
	prefixed := a.planCommand(parsed.Command)
	if prefixed == parsed.Command {
		return args
	}
	if out, ok := withBashCommand(args, prefixed); ok {
		return out
	}
	return args
}

// planCommand prefixes one bash command string with the plan shim's PATH and
// store binding, or answers it unchanged when there is no prefix to carry. The
// background road ([Agent.backgroundBash]) starts its job outside the inner
// tool and reaches the same helper here, so both roads a bash-belt worker's
// command can take agree about where `plandb` resolves and which store it
// reads.
func (a *Agent) planCommand(command string) string {
	if strings.TrimSpace(command) == "" || !a.config.mayBashBelt() {
		return command
	}
	g := a.graph()
	if g == nil {
		return command
	}
	prefix := g.planBashPrefix()
	if prefix == "" {
		return command
	}
	return prefix + command
}

// withBashCommand replaces one bash call's command and keeps every other
// argument byte for byte — the background flag and the timeout are the
// model's words and this rewrite has no opinion about them. An argument
// object that will not round-trip comes back unchanged.
func withBashCommand(args json.RawMessage, command string) (json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	// The read goes through the one decoder every tool's arguments go through
	// (toolargs.go), so a call this rewrite cannot read is the same call the
	// envelope refuses, in the same words.
	if err := decodeToolArguments(args, &fields); err != nil {
		return args, false
	}
	encoded, err := json.Marshal(command)
	if err != nil {
		return args, false
	}
	fields["command"] = encoded
	out, err := json.Marshal(fields)
	if err != nil {
		return args, false
	}
	return out, true
}

// branchBashDescription renders the branch bash hand's description from the
// cut it actually applies. The cap it quotes is the cap in force for THIS
// agent's window, in the one place the model learns it: a description quoting
// a limit the hand does not apply is a description the model plans wrongly
// around, which is the same law bare's own descriptions are built on.
func branchBashDescription(caps bare.Caps) string {
	kb := (caps.MaxBytes + 1023) / 1024
	return fmt.Sprintf("Execute a bash command in the current working directory. Returns stdout and stderr. Output over %dKB is cut to its first half and its last half, and the whole output is filed as a file the result names. Optionally provide a timeout in seconds.", kb)
}

// beltOffWords are the spellings of CODEAF_TASK_BELT that send a task back to
// the node belt. They are the ONLY way off the bash belt, and the list is
// deliberately short and closed: an unrecognised word leaves a person on the
// belt they were promised rather than quietly moving them off it, because a
// typo in an environment variable must not be able to change which engine runs
// the work. The empty string is not on the list — an exported-but-empty
// variable is the same as an unset one, which is the default, which is bash.
var beltOffWords = map[string]bool{"node": true, "legacy": true, "off": true}

// bashBeltAsked reads THE BELT SWITCH, and it is the ONE reader of
// CODEAF_TASK_BELT in this package: the belt a task worker is built on
// (newTaskAgentOn) and the landing that judges it (workTaskNode's gate) are
// two halves of one fact, and two readers could disagree about which belt a
// node is on. Unset — every machine that has asked for nothing — it is TRUE:
// the bash belt is the belt a task runs on. The variable was the way IN to an
// experiment and is now the way OUT of the default, so the sense of every
// reader below is unchanged while the answer they get when nobody has spoken
// is the opposite of what it was.
func bashBeltAsked() bool {
	return !beltOffWords[strings.ToLower(strings.TrimSpace(env.Get("CODEAF_TASK_BELT")))]
}

// BashBeltAsked is [bashBeltAsked] as a door outside this package reads it: a
// headless errand that dispatches through the run engine needs the same switch
// the belt and every landing read, and a second reader of CODEAF_TASK_BELT
// outside this package would be a switch two doors could disagree about. It
// goes through [internal/env], the one door onto the environment, so the
// compatibility spelling of the variable and the export seam both keep
// working.
func BashBeltAsked() bool { return bashBeltAsked() }
