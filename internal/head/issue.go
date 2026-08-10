package head

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// One model call produces both the reply and the command it describes, so the
// sentence is always worded before the row exists. That ordering is forced, and
// on its own it is harmless — until the row is refused. Then the thread holds
// "it won't fire anymore" over a store where the rule is still active, and the
// person has no way to know. It is the worst thing the product can do: not a
// failure, a false claim about the world, in the one place they trust.
//
// issueRoutedCommand is the single door between a routed decision and the
// journal, and the law at that door is one line: the optimistic reply is posted
// on the far side of a journaled row, or it is never posted at all. Every kind
// passes through here, because every kind can be refused — a cancel with no
// target, a change to work that finished while the model was thinking, an
// instruction the model forgot to write. What replaces the reply is either a
// resolution (the head works out what was meant and journals THAT) or one plain
// sentence saying what it could not do and what would settle it.
func (h *Head) issueRoutedCommand(user store.Message, decision routeDecision) error {
	kind, reflex, ok := commandKind(decision.Command.Kind)
	if !ok {
		return h.postAgent(user.SessionID, unclearCommandReply, 0)
	}
	target := strings.TrimSpace(decision.Command.Target)
	instruction := strings.TrimSpace(decision.Command.Instruction)
	if instruction == "" {
		// The model stated an intention and left off the words for it. What the
		// command is about is the sentence the person typed, which is sitting
		// right here; the alternative is a refusal over a message nobody
		// misunderstood.
		instruction = strings.TrimSpace(user.Body)
	}
	if reflex {
		// A reflex is untargeted by definition and the store says so, so the
		// honest repair for a targeted one is to drop the field the model
		// should not have set.
		target = ""
		// The consequence gate is restated here rather than trusted from where
		// the decision was decoded. It holds there today, and it held there
		// only because a decision that failed validation could never reach the
		// journal at all — which is exactly the property this door has just
		// given up. A reflex whose words buy, send or delete something is not a
		// reflex; it is ordinary work a person gets to see coming, and the last
		// place that can still be true is the last place before the row exists.
		if consequenceGated(instruction) {
			reflex = false
		}
	}
	if !reflex && isSurgeryCommand(kind) {
		if target == "" {
			return h.resolveDescribedTarget(user, kind, instruction)
		}
		return h.resolveSurgery(user, kind, target, instruction, false)
	}
	if kind == store.CommandSplice && !reflex && target == "" {
		target = h.spliceContinuity(user)
	}
	if instruction == "" {
		return h.postAgent(user.SessionID, unclearCommandReply, 0)
	}
	command, err := h.store.RequestCommand(store.Command{
		SessionID:   user.SessionID,
		Kind:        kind,
		Reflex:      reflex,
		Fresh:       decision.Fresh,
		Target:      target,
		Instruction: instruction,
		Attachments: append([]string(nil), user.Attachments...),
	})
	if err != nil {
		return h.postAgent(user.SessionID, commandErrorReply, 0)
	}
	return h.postAgentFloor(user.SessionID, decision.Reply, command.Seq, decision.model, decision.parts())
}

// describedTarget is one thing a verb with no target could have meant: a job on
// the board, or a standing rule. The two are matched by machinery that already
// exists and answers the same question in its own half of the world; this only
// puts the two answers in one list, because the person asking "stand down the
// stretch reminder" did not say which half they were pointing at and should not
// have to.
type describedTarget struct {
	job  store.SurgeryTarget
	rule store.Charter
}

func (candidate describedTarget) isRule() bool { return candidate.rule.ID != "" }

// describedTargetCap bounds one question. It is the charter askback's cap for
// the same reason: past a handful, this is a list to search rather than a choice
// to make.
const describedTargetCap = charterAskbackCap

// resolveDescribedTarget answers a verb that knows what it wants to do and not
// what to do it to. The three outcomes are the three the design filter allows
// anywhere ambiguity shows up: act when there is one plausible thing, ask one
// short question when there are several, and say so plainly when there are
// none. Nothing here reads phrases — it counts candidates.
func (h *Head) resolveDescribedTarget(user store.Message, kind store.CommandKind, description string) error {
	candidates, err := h.describedTargets(kind, description)
	if err != nil {
		return err
	}
	switch len(candidates) {
	case 0:
		return h.postAgent(user.SessionID, noSuchTargetReply, 0)
	case 1:
		return h.applyDescribedTarget(user, kind, description, candidates[0])
	}
	return h.postDescribedChoice(user, kind, description, candidates)
}

