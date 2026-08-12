package head

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The one tool-loop head.
//
// What used to be here was three things pretending to be one: ten deterministic
// recognizers that answered terminally, a tool loop reachable only behind a
// keyword trigger, and a router that could emit exactly one command as the last
// act of a turn it could not revisit. The head could not read the board, decide,
// spawn and report in one breath; it could not spawn at all except as a router's
// terminal decision; and the prompt above all of it said "it never plans or
// executes work" while the belt below cancelled, steered and revised.
//
// This is the replacement, in one wave rather than by absorption, because two
// brains coexisting mid-flight is the disease rather than a step away from it
// (Part 6 decision 5). One prompt. One belt. One board renderer. One thread
// renderer. The recognizers survive as pre-answers in the prompt (hints.go) and
// as the bodies of the tools that act; nothing they used to decide silently is
// decided silently now.

const (
	// orchestratorToolCallCap is how many tools one message may spend.
	//
	// The control loop allowed four, which was right for a loop that could only
	// read and steer: read, act, read back, one spare for a tool error. This loop
	// also commissions work, writes artifacts and repairs broken deliverables, and
	// the honest shape of a repair is read, write, read back, say — with room for
	// two corrections after a tool error. Eight covers every shape observed in the
	// failure archive and still bounds a model that will not stop calling tools:
	// one sentence can never become an open tab.
	orchestratorToolCallCap = 8
	// orchestratorReadCap is how much of that belt LOOKING may take.
	//
	// It exists because of the shape a diagnosis has. Asked why a delivered PDF
	// was wrong, the head read the board, the result, the plan and the file, then
	// read the file again — eight calls of honest investigation — and reached the
	// end of its belt holding the answer and no hands. What it then said was "I've
	// commissioned a fix and the corrected PDF will land here", which was false:
	// nothing had been commissioned, nothing was coming, and the person waited in
	// front of a conversation that believed work was under way until they typed
	// "start it" a message later.
	//
	// The belt cannot be widened past the point where one sentence becomes an open
	// tab, so it is DIVIDED instead. Looking is capped two calls below the whole,
	// which leaves a turn that spent everything investigating with hands to act:
	// the commission, and the receipt-read after it. A turn that only ever reads
	// loses those two reads, and that is the trade — the archive's read-only turns
	// settle well inside six, and no amount of extra reading has ever been worth
	// arriving at the answer unable to act on it.
	orchestratorReadCap = 6
	// orchestratorMaxTokens is the answer-turn cap, re-set deliberately.
	//
	// It was 600 (head.go:880, the router; control.go, the belt), and 600 is the
	// exact number that cut session bd3c78ed's SVG in half and journaled the half
	// as a finished answer — 12.6.3's smoking gun. Raising the cap was never the
	// fix and is not the fix here: the fix is the artifact door, which takes every
	// deliverable out of this budget entirely and puts it on disk. What is left in
	// the budget is a receipt, an answer, or a tool call's arguments, and 600 was
	// genuinely tight for the third of those once a turn can carry several. 1200
	// is chosen as the smallest ceiling under which no honest turn observed in the
	// archive is cut — and every turn that DOES hit it flows a truncation mark, so
	// a cut answer can never again be presented as a whole one.
	orchestratorMaxTokens = 1200
	// orchestratorSpentBelt is what a tool call past the cap is told. It is a
	// tool result rather than a hard stop so the turn still ends in words.
	//
	// It says what was NOT done as well as what to do next, because the turn that
	// produced this text went on to describe work it had never commissioned. A
	// model at the end of its belt is a model summarizing from memory, and the
	// memory of intending to act reads exactly like the memory of acting.
	orchestratorSpentBelt = "the tool belt is spent for this message — say what happened, in one or two sentences, " +
		"and describe only what a tool of yours actually did: nothing was commissioned, changed or started this turn " +
		"unless a tool result in front of you says so"
	// orchestratorSpentReads is what a read past the reading cap is told. The
	// turn is not over: the calls that are left are for acting, and this says so
	// in the moment the model still has the choice.
	orchestratorSpentReads = "the reading half of the belt is spent for this message — the calls left are for acting, not looking. " +
		"If what you have found needs work doing, commission it now with what you know; otherwise say what you found"
)

