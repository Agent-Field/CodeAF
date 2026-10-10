package head

import (
	"context"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/store"
)

// THE ANSWER SIDE OF THE DURABLE QUESTION QUEUE. A reply reaches a question here
// and is turned into what the question was for: a compiler askback continued,
// a surgery or class confirmation applied, a service or craft decision written
// down. Standing rules had their own half of this file — ratifying, retiming
// and approving a rule's firings, and the offer to keep watching with no
// terminal open — and it went with the v1 scheduler; a question an older build
// asked on a rule's behalf lapses in the resident before anything here sees it.

// craftContinueOption and craftStopOption are the two answers a craft run's
// money stop offers. The words are written here as well as where the question
// is minted because they are a wire form between two halves of the system, the
// same way every other option value in this file is — and because a decoder
// that imported its own encoder would only be able to read questions this
// binary happened to write.
const (
	craftContinueOption = "craft:continue:"
	craftStopOption     = "craft:stop:"
)

// answerAgentQuestion routes replies to the durable reverse-direction queue.
// An explicit QuestionSeq wins — every surface that knows which question is on
// screen carries it, and a reply that names its question can never hit another
// one. Without it the store applies the same no-intervening-user-turn recency
// rule as ordinary conversational askbacks, and aimAgentQuestion stands between
// that rule and a silent wrong answer when more than one question is open.
func (h *Head) answerAgentQuestion(ctx context.Context, user store.Message) (bool, error) {
	question, found, err := h.store.QuestionForAnswer(user.SessionID, user.Seq, user.QuestionSeq)
	if err != nil {
		return false, err
	}
	if !found {
		// Nothing is answerable, which is exactly the state the ambiguity ask
		// leaves behind: the reply it was asking about is itself an intervening
		// user turn for every question it named. So this is where the answer to
		// that ask is read, and nowhere else pays for the lookup.
		return h.answerQuestionChoice(ctx, user)
	}
	if user.QuestionSeq == 0 {
		aimed, asked, aimErr := h.aimAgentQuestion(user, question)
		if asked || aimErr != nil {
			return asked, aimErr
		}
		question = aimed
	}
	return h.resolveAgentQuestion(ctx, user, question, user.Body)
}

// resolveAgentQuestion settles one identified question with one body of words.
// The words are a parameter rather than user.Body because the turn that names
// which question was meant is not the turn that answered it.
func (h *Head) resolveAgentQuestion(ctx context.Context, user store.Message,
	question store.AgentQuestion, body string) (bool, error) {
	if question.Status == store.QuestionAnswered || question.Status == store.QuestionExpired {
		return true, nil
	}
	if isAskQuestion(question.Options) {
		// A categorized ask is a durable row so that the meta loop can count it,
		// not so that the gates can apply it. This path applies answers — it would
		// resolve the row, post "Got it", and end the turn, leaving the loop that
		// asked the question never told what came back. So: settle the row for the
		// measurement, then decline, and the reply travels on to the loop with the
		// question beside it.
		if err := h.settleLearnedAsk(question.Seq, question.Options, body, user.Seq); err != nil {
			return false, err
		}
		// One categorized ask is settled HERE rather than declined, and it is the
		// only one: a yes to the split offer moves the person's window, and a
		// window move is not something a model can be asked to say. The turn ends
		// with it — their pivot is reposted into the new room and answered there
		// by the ordinary poll, so a reply in this room would land behind them.
		if splitAnswered(question, body) {
			split, err := h.settleThreadSplit(user)
			if err != nil {
				return false, err
			}
			if split {
				return true, nil
			}
		}
		return false, nil
	}
	answer := strings.TrimSpace(body)
	if option, selected := selectQuestionOption(body, question.Options); selected {
		answer = strings.TrimSpace(option.Label)
		if answer == "" {
			answer = strings.TrimSpace(option.Value)
		}
		if handled, err := h.answerCraftBudgetQuestion(user, question.Seq, option); handled {
			return true, err
		}
		if err := h.store.ResolveQuestion(question.Seq, store.QuestionAnswered, answer, user.Seq); err != nil {
			return true, err
		}
		return true, h.applyAgentQuestionOption(ctx, user, question, option)
	}
	if answer == "" {
		return true, h.postAgent(user.SessionID, "Tell me what you want me to use for that question.", 0)
	}
	if err := h.store.ResolveQuestion(question.Seq, store.QuestionAnswered, answer, user.Seq); err != nil {
		return true, err
	}
	if question.OriginCommandSeq != 0 {
		return true, h.continueAgentCompilerQuestion(user, question, answer)
	}
	return true, h.postAgent(user.SessionID, "Got it — I’ll use that.", 0)
}

