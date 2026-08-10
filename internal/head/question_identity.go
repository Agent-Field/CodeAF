package head

import (
	"context"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A reply carries the identity of the question it answers whenever the surface
// that took it knew which one was on screen — a clicked option row, a chosen
// card, a question picked out of the dock. This file is what happens when it
// does not: a bare "1" typed into the composer while two jobs are both waiting
// on an answer.
//
// The store's rule for that case is "the newest surfaced question wins", and
// with one question open it is exactly right. With two it is a coin toss
// wearing a rule, and the losing side of the toss is not a typo — it is a plan
// approved that nobody read, and a worker left blocked on a question that has
// been silently spent. So when more than one open question would accept the
// same reply, the head does what a person does: it asks, once, in plain words,
// and applies the answer it was already given to whichever one they name.
//
// Nothing here is a picker. There are no numbered options, no new surface and
// no mode: one sentence naming the two things, and the next sentence settles
// it. If that next sentence names neither, the question was not a choice
// between them and it goes on to ordinary conversation — the ask never repeats.
const questionChoiceTail = "Which of those is it for?"

// questionChoiceLabelBytes keeps each name in the ask short enough that two of
// them are still one readable sentence.
const questionChoiceLabelBytes = 64

// aimAgentQuestion decides which open question a reply with no explicit
// identity is for. It returns the question to answer, or reports that it asked
// the user instead — in which case the caller has already had its one visible
// reply and must stop.
//
// Only questions that would actually take this reply compete, which is the same
// test the resolution path applies a moment later: a standing-watch decision or
// an unratified charter lets ordinary words go by, so words those questions
// would ignore are not a choice between them. A reply exactly one open question
// would take is aimed at that one even when a newer question sits on top —
// that is the evidence a person would use, and the newest-wins rule has no
// claim on it.
func (h *Head) aimAgentQuestion(user store.Message, newest store.AgentQuestion) (store.AgentQuestion, bool, error) {
	candidates, err := h.store.QuestionsForAnswer(user.SessionID, user.Seq)
	if err != nil || len(candidates) < 2 {
		return newest, false, err
	}
	// Words that are one question's own option and no other's are not ambiguous
	// at all, whichever question happens to be newest. "release/2.4" names the
	// branch question the way a person names it.
	if selecting := questionsSelecting(user.Body, candidates); len(selecting) == 1 {
		return selecting[0], false, nil
	}
	accepting := questionsAccepting(user.Body, candidates)
	switch len(accepting) {
	case 0:
		return newest, false, nil
	case 1:
		return accepting[0], false, nil
	}
	return newest, true, h.askWhichQuestion(user, accepting)
}

// answerQuestionChoice reads the reply to the ask above. Nothing is stored
// between the two turns: the ask is recognized by its own closing sentence, the
// answer it was carrying is the user turn in front of it, and the candidates
// are re-derived from the journal exactly as they were derived the first time.
// A question that was settled in between simply drops out of the set.
func (h *Head) answerQuestionChoice(ctx context.Context, user store.Message) (bool, error) {
	recent, err := h.recentThread(user.SessionID, user.Seq)
	if err != nil || len(recent) == 0 {
		return false, err
	}
	ask := recent[len(recent)-1]
	if ask.Role != store.RoleAgent || !strings.HasSuffix(strings.TrimSpace(ask.Body), questionChoiceTail) {
		return false, nil
	}
	var original store.Message
	for index := len(recent) - 2; index >= 0; index-- {
		if recent[index].Role == store.RoleUser {
			original = recent[index]
			break
		}
	}
	if original.Seq == 0 {
		return false, nil
	}
	candidates, err := h.store.QuestionsForAnswer(user.SessionID, original.Seq)
	if err != nil {
		return false, err
	}
	ordered := questionsOldestFirst(questionsAccepting(original.Body, candidates))
	if len(ordered) < 2 {
		return false, nil
	}
	index, named := h.matchQuestionChoice(user.Body, ordered)
	if !named {
		return false, nil
	}
	return h.resolveAgentQuestion(ctx, user, ordered[index], original.Body)
}

// askWhichQuestion is the one short question, in the head's own voice, anchored
// to nothing so it reads as the front desk speaking rather than either job.
func (h *Head) askWhichQuestion(user store.Message, candidates []store.AgentQuestion) error {
	labels := h.questionChoiceLabels(questionsOldestFirst(candidates))
	body := "That could answer " + joinChoices(labels) + ". " + questionChoiceTail
	return h.postAgentFloor(user.SessionID, body, 0, "", nil)
}

func joinChoices(labels []string) string {
	switch len(labels) {
	case 0:
		return ""
	case 1:
		return labels[0]
	case 2:
		return labels[0] + " or " + labels[1]
	}
	return strings.Join(labels[:len(labels)-1], ", ") + ", or " + labels[len(labels)-1]
}

// questionChoiceLabels names each open question the way the user already saw
// it: the job's own title when a job asked, and otherwise the words of the
// question itself. Both are strings that are already on their screen.
func (h *Head) questionChoiceLabels(candidates []store.AgentQuestion) []string {
	labels := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		labels = append(labels, "“"+h.questionChoiceLabel(candidate)+"”")
	}
	return labels
}

