package session

import (
	"errors"
	"time"
)

// PhoneFixtureHead is the irreversible ask a test build opens without a model
// call. The live phone test waits for these exact words.
const PhoneFixtureHead = "Apply the selected change?"

// PhoneFixtureAllow is the first answer on that ask, spelled the way the phone
// draws the button.
const PhoneFixtureAllow = "Allow once"

// ParkFixtureConsent opens one durable permission ask and waits for its answer
// on the consent lane. No model is called. The question is the engine's own:
// OpenQuestions lists it, and ResolveQuestion is what closes it.
func (a *Agent) ParkFixtureConsent() error {
	q := Question{
		ID:     1,
		Kind:   QuestionConsent,
		Ask:    AskPermission,
		Form:   FormLine,
		Head:   PhoneFixtureHead,
		Reason: "the change cannot be taken back",
		Stakes: StakesIrreversible,
		Options: []AnswerOption{
			{Key: "1", Label: PhoneFixtureAllow},
			{Key: "3", Label: "deny", Safe: true},
		},
		Blocking: Blocking{Turn: true},
		Asked:    time.Now(),
	}
	if err := q.Check(a.Decisions()); err != nil {
		return err
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return errAgentClosed
	}
	if a.consent == nil {
		a.consent = make(map[uint64]chan consentAnswer, 1)
	}
	if _, waiting := a.consent[q.ID]; waiting {
		a.mu.Unlock()
		return errors.New("fixture ask is already open")
	}
	answers := make(chan consentAnswer, 1)
	a.consent[q.ID] = answers
	a.mu.Unlock()
	// The returned letGo is not deferred. Calling it would withdraw the ask.
	// The answer claims the words first; the channel is what the consent lane delivers to.
	_ = a.raiseQuestion(q, nil)
	go func() { <-answers }()
	return nil
}
