package knowledge

import (
	"context"
	"strings"

	"github.com/Agent-Field/codeaf/internal/provider"
)

// A PERSON'S DELIBERATE ADD NEVER WAITS ON LEARNING (owner decision 5,
// 2026-11-17).
//
// "Learn from chats and jobs" is the switch for what the Library takes on its
// own: the saved chats it reads, the turns it notices. Add to Library on a
// workstream file, and Keep in Library on a passage of a reply, are different:
// a person chose that one thing, so it is kept and written into the Library
// whatever the switch says. With learning off nothing else moves — no saved
// chat is read, and a compile admits only the source the person just added.
//
// What learning added stays marked as such ([LearnedFromChats]) so a person can
// see it came from their chats and remove it.

// KeptPassagePrefix starts the id of a passage a person kept from a saved
// reply with Keep in Library. Like a kept workstream file, it is a deliberate
// add and never something learning took on its own.
const KeptPassagePrefix = "desk:"

// WorkstreamFileSourcePrefix starts the id of a workstream file a person added
// to the Library ([KindWorkstreamFile]).
const WorkstreamFileSourcePrefix = "workstream-file:"

// DeliberateSource reports whether a source id is one a person added on
// purpose: a kept workstream file or a kept passage.
func DeliberateSource(id string) bool {
	return strings.HasPrefix(id, WorkstreamFileSourcePrefix) || strings.HasPrefix(id, KeptPassagePrefix)
}

// LearnedFromChats reports whether any of a page's evidence came from learning
// rather than from something a person added on purpose. The Library marks such
// a page "From your chats".
func LearnedFromChats(p Page) bool {
	learned := func(list []Evidence) bool {
		for _, e := range list {
			if e.Source != "" && !DeliberateSource(e.Source) {
				return true
			}
		}
		return false
	}
	if learned(p.Evidence) {
		return true
	}
	for _, s := range p.Sections {
		for _, para := range s.Paragraphs {
			if learned(para.Evidence) {
				return true
			}
		}
	}
	return false
}

type deliberateKey struct{}

// deliberate reports whether ctx carries a person's deliberate add, which a
// compile and its model calls run for even with learning off.
func deliberate(ctx context.Context) bool {
	on, _ := ctx.Value(deliberateKey{}).(bool)
	return on
}

// mayLearn is the one gate every compile and model step reads: learning is on,
// or this is a person's deliberate add.
func (v *Vault) mayLearn(ctx context.Context) bool {
	return v.Enabled() || deliberate(ctx)
}

// Admit keeps one source a person added on purpose, whatever the learning
// switch says. It only stores the words; [Vault.Write] turns them into pages.
// A source that is not deliberate (its id names no kept file or passage) is
// refused, so this door can never become a way round the switch.
func (v *Vault) Admit(source Source) error {
	if !DeliberateSource(source.ID) {
		return ErrDisabled
	}
	if err := validateCapture(source); err != nil {
		return err
	}
	if source.Kind == "" {
		source.Kind = "agent_synthesis"
		if source.Role == "user" {
			source.Kind = "user_statement"
		}
	}
	return v.capture(source)
}

// Write compiles what a person added under session into the Library now. It is
// scoped to that session's sources alone, so with learning off a compile never
// takes in a chat the person did not choose; with learning on it is the same
// compile the background build would have run, a little sooner.
func (v *Vault) Write(ctx context.Context, key, session string) error {
	ctx = context.WithValue(ctx, deliberateKey{}, true)
	ctx = WithOwnerSessions(provider.WithSession(ctx, session), []string{session})
	return v.Flush(ctx, key, true)
}
