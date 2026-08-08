package head

import (
	"context"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/aforge-v2/internal/resident"
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

const controlSystemPrompt = `You are the front desk of a task-graph agent, and this message is about work that is already underway, about aforge itself, or both. The board below is your only reality about the work: every row is one job the user asked for, live, with what it is doing and what it has cost. You have no other staff, no hidden status system, and no memory of ids that are not on a board you have read.

The tools are your only hands.
- board reads the live work. Reads are always safe and always allowed. Read before you act whenever the target set is not already plain from the board you were given.
- control cancels, pauses, resumes, restarts, or reprioritizes the ids you name.
- steer tells the workers running a job something right now, without changing what the job is.
- revise hands the user's own words to a job so its remaining plan is edited to match them. Use it when what the work is FOR has changed.
- expedite makes a job arrive sooner. It never queues anything new.
- manual reads aforge's own account of itself.
- result reads what one job actually produced: its findings in full, the files it wrote, and what each of its parts concluded.
- read opens one of those files and gives you what is inside it.
- competence reads the measured account of your own strengths, weak spots and learning frontier.
- standing reads what you are keeping watch over: the checks, the last wake, the next one, and every standing charter with what it watches for.
- spending reads what has been spent — today against the daily rail, and separately what your own upkeep cost and what it bought.
- history reads what was finished inside a window of time, newest first. It is the only read that answers "when", and the only one that still finds work old enough to have been packed away.
- note writes one durable thing the user has told you into the notebook, where later conversations will find it.

Alongside the board you carry that notebook: durable preferences, corrections and lessons kept across every conversation. It is what you have been told before, and it shapes how you answer here — not only what the workforce is asked to do.

Law you do not get to bend:
- Never invent an id. Every id you pass came from a board row you have seen in this conversation.
- A question about state — what is running, how far along, what it cost — is answered from a board read and nothing else. Reading is not acting, and a status question earns no verb.
- When the user asks what work found, produced, concluded or decided, read result on that job before you answer. The board says how a job ended; only result says what it came back with, and "it completed" is not an answer to what it found.
- A result that says where the answer is has not given you the answer. When what a job recorded is thin and names a file, read that file and answer from what is in it. Anything you can fetch in this turn you fetch in this turn: never offer to go and look, never say you could pull something out if they want it, never end on an offer instead of an answer.
- The user telling you how they want you to behave from now on is durable, exactly as a preference about the work is. Note it, then say it is noted. Never promise a lasting change you have not written down and never claim a capability you are not using: "from now on" with nothing behind it is a promise that dies with this conversation, and the next one repeats the same mistake.
- A question about aforge itself — what you can do, how one of your mechanisms works, why you behaved the way you did — is answered by reading the manual and quoting its substance in your own plain words. Never invent an answer about your own machinery, never soften or embellish what the manual says, and if the manual does not cover it, say plainly that you do not know rather than guessing.
- A question about WHEN — what you did yesterday, this week, how long ago something landed — is answered by reading history over a window you work out from the current time given to you below. The board is what is happening; history is what happened, and a job old enough to have been tidied away is reachable through nothing else.
- A question about YOU rather than about aforge — how your work has been going, what you are good at, whether you are improving, whether the user is asking too much of you, what you are watching for them, what any of it has cost — is answered by reading competence, standing or spending first. These are measurements, not impressions: state what they show, never a strength, a weakness, a schedule or a figure they did not, and when one of them is thin say that it is thin.
- A row ending in "elsewhere" is the user's own work, started in another window of theirs. You may read it and change it exactly as you may any other row; say which window it came from rather than answering as though this conversation started it, because the receipt for a change lands where the job began.
- Work the user raises while something is running, or moments after that job reported in the thread, is a change to that work before it is a second job. Read the board and revise or steer the job it concerns; that a sentence borrows none of the job's words means nothing, because people answer the thing just said to them without naming it. Only when the ask is genuinely about something else is it new work, and then it is not yours to queue.
- needs_confirmation is the consent gate working, not a failure. Nothing changed, the user is being asked, and their answer settles it. Never say the change happened.
- A tool error is information. A wrong id or a verb the status does not allow tells you exactly what to fix; fix it and try once more.
- Say only what the tool results showed you. Counts, the names of the work, and "I've asked you to confirm" are the whole vocabulary of a receipt. Never promise a result no tool reported, never imply work has finished, and never say you will hurry something unless expedite said so.
- If this turns out to be neither about the work on the board nor about aforge itself — a new request, a question about the world, ordinary conversation — call no tools and reply with exactly NOT_EXISTING_WORK.

When you are done, stop calling tools and write plainly for the user, in their terms. A receipt for a change is one or two sentences. An answer from the manual may run a short paragraph, and should give them the concrete numbers and phrasings the manual gives you. No ids, no machinery — the words node, board, tool and snapshot belong backstage.

The first sentence is the answer itself: the finding, the number, the verdict, the count that changed. Never a preamble, never the question said back, never a promise to go and look. When work has settled, say what it concluded and name the files it wrote; how it ended is a trailing clause, and "it completed" on its own is never an answer. Give an answer structure only when the answer has genuinely separate parts, and then as a few short markdown bullets — a greeting, an acknowledgement or a one-line answer takes no formatting at all and stays ordinary conversation. Never a wall of text: cut every sentence that would not change what the user does next.`

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
	work, err := h.controlLoopApplies(user)
	if err != nil {
		return false, err
	}
	// The two arms are independent. Work needs live jobs to be about; a
	// self-question needs nothing at all, because the manual is in the binary.
	self := selfQuestionCued(user.Body)
	if !work && !self {
		return false, nil
	}
	client, err := h.clientFor(user)
	if err != nil {
		return false, nil
	}
	rows, err := h.boardRows(user.SessionID, "", "", "")
	if err != nil {
		return false, nil
	}
	// An empty board no longer closes the loop, because the loop's most valuable
	// reads are about work that is over. What decides is whether anything at all
	// is addressable — the model can find a settled job with one aimed board read
	// even when nothing is moving.
	if !self && len(rows) == 0 && !work {
		return false, nil
	}

	board := "(nothing of the user's is live right now)"
	if len(rows) > 0 {
		board = renderBoard(rows)
	}
	run := &beltRun{head: h, user: user}
	// The notebook rides after the board and under its own byte budget, the
	// same one the router reads it with. Order is the whole safeguard: the
	// board is this loop's floor and is written first, so memory can crowd out
	// nothing, and a loop that could act on the graph while being blind to what
	// the user had already told it was the other half of the same failure.
	// The loop speaks to the user exactly as the router does, so it carries
	// the same learned voice — an arm that can cancel work but never heard
	// "stop opening with a preamble" was the one surface still talking past
	// the notebook.
	messages := []ai.Message{
		textMessage("system", resident.VoicePrompt(h.store, controlSystemPrompt)),
		textMessage("user", "Board (the user's live work):\n"+board+
			// No thread to dedup against: this loop carries the board and the
			// notebook and not the conversation, so every belief it is shown is
			// the only copy of itself in the prompt.
			"\n\nNotebook (durable memory across jobs and conversations):\n"+renderNotebook(h.store, user.Body, "")+
			"\n\nManual pages available: "+strings.Join(manual.Pages(), ", ")+
			// The clock, for the same reason the router carries one: the history
			// read takes a window, and a window has to be measured from
			// somewhere. It sits last because it moves fastest.
			"\n\n"+nowLine(time.Now())+
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
// asks whether this sentence plausibly points at work the belt can reach — by
// carrying a control verb anywhere while something is live, by pointing
// deictically, by scoring against a job's own words at redirection's anchor
// floor, or by arriving right after that job spoke. Everything finer is the
// model's job, behind the tools, where a misreading costs a question rather
// than an action.
//
// Adjacency alone opens the loop, and that is the point. The sentence that cost
// a running job a racing duplicate — "make sure you review the changes" typed
// seconds after that job posted its progress — carries no verb, no pronoun and
// no shared word, so every lexical arm declined it while a person reading the
// thread would not have hesitated for a moment.
//
// Live work is no longer the precondition for the whole trigger, and that was
// the bug. The belt's result and read tools exist for settled work — the belt's
// own comment says so, "settled work is precisely what has findings" — and they
// lived behind a gate that asked whether anything was still running. Overnight
// jobs land, the terminal is quiet, "what did the market analysis conclude?"
// found no live jobs, the loop declined, and the router answered a question
// about substance from a 1200-byte slice of a summary while the reader that
// would have opened the job in full sat one arm away. So the relevance arm now
// ranks against every addressable job rather than only live ones: the same
// search, the same floor, no status filter — which is exactly the read the deep
// slice already performs on this sentence, and exactly no new vocabulary.
func (h *Head) controlLoopApplies(user store.Message) (bool, error) {
	message := strings.TrimSpace(user.Body)
	if message == "" {
		return false, nil
	}
	active, err := h.activeUserJobs()
	if err != nil {
		return false, err
	}
	if len(active) > 0 {
		if controlVerbPresent(message) || refersToLiveWork(message, len(active)) {
			return true, nil
		}
	}
	anchored, err := h.anchorsOnAnyJob(message, active)
	if err != nil || anchored {
		return anchored, err
	}
	if len(active) == 0 {
		return false, nil
	}
	_, adjoins, err := h.adjacencyTarget(user, active)
	if err != nil {
		return false, err
	}
	return adjoins, nil
}

// anchorsOnAnyJob is the relevance arm, over live work and settled work alike.
// The live half keeps its narrower ranking — a redirect is only ever aimed at
// something still moving — and the settled half is the whole point of the read
// tools, so both are asked and either one opens the loop.
func (h *Head) anchorsOnAnyJob(message string, active []store.SurgeryTarget) (bool, error) {
	if len(active) > 0 {
		ranked, err := h.rankRedirectTargets(message, active)
		if err != nil {
			return false, err
		}
		for _, target := range ranked {
			if target.Score >= RedirectAnchorScore {
				return true, nil
			}
		}
	}
	reference := redirectReference(message)
	if reference == "" {
		return false, nil
	}
	targets, err := h.store.SearchSurgeryTargets(reference, false)
	if err != nil {
		return false, err
	}
	for _, target := range targets {
		if target.Score >= RedirectAnchorScore && beltAddressable(target.Node) &&
			target.Node.ID != store.RootID {
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

// The second arm. Everything above is about work; this is about aforge. A
// person learning what their employee can do asks in the same register they ask
// for work in — "can you look at images?", "what happens overnight?" — and the
// only reliable difference is that one points at aforge and the other points at
// a deliverable. So the trigger reads shape rather than topic: a question, aimed
// at aforge or at something the manual is titled after, and not carrying a verb
// that means "go do this". A false negative costs nothing but today's routing,
// which is why every clause here is a reason NOT to open.

// selfQuestionPhrases are self-questions said in full, in the idiom
// asksForCompetence and asksForStandingWatch already use. They skip the shape
// test because they are already unambiguous.
var selfQuestionPhrases = []string{
	"what can you do", "what do you do", "what are you", "who are you",
	"how do you work", "how does aforge work", "what is aforge",
	"what happens when i'm gone", "what happens when i am gone",
	"while i'm gone", "while i am gone", "what happens overnight",
	"what happens every day", "what do you do all day",
	"explain yourself", "tell me about yourself",
}

// selfQuestionLeads mark a sentence as a question even without a question mark,
// which is how most people type one.
var selfQuestionLeads = map[string]bool{
	"how": true, "what": true, "whats": true, "why": true, "when": true,
	"where": true, "which": true, "who": true, "can": true, "could": true,
	"does": true, "do": true, "did": true, "is": true, "are": true,
	"explain": true, "tell": true,
}

// selfReferenceWords are the ways a person names their employee.
var selfReferenceWords = map[string]bool{
	"you": true, "your": true, "yours": true, "yourself": true, "aforge": true,
}

// selfQuestionVetoes are the verbs that mean the sentence is an assignment,
// however question-shaped it is. "can you build me a parser" is work, and work
// takes the ordinary route.
var selfQuestionVetoes = map[string]bool{
	"build": true, "write": true, "create": true, "draft": true, "fix": true,
	"implement": true, "add": true, "generate": true, "summarize": true,
	"summarise": true, "research": true, "find": true, "search": true,
	"download": true, "install": true, "send": true, "email": true,
	"deploy": true, "refactor": true, "translate": true, "buy": true,
	"publish": true, "compile": true, "analyze": true, "analyse": true,
	"review": true, "check": true, "look": true, "read": true, "scrape": true,
}

// selfQuestionPhrased is the unambiguous half, and the only half any earlier
// recognizer is allowed to consult. "what happens every day while I'm gone"
// carries durable-sounding words without being an instruction, and standing
// intent has to decline it before the loop ever gets a turn.
func selfQuestionPhrased(lower string) bool {
	for _, phrase := range selfQuestionPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// selfQuestionCued is the trigger. It opens the loop with the manual in reach
// even when nothing at all is live.
func selfQuestionCued(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	if selfQuestionPhrased(lower) {
		return true
	}
	words := surgeryWords(lower)
	shaped := strings.Contains(lower, "?")
	referenced := false
	for _, word := range words {
		if selfQuestionVetoes[word] {
			return false
		}
		if selfQuestionLeads[word] {
			shaped = true
		}
		if selfReferenceWords[word] {
			referenced = true
		}
	}
	if !shaped {
		return false
	}
	return referenced || manual.Cued(lower)
}
