package head

import (
	"context"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The control loop is the last reading before the ordinary router, and it runs
// only over sentences the deterministic recognizers declined. That order is
// load-bearing: everything surgery, class, standing and redirection already
// claim keeps its free, instant path, and nothing they handle ever reaches a
// model call here. What is left is the long tail — "kill everything except the
// finance one", "hold the scans until the research lands" — where the intent is
// obvious to a person and unreachable by any cue list. Those get the toolbelt.

const controlSystemPrompt = `You are the front desk of a task-graph agent, and this message is about work that is already underway. The board below is your only reality: every row is one job the user asked for, live, with what it is doing and what it has cost. You have no other staff, no hidden status system, and no memory of ids that are not on a board you have read.

The tools are your only hands.
- board reads the live work. Reads are always safe and always allowed. Read before you act whenever the target set is not already plain from the board you were given.
- control cancels, pauses, resumes, restarts, or reprioritizes the ids you name.
- steer tells the workers running a job something right now, without changing what the job is.
- revise hands the user's own words to a job so its remaining plan is edited to match them. Use it when what the work is FOR has changed.
- expedite makes a job arrive sooner. It never queues anything new.

Law you do not get to bend:
- Never invent an id. Every id you pass came from a board row you have seen in this conversation.
- A question about state — what is running, how far along, what it cost — is answered from a board read and nothing else. Reading is not acting, and a status question earns no verb.
- needs_confirmation is the consent gate working, not a failure. Nothing changed, the user is being asked, and their answer settles it. Never say the change happened.
- A tool error is information. A wrong id or a verb the status does not allow tells you exactly what to fix; fix it and try once more.
- Say only what the tool results showed you. Counts, the names of the work, and "I've asked you to confirm" are the whole vocabulary of a receipt. Never promise a result no tool reported, never imply work has finished, and never say you will hurry something unless expedite said so.
- If this turns out not to be about the work on the board — a new request, a question about the world, ordinary conversation — call no tools and reply with exactly NOT_EXISTING_WORK.

When you are done, stop calling tools and write one or two plain sentences for the user: what happened, in their terms. No markdown, no ids, no machinery — the words node, board, tool and snapshot belong backstage.`

const (
	// controlToolCallCap is how many tools one message may spend. Four is read,
	// act, read back, and one spare for a call the model has to correct after a
	// tool error — every honest shape of this conversation fits, and a model
	// looping over the board cannot turn one sentence into an open tab.
	controlToolCallCap = 4
	// controlLoopMaxTokens matches the router's ceiling. The loop's output is a
	// tool call or two sentences; anything longer is a monologue.
	controlLoopMaxTokens = 600
	// controlNotWorkSentinel is how the model declines. Its false positives are
	// meant to be cheap, so saying "this was not about the board" has to be one
	// unambiguous token rather than a sentence someone has to parse.
	controlNotWorkSentinel = "NOT_EXISTING_WORK"
)

// controlVerbs are the words that make a sentence plausibly about existing
// work. This list is deliberately not a grammar and never grows into one: it is
// a cheap trigger, not a reading. A false positive costs one model call that
// returns the sentinel; a false negative costs nothing but today's behaviour.
var controlVerbs = map[string]bool{
	"cancel": true, "cancelled": true, "stop": true, "stopped": true, "halt": true,
	"kill": true, "abort": true, "abandon": true, "scrap": true, "ditch": true,
	"pause": true, "paused": true, "hold": true, "freeze": true, "suspend": true,
	"resume": true, "unpause": true, "unfreeze": true, "continue": true,
	"restart": true, "retry": true, "rerun": true, "redo": true, "again": true,
	"skip": true, "drop": true, "keep": true, "leave": true, "except": true,
	"finish": true, "complete": true, "wrap": true, "hurry": true, "rush": true,
	"wait": true, "until": true, "first": true, "prioritize": true,
	"reprioritize": true, "deprioritize": true, "cheaper": true, "faster": true,
}

// manageControl is the graph control loop. It runs after every deterministic
// recognizer has declined, only over messages that are plausibly about existing
// work, and it never becomes a dead end: anything it cannot honestly settle
// falls through to the ordinary router exactly as before.
func (h *Head) manageControl(ctx context.Context, user store.Message) (bool, error) {
	applies, err := h.controlLoopApplies(user.Body)
	if err != nil || !applies {
		return false, err
	}
	client, err := h.clientFor(user)
	if err != nil {
		return false, nil
	}
	rows, err := h.boardRows("", "", "")
	if err != nil || len(rows) == 0 {
		return false, nil
	}

	run := &beltRun{head: h, user: user}
	messages := []ai.Message{
		textMessage("system", controlSystemPrompt),
		textMessage("user", "Board (the user's live work):\n"+renderBoard(rows)+
			"\n\nCurrent user message (verbatim):\n"+strings.TrimSpace(user.Body)),
	}
	definitions := beltDefinitions()
	final := ""
	spent := 0
	// One turn per tool call the belt allows, one to be told the belt is spent,
	// and one to speak. A model that will not stop calling tools still ends in
	// at most this many provider calls.
	for turn := 0; turn < controlToolCallCap+2; turn++ {
		response, err := client.CompleteWithMessages(ctx, messages,
			ai.WithTools(definitions), ai.WithMaxTokens(controlLoopMaxTokens))
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			break
		}
		if response == nil {
			break
		}
		calls := response.ToolCalls()
		if len(calls) == 0 {
			final = strings.TrimSpace(response.Text())
			break
		}
		messages = append(messages, ai.Message{
			Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: response.Text()}},
			ToolCalls: calls,
		})
		for _, call := range calls {
			body := "the tool belt is spent for this message — say what happened, in one or two sentences"
			if spent < controlToolCallCap {
				spent++
				result, failed := run.execute(call.Function.Name, call.Function.Arguments)
				if failed {
					result = "ERROR: " + result
				}
				body = result
			}
			messages = append(messages, ai.Message{
				Role: "tool", ToolCallID: call.ID,
				Content: []ai.ContentPart{{Type: "text", Text: body}},
			})
		}
	}

	// The gate's question is the reply. Posting the model's prose beside it
	// would put two voices on one decision, and only one of them knows what the
	// consent actually covers.
	if run.confirm != nil {
		return true, h.askBeltConfirm(user, run.confirm)
	}
	if run.acted {
		if final == "" || strings.Contains(final, controlNotWorkSentinel) {
			final = run.summary()
		}
		return true, h.postAgent(user.SessionID, final, run.commandSeq)
	}
	// A loop that read nothing has no grounding — the board is its only reality,
	// so text produced without touching it is not an answer about the work. That
	// and the sentinel are the same case: hand the message back to the router.
	if spent == 0 || final == "" || strings.Contains(final, controlNotWorkSentinel) {
		return false, nil
	}
	return true, h.postAgent(user.SessionID, final, 0)
}

// controlLoopApplies is the trigger, and it is meant to be broad and cheap. It
// asks two questions only: is there live work of the user's at all, and does
// this sentence plausibly point at it — by carrying a control verb anywhere, by
// pointing deictically, or by scoring against a live job's own words at
// redirection's anchor floor. Everything finer is the model's job, behind the
// tools, where a misreading costs a question rather than an action.
func (h *Head) controlLoopApplies(message string) (bool, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return false, nil
	}
	active, err := h.activeUserJobs()
	if err != nil || len(active) == 0 {
		return false, err
	}
	if controlVerbPresent(message) || refersToLiveWork(message, len(active)) {
		return true, nil
	}
	ranked, err := h.rankRedirectTargets(message, active)
	if err != nil {
		return false, err
	}
	for _, target := range ranked {
		if target.Score >= RedirectAnchorScore {
			return true, nil
		}
	}
	return false, nil
}

func controlVerbPresent(message string) bool {
	for _, word := range surgeryWords(strings.ToLower(message)) {
		if controlVerbs[word] {
			return true
		}
	}
	return false
}
