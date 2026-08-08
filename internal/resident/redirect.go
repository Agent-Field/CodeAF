// The user is the second event source. A landed result and a person changing
// their mind mid-flight are the same kind of thing — new information about a
// plan that is still running — so they reach the same sentinel by the same
// path. The only differences are that the person speaks with authority, and
// that the workers already in motion have to be told in their own transcripts.
package resident

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// redirectSteerPrefix marks the user's words in a worker's transcript. The
// steering mailbox delivers node-anchored user messages verbatim, so the
// prefix is all that separates guidance for this leaf from a change of course
// for the whole job.
const redirectSteerPrefix = "redirection from the user: "

// redirectCutReason is journaled on every node the user's words removed.
const redirectCutReason = "revision: the user cut this work"

// urgencySteerLine is what a worker mid-turn hears when the user is out of
// patience. It promises nothing on anyone's behalf — it names the trade and
// leaves the judgment where the work is.
const urgencySteerLine = "the user wants results now — prefer the direct path, " +
	"cut nice-to-haves, and deliver the strongest answer you can from what you already have."

// urgencyRevisionInstruction is the same pressure said to the sentinel, which
// is the only party allowed to decide that a remaining step is not needed.
const urgencyRevisionInstruction = "the user wants the result as soon as possible; " +
	"drop or fold any remaining step that is not strictly needed for the core deliverable"

// urgencyPriorityReason is journaled with the claim-order change.
const urgencyPriorityReason = "expedite: the user asked for this sooner"

// RevisionFlavor is why the user spoke: to change what the work is, or to
// change how long they are willing to wait for it. Both reach the same sentinel
// by the same path; only the event they carry differs.
type RevisionFlavor string

const (
	RevisionRedirect RevisionFlavor = "redirect"
	RevisionExpedite RevisionFlavor = "expedite"
)

// Redirection is what one user-driven revision pass actually did, in the terms
// the receipt speaks: counts, the store's refusals, and the running leaves the
// sentinel wanted gone — which the store will not simply delete.
type Redirection struct {
	Added   int
	Dropped int
	Amended int
	Notes   []string
	// RunningRemovals names store nodes the revision wanted removed while a
	// worker is inside them. Started work is frozen, so removal degrades to a
	// cancel request, gated by what it would throw away.
	RunningRemovals []string
}

// RedirectFunc revises one job's remaining plan in light of the user's own
// words. It is injected rather than built here because the live plan graph
// belongs to the process that planned it; the reconciler owns everything that
// follows — the cancels, the broadcast, and the receipt.
type RedirectFunc func(ctx context.Context, job store.Node, message string, flavor RevisionFlavor) (Redirection, error)

// WithRedirector registers the plan-revision half of user-driven redirection.
// Without it the words still reach every running worker as steering.
func (r *Reconciler) WithRedirector(redirect RedirectFunc) *Reconciler {
	r.redirect = redirect
	return r
}

// UserRevisionEvent phrases a revision for the sentinel. The landed-result
// event describes something that happened; this one describes someone who
// decides — the sentinel's standing default of "no change" is overridden by
// the owner of the work, not by evidence. Under urgency the authority is the
// same and only the licence changes: the remaining plan may lose its tail.
func UserRevisionEvent(message string, flavor RevisionFlavor) string {
	message = strings.TrimSpace(message)
	if len(message) > 1200 {
		message = message[:1200] + "…"
	}
	if flavor == RevisionExpedite {
		return "The user is out of patience with this job. What they want:\n" + message +
			"\n\nThis is the owner of the work speaking, and they are spending the deliverable's " +
			"completeness to buy time. Cut the remaining plan to the shortest path to the core " +
			"deliverable: drop or fold every unstarted step that is not strictly needed for it. " +
			"Never add work, and never touch a step that has already started."
	}
	return "The user has redirected this job. Their words, verbatim:\n" + message +
		"\n\nThis is the owner of the work speaking, with authority over what it is for. " +
		"Edit the remaining plan to comply. Prefer the fewest edits that make the plan match " +
		"what they now want, and never re-add work they cut."
}

