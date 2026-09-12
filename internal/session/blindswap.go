package session

// The transcript guard: what happens to the pictures in a conversation when the
// model swaps to one that cannot see them.
//
// SubmitImage's gate reads the model the next turn will ride, which closes the
// hole in one direction only. A picture attached to a model that could see it
// sits in the LIVE transcript as a base64 data URL and is re-sent on every step
// of every turn afterwards — so a person who attaches a screenshot and then
// switches to a model without eyes (/model, one keystroke) sends that data URL
// to a blind model on a turn where nobody attached anything and no gate ran.
// That is the 400 the gate exists to prevent, arriving from behind.
//
// So the SWAP is the seam, and the resume beside it. A model that can see is
// handed the conversation untouched. A model that cannot reads each picture's
// PLACEHOLDER — [journalPart.placeholderBecause], the replay sentence's shape
// with this seam's own honest reason ("this model cannot see images") — naming
// the path, because the path is the half a person can act on and the half the
// model can hand to view_image.
//
// THREE THINGS ARE DELIBERATE. The journal is never rewritten, by stub.go's
// law: the file is the RECORD, and the pictures in it are what a resume rebuilds
// from. The scrub is ONE-WAY — switching back to a seeing model does not restore
// the bytes — because the alternative is re-reading files off disk mid-turn,
// which is the second quiet place a changed file becomes a picture nobody
// attached. And a picture's journaled path is re-indexed onto its placeholder,
// so a surface drawing the conversation still names what was attached, exactly
// as it does for a replayed picture whose file went away.

import "github.com/Agent-Field/agentfield/sdk/go/ai"

// scrubBlindImagePartsLocked replaces this transcript's image parts with their
// placeholders when the given model cannot read them, with a.mu held. It is a
// no-op — not one allocation — for a model that can see and for the
// conversations, which are nearly all of them, that carry no pictures.
//
// IT SCRUBS ON A POSITIVE `BLIND` AND ON NOTHING ELSE ([ModelSight]). An
// unvouched model — a cold catalog, a failed fetch, a row that published no
// modalities — keeps its pictures. That asymmetry with the send gate, which
// refuses on anything but a positive `sees`, is the point: refusing to send
// costs a person one turn and a sentence, and scrubbing costs them the
// screenshot, for ever, with no way back. On 2026-09-11 the two shared one bool
// and the same model id answered `sees` off the disk cache and `blind` a minute
// later off a catalog fetch that had failed — which was enough to strip a
// conversation's pictures with nobody having swapped anything.
func (a *Agent) scrubBlindImagePartsLocked(model string) {
	if a.sightOf(model) != SightBlind {
		return
	}
	for index, message := range a.messages {
		if !hasImagePart(message) {
			continue
		}
		content := make([]ai.ContentPart, len(message.Content))
		for at, part := range message.Content {
			if part.Type != "image_url" || part.ImageURL == nil {
				content[at] = part
				continue
			}
			path := a.file.imagePath(part)
			placeholder := journalPart{Type: journalPartImage, Path: path}.placeholderBecause("this model cannot see images")
			a.file.rememberImagePath(placeholder, path)
			content[at] = placeholder
		}
		// A NEW message, never a write into the old content, for the reason
		// stub.go states at its own replacement: a request already in flight
		// holds a shallow copy of this message ([Agent.snapshot]), and editing
		// the parts underneath it would edit a request the provider is reading.
		a.messages[index] = ai.Message{
			Role:       message.Role,
			Content:    content,
			ToolCalls:  message.ToolCalls,
			ToolCallID: message.ToolCallID,
		}
	}
}

// hasImagePart is the cheap question asked of every message before anything is
// allocated: does this one carry a picture at all.
func hasImagePart(message ai.Message) bool {
	for _, part := range message.Content {
		if part.Type == "image_url" && part.ImageURL != nil {
			return true
		}
	}
	return false
}

// ── WHAT THIS BUILD KNOWS ABOUT A MODEL'S EYES ──────────────────────────────

// ModelSight is the answer to "can this model read a picture", in THREE states,
// because two were not enough to hold both decisions that ask.
//
// THE THIRD STATE IS THE ONE THAT MATTERS. A catalog that has not loaded, a
// fetch that failed, a model nobody published modalities for — all of them are
// "nobody has said", and for a whole release that was spelled the same way as
// "this model is blind". The send gate was right to refuse on it. The scrub was
// wrong to fire on it, and the scrub is the one that cannot be undone.
type ModelSight uint8

const (
	// SightUnknown is nobody having vouched either way. It REFUSES A SEND and
	// FORBIDS A SCRUB: neither act is safe on a guess, and they fail in opposite
	// directions, so the same ignorance answers them differently.
	SightUnknown ModelSight = iota
	// SightBlind is a model this build knows cannot read pictures.
	SightBlind
	// SightSees is a model this build knows can.
	SightSees
)

// SightOf is a yes-or-no oracle's answer as a sight, for a caller that really
// does know both ways — a fixture, a catalog row that carries modalities. A
// caller that might be ignorant must say [SightUnknown] itself rather than
// passing false, which is exactly the collapse this type exists to prevent.
func SightOf(sees bool) ModelSight {
	if sees {
		return SightSees
	}
	return SightBlind
}

// sightOf is what this session knows about one model, with [SightUnknown] for a
// session wired with no oracle at all.
func (a *Agent) sightOf(model string) ModelSight {
	if a.config.SeesImages == nil {
		return SightUnknown
	}
	return a.config.SeesImages(model)
}
