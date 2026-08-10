package head

import (
	"context"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A redirection used to end in silence, and the silence was structural. The
// route journaled the command and returned; the reconciler applied it a moment
// later and filed its receipt under the job's own card, which is where ambient
// progress goes and not where the conversation is. So a person who typed "you
// are wasting time understanding the repo, work faster" watched a still thread,
// learned nothing about whether their words had landed, and the next agent line
// they saw was an unrelated progress heartbeat a hundred seconds later.
//
// The second half of the same failure was quieter. That sentence carried two
// things — a change to the running job AND a lesson meant to outlive it — and
// the route consumed the whole message for the first. The words were whispered
// verbatim into two running workers and died with them; the notebook, which the
// router writes to on every other message, was never touched, so the next job
// studied the repo exactly as long.
//
// Both halves are one model call, and it is the same judgment the router already
// makes about every other message: say what happened in the head's own voice,
// and decide whether the sentence taught something a future job should read. The
// capture goes through the router's own seam — one notebook, one writer.

// revisionVoicePrompt composes the one line a mid-flight revision owes the
// person who asked for it, and judges what of their words outlives the job.
const revisionVoicePrompt = `You are the front desk of a task-graph agent, speaking to the person who owns the work. They have just changed something that is already running, and the change is made: their words are journaled as a revision of that job, every worker mid-turn hears them verbatim before its next step, and the remaining plan is being edited to match them.

Return exactly one JSON object with this shape and no text outside it:
{"reply":"<what to say right now>","remember":null}
and remember may instead be {"scope":"<scope>","kind":"preference|lesson","body":"<one sharp sentence>"}.

The reply is one or two short sentences in their terms: which work you took it to, whether anything already under way heard it, and that you will say what changed once it lands. Say only what the facts below actually support — never a completion, never a speed you cannot deliver, never a plan edit nobody has reported to you yet. No preamble, no exclamation, no saying their own sentence back to them, and none of the machinery's vocabulary: not node, leaf, graph, splice, worker, charter, craft, rail, firing, notebook, or a raw id. Count steps of the work, never workers.

remember is the part of the message that outlives this job. People steer and teach in the same breath — "stop over-studying the repo and move faster, and learn that for next time" is a redirection AND a durable lesson about how they want work paced — and the steering is spent the moment the job ends while the lesson is not. Judge durability by one test: will this still matter after this job is forgotten and this conversation is gone? Write the body as guidance a stranger could act on months from now: the lesson about how to work, never the pointing sentence that carried it, because "this one", "right now" and "don't change anything else" name nothing a later reader can find. Scope it to the narrowest thing it is about: user for how they like work done, tool:<name>, repo:<path>, file:<path>, or domain:<topic>. Most redirections teach nothing lasting and remember stays null. When it is not null the reply says plainly that you have noted it — and when it is null the reply may not claim you will remember anything, because a promise nothing recorded is the one sentence you may never write.`

// revisionDecision is the revision route's half of the router's decision
// surface: the reply the user sees, and the belief the sentence may have
// carried. It is deliberately the same routeMemory the router captures, so both
// doors write the notebook through one seam rather than two.
type revisionDecision struct {
	Reply    string       `json:"reply"`
	Remember *routeMemory `json:"remember"`
	model    string
}

// speakRevision is the reply floor under every mid-flight revision. The command
// is already journaled when this runs, so nothing here may fail loudly enough to
// swallow the acknowledgement: a composer that errors, returns nothing, or was
// never reachable falls back to the deterministic line, and the post itself goes
// through the seam that refuses to carry an empty body.
func (h *Head) speakRevision(ctx context.Context, user store.Message, kind store.CommandKind,
	target string, commandSeq int64) error {
	label := target
	if node, found, err := h.store.Node(target); err == nil && found {
		label = surgeryTargetLabel(node)
	}
	// Who is about to hear the user's words, counted by the broadcast's own
	// membrane rather than by a second rule that could drift away from it.
	audience, err := resident.RedirectAudience(h.store, target)
	if err != nil {
		audience = 0
	}
	decision := h.composeRevision(ctx, user, kind, label, audience)
	if decision.Remember != nil {
		h.remember(decision.Remember)
	}
	reply := strings.TrimSpace(decision.Reply)
	if reply == "" {
		reply = revisionFloorReply(kind, label, audience)
	}
	// No end mark: this route's own call is not read for one yet, and the reply
	// posted here may be the floor sentence rather than the model's words —
	// marking a sentence the head wrote itself as truncated would be a lie in
	// the other direction. Wave 3 gives this seam the same capture the routing
	// call has.
	return h.postAgentFloor(user.SessionID, reply, commandSeq, decision.model, nil)
}

// composeRevision runs the one model call this route makes. Everything it can
// fail at returns the zero decision, which the caller reads as "say the plain
// thing and record nothing" — the route must never trade an acknowledgement for
// a provider's bad afternoon.
func (h *Head) composeRevision(ctx context.Context, user store.Message, kind store.CommandKind,
	label string, audience int) revisionDecision {
	client, err := h.clientFor(user)
	if err != nil || client == nil {
		return revisionDecision{}
	}
	// Assembled by volatility, not by subject. The work's name and what was done
	// with the user's words hold still for the whole job, so they lead and the
	// appending thread follows them. The audience is a count of steps that are
	// mid-turn right now — it changes as workers finish, which is every few
	// seconds — so it rides at the bottom beside the message it is about, where
	// a new value invalidates nothing but itself.
	body := "The work: " + label +
		"\nWhat has already been done with their words: " + revisionFacts(kind)
	if recent, err := h.recentThread(user.SessionID, user.Seq); err == nil {
		body += "\n\nRecent thread before this message:\n" + h.renderThread(recent)
	}
	body += "\n\nNotebook (durable memory across jobs and conversations):\n" +
		renderNotebook(h.store, user.Body, "") +
		fmt.Sprintf("\n\nSteps of that work already under way, which hear their words verbatim: %d", audience) +
		"\n\nCurrent user message (verbatim):\n" + strings.TrimSpace(user.Body)
	response, err := client.CompleteWithMessages(ctx, []ai.Message{
		textMessage("system", resident.VoicePrompt(h.store, revisionVoicePrompt)),
		textMessage("user", body),
	}, ai.WithMaxTokens(300))
	if err != nil || response == nil {
		return revisionDecision{}
	}
	var decision revisionDecision
	if err := decodeJSONObject(response.Text(), &decision); err != nil {
		// Prose where an object was asked for is still the model speaking to the
		// user, and it is a better acknowledgement than the fallback. It carries
		// no capture, which is the honest reading: nothing said to remember.
		return revisionDecision{Reply: strings.TrimSpace(response.Text()),
			model: replyModel(user, client, strings.TrimSpace(response.Model))}
	}
	decision.model = replyModel(user, client, strings.TrimSpace(response.Model))
	return decision
}

// revisionFacts is what the composer is allowed to claim, said once. Redirection
// edits what the work is; urgency edits how long the user will wait for it.
func revisionFacts(kind store.CommandKind) string {
	if kind == store.CommandExpedite {
		return "the job moves up the claim order and its unstarted tail is being trimmed to the shortest path to the deliverable"
	}
	return "the remaining plan is being edited to match their words"
}

// revisionFloorReply is the plain sentence when no model composed one. It
// promises only the handoff — what actually changed in the plan is written a
// moment later by the reconciler that did it, which is the only party that
// honestly knows.
func revisionFloorReply(kind store.CommandKind, label string, audience int) string {
	took := "Taking that to " + label
	if kind == store.CommandExpedite {
		took = "Pushing " + label + " to the front and trimming what it has not started"
	}
	if audience > 0 {
		return fmt.Sprintf("%s — %d %s already under way %s it now; I'll say what changed once it lands.",
			took, audience, pluralWord(audience, "step", "steps"),
			pluralWord(audience, "hears", "hear"))
	}
	return took + " — nothing is part-way through there, so it lands as a change to the remaining plan; I'll say what changed."
}