func (h *Head) questionChoiceLabel(question store.AgentQuestion) string {
	if node := strings.TrimSpace(question.OriginNodeID); node != "" && h.store != nil {
		if found, ok, err := h.store.Node(node); err == nil && ok {
			if label := strings.TrimSpace(surgeryTargetLabel(found)); label != "" {
				return truncateBytes(label, questionChoiceLabelBytes)
			}
		}
	}
	return truncateBytes(firstLine(question.Text), questionChoiceLabelBytes)
}

// matchQuestionChoice reads which of the named questions the reply picked out.
// Two kinds of answer are understood because those are the two people give:
// the position in the sentence they were just shown ("the first one"), and the
// words of the thing itself ("the auth one"). Anything else is not a choice
// between them.
func (h *Head) matchQuestionChoice(reply string, candidates []store.AgentQuestion) (int, bool) {
	words := choiceWords(reply)
	if len(words) == 0 {
		return 0, false
	}
	if index, ok := ordinalChoice(words, len(candidates)); ok {
		return index, true
	}
	best, bestScore, tied := 0, 0, false
	for index, candidate := range candidates {
		score := 0
		for _, word := range choiceWords(h.questionChoiceLabel(candidate) + " " + firstLine(candidate.Text)) {
			if containsWord(words, word) {
				score++
			}
		}
		switch {
		case score > bestScore:
			best, bestScore, tied = index, score, false
		case score == bestScore && score > 0:
			tied = true
		}
	}
	if bestScore == 0 || tied {
		return 0, false
	}
	return best, true
}

func ordinalChoice(words []string, count int) (int, bool) {
	for _, word := range words {
		switch word {
		case "first", "1st", "former", "older", "earlier", "earliest", "original":
			return 0, true
		case "second", "2nd", "latter":
			if count > 1 {
				return 1, true
			}
		case "third", "3rd":
			if count > 2 {
				return 2, true
			}
		case "last", "latest", "newer", "newest", "later", "recent":
			return count - 1, true
		}
	}
	return 0, false
}

// choiceStopWords are the words both a question and its answer are made of, so
// their overlap says nothing about which question was meant.
var choiceStopWords = map[string]bool{
	"the": true, "one": true, "that": true, "this": true, "these": true, "those": true,
	"for": true, "about": true, "question": true, "questions": true, "ask": true,
	"asked": true, "answer": true, "and": true, "or": true, "with": true, "your": true,
	"you": true, "yours": true, "its": true, "was": true, "were": true, "are": true,
	"should": true, "would": true, "which": true, "what": true, "meant": true,
	"mean": true, "please": true, "thing": true, "want": true, "wanted": true,
}

func choiceWords(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	words := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) < 3 || choiceStopWords[field] {
			continue
		}
		words = append(words, field)
	}
	return words
}

func containsWord(words []string, want string) bool {
	for _, word := range words {
		if word == want {
			return true
		}
	}
	return false
}

func questionsSelecting(reply string, candidates []store.AgentQuestion) []store.AgentQuestion {
	selecting := make([]store.AgentQuestion, 0, len(candidates))
	for _, candidate := range candidates {
		if _, ok := selectQuestionOption(reply, candidate.Options); ok {
			selecting = append(selecting, candidate)
		}
	}
	return selecting
}

func questionsAccepting(reply string, candidates []store.AgentQuestion) []store.AgentQuestion {
	accepting := make([]store.AgentQuestion, 0, len(candidates))
	for _, candidate := range candidates {
		if questionAcceptsReply(reply, candidate) {
			accepting = append(accepting, candidate)
		}
	}
	return accepting
}

// questionAcceptsReply mirrors, exactly, what resolveAgentQuestion would do
// with these words. Anything looser would ask about a question the words could
// never have settled; anything tighter would let one be settled without the
// user ever being asked which.
func questionAcceptsReply(reply string, question store.AgentQuestion) bool {
	if _, selected := selectQuestionOption(reply, question.Options); selected {
		return true
	}
	if standingWatchQuestion(question.Options) {
		return false
	}
	if _, isCharter := charterQuestionID(question.Options); isCharter {
		return strings.TrimSpace(extractCadence(reply)) != ""
	}
	return strings.TrimSpace(reply) != ""
}

// questionsOldestFirst puts the candidates in the order the user met them, so
// "the first one" means the first one they were asked and the first one named
// in the sentence that asks them to choose.
func questionsOldestFirst(candidates []store.AgentQuestion) []store.AgentQuestion {
	ordered := make([]store.AgentQuestion, len(candidates))
	for index, candidate := range candidates {
		ordered[len(candidates)-1-index] = candidate
	}
	return ordered
}