// redirectJob applies one CommandRedirect: revise what has not started, stop
// what the user cut, tell whoever is mid-turn, and say what happened once.
func (r *Reconciler) redirectJob(ctx context.Context, command store.Command) (commandOutcome, error) {
	job, found, err := r.store.Node(command.Target)
	if err != nil {
		return commandOutcome{}, err
	}
	if !found {
		return commandOutcome{}, fmt.Errorf("target %q no longer exists", command.Target)
	}
	// The words go first, before anything slow, and this ordering is the whole
	// difference between steering and paperwork. The broadcast is a store write
	// measured in milliseconds; the plan revision under it is a model call
	// measured in seconds. Running the model call first meant the user's own
	// sentence reached the workers who could still act on it a full round-trip
	// after they could have had it — which is how "make it about the sea"
	// arrived at a leaf that had already delivered a poem about mountains.
	// The fallible half must not swallow the reliable one, and it must not
	// delay it either.
	informed, err := BroadcastRedirection(r.store, job.ID, command.SessionID, command.Instruction)
	if err != nil {
		return commandOutcome{}, err
	}
	revision, revised := Redirection{}, true
	if r.redirect != nil {
		edited, revisionErr := r.redirect(ctx, job, command.Instruction, RevisionRedirect)
		if revisionErr != nil {
			revised = false
			revision.Notes = append(revision.Notes, "the remaining plan could not be revised: "+revisionErr.Error())
		} else {
			revision = edited
		}
	}
	cancelled, gated, err := r.cutRunningWork(revision.RunningRemovals)
	if err != nil {
		return commandOutcome{}, err
	}
	revision.Dropped += len(cancelled)
	for _, node := range gated {
		if err := r.askBeforeCutting(command, node); err != nil {
			return commandOutcome{}, err
		}
	}
	return commandOutcome{
		status: store.CommandApplied,
		result: fmt.Sprintf("redirected %s: %d added, %d dropped, %d amended, %d informed",
			job.ID, revision.Added, revision.Dropped, revision.Amended, informed),
		receipt: redirectReceipt(job, revision, informed, revised),
	}, nil
}

// expediteJob applies one CommandExpedite. It is redirectJob with the same
// three moves aimed at time instead of content: claim order, the workers
// mid-turn, and the unstarted tail. It compiles nothing — the entire point is
// that the queue must get shorter, never longer.
func (r *Reconciler) expediteJob(ctx context.Context, command store.Command) (commandOutcome, error) {
	job, found, err := r.store.Node(command.Target)
	if err != nil {
		return commandOutcome{}, err
	}
	if !found {
		return commandOutcome{}, fmt.Errorf("target %q no longer exists", command.Target)
	}
	moved, err := r.raiseClaimOrder(job.ID, urgencyPriorityReason)
	if err != nil {
		return commandOutcome{}, err
	}
	unstarted, open, err := r.jobTail(job.ID)
	if err != nil {
		return commandOutcome{}, err
	}
	// Pressure reaches the people under it first, for the same reason a
	// redirect's words do: the trim below is a model call, and a worker told to
	// cut to the essentials one round-trip late has spent that round-trip not
	// cutting to the essentials.
	informed, err := BroadcastRedirection(r.store, job.ID, command.SessionID, urgencySteerLine)
	if err != nil {
		return commandOutcome{}, err
	}
	revision, revised := Redirection{}, false
	// A tail is the only thing a trim can take. With nothing unstarted the
	// model call would be spend with no outcome available to it.
	if r.redirect != nil && unstarted > 0 {
		revised = true
		edited, revisionErr := r.redirect(ctx, job, urgencyRevisionInstruction, RevisionExpedite)
		if revisionErr != nil {
			revised = false
			revision.Notes = append(revision.Notes, "the remaining plan could not be trimmed: "+revisionErr.Error())
		} else {
			revision = edited
		}
	}
	// Urgency never throws away work already in motion — that would be paying
	// for speed with the very minutes the user is waiting on — so a removal the
	// sentinel aimed at a running leaf degrades to the same consented cut.
	cancelled, gated, err := r.cutRunningWork(revision.RunningRemovals)
	if err != nil {
		return commandOutcome{}, err
	}
	revision.Dropped += len(cancelled)
	for _, node := range gated {
		if err := r.askBeforeCutting(command, node); err != nil {
			return commandOutcome{}, err
		}
	}
	remaining := open - revision.Dropped
	if remaining < 0 {
		remaining = 0
	}
	return commandOutcome{
		status: store.CommandApplied,
		result: fmt.Sprintf("expedited %s: moved=%t, %d dropped, %d amended, %d added, %d informed",
			job.ID, moved, revision.Dropped, revision.Amended, revision.Added, informed),
		receipt: expediteReceipt(job, moved, revision, informed, remaining, revised),
	}, nil
}

