package session

// THE PERSON NAMED A PROGRAM: what a proposal meets when the person's own
// message said which program codeaf carries should do the work.
//
// A PROMPT IS REMEMBERED EXACTLY AS OFTEN AS THE MODEL REMEMBERS IT. The
// hand-off page says a program the person asks for is used (delegate_door.go's
// [delegateFact]), and a model that has just read a long issue and decided it
// is a task writes the proposal it always writes, with no `via`. So the
// person's words are read here, in code, for the names of the programs this
// build carries, and two things follow from them that cost no prompt bytes:
//
//   - A proposal with no `via` is turned back ONCE for the message that named
//     a program, with a sentence saying which program was named and both ways
//     to answer it. The next proposal for that message passes as it is, which
//     is how "fix it with senior-dev" and "don't use senior-dev for this" both
//     come out right: which of the two the person said is read by the model,
//     from words code cannot weigh.
//   - A proposal whose `via` is the program the person named is never too
//     small. The spawn floor (spawnfloor.go) keeps a one-file fix in the
//     conversation, and a person who typed "fix this file with senior-dev" has
//     overruled it already, exactly as a person who typed `/task` has.
//
// ONLY WHERE A `via` COULD BE HONOURED. Inside a task, with no run road, or
// with no program carried, a `via` is refused anyway ([Agent.stageTask]), and a
// bounce there would be a round trip that ends where it began.

import (
	"slices"
	"strings"
)

// programNamedSentence is what a proposal with no `via` reads back when the
// person's message named a program. It is a result the turn goes on from, and
// it offers both answers, because only the model can tell "use it" from
// "don't use it" and from a word that was never meant as the program's name.
func programNamedSentence(name string) string {
	return "the person named " + name + ": if they want it to do this work, propose this again with `via: \"" + name + "\"`; " +
		"if they asked for it not to be used, or did not mean the program, propose it again unchanged"
}

// mayHandToProgram says a `via` on a proposal from this agent could be
// honoured: it is a conversation rather than a task, and the run road a
// program rides is linked. It is the one reading of that, asked by the
// refusal in [Agent.stageTask] and by the bounce below, so the two cannot
// disagree about where a program may be named.
func (a *Agent) mayHandToProgram() bool {
	return !a.config.InTask && chatRunEngine != nil
}

// programAskBounce is the once-per-message refusal of a proposal that left
// out the program the person named, or "" when this proposal is not turned
// back.
//
// ONCE IS COUNTED PER MESSAGE OF THE PERSON'S. [Agent.personSeq] numbers what
// they have typed, steering included, so a new message of theirs that names
// the program again earns one more bounce, and a woken turn, which types
// nothing, inherits the count of the message it is still answering. The check
// and the mark are made under one lock, so a batch of proposals staged side by
// side cannot both be the first.
func (a *Agent) programAskBounce(spec taskSpec) string {
	if spec.via != "" || !a.mayHandToProgram() || len(a.config.Delegates) == 0 {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.personSeq == 0 || a.programBounced == a.personSeq {
		return ""
	}
	name := a.config.programNamedIn(a.personAsk)
	if name == "" {
		return ""
	}
	a.programBounced = a.personSeq
	return programNamedSentence(name)
}

// askedForProgram says `via` names a program this build carries and the
// person's words named that same program, which is the one thing that lifts
// the spawn floor for a proposal ([Agent.refuseProposedTask]).
func (c Config) askedForProgram(asked, via string) bool {
	if via == "" || !slices.Contains(c.delegateNames(), via) {
		return false
	}
	return namesProgram(normalizedWords(asked), via)
}

// programNamedIn is the first program, by name, that the person's words name,
// or "" when they name none.
func (c Config) programNamedIn(asked string) string {
	words := normalizedWords(asked)
	for _, name := range c.delegateNames() {
		if namesProgram(words, name) {
			return name
		}
	}
	return ""
}

// namesProgram says the words hold a program's name the ways a person types
// it: in any case, as `/name`, and with spaces or with nothing where the name
// has hyphens ("senior dev", "seniordev"). [normalizedWords] has already
// lowered the case and split on every mark that is not a letter or a digit, so
// the name is a run of whole words, or those words written as one.
func namesProgram(words []string, name string) bool {
	parts := normalizedWords(name)
	if len(parts) == 0 {
		return false
	}
	joined := strings.Join(parts, "")
	for at := range words {
		if words[at] == joined || slices.Equal(words[at:min(at+len(parts), len(words))], parts) {
			return true
		}
	}
	return false
}
