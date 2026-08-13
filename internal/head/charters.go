package head

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

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
		if handled, err := h.answerCharterFiringQuestion(user, question.Seq, option); handled {
			return true, err
		}
		if handled, err := h.answerCraftBudgetQuestion(user, question.Seq, option); handled {
			return true, err
		}
		if err := h.store.ResolveQuestion(question.Seq, store.QuestionAnswered, answer, user.Seq); err != nil {
			return true, err
		}
		return true, h.applyAgentQuestionOption(ctx, user, question, option)
	}
	if standingWatchQuestion(question.Options) {
		// Unattended presence is decided once and never asked again, so only an
		// explicit choice may cross it. Free text stays ordinary conversation
		// rather than silently spending the single question.
		return false, nil
	}
	if charterID, ok := charterQuestionID(question.Options); ok {
		if cadence := extractCadence(body); cadence != "" {
			if err := h.store.ResolveQuestion(question.Seq, store.QuestionAnswered, cadence, user.Seq); err != nil {
				return true, err
			}
			return true, h.requestCharterCommand(user, store.CommandCharterCadence, charterID, cadence)
		}
		// Preserve the existing charter behavior: unrelated free text remains
		// ordinary conversation rather than accidentally ratifying spend.
		return false, nil
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
	parts := strings.Split(option.Value, ":")
	if len(parts) == 2 && parts[0] == "standing-watch" {
		switch parts[1] {
		case "enable":
			return h.requestStandingWatchCommand(user, store.CommandStandingWatchEnable, option.Label)
		case "decline":
			return h.requestStandingWatchCommand(user, store.CommandStandingWatchDecline, option.Label)
		}
	}
	if len(parts) >= 3 && parts[0] == "charter" {
		id := parts[2]
		switch parts[1] {
		case "ratify":
			return h.requestCharterCommand(user, store.CommandCharterRatify, id, option.Label)
		case "pause":
			return h.requestCharterCommand(user, store.CommandCharterPause, id, option.Label)
		case "retire":
			return h.requestCharterCommand(user, store.CommandCharterRetire, id, option.Label)
		case "once":
			return h.requestCharterCommand(user, store.CommandCharterOnce, id, option.Label)
		case "cadence":
			if len(parts) > 3 {
				return h.requestCharterCommand(user, store.CommandCharterCadence, id,
					strings.Join(parts[3:], ":"))
			}
			return h.askForCadence(user.SessionID, id)
		case "wording":
			if len(parts) > 3 {
				return h.requestCharterCommand(user, store.CommandCharterWording, id,
					strings.Join(parts[3:], ":"))
			}
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
		if question.QuestionSeq != 0 {
			if handled, err := h.answerCharterFiringQuestion(user, question.QuestionSeq, option); handled {
				return true, err
			}
		}
		return true, h.applyQuestionOption(ctx, user, question, option)
	}

	if charterID, ok := charterQuestionID(question.Options); ok {
		if cadence := extractCadence(user.Body); cadence != "" {
			return true, h.requestCharterCommand(user, store.CommandCharterCadence, charterID, cadence)
		}
		// Ratification keeps free text available to the conversational router;
		// only an explicit choice or cadence phrase crosses a durable transition.
		return false, nil
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
			(strings.HasPrefix(label, "yes") || strings.Contains(value, ":ratify:") ||
				strings.Contains(value, ":fire:") || strings.HasPrefix(value, craftContinueOption)) {
			return option, true
		}
		if negativeReply(normalized) &&
			(strings.Contains(label, "not standing") || strings.Contains(value, ":once:") ||
				strings.Contains(value, ":decline:") || strings.HasPrefix(value, craftStopOption) ||
				strings.HasPrefix(label, "only while") || value == "standing-watch:decline" ||
				strings.HasPrefix(label, "keep ") || strings.Contains(value, "surgery:keep:")) {
			return option, true
		}
	}
	return store.QuestionOption{}, false
}

func negativeReply(reply string) bool {
	switch reply {
	case "n", "no", "no thanks", "decline", "never", "not standing", "once", "just once":
		return true
	default:
		return false
	}
}

func standingWatchQuestion(options []store.QuestionOption) bool {
	for _, option := range options {
		if strings.HasPrefix(strings.TrimSpace(option.Value), "standing-watch:") {
			return true
		}
	}
	return false
}

func charterQuestionID(options []store.QuestionOption) (string, bool) {
	for _, option := range options {
		parts := strings.Split(option.Value, ":")
		if len(parts) >= 3 && parts[0] == "charter" && strings.TrimSpace(parts[2]) != "" {
			return parts[2], true
		}
	}
	return "", false
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
	parts := strings.Split(option.Value, ":")
	if len(parts) == 2 && parts[0] == "standing-watch" {
		switch parts[1] {
		case "enable":
			return h.requestStandingWatchCommand(user, store.CommandStandingWatchEnable, option.Label)
		case "decline":
			return h.requestStandingWatchCommand(user, store.CommandStandingWatchDecline, option.Label)
		}
	}
	if len(parts) >= 3 && parts[0] == "charter" {
		id := parts[2]
		switch parts[1] {
		case "ratify":
			return h.requestCharterCommand(user, store.CommandCharterRatify, id, option.Label)
		case "pause":
			return h.requestCharterCommand(user, store.CommandCharterPause, id, option.Label)
		case "retire":
			return h.requestCharterCommand(user, store.CommandCharterRetire, id, option.Label)
		case "once":
			return h.requestCharterCommand(user, store.CommandCharterOnce, id, option.Label)
		case "cadence":
			if len(parts) > 3 {
				return h.requestCharterCommand(user, store.CommandCharterCadence, id,
					strings.Join(parts[3:], ":"))
			}
			return h.askForCadence(user.SessionID, id)
		case "wording":
			if len(parts) > 3 {
				return h.requestCharterCommand(user, store.CommandCharterWording, id,
					strings.Join(parts[3:], ":"))
			}
		case "fire":
			if len(parts) < 4 {
				return h.postAgent(user.SessionID, "That firing approval is stale.", 0)
			}
			return h.requestCharterCommand(user, store.CommandCharterFire, id, "wake:"+parts[3])
		case "decline":
			if len(parts) < 4 {
				return h.postAgent(user.SessionID, "That firing proposal is stale.", 0)
			}
			return h.requestCharterCommand(user, store.CommandCharterDecline, id, "wake:"+parts[3])
		case "always":
			if len(parts) < 4 {
				return h.postAgent(user.SessionID, "That firing approval is stale.", 0)
			}
			return h.requestCharterCommand(user, store.CommandCharterAlways, id, "wake:"+parts[3])
		case "never":
			if len(parts) < 4 {
				return h.postAgent(user.SessionID, "That firing approval is stale.", 0)
			}
			return h.requestCharterCommand(user, store.CommandCharterNever, id, "wake:"+parts[3])
		case "probation":
			return h.requestCharterCommand(user, store.CommandCharterProbation, id, "back to asking")
		}
	}
	return h.continueCompilerQuestion(user, question, compilerAnswerFromOption(option))
}

func (h *Head) requestStandingWatchCommand(user store.Message, kind store.CommandKind, instruction string) error {
	_, err := h.store.RequestCommand(store.Command{
		SessionID: user.SessionID, Kind: kind, Instruction: instruction,
	})
	return err
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
		// carrying the same files.
		Attachments: append([]string(nil), source.Attachments...),
	})
	if err != nil {
		return err
	}
	return h.postAgent(user.SessionID, "Got it — proceeding with that choice.", command.Seq)
}