// jobTail counts what is left of a job below its root: the steps not yet
// started, which a trim may take, and every open step, which the receipt owes
// the user as the honest remainder.
func (r *Reconciler) jobTail(jobRoot string) (unstarted, open int, err error) {
	nodes, err := r.store.Nodes()
	if err != nil {
		return 0, 0, err
	}
	ids, ok := descendants(nodes, jobRoot)
	if !ok {
		return 0, 0, fmt.Errorf("target %q no longer exists", jobRoot)
	}
	byID := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	for _, id := range ids {
		node, present := byID[id]
		if id == jobRoot || !present || terminal(node.Status) {
			continue
		}
		open++
		if node.Status == store.Pending {
			unstarted++
		}
	}
	return unstarted, open, nil
}

// raiseClaimOrder is the whole of reprioritization at the store: one pending
// node moved ahead of its pending siblings, dependencies untouched. Work that
// already started has no claim order left to change, and work with nothing
// queued beside it is already first — both say no rather than claim a move
// the receipt would then have to overstate.
func (r *Reconciler) raiseClaimOrder(target, reason string) (bool, error) {
	node, found, err := r.store.Node(target)
	if err != nil || !found {
		return false, err
	}
	if node.Status != store.Pending {
		return false, nil
	}
	queued, err := r.pendingSiblings(node)
	if err != nil || queued == 0 {
		return false, err
	}
	priority, err := r.store.NextSiblingPriority(target)
	if err != nil {
		return false, err
	}
	if priority <= node.Priority {
		return false, nil
	}
	if err := r.store.SetNodePriority(target, priority, reason); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Reconciler) pendingSiblings(node store.Node) (int, error) {
	return r.store.PendingSiblingCount(node.ID, node.Parent)
}

// expediteReceipt says what impatience actually bought. Every clause is
// something that happened; when nothing did, it says that and what remains,
// because a promise of speed with no mechanism behind it is the failure this
// path exists to end.
func expediteReceipt(job store.Node, moved bool, revision Redirection, informed, remaining int, revised bool) string {
	did := make([]string, 0, 4)
	if moved {
		did = append(did, "moved it to the front of the queue")
	}
	if informed > 0 {
		did = append(did, fmt.Sprintf("told the %d %s already under way to cut to the essentials",
			informed, plural(informed, "step", "steps")))
	}
	if revision.Dropped > 0 {
		did = append(did, fmt.Sprintf("dropped %d remaining %s",
			revision.Dropped, plural(revision.Dropped, "step", "steps")))
	}
	if revision.Amended > 0 {
		did = append(did, fmt.Sprintf("folded %d %s down",
			revision.Amended, plural(revision.Amended, "step", "steps")))
	}
	// The urgency event forbids additions. If one happens anyway the receipt
	// still says so — the user is owed the queue as it actually is.
	if revision.Added > 0 {
		did = append(did, fmt.Sprintf("added %d %s", revision.Added, plural(revision.Added, "step", "steps")))
	}
	receipt := "understood — " + surgeryLabel(job) + ": "
	switch {
	case len(did) > 0:
		receipt += strings.Join(did, ", ")
	case revised:
		receipt += "nothing left in the plan could be dropped, and nobody is mid-turn to press"
	default:
		receipt += "there is nothing here left to accelerate"
	}
	if remaining > 0 {
		receipt += fmt.Sprintf("; %d %s still to run", remaining, plural(remaining, "step", "steps"))
	}
	for _, note := range revision.Notes {
		receipt += "\n· " + clipLabel(firstLine(note), 160)
	}
	return receipt
}

// BroadcastRedirection posts the user's words into every running leaf of the
// job as a node-anchored user message — the same move an amendment makes for
// one node, made plural. The executor's steering mailbox delivers them before
// the next turn, so a worker learns the goal moved without being restarted.
func BroadcastRedirection(graph *store.Store, jobRoot, sessionID, message string) (int, error) {
	nodes, err := graph.Nodes()
	if err != nil {
		return 0, err
	}
	informed := 0
	for _, id := range redirectAudience(nodes, jobRoot) {
		if _, err := graph.PostMessage(store.Message{
			SessionID: sessionID, Role: store.RoleUser, NodeID: id,
			Body: redirectSteerPrefix + strings.TrimSpace(message),
		}); err != nil {
			return informed, err
		}
		informed++
	}
	return informed, nil
}

