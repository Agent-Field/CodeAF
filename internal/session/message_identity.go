package session

// ensureUserIdentity runs only where a NEW user record lands. Replayed legacy
// messages remain unknown; authored notes are not relabelled as user requests.
// It copies the small presentation mark, never the immutable content allocation.
func ensureUserIdentity(user userMessage) userMessage {
	if user.authored {
		return user
	}
	if user.person != nil && user.person.Composed != nil && user.person.Composed.MessageID != "" {
		return user
	}
	id, err := newMessageID()
	if err != nil {
		return user
	}
	mark := messagePresentation{Audience: audiencePerson}
	if user.person != nil {
		mark = *user.person
	}
	composed := ComposedMark{MessageID: id}
	if mark.Composed != nil {
		composed = *mark.Composed
		composed.MessageID = id
	}
	// No delivery is invented for old submission doors: only the explicit
	// composed road knows whether actual pictures or descriptions were sent.
	mark.Composed = &composed
	user.person = &mark
	return user
}