func (h *Head) askForCadence(sessionID, charterID string) error {
	return h.postQuestion(sessionID, "When should I do it?", 0, []store.QuestionOption{
		{Label: "every hour", Value: "charter:cadence:" + charterID + ":hourly"},
		{Label: "every day", Value: "charter:cadence:" + charterID + ":daily"},
		{Label: "every week", Value: "charter:cadence:" + charterID + ":weekly"},
	})
}

func (h *Head) requestCharterCommand(user store.Message, kind store.CommandKind, id, instruction string) error {
	command, err := h.store.RequestCommand(store.Command{
		SessionID: user.SessionID, Kind: kind, Target: id, Instruction: instruction,
	})
	if err != nil {
		return err
	}
	return h.acknowledgeCharterCommand(user, command)
}

func (h *Head) answerCharterFiringQuestion(user store.Message, questionSeq int64, option store.QuestionOption) (bool, error) {
	kind, id, instruction, ok := charterFiringCommand(option)
	if !ok {
		return false, nil
	}
	resolution := strings.TrimSpace(option.Label)
	if resolution == "" {
		resolution = strings.TrimSpace(option.Value)
	}
	command, requested, err := h.store.ResolveQuestionWithCommand(questionSeq, resolution, user.Seq, store.Command{
		SessionID: user.SessionID, Kind: kind, Target: id, Instruction: instruction,
	})
	if err != nil || !requested {
		return true, err
	}
	return true, h.acknowledgeCharterCommand(user, command)
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

func charterFiringCommand(option store.QuestionOption) (store.CommandKind, string, string, bool) {
	parts := strings.Split(option.Value, ":")
	if len(parts) != 4 || parts[0] != "charter" || strings.TrimSpace(parts[2]) == "" || strings.TrimSpace(parts[3]) == "" {
		return "", "", "", false
	}
	var kind store.CommandKind
	switch parts[1] {
	case "fire":
		kind = store.CommandCharterFire
	case "decline":
		kind = store.CommandCharterDecline
	case "always":
		kind = store.CommandCharterAlways
	case "never":
		kind = store.CommandCharterNever
	default:
		return "", "", "", false
	}
	return kind, parts[2], "wake:" + parts[3], true
}

func (h *Head) acknowledgeCharterCommand(user store.Message, command store.Command) error {
	reply := "Updating that standing rule."
	switch command.Kind {
	case store.CommandCharterRatify:
		reply = "Standing it up."
	case store.CommandCharterPause:
		reply = "Pausing that rule."
	case store.CommandCharterRetire:
		reply = "Retiring that rule."
	case store.CommandCharterOnce:
		reply = "Keeping it one-time."
	case store.CommandCharterCadence:
		reply = "Changing when that runs."
	case store.CommandCharterWording:
		reply = "Changing what it says."
	case store.CommandCharterFire:
		reply = "Approved for this time."
	case store.CommandCharterDecline:
		reply = "Okay — I won’t do this firing. I’ll ask again next time."
	case store.CommandCharterAlways:
		reply = "I’ll take this one and handle future firings on my own."
	case store.CommandCharterNever:
		reply = "I won’t do that, and I’m pausing that rule."
	case store.CommandCharterProbation:
		reply = "I’ll ask before firing again."
	}
	return h.postAgent(user.SessionID, reply, command.Seq)
}

// charterIntent is one recognized instruction about a standing rule: which
// durable transition it asks for, how the user pointed at the rule, and the new
// words — a rhythm or a message — the transition carries.
type charterIntent struct {
	Kind      store.CommandKind
	Reference string
	Cadence   string
	Wording   string
}

// charterAskbackCap bounds the rules one askback offers. A question with more
// rows than this is a list to be searched rather than a choice to be made, and
// the freshest handful is what a sentence with no description can plausibly
// mean.
const charterAskbackCap = 4

// charterReferenceWindow is how far back a rule stays a plausible referent for
// an utterance that describes nothing — "change it to Tuesday". A rule nobody
// has touched or run in a month is not what "it" means.
const charterReferenceWindow = 30 * 24 * time.Hour

// charterCandidates resolves what the user pointed at. A description is matched
// against the rules themselves; a sentence that describes nothing — "change it
// to Tuesday", the one the audit found unanswerable — falls back to the rules
// recently enough alive to be what "it" means, newest first.
//
// The one thing it must not do is guess between equals. Two plausible rules is
// one plain question, which is the same answer the design filter gives
// everywhere else ambiguity shows up.
func (h *Head) charterCandidates(reference string) ([]store.Charter, error) {
	matches, err := h.store.SearchActiveCharters(reference)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(reference) == "" && len(matches) > 1 {
		matches = freshCharters(matches, time.Now())
	}
	if len(matches) > charterAskbackCap {
		matches = matches[:charterAskbackCap]
	}
	return matches, nil
}

// freshCharters keeps the rules that have been created, woken, or checked
// inside the reference window, newest first. If nothing is fresh the whole set
// comes back rather than nothing: an old rule is still a rule, and the question
// that follows is the same one.
func freshCharters(charters []store.Charter, now time.Time) []store.Charter {
	fresh := make([]store.Charter, 0, len(charters))
	for _, charter := range charters {
		if now.Sub(charterFreshness(charter)) <= charterReferenceWindow {
			fresh = append(fresh, charter)
		}
	}
	if len(fresh) == 0 {
		return charters
	}
	sort.SliceStable(fresh, func(i, j int) bool {
		return charterFreshness(fresh[i]).After(charterFreshness(fresh[j]))
	})
	return fresh
}

func charterFreshness(charter store.Charter) time.Time {
	latest := charter.CreatedAt
	for _, at := range []time.Time{charter.LastWake, charter.LastChecked} {
		if at.After(latest) {
			latest = at
		}
	}
	return latest
}

func charterOptionValue(intent charterIntent, id string) string {
	value := "charter:" + charterOptionAction(intent.Kind) + ":" + id
	switch intent.Kind {
	case store.CommandCharterCadence:
		return value + ":" + intent.Cadence
	case store.CommandCharterWording:
		return value + ":" + intent.Wording
	}
	return value
}

// charterWordingCue is how a person says "keep the rule, change what it says".
// The words after it are the new message, verbatim.
const charterWordingCue = " to say "

// charterVerbs open an edit to an existing rule. They are the verbs people
// actually reach for when they move a reminder — "push the reminder to 8pm",
// "move it to tuesday" — and before they were here those sentences were claimed
// by node surgery, resolved to nothing, and answered with a list of jobs.
var charterVerbs = []string{"make ", "change ", "set ", "move ", "push ",
	"switch ", "shift ", "reword ", "rephrase "}

func charterManagement(message string) (charterIntent, bool) {
	trimmed := strings.TrimSpace(message)
	lower := strings.ToLower(trimmed)
	intent := charterIntent{}
	switch {
	case strings.Contains(lower, "back to asking"):
		intent.Kind = store.CommandCharterProbation
	case strings.Contains(lower, "stop watching") || strings.Contains(lower, "stop monitoring") ||
		strings.HasPrefix(lower, "retire "):
		intent.Kind = store.CommandCharterRetire
	case strings.HasPrefix(lower, "pause ") || strings.Contains(lower, " pause the "):
		intent.Kind = store.CommandCharterPause
	case charterVerbPresent(lower):
		// Wording is read before rhythm: "change the sunday reminder to say
		// water the plants" names a day and is not about the day.
		if cue := strings.Index(lower, charterWordingCue); cue >= 0 {
			intent.Wording = strings.TrimSpace(trimmed[cue+len(charterWordingCue):])
			if intent.Wording != "" {
				intent.Kind = store.CommandCharterWording
				intent.Reference = charterReference(lower[:cue], "")
				return intent, true
			}
		}
		if intent.Cadence = extractCadence(message); intent.Cadence != "" {
			intent.Kind = store.CommandCharterCadence
		}
	}
	if intent.Kind == "" {
		return charterIntent{}, false
	}
	intent.Reference = charterReference(lower, intent.Cadence)
	return intent, true
}

func charterVerbPresent(lower string) bool {
	for _, verb := range charterVerbs {
		if strings.HasPrefix(lower, verb) {
			return true
		}
	}
	return false
}

func charterReference(message, cadence string) string {
	if cadence != "" {
		message = strings.ReplaceAll(message, strings.ToLower(cadence), " ")
	}
	stop := map[string]bool{
		"please": true, "stop": true, "watching": true, "watch": true,
		"monitoring": true, "monitor": true, "retire": true, "pause": true,
		"make": true, "change": true, "set": true, "cadence": true,
		"move": true, "push": true, "switch": true, "shift": true,
		"reword": true, "rephrase": true, "my": true, "that": true,
		"back": true, "asking": true, "the": true, "it": true, "to": true,
	}
	var kept []string
	for _, word := range strings.FieldsFunc(message, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if !stop[word] {
			kept = append(kept, word)
		}
	}
	return strings.Join(kept, " ")
}

func charterOptionAction(kind store.CommandKind) string {
	switch kind {
	case store.CommandCharterPause:
		return "pause"
	case store.CommandCharterRetire:
		return "retire"
	case store.CommandCharterProbation:
		return "probation"
	case store.CommandCharterWording:
		return "wording"
	default:
		return "cadence"
	}
}

func managementInstruction(intent charterIntent) string {
	switch intent.Kind {
	case store.CommandCharterCadence:
		return intent.Cadence
	case store.CommandCharterWording:
		return intent.Wording
	}
	return string(intent.Kind)
}
