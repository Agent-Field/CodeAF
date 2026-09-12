package session

// The picture guard: what a model that cannot see is sent when the conversation
// in front of it carries pictures.
//
// SubmitImage's gate reads the model the work is on, which closes the hole in
// one direction only. A picture attached to a model that could see it sits in
// the LIVE transcript as a base64 data URL and is re-sent on every request
// afterwards — so a person who attaches a screenshot and then moves onto a model
// without eyes (/model, one keystroke; or a rescue hopping off a rate limit,
// which nobody asked for at all) sends that data URL to a blind model on a
// request where nobody attached anything and no gate ran. That is the 400 the
// gate exists to prevent, arriving from behind.
//
// SO THE SUBSTITUTION IS ON THE WIRE AND THE BYTES ARE NEVER TOUCHED. The
// request assembled for a blind model carries each picture's PLACEHOLDER —
// [journalPart.placeholderBecause], the replay sentence's shape with this seam's
// own honest reason ("this model cannot see images") — naming the path, because
// the path is the half a person can act on and the half the model can hand to
// view_image. The transcript keeps the picture.
//
// IT WAS A ONE-WAY REWRITE OF THE TRANSCRIPT UNTIL 2026-09-12, AND THAT WAS THE
// DEFECT, not the implementation of it. Three ways to lose a screenshot for
// good, all of them arriving with nobody having asked for anything:
//
//   - a RESCUE HOP. A model stops answering, the ladder moves the step onto a
//     fallback that happens to be blind, and a rewrite at the move destroys the
//     pictures of a conversation whose own model can see them perfectly well.
//   - an ORACLE THAT CHANGES ITS MIND. The same id answered `sees` off a warm
//     cache and `blind` a minute later off a catalog fetch that had failed.
//   - AN ORDERING. A picture queued while a turn runs lands in the transcript
//     after the rewrite for that model has already happened.
//
// A remembered "I already did this one" ([Agent.scrubbedFor]) patched the second
// and made the third worse. A function of the request cannot have any of them:
// it is asked again for every request, it is the same answer for the same model,
// and hopping back to a model that sees shows it the pictures again.
//
// THE JOURNAL IS NEVER REWRITTEN EITHER, by stub.go's law: the file is the
// RECORD, and the pictures in it are what a resume rebuilds from.

import "github.com/Agent-Field/agentfield/sdk/go/ai"

// blindSafe is the messages as they may be SENT to this model: unchanged for a
// model that can see and for a model nobody has vouched for, and with every
// picture replaced by its placeholder for a model known to be blind.
//
// IT IS A PURE FUNCTION OF THE MODEL AND THE MESSAGES, which is the whole design
// (see the header). Nothing is remembered, nothing is written back, and asking
// twice gives the same answer.
//
// IT SUBSTITUTES ON A POSITIVE `BLIND` AND ON NOTHING ELSE. An unvouched model —
// a cold catalog, a failed fetch, a row that published no modalities — is sent
// the conversation as it stands. That asymmetry with the send gate, which
// refuses on anything but a positive `sees`, is deliberate: refusing to send
// costs a person one turn and a sentence, and the alternative here is sending a
// picture to a model that can read it perfectly well but published nothing about
// itself. The gate is the cautious one because it is the reversible one.
//
// It allocates nothing for the conversations, which are nearly all of them, that
// carry no pictures at all.
func (a *Agent) blindSafe(messages []ai.Message, sees, known bool) []ai.Message {
	if sees || !known {
		return messages
	}
	var out []ai.Message
	for index, message := range messages {
		if !hasImagePart(message) {
			continue
		}
		if out == nil {
			out = make([]ai.Message, len(messages))
			copy(out, messages)
		}
		content := make([]ai.ContentPart, len(message.Content))
		for at, part := range message.Content {
			if part.Type != "image_url" || part.ImageURL == nil {
				content[at] = part
				continue
			}
			content[at] = journalPart{Type: journalPartImage, Path: a.file.imagePath(part)}.
				placeholderBecause("this model cannot see images")
		}
		// A NEW message, never a write into the old content, for the reason
		// stub.go states at its own replacement: the transcript keeps these
		// messages and a request already in flight holds a shallow copy of them,
		// so editing the parts underneath would edit what somebody else is
		// reading.
		out[index] = ai.Message{
			Role:       message.Role,
			Content:    content,
			ToolCalls:  message.ToolCalls,
			ToolCallID: message.ToolCallID,
		}
	}
	if out == nil {
		return messages
	}
	return out
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

// seesImages is what this session knows about one model: whether it can read a
// picture, and whether anybody has said either way.
//
// THE SECOND RETURN IS THE ONE THAT MATTERS, and it is the shape the facts
// beside it already use ([Config.ReasoningProfile], [Config.SupportsParameter],
// catalog.go's PriceNow). A catalog that has not loaded, a fetch that failed, a
// model nobody published modalities for — all of them are "nobody has said", and
// for a whole release that was spelled the same way as "this model is blind".
// The send gate was right to refuse on it. The substitution would have been
// wrong to fire on it, and when the substitution rewrote the transcript it was
// the one that could not be undone.
//
// A session wired with no oracle at all knows nothing, which refuses every send
// and hides no picture.
func (a *Agent) seesImages(model string) (sees, known bool) {
	if a.config.SeesImages == nil {
		return false, false
	}
	return a.config.SeesImages(model)
}