func (h *Head) applyAgentQuestionOption(ctx context.Context, user store.Message, question store.AgentQuestion, option store.QuestionOption) error {
	if handled, err := h.applyRedirectOption(ctx, user, option); handled {
		return err
	}
	if action, kind, target, instruction, ok := decodeSurgeryOption(option.Value); ok {
		if handled, err := h.applyClassOption(user, action, kind, target, instruction); handled {
			return err
		}
		switch action {
		case "apply":
			return h.resolveSurgery(user, kind, target, instruction, true)
		case "keep":
			return h.postAgent(user.SessionID, "Keeping it as-is.", 0)
		}
	}
	// Only the hygiene nudge acts from the durable queue. A leaf's promotion
	// consent is read back by the waiting runner, not applied here.
	if parts := strings.Split(option.Value, ":"); len(parts) == 3 && parts[0] == "service" &&
		strings.HasPrefix(parts[1], "hygiene-") {
		if handled, err := h.applyServiceOption(user, option); handled {
			return err
		}
	}
	answer := compilerAnswerFromOption(option)
	if question.OriginCommandSeq != 0 {
		return h.continueAgentCompilerQuestion(user, question, answer)
	}
	return h.postAgent(user.SessionID, "Got it — I’ll use that.", 0)
}

// compilerAnswerFromOption is the exact words a chosen option sends back to
// the compiler. A value the label merely wraps — "moonshotai/kimi-k2" inside
// "use moonshotai/kimi-k2" — is the precise form and wins, so resolution is
// exact rather than re-parsed out of a sentence. Anywhere else the value is an
// internal code and the label is the answer a person would have typed.
func compilerAnswerFromOption(option store.QuestionOption) string {
	label := strings.TrimSpace(option.Label)
	value := strings.TrimSpace(option.Value)
	if value != "" && (label == "" || strings.Contains(strings.ToLower(label), strings.ToLower(value))) {
		return value
	}
	return label
}

func (h *Head) continueAgentCompilerQuestion(user store.Message, question store.AgentQuestion, answer string) error {
	source, found, err := h.store.CommandBySeq(question.OriginCommandSeq)
	if err != nil {
		return err
	}
	if !found || source.Kind != store.CommandSplice || strings.TrimSpace(answer) == "" {
		return h.postAgent(user.SessionID, "Tell me which option you want, or answer in your own words.", 0)
	}
	instruction := SpliceCompilerAnswer(source.Instruction, answer)
	command, err := h.store.RequestCommand(store.Command{
		SessionID: user.SessionID, Kind: store.CommandSplice, Instruction: instruction,
		// The answer joins the ASK, which is what it is an answer to. The
		// conversation the original ask came out of travels beside it, still in
		// its own field: a continuation that dropped it would plan the second
		// half of the job with less than the first half had.
		Context: source.Context,
		// The answer continues the original ask, and what the user attached to
		// it is part of that ask. Dropping the files here is how a question
		// about a PDF turns into a job that never sees it.
		Attachments: append([]string(nil), source.Attachments...),
	})
	if err != nil {
		return err
	}
	return h.postAgent(user.SessionID, "Got it — proceeding with that choice.", command.Seq)
}

func (h *Head) answerPendingQuestion(ctx context.Context, user store.Message) (bool, error) {
	question, pending, err := h.store.PendingQuestion(user.SessionID, user.Seq)
	if err != nil || !pending {
		return false, err
	}
	if isAskQuestion(question.Options) {
		// The loop asked it, so the loop settles it. Declining here is what sends
		// the message on with both halves — the question and the choice — in the
		// thread the loop is about to read, which is the only place the answer
		// means anything.
		//
		// A categorized ask carries a durable row as well, and that row is the
		// only thing the meta loop can count: an ask nobody settles teaches the
		// gate nothing, so it would go on asking a question the person has now
		// answered the same way a dozen times. Settle the row here and still hand
		// the message on — recording the answer is a measurement, not a reply.
		return false, h.settleLearnedAsk(question.QuestionSeq, question.Options, user.Body, user.Seq)
	}
	option, selected := selectQuestionOption(user.Body, question.Options)
	if selected {
		return true, h.applyQuestionOption(ctx, user, question, option)
	}
	if question.CommandSeq == 0 {
		return false, nil
	}
	return true, h.continueCompilerQuestion(user, question, strings.TrimSpace(user.Body))
}