// A broadcast is delivery to a mailbox, not delivery to a mind. The steering
// poll runs between turns, so words that arrive after the last turn of a leaf
// are read by nobody — and the leaf then lands with a result written before the
// user spoke. That is what happened at 03:30:37: "make sure you test the
// functionality at the end" reached a worker that completed two seconds later
// saying "everything is verified". The redirection changed nothing, nobody
// re-checked, and — the part that made it a betrayal rather than a race — nobody
// said so. The user was left believing their correction had been taken.
//
// The read is structural: the informs are journaled at the node, the landing has
// a time, and a landing this close to the words means there was no turn boundary
// between them.
const (
	// directionMissWindow is shorter than a single worker turn on purpose. Past
	// it, a leaf has almost certainly polled its mailbox and the words were
	// heard; inside it, claiming they were heard is a guess in the direction
	// that costs the user their correction.
	directionMissWindow = 15 * time.Second
	// missedDirectionScan bounds the mailbox read. A leaf steered more times
	// than this has a conversation of its own, and the newest words are the ones
	// that were missed.
	missedDirectionScan = 50
)

// reportMissedDirection says so when a redirection arrived too late to be read.
// It respawns nothing: the words were about work that no longer exists to change,
// and spending on a re-run the user has not asked for twice is the opposite of
// the honesty this is for. What it does instead is put the job back in front of
// them with the fact attached, so their next sentence reaches the correction path
// — which owns exactly this shape, a delivered result the user disagrees with —
// with everything it needs to anchor.
func (r *Reconciler) reportMissedDirection(node store.Node) error {
	sessionID := r.effectiveSessionID(node)
	if sessionID == "" || node.FinishedAt.IsZero() {
		return nil
	}
	messages, err := r.store.NodeMessages(node.ID, 0, missedDirectionScan)
	if err != nil {
		return err
	}
	var arrived time.Time
	for _, message := range messages {
		// Only a redirection carries the user's own words. Urgency broadcasts a
		// fixed line about pace, and a missed "hurry up" on finished work is not
		// news anyone needs.
		if message.Role == store.RoleUser && strings.HasPrefix(message.Body, redirectSteerPrefix) {
			arrived = message.Time
		}
	}
	if !directionMissed(node.FinishedAt, arrived) {
		return nil
	}
	_, err = r.store.PostMessage(store.Message{
		SessionID: r.deliverySessionID(sessionID),
		Role:      store.RoleAgent,
		Body: "Your words reached " + surgeryLabel(node) +
			" as it was already finishing, so nothing there acted on them. " +
			"Tell me what you want done and I'll take it from there.",
	})
	return err
}

// directionMissed is the whole judgment, and it is arithmetic on two journal
// times. A negative difference is words that arrived after the work was already
// recorded as over; a small positive one is words that arrived with no turn left
// to read them in.
func directionMissed(finished, arrived time.Time) bool {
	if finished.IsZero() || arrived.IsZero() {
		return false
	}
	return finished.Sub(arrived) <= directionMissWindow
}

// RedirectAudience counts who a redirection would reach if it were broadcast
// now. It exists so the head can say how many workers are about to hear the
// user's words in the moment the user asks, rather than waiting for the
// reconciler to say how many did — and it shares the broadcast's own membrane
// rather than restating it, because two copies of that rule would drift and the
// receipt would then name a number nobody was told.
func RedirectAudience(graph *store.Store, jobRoot string) (int, error) {
	nodes, err := graph.Nodes()
	if err != nil {
		return 0, err
	}
	return len(redirectAudience(nodes, jobRoot)), nil
}

// redirectAudience is the membrane itself: the leaves of the job that are
// mid-turn, minus the ones this same revision is withdrawing.
func redirectAudience(nodes []store.Node, jobRoot string) []string {
	ids, ok := descendants(nodes, jobRoot)
	if !ok {
		return nil
	}
	byID := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	audience := make([]string, 0, len(ids))
	for _, id := range ids {
		node, present := byID[id]
		if !present || (node.Status != store.Running && node.Status != store.Claimed) {
			continue
		}
		// A leaf the same redirection just withdrew is on its way out; new
		// guidance for it would be a message about work it will never do.
		if node.CancelRequested {
			continue
		}
		audience = append(audience, id)
	}
	return audience
}

