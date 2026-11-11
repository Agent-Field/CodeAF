package appwire

import "strings"

// asks.answer_secret is the one door a credential crosses this wire by.
//
// IT IS A METHOD OF ITS OWN SO A WIRE TRACE CAN KNOW IT BY NAME. The app keeps
// a trace of every line it sends (UI2_WIRE_LOG) and takes a credential out of
// a line by the method that carries it, before the line reaches the file. An
// answer's ordinary `text` is evidence worth keeping and must stay readable,
// so a key sent as asks.answer text would be a key in that file; sent here,
// the trace drops `secret` without having to know which asks are secret.
//
// AND IT ANSWERS ONLY AN ASK THAT SAYS IT TAKES ONE ([Ask.Secret]). A secret
// sent at an ordinary question would become that question's words, and the
// words of an ordinary question go to the model.
const MethodAnswerSecret = "asks.answer_secret"

// SecretAnswerer answers an open ask whose answer is a credential with what the
// person typed into its masked box.
type SecretAnswerer interface {
	AnswerAskSecret(agent, id, secret string) error
}

func init() {
	methods[MethodAnswerSecret] = typed(func(e Engine, p struct {
		Agent  string `json:"agent"`
		ID     string `json:"id"`
		Secret string `json:"secret"`
	}) (*Empty, error) {
		s, ok := e.(SecretAnswerer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", MethodAnswerSecret)
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.ID) == "" {
			return nil, Errorf(CodeBadParams, "%s needs an agent and an id", MethodAnswerSecret)
		}
		if strings.TrimSpace(p.Secret) == "" {
			return nil, Errorf(CodeBadParams, "%s needs the secret; to say no, answer the ask's Decline option", MethodAnswerSecret)
		}
		return done(s.AnswerAskSecret(p.Agent, p.ID, p.Secret))
	})
}