// runTurn answers one folded turn with one agentic loop.
func (h *Head) runTurn(ctx context.Context, user store.Message) error {
	client, err := h.clientFor(user)
	if err != nil {
		return h.postAgent(user.SessionID, providerErrorReply, 0)
	}
	prompt, err := h.turnPrompt(user)
	if err != nil {
		return err
	}
	messages := []ai.Message{
		// No retrieval cue from the message: voice preferences are standing user
		// style, not query-relevant, and cueing them on the current message made
		// the system prompt a different string every turn — the one place in the
		// whole call that can be identical from message to message. It is now the
		// ONLY system prompt, so that identity is worth more than it ever was.
		textMessage("system", resident.VoicePrompt(h.store, orchestratorPrompt)),
		textMessage("user", prompt),
	}
	if h.supportsImages(client) && len(user.Attachments) > 0 {
		parts := messages[1].Content
		for _, path := range user.Attachments {
			if part, ok := imageContentPart(path); ok {
				parts = append(parts, part)
			}
		}
		messages[1].Content = parts
	}

	definitions := beltDefinitions()
	run := &beltRun{head: h, user: user}
	final := ""
	servedModel := ""
	spent := 0
	reads := 0
	// How the answering call ended. Nil is the ordinary case and posts nothing; a
	// cap or a dropped stream lands on the reply as a store part. This is one of
	// the two finish_reason seams (the other is pool.TurnEnd) and it exists
	// because a turn ended by anything other than its own completion must say so
	// — 12.5's truncation law. It is taken from the call whose words are used.
	var ended *store.EndedPart

	// One provider call per tool call the belt allows, one to be told the belt is
	// spent, and one to speak. A model that will not stop calling tools still ends
	// in at most this many calls.
	for turn := 0; turn < orchestratorToolCallCap+2; turn++ {
		callContext := ctx
		stopWatch := func() {}
		if turn == 0 {
			// The only moment at which the person can overtake the turn for free:
			// nothing has acted, nothing has been said, and a turn withdrawn here
			// has nothing to undo. Once a tool has journalled anything the turn
			// owes a receipt and may not withdraw.
			callContext, stopWatch = h.watchForFold(ctx)
		}
		response, err := client.CompleteWithMessages(callContext, messages,
			ai.WithTools(definitions), ai.WithMaxTokens(orchestratorMaxTokens))
		stopWatch()
		if err != nil {
			if h.refolding() && turn == 0 && !run.acted {
				return errRefold
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// One transient failure should not surface as "try again" — the person
			// already tried. Retry once, and only on the opening call, where
			// nothing has acted and a second attempt cannot double an effect.
			if turn == 0 && !run.acted {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(400 * time.Millisecond):
				}
				continue
			}
			break
		}
		if response == nil {
			break
		}
		servedModel = strings.TrimSpace(response.Model)
		ended = store.EndedFor(provider.FinishReason(response), provider.Streaming(callContext))
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
			name := call.Function.Name
			looking := beltReadOnly(name)
			var body string
			switch {
			case spent >= orchestratorToolCallCap:
				body = orchestratorSpentBelt
			case looking && reads >= orchestratorReadCap:
				// The slot is not spent: a read refused here leaves the belt
				// exactly as wide as it was, so the acting call this turn needs
				// is still there to be made.
				body = orchestratorSpentReads
			default:
				spent++
				if looking {
					reads++
				}
				result, failed := run.execute(name, call.Function.Arguments)
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
		// A tool that stopped at a consent gate ends the turn: the question IS the
		// reply, and the gates own the words a consent decision is described in.
		// So does a tool that already spoke for the whole turn — the numbered
		// question, the total shutdown's gate, the stop. Letting the loop carry on
		// past either would let it say a second thing over the top of the first,
		// which is the thread talking to itself in front of the person.
		if run.confirm != nil || run.spoke {
			break
		}
	}

	// The gate's question is the reply. Posting the model's prose beside it would
	// put two voices on one decision, and only one of them knows what the consent
	// actually covers.
	if run.confirm != nil {
		return h.askBeltConfirm(user, run.confirm)
	}
	// A tool that already spoke for the whole turn — a consent question, a stop —
	// owns the words. A second voice over the top of it is the thread talking to itself.
	if run.spoke && strings.TrimSpace(final) == "" {
		return nil
	}
	if strings.TrimSpace(final) == "" && run.acted {
		// Assembled from what the tools reported, never from intent. It is the
		// floor under a model that acted and then said nothing useful, and it is
		// also 5.20's visible-dispatch rule holding when the words fail: prose
		// turned into work is never a silent side effect.
		final = run.summary()
		// And the mark does not travel with it. Whatever cut the model's last
		// call describes words that are not being posted; these are the head's
		// own and they are complete. Marking them would be a lie in the other
		// direction, which the truncation law has no more use for than the first.
		ended = nil
	}
	// The last thing read before the words are the person's: a reply may not
	// promise what this head has no way to do. What it MAY promise moved when
	// the delivery wake landed — see promise.go for the line and why it is
	// checked here rather than trusted to the prompt.
	final = h.keepable(ctx, client, messages, final, run.commandSeq != 0, run.acted)
	return h.postAgentFloor(user.SessionID, final, run.commandSeq,
		replyModel(user, client, servedModel), endedParts(ended))
}

// endedParts is what a turn hands the posting door. Today it is only the end
// mark, and only when there is one — so an ordinary turn posts the exact message
// it posted before parts existed, byte for byte.
func endedParts(ended *store.EndedPart) []store.MessagePart {
	if ended == nil {
		return nil
	}
	return []store.MessagePart{store.EndedMark(*ended)}
}

// turnPrompt is the one user message, assembled stable-first.
//
// The order is a cost decision, not a rhetorical one, and the rule is position
// by volatility rather than by semantic category (12.4.1). Every endpoint we
// ride caches by prefix: the bytes before the earliest change are billed at a
// tenth and everything from that byte onward at full price, so a block that is
// rewritten in place invalidates everything after it and nothing before it.
//
//   - the thread only ever APPENDS, and moves its own front in big steps rather
//     than every turn, so it leads;
//   - measured self-knowledge is stable between jobs and rewritten in place
//     during one, so it sits under the thread rather than above it;
//   - the manual's page list is a compile-time constant and rides with the
//     stable half;
//   - then the volatile floor, fastest last: the board ticks with every status,
//     depth is retrieved against this message, the notebook is retrieved against
//     this message, the readings appear and vanish with the sentence, the clock
//     moves every minute and the spend line moves every cent.
func (h *Head) turnPrompt(user store.Message) (string, error) {
	recent, err := h.recentThread(user.SessionID, user.Seq)
	if err != nil {
		return "", fmt.Errorf("serve head: read recent thread: %w", err)
	}
	thread := h.renderThread(recent)

	// Live work, read once and shared by every block below that needs it: the
	// board itself, the lexical hint arm, and the adjacency arm.
	active, err := h.activeUserJobs()
	if err != nil {
		return "", fmt.Errorf("serve head: read live work: %w", err)
	}
	// Depth is bought in its own budget and only for the jobs this message is
	// about. A message about nothing on the board adds nothing at all. It is
	// computed before the board because the board is the floor and must never
	// starve: it still gets its line for every job and drops only the clause the
	// block below is about to quote in full.
	deep, opened := h.renderDeep(user.Body, thread)
	board, err := h.renderTurnBoard(user.SessionID, thread, opened)
	if err != nil {
		return "", err
	}

	var body strings.Builder
	body.WriteString("Recent thread before this message:\n" + thread)
	if h.knowledge != nil {
		if measured := strings.TrimSpace(h.knowledge()); measured != "" {
			body.WriteString("\n\nMeasured execution history (evidence for routing priors):\n" + measured)
		}
	}
	body.WriteString("\n\nManual pages available: " + strings.Join(manual.Pages(), ", "))
	body.WriteString("\n\nLive board (the work you can read and act on):\n" + board)
	if services := renderServices(h.store); services != "" {
		body.WriteString("\n" + services)
	}
	if deep != "" {
		body.WriteString("\n\n" + deep)
	}
	body.WriteString("\n\nNotebook (durable memory across jobs and conversations):\n" +
		renderNotebook(h.store, user.Body, thread))
	if hints := h.renderHints(user, active); hints != "" {
		body.WriteString("\n\n" + hints)
	}
	// The clock. Every other block is time-ordered and none of them says what
	// time it is, so "yesterday", "this week" and "how long has that been sitting
	// there" were words the head could read and never resolve. At minute
	// resolution it holds still for the length of an exchange.
	body.WriteString("\n\n" + nowLine(time.Now()))
	// The fastest-moving fact in the prompt, so it is the last thing before the
	// message. It is never dropped: the daily-rail approval flow reads the
	// person's "yes" against it.
	if h.dailyRailSet {
		if rail, railErr := h.store.DailyRailToday(h.dailyBudgetUSD); railErr == nil {
			line := fmt.Sprintf("today's spend: $%.2f of $%.2f daily rail", rail.Spend, rail.Ceiling)
			if rail.Unlimited {
				line = fmt.Sprintf("today's spend: $%.2f; daily rail unlimited", rail.Spend)
			}
			body.WriteString("\n\n" + line)
		}
	}
	body.WriteString("\n\nCurrent user message (verbatim):\n" + strings.TrimSpace(user.Body))
	return body.String(), nil
}

// renderTurnBoard is the prompt's board, and it is the same board the board tool
// returns — one renderer, one query, one vocabulary. The router's own snapshot
// renderer used to sit beside this one, listing every node as a peer with its
// own separate byte budget and its own separate rules about what is addressable,
// and the two disagreeing is how the head once described a job in one sentence
// and denied its existence in the next.
func (h *Head) renderTurnBoard(sessionID, thread string, opened map[string]bool) (string, error) {
	rows, err := h.boardRowsAt(sessionID, "", "", "", time.Now())
	if err != nil {
		return "", fmt.Errorf("serve head: read board: %w", err)
	}
	return boardBlock(rows, thread, opened), nil
}

// boardBlock is the prompt's board as a string, clock and all, so the ordering
// and truncation rules have one spelling rather than one per caller.
func boardBlock(rows []boardRow, thread string, opened map[string]bool) string {
	if len(rows) == 0 {
		return emptyBoardLine
	}
	return renderBoardWithin(rows, thread, opened, maxGraphContextBytes)
}

// emptyBoardLine says what an empty board means without saying the head is
// blind: a read aimed by id or by the person's own words still reaches finished
// work, which is where findings live.
const emptyBoardLine = "(nothing of the person's is live right now — a read aimed by id or by their own words still reaches finished work)"

// boardFor is the one board a caller outside the turn can ask for: the same
// query, the same renderer, the same dedup, with the clock passed in. Every
// assertion about what the head can SEE goes through it, which is the point —
// there is no second board to assert against any more.
func (h *Head) boardFor(sessionID, thread string, opened map[string]bool, now time.Time) string {
	rows, err := h.boardRowsAt(sessionID, "", "", "", now)
	if err != nil {
		return emptyBoardLine
	}
	return boardBlock(rows, thread, opened)
}
