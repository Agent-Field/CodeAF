package main

import "github.com/Agent-Field/codeaf/internal/appwire"

// AnswerAskSecret is asks.answer_secret: what the person typed into an ask's
// masked box, answered through the same door as every other answer. The
// engine's lane decides where the value goes — the model's own question keeps
// it in a private file and tells the model the path (session's
// secret_request.go), a connection's question hands it to that connection —
// and this refuses an ask that never asked for a secret, because the words of
// an ordinary question go to the model.
func (b *appBackend) AnswerAskSecret(agent, id, secret string) error {
	a, err := b.findCrewOwner(agent)
	if err != nil {
		return err
	}
	open, takesSecret := false, false
	b.locked(func() {
		if a.live == nil {
			return
		}
		q, found := a.live.questions[id]
		open, takesSecret = found, found && q.SecretAnswer()
	})
	if !open {
		return appwire.Errorf(appwire.CodeNotFound, "the ask %s is not open; it may have been answered elsewhere", id)
	}
	if !takesSecret {
		return appwire.Errorf(appwire.CodeBadParams, "the ask %s does not take a secret", id)
	}
	return b.answerAskScope(agent, id, "", secret, "")
}