// describedTargets is the union of the two candidate readers the head already
// trusts: the surgery search over live work, and the charter search over
// standing rules. A rule joins the list only for the verbs a rule can actually
// answer — the mapping below is the store's own table, not a guess — so a
// candidate offered here is always a candidate that can be journaled.
func (h *Head) describedTargets(kind store.CommandKind, description string) ([]describedTarget, error) {
	lower := strings.ToLower(strings.TrimSpace(description))
	jobs, err := h.surgeryMatches(surgeryIntent{
		Kind: kind, Reference: surgeryReference(lower),
		IncludeLeaves: kind == store.CommandAmend || kind == store.CommandReprioritize ||
			kind == store.CommandRestart,
	})
	if err != nil {
		return nil, err
	}
	if len(jobs) > describedTargetCap {
		jobs = jobs[:describedTargetCap]
	}
	candidates := make([]describedTarget, 0, len(jobs)+describedTargetCap)
	for _, job := range jobs {
		candidates = append(candidates, describedTarget{job: job})
	}
	if _, ok := charterTransition(kind); !ok {
		return candidates, nil
	}
	rules, err := h.charterCandidates(charterReference(lower, ""))
	if err != nil {
		return nil, err
	}
	for _, rule := range rules {
		candidates = append(candidates, describedTarget{rule: rule})
	}
	return candidates, nil
}

// charterTransition maps a verb aimed at work onto the durable transition the
// same verb means for a standing rule. Only these two exist: standing a rule
// down is retiring it, and holding it is pausing it. A verb with no entry here
// simply cannot be about a rule, and the candidate list says so by leaving
// rules out rather than by offering one it would have to refuse.
func charterTransition(kind store.CommandKind) (store.CommandKind, bool) {
	switch kind {
	case store.CommandCancel:
		return store.CommandCharterRetire, true
	case store.CommandPause:
		return store.CommandCharterPause, true
	default:
		return "", false
	}
}

func (h *Head) applyDescribedTarget(user store.Message, kind store.CommandKind,
	description string, candidate describedTarget) error {
	if candidate.isRule() {
		charterKind, ok := charterTransition(kind)
		if !ok {
			return h.postAgent(user.SessionID, noSuchTargetReply, 0)
		}
		return h.requestCharterCommand(user, charterKind, candidate.rule.ID, description)
	}
	return h.resolveSurgery(user, kind, candidate.job.Node.ID, description, false)
}

// postDescribedChoice is the ordinary numbered askback, over a list that happens
// to hold both kinds of thing. Both option shapes below are the ones the answer
// path already decodes — a surgery selection and a charter transition — so an
// answer to this question lands exactly where an answer to the older questions
// lands, and nothing new had to be invented to carry it.
func (h *Head) postDescribedChoice(user store.Message, kind store.CommandKind,
	description string, candidates []describedTarget) error {
	options := make([]store.QuestionOption, 0, describedTargetCap)
	rules, jobs := 0, 0
	for _, candidate := range shortlist(candidates) {
		if candidate.isRule() {
			rules++
			charterKind, ok := charterTransition(kind)
			if !ok {
				continue
			}
			options = append(options, store.QuestionOption{
				Label: firstLine(candidate.rule.Invariant),
				Value: "charter:" + charterOptionAction(charterKind) + ":" + candidate.rule.ID,
			})
			continue
		}
		jobs++
		options = append(options, store.QuestionOption{
			Label: surgeryTargetLabel(candidate.job.Node),
			Hint:  surgeryTargetHint(candidate.job),
			Value: encodeSurgeryOption("select", kind, candidate.job.Node.ID, description),
		})
	}
	if len(options) == 0 {
		return h.postAgent(user.SessionID, noSuchTargetReply, 0)
	}
	return h.postQuestion(user.SessionID, describedChoicePrompt(rules, jobs), 0, options)
}

// shortlist cuts a candidate list down to one question's worth by taking from
// the two halves in turn rather than from the front. The two halves are ranked
// by different machinery over different corpora, so their scores mean nothing
// to each other; taking the front would let four live jobs bury the standing
// rule the person was pointing at, and the rule is the case this exists for.
func shortlist(candidates []describedTarget) []describedTarget {
	if len(candidates) <= describedTargetCap {
		return candidates
	}
	var jobs, rules []describedTarget
	for _, candidate := range candidates {
		if candidate.isRule() {
			rules = append(rules, candidate)
			continue
		}
		jobs = append(jobs, candidate)
	}
	shortlisted := make([]describedTarget, 0, describedTargetCap)
	for index := 0; len(shortlisted) < describedTargetCap; index++ {
		taken := false
		if index < len(jobs) {
			shortlisted = append(shortlisted, jobs[index])
			taken = true
		}
		if index < len(rules) && len(shortlisted) < describedTargetCap {
			shortlisted = append(shortlisted, rules[index])
			taken = true
		}
		if !taken {
			break
		}
	}
	return shortlisted
}

// describedChoicePrompt asks in the words of whatever the candidates turned out
// to be. The mixed case is the honest one: the head genuinely does not know
// whether the person meant a job or a rule, and pretending otherwise in the
// question would be the first wrong step.
func describedChoicePrompt(rules, jobs int) string {
	switch {
	case jobs == 0:
		return "Which rule do you mean?"
	case rules == 0:
		return "Which job do you mean?"
	default:
		return "Which one do you mean?"
	}
}
