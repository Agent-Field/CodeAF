package session

// facts.go is the SMALL SET OF FACTS A FRAME READS, gathered in one place.
//
// A surface sitting in front of a local agent asks these one at a time and each
// answer costs a lock: the model, the name the conversation gave itself, what
// has been spent, what the conversation weighs, how hard each model is being
// asked to think. That is the right shape at home, where a lock is nanoseconds.
//
// It is the WRONG shape over a connection. `aforge chat --host devbox` puts an
// ssh pipe between the frame and every one of those questions, and a status
// line that asks five of them is five round trips per repaint — which is the
// defect this file exists to end. A [Facts] is the whole set as ONE value, so
// the engine can hand it over unasked and the surface can read it from memory
// (internal/remote's replica).
//
// NOTHING HERE IS DERIVED AND NOTHING HERE IS CACHED. Every field is copied
// straight off the agent at the moment it is read, which is what makes a Facts
// a photograph rather than a second authority — the engine machine stays the
// authority on every one of these (docs/REMOTE.md, Decision 6) and this is only
// how the picture travels.

// Facts is what a conversation says about itself in one value.
//
// EVERY FIELD IS A FACT A FRAME DRAWS, and nothing else is here: the transcript
// is not (it is large, and it is asked for once), the workspace is not (the
// welcome carries it and it never changes), the standing items are not (they
// live on their own beat). The rule for adding a field is the one that put
// these five here — a frame or a keystroke reads it, so waiting on the wire for
// it is a terminal that has stopped repainting.
type Facts struct {
	// Model is the model the next request will use ([Agent.Model]).
	Model string `json:"model,omitempty"`
	// Title is the name the session gave itself ([Agent.Title]), and empty for
	// a conversation that has not earned one yet.
	Title string `json:"title,omitempty"`
	// Spent is the session's running total ([Agent.Usage]).
	Spent Usage `json:"spent,omitzero"`
	// ContextTokens is what the conversation weighs right now
	// ([Agent.ContextTokens]).
	ContextTokens int `json:"contextTokens,omitempty"`
	// Reasoning is the level held for each model id anybody has dialled, keyed
	// the way [ReasoningKey] folds an id.
	//
	// IT IS THE WHOLE MAP AND NOT THE ONE LEVEL IN USE, because the question a
	// surface asks is per-model and not per-session: a picker draws a row for a
	// model nobody has switched to and names the level waiting on it, and ctrl+t
	// on that row reads the level back before cycling it. A photograph holding
	// only the current model's level would leave both of those asking over the
	// wire — which is the whole thing this type is here to stop.
	//
	// A model with no level set has NO ENTRY, never an empty one: the agent
	// stores absence as absence, and so does this.
	Reasoning map[string]string `json:"reasoning,omitempty"`
}

// LevelFor is the reasoning level held for one model id, and "" for a model
// nobody has dialled. It folds the id exactly as the agent's own map does, so a
// level set from a picker row is found again by a `/model <slug>` typed in
// another case.
func (f Facts) LevelFor(model string) string {
	if len(f.Reasoning) == 0 {
		return ""
	}
	return f.Reasoning[ReasoningKey(model)]
}

// ReasoningKey folds a model id the way the level map is keyed. It is exported
// because a REPLICA of that map lives on another machine (internal/remote) and
// two spellings of one folding rule is a level that is set under one key and
// looked up under another.
func ReasoningKey(model string) string { return reasoningKey(model) }

// FactSource is anything that can answer the five questions a [Facts] is made
// of. [Agent] satisfies it, and so does the slice of an agent an engine serves
// across a connection (internal/remote's WrappedAgent) — which is the point:
// ONE BUILDER fills a Facts, on either side of the seam, so the value a surface
// reads and the value a test builds cannot drift apart.
type FactSource interface {
	Model() string
	Title() string
	Usage() Usage
	ContextTokens() int
	ReasoningLevels() map[string]string
}

// FactsOf reads the whole set off one source.
//
// THE FIVE READS ARE NOT ATOMIC WITH EACH OTHER, and they do not need to be:
// each field is taken under the agent's own lock, so every one of them is a
// value that was true, and the set is published again the next time any of them
// moves. A frame drawn from a photograph in which the usage is one instant
// newer than the title is a frame nobody can tell from a correct one; a lock
// held across all five would be the conversation waiting on a status line.
func FactsOf(source FactSource) Facts {
	if source == nil {
		return Facts{}
	}
	return Facts{
		Model:         source.Model(),
		Title:         source.Title(),
		Spent:         source.Usage(),
		ContextTokens: source.ContextTokens(),
		Reasoning:     source.ReasoningLevels(),
	}
}

// Facts is this conversation's own photograph of itself.
func (a *Agent) Facts() Facts { return FactsOf(a) }

// ReasoningLevels is every model id somebody has dialled and the level it holds,
// as a plain map a wire can carry.
//
// IT HANDS BACK A COPY. The agent's own map is written under its lock by every
// picker keystroke, and a caller ranging over the live one while somebody
// presses ctrl+t is a race — so the copy is the contract, not an optimization.
// Nothing is here for a model set back to off: absence is stored as absence.
func (a *Agent) ReasoningLevels() map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.reasoning) == 0 {
		return nil
	}
	levels := make(map[string]string, len(a.reasoning))
	for model, rung := range a.reasoning {
		levels[model] = rung.String()
	}
	return levels
}
