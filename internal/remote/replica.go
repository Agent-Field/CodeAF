package remote

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/session"
)

// replica.go is THE SURFACE'S COPY OF WHAT THE ENGINE KNOWS ABOUT ITSELF.
//
// THE DEFECT IT ENDS. Every fact a status line draws used to be a question:
// [Agent.Model], [Agent.Title], [Agent.Usage], [Agent.ContextTokens] and
// [Agent.ReasoningFor] were each one frame out and one frame back. At home that
// is a lock; over `--host devbox` it is a round trip on an ssh pipe, taken from
// the update loop — and internal/tui3's view.go asks the reasoning level on
// EVERY FRAME it draws, which made the repaint rate of a terminal in London a
// function of the round-trip time to a machine in Frankfurt. A frame is not
// allowed to wait on a network.
//
// SO THE FACTS COME DOWN UNASKED. The engine states them at the door
// ([Welcome.Facts]) and again whenever one moves ([Session.announce], on the
// "facts" frame), and everything above becomes a memory read of this struct.
// What still goes UP is intent: a message, a keystroke, an answer — the things
// the far end genuinely cannot know without being told.
//
// STALENESS IS BOUNDED BY WHAT MOVED IT, not by a clock. There is no polling
// here and no expiry: a replica is right until the engine says otherwise, and
// the engine says otherwise at every moment one of these fields changes. A
// connection that has dropped leaves the last stated set standing, which is the
// honest picture — the conversation is not moving, so neither is the row.

// replica is the fact set this surface answers from.
type replica struct {
	mu sync.Mutex
	// rev is the highest [FactsPush.Rev] taken. A push at or below it is DROPPED
	// rather than applied, which is the whole of the ordering law: two facts
	// that move in the same instant race to the writer on the engine, and a late
	// push applied over a newer one would make a live row go backwards.
	rev   uint64
	facts session.Facts
}

// fill takes the set a welcome arrived with, REPLACING whatever was held.
//
// It resets the revision rather than comparing against it, because a welcome is
// not a later statement about the same conversation — it is the first statement
// about whichever conversation the engine now has open. A /new or a /resume
// swaps the session under this client (client.go's swap), and a rev kept across
// that swap would make the new conversation's first push look like old news.
//
// A NIL PUSH CHANGES NOTHING. An engine that stated no facts is not an engine
// whose model is the empty string, and a replica emptied by one would draw a
// conversation that has no model and has spent nothing.
func (r *replica) fill(push *FactsPush) {
	if push == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rev, r.facts = push.Rev, push.Facts
}

// take applies one "facts" frame, newest wins and nothing else lands.
//
// A payload that will not decode is DROPPED rather than fatal, for the reason
// [stream.push] drops an unreadable event: one bad line is one lost statement,
// the next one is complete (a push is never a delta), and the framing was
// chosen so a torn write costs a line and not the stream.
func (r *replica) take(payload json.RawMessage) bool {
	if len(payload) == 0 {
		return false
	}
	var push FactsPush
	if err := json.Unmarshal(payload, &push); err != nil {
		return false
	}
	return r.takePush(push)
}

// takePush is [replica.take] for a push that has already been decoded, and it
// answers WHETHER THIS SET LANDED.
//
// The answer is not decoration. The naming lane carries its own fact set so a
// name and the facts it belongs to cannot be read out of order (client.go), and
// "this push was refused as old news" is exactly the sentence that must also
// refuse the name riding with it: a title minted for the conversation this
// surface was in a moment ago is older than the welcome of the one it is in
// now, and drawing it would put the previous conversation's name on this tab.
func (r *replica) takePush(push FactsPush) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if push.Rev <= r.rev {
		return false
	}
	r.rev, r.facts = push.Rev, push.Facts
	return true
}

// read is the whole set as it stands.
func (r *replica) read() session.Facts {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.facts
}

// setModel and setLevel are THE OPTIMISTIC HALF, and the only two writes this
// side ever makes.
//
// A person switching model in the picker presses one key and looks at the
// status line in the same instant. The engine's own push is on its way — the
// setter announces (server.go) — but "on its way" is a round trip, and a
// segment that took a round trip to change would read as a key that did not
// work. So the replica moves first and the push confirms it.
//
// IT CANNOT DRIFT, and that is what makes the optimism safe rather than a
// second authority: the engine's push carries a higher revision and lands over
// the top of whatever was assumed here, so an assumption the far end refused
// survives exactly as long as the round trip. These two write UNDER the current
// revision and never mint one, for the same reason — the number belongs to the
// machine that owns the fact.
func (r *replica) setModel(model string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.facts.Model = model
}