// cutRunningWork degrades removal to the cooperative cancel control, because
// the store refuses to edit work that started. A cheap young leaf goes quietly;
// anything with real money or real time in it is not thrown away without a word.
func (r *Reconciler) cutRunningWork(ids []string) (cancelled []string, gated []store.Node, err error) {
	now := r.now()
	for _, id := range ids {
		node, found, err := r.store.Node(id)
		if err != nil {
			return nil, nil, err
		}
		if !found || (node.Status != store.Running && node.Status != store.Claimed) {
			continue
		}
		impact, err := r.store.Impact(id, now)
		if err != nil {
			return nil, nil, err
		}
		if impact.Cost > store.SurgerySpendGateUSD || impact.RunningFor > store.SurgeryRuntimeGate {
			gated = append(gated, node)
			continue
		}
		if err := r.store.RequestNodeCancel(id, redirectCutReason); err != nil {
			return nil, nil, err
		}
		cancelled = append(cancelled, id)
	}
	return cancelled, gated, nil
}

func (r *Reconciler) askBeforeCutting(command store.Command, node store.Node) error {
	impact, err := r.store.Impact(node.ID, r.now())
	if err != nil {
		return err
	}
	prompt := fmt.Sprintf("Your change would drop %s, but it is already running — %s Stop it?",
		surgeryLabel(node), redirectLoss(impact))
	allowFree := false
	options := []store.QuestionOption{
		{Label: "yes, stop it", Value: store.RedirectOptionValue("cancel", node.ID, command.Instruction)},
		{Label: "let it finish", Value: store.RedirectOptionValue("keep", node.ID, command.Instruction)},
	}
	_, err = r.askQuestionLocked(store.AgentQuestion{
		SessionID: command.SessionID,
		Text: boundMessage(store.QuestionMessageBody(prompt, options, store.QuestionConfig{
			Kind: store.QuestionConfirm, Category: store.QuestionCategorySurgeryConfirm,
			Default: "2", AllowFree: &allowFree,
		})),
		// Anchored to the leaf rather than to the command: when that worker
		// finishes on its own the question has answered itself and expires.
		OriginNodeID: node.ID, Urgency: store.QuestionBlocking,
		Options: options, Category: store.QuestionCategorySurgeryConfirm, DefaultAnswer: "2",
	})
	return err
}

func redirectLoss(impact store.SurgeryImpact) string {
	parts := make([]string, 0, 2)
	if impact.RunningFor > 0 {
		minutes := int(impact.RunningFor.Round(time.Minute) / time.Minute)
		if minutes < 1 {
			minutes = 1
		}
		parts = append(parts, fmt.Sprintf("%d %s in", minutes, plural(minutes, "minute", "minutes")))
	}
	if impact.Cost > 0 {
		parts = append(parts, fmt.Sprintf("~$%.2f spent", impact.Cost))
	}
	if len(parts) == 0 {
		parts = append(parts, "its partial work would be discarded")
	}
	return strings.Join(parts, " and ") + "."
}

// redirectReceipt says what moved in one line. Silence is the one thing it may
// never be: a redirection that changed nothing is still an answer.
func redirectReceipt(job store.Node, revision Redirection, informed int, revised bool) string {
	changes := make([]string, 0, 3)
	if revision.Amended > 0 {
		changes = append(changes, fmt.Sprintf("%d %s amended", revision.Amended, plural(revision.Amended, "step", "steps")))
	}
	if revision.Added > 0 {
		changes = append(changes, fmt.Sprintf("%d added", revision.Added))
	}
	if revision.Dropped > 0 {
		changes = append(changes, fmt.Sprintf("%d dropped", revision.Dropped))
	}
	told := fmt.Sprintf("%d %s already under way heard it", informed, plural(informed, "step", "steps"))
	receipt := "redirected " + surgeryLabel(job) + " — "
	switch {
	case !revised && informed > 0:
		receipt += "the remaining plan is unchanged; " + told
	case !revised:
		receipt += "I could not revise the remaining plan"
	case len(changes) > 0 && informed > 0:
		receipt += strings.Join(changes, ", ") + "; " + told
	case len(changes) > 0:
		receipt += strings.Join(changes, ", ")
	case informed > 0:
		receipt += "nothing in the remaining plan needed to change; " + told
	default:
		receipt += "nothing in the remaining plan needed to change, and nobody was mid-turn to tell"
	}
	for _, note := range revision.Notes {
		receipt += "\n· " + clipLabel(firstLine(note), 160)
	}
	return receipt
}