func selectQuestionOption(reply string, options []store.QuestionOption) (store.QuestionOption, bool) {
	normalized := strings.ToLower(strings.Trim(strings.TrimSpace(reply), " .,!?:;\t\n\r"))
	if number, err := strconv.Atoi(normalized); err == nil && number > 0 && number <= len(options) {
		return options[number-1], true
	}
	for _, option := range options {
		if normalized == strings.ToLower(strings.TrimSpace(option.Label)) ||
			(option.Value != "" && normalized == strings.ToLower(strings.TrimSpace(option.Value))) {
			return option, true
		}
	}
	for _, option := range options {
		label := strings.ToLower(strings.TrimSpace(option.Label))
		value := strings.ToLower(strings.TrimSpace(option.Value))
		if affirmativeRailReply(normalized) &&
			(strings.HasPrefix(label, "yes") || strings.HasPrefix(value, craftContinueOption)) {
			return option, true
		}
		if negativeReply(normalized) &&
			(strings.HasPrefix(value, craftStopOption) ||
				strings.HasPrefix(label, "keep ") || strings.Contains(value, "surgery:keep:")) {
			return option, true
		}
	}
	return store.QuestionOption{}, false
}

func negativeReply(reply string) bool {
	switch reply {
	case "n", "no", "no thanks", "decline", "never":
		return true
	default:
		return false
	}
}

func (h *Head) applyQuestionOption(ctx context.Context, user store.Message, question store.Message, option store.QuestionOption) error {
	if handled, err := h.applyServiceOption(user, option); handled {
		return err
	}
	if handled, err := h.applyRedirectOption(ctx, user, option); handled {
		return err
	}
	if action, kind, target, instruction, ok := decodeSurgeryOption(option.Value); ok {
		if handled, err := h.applyClassOption(user, action, kind, target, instruction); handled {
			return err
		}
		switch action {
		case "select":
			return h.resolveSurgery(user, kind, target, instruction, false)
		case "apply":
			return h.resolveSurgery(user, kind, target, instruction, true)
		case "keep":
			return h.postAgent(user.SessionID, "Keeping it as-is.", 0)
		}
	}
	return h.continueCompilerQuestion(user, question, compilerAnswerFromOption(option))
}

func (h *Head) continueCompilerQuestion(user store.Message, question store.Message, answer string) error {
	source, found, err := h.store.CommandBySeq(question.CommandSeq)
	if err != nil {
		return err
	}
	if !found || source.Kind != store.CommandSplice || strings.TrimSpace(answer) == "" {
		return h.postAgent(user.SessionID, "Tell me which option you want, or answer in your own words.", 0)
	}
	instruction := SpliceCompilerAnswer(source.Instruction, answer)
	command, err := h.store.RequestCommand(store.Command{
		SessionID: user.SessionID, Kind: store.CommandSplice, Instruction: instruction,
		// Same law on the conversational path: the continuation is the same ask
		// carrying the same conversation and the same files.
		Context:     source.Context,
		Attachments: append([]string(nil), source.Attachments...),
	})
	if err != nil {
		return err
	}
	return h.postAgent(user.SessionID, "Got it — proceeding with that choice.", command.Seq)
}

// answerCraftBudgetQuestion settles a craft run's money stop. The run itself
// applies the decision — it is the only thing that knows what it has spent and
// what it still has to do — so the whole job here is to write the choice down
// in a form the run can read without guessing: the option's own value becomes
// the question's durable resolution, and the run polls it exactly as a waiting
// leaf polls for service consent. Without this branch the answer fell through
// to "Got it — I'll use that", which acknowledged a decision nothing acted on.
func (h *Head) answerCraftBudgetQuestion(user store.Message, questionSeq int64, option store.QuestionOption) (bool, error) {
	value, keepGoing, ok := decodeCraftBudgetOption(option.Value)
	if !ok {
		return false, nil
	}
	if err := h.store.ResolveQuestion(questionSeq, store.QuestionAnswered, value, user.Seq); err != nil {
		return true, err
	}
	reply := "Okay — it'll deliver what already landed."
	if keepGoing {
		reply = "Keeping it going."
	}
	return true, h.postAgent(user.SessionID, reply, 0)
}

// decodeCraftBudgetOption reads one craft money answer, fail-closed like every
// other option decoder here: the run's own id namespace has to be there, or
// this is not a craft consent and must not be treated as one.
func decodeCraftBudgetOption(value string) (resolution string, keepGoing bool, ok bool) {
	value = strings.TrimSpace(value)
	var prefix string
	switch {
	case strings.HasPrefix(value, craftContinueOption):
		prefix, keepGoing = strings.TrimPrefix(value, craftContinueOption), true
	case strings.HasPrefix(value, craftStopOption):
		prefix, keepGoing = strings.TrimPrefix(value, craftStopOption), false
	default:
		return "", false, false
	}
	if strings.TrimSpace(prefix) == "" {
		return "", false, false
	}
	return value, keepGoing, true
}