// setThinking is that same optimism for the conversation's rung, and it is
// written with THE ENGINE'S OWN ANSWER rather than with the word that was asked
// for (effort.go's [Agent.SetConversationEffort] reads it back).
//
// It has to be read back rather than assumed, because a rung set here is not
// always the rung that wins: a level dialled onto the model itself outranks the
// conversation's (internal/effort's Resolve), and a dial that showed the word a
// person pressed while the machine ran at another one would be the exact defect
// the ladder was written to end. The engine's push lands over the top of this at
// a higher revision, as it does for the model.
func (r *replica) setThinking(rung string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.facts.Thinking = rung
}

// setApproval is the same optimism for the conversation's posture on the tool
// gate, written with the engine's own answer for [replica.setThinking]'s reason
// (approval.go's [Agent.SetApprovalPosture] reads it back).
func (r *replica) setApproval(posture string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.facts.Approval = posture
}

// referPlace and removePlace are the same optimism for the folders a person
// attaches, and they are here for the same reason: the folder indicator is drawn
// in the frame that follows the keystroke, and a chip that took a round trip to
// appear would read as a pick that did not land.
//
// THE ANSWER THEY WRITE IS ALREADY THE ENGINE'S. Unlike the two above, these run
// only after the far end has accepted — [Agent.ReferPlace] fails before reaching
// here, and the ref it writes is the root-snapped path the engine sent back — so
// what is optimistic is the ORDER of the set and not the fact of it. The push
// that the engine's own announce is already sending lands over the top with a
// higher revision, exactly as it does for the model.
func (r *replica) referPlace(ref session.PlaceRef) {
	if strings.TrimSpace(ref.Path) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	places := make([]session.PlaceRef, 0, len(r.facts.Places)+1)
	places = append(places, ref)
	for _, place := range r.facts.Places {
		if place.Path != ref.Path {
			places = append(places, place)
		}
	}
	r.facts.Places = places
}

// setSkills writes the attachment a skill door just answered, so the chip this
// window draws next is the set the engine now holds, before the push that
// states it to every other window arrives. Absence is stored as absence.
func (r *replica) setSkills(names []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(names) == 0 {
		r.facts.Skills = nil
		return
	}
	r.facts.Skills = append([]string(nil), names...)
}

// removePlace drops a row by the path THE CALLER NAMED, which may not be the
// path the engine holds: a person removing `~/code/repo/internal` is removing
// the repository the engine snapped that to. A miss here costs nothing and is
// not worth a second copy of the snapping rule on this side — the engine's push
// is already on its way and carries the set as it truly stands.
func (r *replica) removePlace(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := make([]session.PlaceRef, 0, len(r.facts.Places))
	for _, place := range r.facts.Places {
		if place.Path != path {
			kept = append(kept, place)
		}
	}
	if len(kept) == 0 {
		// Absence is stored as absence, for [replica.setLevel]'s reason.
		r.facts.Places = nil
		return
	}
	r.facts.Places = kept
}

func (r *replica) setLevel(model, level string) {
	key := session.ReasoningKey(model)
	if key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Absence is stored as absence, exactly as internal/session stores it: a map
	// holding empty entries would answer "this model has a level" for every model
	// anybody ever cycled back to off.
	if level == "" {
		delete(r.facts.Reasoning, key)
		return
	}
	if r.facts.Reasoning == nil {
		r.facts.Reasoning = make(map[string]string, 2)
	}
	r.facts.Reasoning[key] = level
}

// Facts is what this connection last heard the engine say about itself. It is a
// memory read and it never touches the wire.
//
// A DOOR THAT WANTS THE WHOLE SET WANTS THIS ONE. The five getters on [Agent]
// are the tui3.Agent interface's own shape and each answers one field off this
// same value; anything else asking about a remote conversation should ask here,
// because a caller that asked four getters would be reading four independent
// photographs of a set that is published whole.
func (c *Client) Facts() session.Facts { return c.facts.read() }
