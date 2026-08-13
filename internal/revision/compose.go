package revision

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// COMPOSING IS NOT RE-EXECUTING.
//
// A delivery gate that fails a finished coding leaf used to buy exactly one
// thing: another run of the whole worker, with the critique appended to its
// task. For a worker whose deliverable is prose, that is the right purchase —
// the thing that was wrong is the thing being remade.
//
// For a worker whose deliverable is a CHANGE, it is close to the worst purchase
// available, and it was measured being made. The gap was about the wording of
// the summary; the change had already landed on the shared workspace; the
// leaf's own view had been removed with its checkpoint in it, so the second run
// started from a tree where the fix was already in and its baseline was already
// green. It found nothing to do — 23 model calls, zero edits — spent 80% of the
// leaf's whole bill doing it, and then, because the second run's outcome
// replaced the first's wholesale, delivered an account with the checks in it and
// not one file row, having discarded the pass that actually did the work.
//
// And it cost parallelism twice over: a second engine run holds a leaf's slot
// for its full duration, and its landing takes the shared working tree's lease,
// which every sibling view's landing waits on.
//
// So when the work is done and the WORDS are what failed, the words are what is
// bought. One text-only call, handed the substrate account, the patch, and the
// worker's own last word, writing the deliverable the person will read. No
// process, no workspace, no landing, no lease — and nothing that could touch the
// tree, which is what makes it safe to say the change is unaffected.
//
// The judgement of which case this is deliberately reads no prose. It is three
// facts about the worker and the work: the worker's product is a change
// ([exec.Mutates]), the change is in the repository ([exec.Account.Landed]), and
// the work's own checks settled ([exec.Account.Verified]). A gap naming work
// that is not done cannot satisfy all three, because the work is not there to be
// found.

// composePrompt writes one deliverable from a finished change.
//
// It is deliberately not a repair prompt. The model is not being asked to fix
// anything, argue with a reviewer, or describe what a previous attempt got
// wrong: it is being handed a completed piece of work and the evidence of it,
// and asked for the account of that work the person will read. The review's
// wording rides in only as what the account must cover.
const composePrompt = `You are writing the account of a piece of software work that is FINISHED.

The change is already made. It is committed, its own checks have been run, and
the diff below is exactly what is in the repository. Nothing you write changes
any of that, and you are not being asked to change any of it.

What is missing is the account of it — what the person who asked will read.

Write that account. It stands entirely on its own: the person has not seen a
previous draft, has not read the review, and is not interested in either. Do not
mention the review, the gaps, an earlier attempt, or the fact that anything was
rewritten.

Every factual claim you make must come from the record below and from nothing
else. The diff is the whole of what changed and it is the only thing that can
settle WHY it changed: if you state a root cause, a mechanism, or a reason, the
lines that show it must be in that diff, and you should be able to point at them.
Where the record does not settle something the account would otherwise want to
say, say that the record does not name it. That sentence is always available to
you and it is always the right answer for a fact you do not have — an
illustration, a plausible example, or an inference from the fact that the tests
now pass is a false statement about somebody's repository, and is worse than the
gap it fills.

Write prose the person can act on. No preamble, no headings unless the material
genuinely has sections, no restatement of the request.`

// Composition is one composed deliverable and what it cost. Text empty means
// nothing was composed and the caller keeps what it had — which is the only
// failure mode here, and it is deliberately not an error: a compose that cannot
// be reached must leave a finished, landed, verified piece of work delivered.
type Composition struct {
	Text  string
	Usage exec.Usage
	Model string
}

// Composable reports that the gap this gate named is about the account of the
// work rather than about work still to do.
//
// All three conditions are structural facts, and none of them is a reading of
// the gap's words. That is the whole design: a keyword test on a critique is a
// judgement dressed as a mechanism, and it fails in the direction that skips
// real work. These fail in the other direction — a worker with nothing derived
// from the repository, or with an unsettled suite, re-runs exactly as it always
// did.
func Composable(worker exec.Executor, outcome *exec.Outcome) bool {
	if !exec.Mutates(worker) || outcome == nil {
		return false
	}
	return outcome.Account.Landed() && outcome.Account.Verified()
}

// composeBudgetShare is what the diff may take of the composing call's window.
// It is the largest single block in this prompt by design: the account being
// written is an account OF the diff, and a writer shown a third of the change is
// a writer who will infer the rest. The literal is the fallback for a window
// nobody measured.
const (
	composePatchShare = 40
	composePatchBytes = 24 << 10
)

