package head

import (
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
)

// The head's asks, as parts rather than as prose carrying a payload.
//
// 13.3's first bug, from the producing side. The head has two ways to ask: a
// conversational askback that lives on a message alone — "which job do you
// mean?", "when should I do it?", the ask tool's numbered options — and a
// durable question that goes through the agent-question lifecycle, which is
// every consent gate. Both used to render their choices twice: once as the
// typed options column that nothing read, and once as a JSON blob inside the
// message body that the old chat recovered with a brace scanner. The moment a
// renderer drew the journal honestly, the blob was on screen as words the head
// appeared to have said.
//
// The durable half is fixed in the store, at the one function every producer's
// question reaches (surfaceQuestion). This file is the conversational half, and
// it is the same contract: the body says what a person would say, the parts say
// what a renderer needs, and the options stay exactly where they already were.

// postQuestion posts one conversational askback.
//
// There is no lifecycle row behind it, which is the whole difference between
// this and a consent gate: the answer comes back as an ordinary message and the
// party that asked is the party that reads it. So the question part carries
// seq 0 and says so, rather than pointing at a row that does not exist.
//
// The body is humane — the prompt, then one numbered line per option — because
// nothing in a conversational askback needs the payload's two extra facts: they
// are all choose questions, none preselects an answer, and all of them accept
// free text. The existing chat parses that spelling at full fidelity, so it
// loses nothing at all by the change (11.1).
func (h *Head) postQuestion(sessionID, prompt string, commandSeq int64, options []store.QuestionOption) error {
	_, err := thread.Post(h.store, store.Message{
		SessionID:  sessionID,
		Role:       store.RoleAgent,
		Body:       store.HumaneQuestionBody(prompt, options),
		CommandSeq: commandSeq,
		Options:    options,
		Parts: store.QuestionParts(prompt, store.QuestionPart{
			Kind: store.QuestionChoose,
			// Conservative by law (9.4): an askback the head minted is the
			// person's to answer. Nothing here is ever informational, so nothing
			// here ever says it is.
			Class:     store.QuestionConsent,
			AllowFree: true,
		}),
	})
	return err
}

// askConfig is the one place a durable question's drawing contract is stated.
//
// Every consent gate in this package built the same three-field config inline,
// which meant three copies of a decision — how it is drawn, what stands if
// nobody answers, whether free text is accepted — that has to agree for the
// gate to mean anything. It agrees here instead.
func askConfig(kind store.QuestionKind, category store.QuestionCategory,
	fallback string, allowFree bool) store.QuestionConfig {
	return store.QuestionConfig{
		Kind: kind, Category: category, Default: fallback, AllowFree: &allowFree,
	}
}

// askBody is the body a durable question is journaled with. It goes through the
// store's own chooser rather than straight to the payload encoder: a question
// that preselects an answer or refuses free text keeps the payload, because the
// numbered spelling cannot carry either and both are consent semantics the
// existing chat must not lose; anything else stops carrying a payload at all.
func askBody(prompt string, options []store.QuestionOption, config store.QuestionConfig) string {
	return store.QuestionBodyFor(prompt, options, config)
}
