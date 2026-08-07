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
type RedirectFunc func(ctx context.Context, job store.Node, message string) (Redirection, error)

// WithRedirector registers the plan-revision half of user-driven redirection.
// Without it the words still reach every running worker as steering.
func (r *Reconciler) WithRedirector(redirect RedirectFunc) *Reconciler {
	r.redirect = redirect
	return r
}

// UserRevisionEvent phrases a redirection for the sentinel. The landed-result
// event describes something that happened; this one describes someone who
// decides — the sentinel's standing default of "no change" is overridden by
// the owner of the work, not by evidence.
func UserRevisionEvent(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 1200 {
		message = message[:1200] + "…"
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
	revision, revised := Redirection{}, true
	if r.redirect != nil {
		// The plan pass is the fallible half of this — it is a model call. Its
		// failure must not swallow the reliable half: the words still reach
		// everyone who is mid-turn, and the receipt says which part happened.
		edited, revisionErr := r.redirect(ctx, job, command.Instruction)
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
	informed, err := BroadcastRedirection(r.store, job.ID, command.SessionID, command.Instruction)
	if err != nil {
		return commandOutcome{}, err
	}
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

// BroadcastRedirection posts the user's words into every running leaf of the
// job as a node-anchored user message — the same move an amendment makes for
// one node, made plural. The executor's steering mailbox delivers them before
// the next turn, so a worker learns the goal moved without being restarted.
func BroadcastRedirection(graph *store.Store, jobRoot, sessionID, message string) (int, error) {
	nodes, err := graph.Nodes()
	if err != nil {
		return 0, err
	}
	ids, ok := descendants(nodes, jobRoot)
	if !ok {
		return 0, nil
	}
	byID := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	informed := 0
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
	told := fmt.Sprintf("%d running %s informed", informed, plural(informed, "worker", "workers"))
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
