package revision

// The one question that can end a run: is the request, exactly as the person
// wrote it, satisfied by what is in hand?
//
// NOTHING IN THIS SYSTEM ASKED IT. The headless door settles when every node is
// terminal; the growth governor judges the PLAN's own criterion; the gate names
// absences against a checklist. "Done" was when the plan ran out, never when the
// request was met — and the measured cost of that is an errand that was
// satisfied by its first leaf two minutes in and then ran into a 700-second wall
// twice out of two, spending more than 97% of its money after the answer already
// existed (2026-09-02, deepseek-v4-flash).
//
// IT IS ASKED AT THE MOMENT A ROUND WOULD OTHERWISE BE BOUGHT, AND NOWHERE
// ELSE. Not per turn, not per node, not on the happy path: at the gate about to
// buy a repair, and at the extension about to buy a remainder. Its cost is
// therefore bounded by the rounds it replaces — one call in place of a leaf —
// which is the whole reason it is affordable to ask at all.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// RequestMetWords is the receipt a run earns by having done what was asked. It
// is stated once because four readers spell it: the settlement sets it, the
// journal keeps it, the node's own record carries it, and the closing line a
// person reads is built out of it.
const RequestMetWords = "the request was met as stated"

// RequestQuestion is the ability to put that question, carried on a judgement
// so a door in another package can ask it without holding a model client. See
// Judgment.Request for why it is a func rather than four more parameters.
type RequestQuestion func(ctx context.Context) (met bool, receipt string, asked bool)

// requestMetPrompt decides one thing and is given no room to decide anything
// else.
//
// Every sentence after the first is a refusal of a way this question has
// already been answered wrongly by the readers around it. "Form is not
// substance" is the judge's own rule restated where it kept being broken — the
// measured errand's judge failed a correct final line for arriving after the
// sentence "The command completed. The final line printed was:", calling it "a
// report about the output, not the output itself". "Do not add what a careful
// engineer would also do" is the acceptance prompt's rule, and it is here for
// the identical reason: every requirement invented at this seam is a round
// bought against a request nobody made. And verification of finished work is
// named explicitly because the growth governor's spiral doctrine is exactly
// this failure one level up — checking it again is not the request, it is a
// second job.
//
// It is domain-neutral and carries no example. An example is a template, and a
// model handed one answers about the example.
const requestMetPrompt = `You decide whether a request, exactly as the person wrote it, has been satisfied by what is in hand.

You receive the verbatim request, the rules the person stated, the deliverable as it was produced, and the record of what the run changed.

Answer met only when every thing the request asked for is present in the deliverable or on disk as the record shows, and no stated rule was broken.

Form is not substance. An answer wrapped in a sentence, placed in a code block, or given under a heading is the answer. A different wording of the thing that was asked for is the thing that was asked for.

Do not add what a careful engineer would also do. Do not judge quality beyond what the request states. Do not treat verification of finished work as something missing: work that is done and not re-checked is done.

When it is not met, name the ONE thing the request asked for that is absent, quoting the request's own words for it. When it is met, that field is empty.

Answer with one bare JSON object and nothing else — no code fence around it and no sentence before or after it:
{"met": true, "missing": ""}`

var requestMetSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "met": {"type": "boolean"},
    "missing": {"type": "string"}
  },
  "required": ["met", "missing"],
  "additionalProperties": false
}`)

// requestMetDeliverableBytes is what the deliverable may spend. The request
// itself is never clipped — a question about whether the request was satisfied,
// asked over half a request, is a question about something the person never
// wrote — and the record clips itself through the gate's own budget.
const requestMetDeliverableBytes = 24 << 10

// RequestMet puts the question and reads one word back.
//
// asked is false wherever there was no answer to read — no client, no request,
// a call that failed, a reply that could not be parsed. THE FAIL-OPEN
// DIRECTION IS THE EXISTING PATH: a question nobody could answer must leave the
// run exactly where it was, buying the round it was going to buy, because the
// alternative is a delivery ended as satisfied on the strength of a provider
// timeout.
//
// words is the receipt on a yes — the same sentence wherever it is set — and on
// a no it is what the question found absent, in the request's own words. One
// return rather than two because exactly one of the two is ever true of an
// answer, and a caller holding both would have to decide which it was looking
// at from the boolean it already has.
func RequestMet(ctx context.Context, settings config.Config, client *pool.Client,
	node store.Node, grounds Grounds, deliverable string, evidence Evidence,
) (met bool, words string, asked bool) {
	request := strings.TrimSpace(grounds.Intent)
	if client == nil || request == "" || strings.TrimSpace(deliverable) == "" {
		return false, "", false
	}
	budget := bounds{}.budget(requestMetPrompt)
	askCtx := settings.Context(ctx, "gate")
	askCtx = provider.WithCall(askCtx, provider.ClassPlanAudit)
	// The same tag the gate's own verdict carries, because this is the gate
	// asking the last question it has, and a reader of the model-call log wants
	// the two beside each other.
	askCtx = provider.WithCallTag(askCtx, "gate")
	askCtx = pool.WithSpendNode(askCtx, node.ID)
	var answer struct {
		Met     bool   `json:"met"`
		Missing string `json:"missing"`
	}
	// The message order is the cache shape and is deliberate: the static prompt
	// first, then the request and its rules — fixed for the job's lifetime, so
	// two rounds of one job share the whole prefix — and only then the two
	// blocks that move.
	response, err := askVerdict(askCtx, client, []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: requestMetPrompt}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text",
			Text: "The request, verbatim:\n" + request + "\n\n" + statedRulesBlock(grounds, evidence)}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text",
			Text: "The deliverable as it was produced:\n" +
				clipUTF8Bytes(strings.TrimSpace(deliverable), requestMetDeliverableBytes)}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text",
			Text: "The record of what the run changed:\n" + requestMetRecord(evidence, budget)}}},
	}, requestMetSchema, &answer)
	if err != nil || response == nil {
		provider.Report(askCtx, provider.VerdictProviderFailure)
		return false, "", false
	}
	provider.Report(askCtx, provider.VerdictVerifiedSuccess)
	// A yes that names something missing is the model disagreeing with itself,
	// and the safe reading of a disagreement about whether to stop is that it
	// did not say stop. It is plan.Satisfied's rule at the other end of the same
	// question.
	if answer.Met && strings.TrimSpace(answer.Missing) != "" {
		answer.Met = false
	}
	if !answer.Met {
		return false, strings.TrimSpace(answer.Missing), true
	}
	return true, RequestMetWords, true
}

// requestMetRecord is what the run left behind, in the grammar the gate already
// renders it in. An unobserved run renders nothing, which reads as no claim —
// exactly as it does at the gate — and the question then turns on the
// deliverable alone, which is all a run that produced nothing has.
func requestMetRecord(evidence Evidence, budget ctxbudget.Budget) string {
	if block := strings.TrimSpace(evidence.block(budget)); block != "" {
		return block
	}
	return "Nothing was recorded about what the run did."
}

// statedRulesBlock is the rules the person stated, as a list apart from the
// prose they are written in.
//
// It is one function so that the change which reads a request's constraints off
// the record has one place to fill in, and until then it says what is true: the
// rules are in the request above it, which is the text this question is asked
// with. A block that promised a list and showed an empty one would be a model
// told the person stated no rules, over a request whose second sentence is one.
func statedRulesBlock(grounds Grounds, evidence Evidence) string {
	if rules := statedRules(grounds, evidence); len(rules) > 0 {
		return "The rules the person stated:\n- " + strings.Join(rules, "\n- ")
	}
	return "The rules the person stated are in the request itself; there is no separate list."
}

// statedRules is the list itself. Today nothing on the record carries one and
// this names none.
func statedRules(Grounds, Evidence) []string { return nil }

// ConstraintFinding says this verdict's gap is a rule the person stated being
// BROKEN, rather than something the request asked for being ABSENT.
//
// The question above may not be put of one, and the difference is not a
// nicety: a run that produced everything the request asked for in a way the
// request forbade has not met the request as stated, and a reader shown the
// deliverable and the record would say yes. Today no judgement carries its
// constraints and this is always false; the change that gives a judgement its
// constraints is the one line that fills it in.
func ConstraintFinding(Judgment) bool { return false }

// metExtension is the extension a request already satisfied earns: none, and a
// receipt saying why none was owed.
//
// It is the second of the two doors, and it exists for the callers that reach
// the extension without going through the gate's own caller. It asks NOTHING
// where the question has already been put for this judgement — a gate told "not
// met" one door earlier must not pay for the same answer on the way to the same
// conclusion — and nothing where the verdict cannot ask at all.
func metExtension(ctx context.Context, extension Extension, unmet Judgment) (Extension, bool) {
	if unmet.RequestAsked || unmet.Request == nil || ConstraintFinding(unmet) {
		return extension, false
	}
	met, receipt, asked := unmet.Request(ctx)
	if !asked || !met {
		return extension, false
	}
	// Refused carries the receipt because Refused is the field every reader
	// already spends to say why nothing more was started, and what is true here
	// is that nothing more was owed. Unclosed stays false: the gap did not
	// survive the door, it was answered.
	extension.Met, extension.Refused, extension.Unclosed, extension.Spliced = true, receipt, false, 0
	return extension, true
}

// RequestMetNotice is the one line a delivery's own record carries when the run
// ended because what was asked for is in hand.
//
// It sits exactly where a reservation would have been written and it is the
// same length, because the two are the same fact answered the two ways: this
// says the work is finished, GapHandover says what it is short of. A receipt
// nobody can read is a receipt that was never issued (FAILSAFE clause 3).
func RequestMetNotice(receipt string) string {
	return "I'm delivering this as finished — " + receipt + "."
}