// Compose writes the deliverable from the substrate account, the patch and the
// worker's own last word.
//
// It takes the client rather than the executor because it must not be able to
// run anything: the guarantee this whole path rests on is that the tree is not
// touched, and a function with no worker in it cannot touch a tree.
func Compose(ctx context.Context, settings config.Config, client router.Client, node store.Node,
	method string, gaps string, outcome *exec.Outcome, said string, workerModel string,
	options ...Option) Composition {
	if client == nil || outcome == nil {
		return Composition{}
	}
	budget := newBounds(options).budget(composePrompt)

	var body strings.Builder
	body.WriteString("What the person asked for, verbatim:\n" + node.Provenance.Intent)
	body.WriteString("\n\nThe job this work was given:\n" + node.Brief)
	if method = strings.TrimSpace(method); method != "" {
		body.WriteString("\n\nThe working method it was held to:\n" + method)
	}
	// The worker's own last word leads the record, because it is the one part of
	// it written by the thing that was actually there. It is a witness statement
	// and is labelled as one: everything in it that the diff does not corroborate
	// is a claim, not a fact, and this prompt has already said which of the two
	// may be repeated.
	if said = strings.TrimSpace(said); said != "" {
		body.WriteString("\n\nWhat the agent that did the work said when it finished. " +
			"It was there and nothing else in this record was, so it is where the " +
			"reasoning comes from — but it is a statement rather than evidence, and " +
			"the diff below is what settles it:\n" + said)
	}
	if rows := outcome.Account.Lines(); len(rows) > 0 {
		body.WriteString("\n\nWhat the work changed and what it ran to check itself:\n" +
			strings.Join(rows, "\n"))
	}
	if len(outcome.Baseline) > 0 {
		body.WriteString("\n\nWhat was ALREADY failing in this repository before the work began. " +
			"These are not this work's doing:\n" + strings.Join(outcome.Baseline, "\n"))
	}
	if patch := composePatch(outcome.Account, budget); patch != "" {
		body.WriteString("\n\n" + patch)
	}
	// Last, and framed as coverage rather than as a critique. A model handed
	// "a reviewer found these gaps" writes about the gaps; a model handed "the
	// account has to cover these" writes the account.
	if gaps = strings.TrimSpace(gaps); gaps != "" {
		body.WriteString("\n\nThe account has to cover these, which the previous one did not:\n" + gaps)
	}
	body.WriteString("\n\nWrite the account.")

	composeCtx := settings.Context(ctx, "compose")
	// Priced as part of what this deliverable cost, on the node that owns it,
	// exactly as the gate above it is. A composition that billed to the day's
	// overhead would make the path that replaces an engine run look free.
	composeCtx = provider.WithCall(composeCtx, provider.ClassExecLeaf)
	composeCtx = pool.WithSpendNode(composeCtx, node.ID)
	response, err := client.CompleteWithMessages(composeCtx, []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: composePrompt}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: body.String()}}},
	})
	if err != nil || response == nil {
		provider.Report(composeCtx, provider.VerdictProviderFailure)
		return Composition{}
	}
	text := strings.TrimSpace(response.Text())
	if text == "" {
		provider.Report(composeCtx, provider.VerdictSemanticFailure)
		return Composition{}
	}
	provider.Report(composeCtx, provider.VerdictUnverifiedSuccess)
	composition := Composition{Text: text, Model: workerModel}
	if model := provider.CallFrom(composeCtx).Model(); model != "" {
		composition.Model = model
	}
	composition.Usage.Calls = 1
	if usage := response.Usage; usage != nil {
		composition.Usage.PromptTokens = usage.PromptTokens
		composition.Usage.CompletionTokens = usage.CompletionTokens
		// Through the accessor, for plan.Usage.Add's reason: the two endpoint
		// families spell a cache read differently and reading only one of them
		// journals a warm prefix as a cold call.
		composition.Usage.CachedTokens = usage.CacheReadTokens()
		if usage.Cost != nil {
			composition.Usage.Cost = *usage.Cost
		}
	}
	return composition
}

// composePatch renders the change for the writer, clipped and saying so.
func composePatch(account *exec.Account, budget ctxbudget.Budget) string {
	if account == nil || strings.TrimSpace(account.Patch) == "" {
		return ""
	}
	body, err := os.ReadFile(account.Patch)
	if err != nil || strings.TrimSpace(string(body)) == "" {
		return ""
	}
	head := "The change itself, as the repository records it"
	if span := account.ChangeRange(); span != "" {
		head += " (" + span + ")"
	}
	head += ". This is the evidence, and it is the only thing here that can settle " +
		"why anything was changed:\n"
	clipped := clipUTF8Bytes(string(body), budget.Share(composePatchShare, gateShareTotal, composePatchBytes))
	if len(clipped) < len(body) {
		// Said out loud because the instruction above forbids inferring what the
		// record does not name, and a writer who did not know the diff was cut
		// would read its end as the end of the change.
		head += "(the first " + strconv.Itoa(len(clipped)) + " bytes of " + strconv.Itoa(len(body)) +
			" — the rest of the change is NOT shown here, so do not describe it as complete)\n"
	}
	return head + clipped
}
