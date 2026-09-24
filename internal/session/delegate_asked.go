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
//     to answer it. The next proposal the model makes after reading that
//     passes as it is, which is how "fix it with senior-dev" and "don't use
//     senior-dev for this" both come out right: which of the two the person
//     said is read by the model, from words code cannot weigh.
//   - A proposal whose `via` is the program the person named is never too
//     small. The spawn floor (spawnfloor.go) keeps a one-file fix in the
//     conversation, and a person who typed "fix this file with senior-dev" has
//     overruled it already, exactly as a person who typed `/task` has.
//
// AND THE PERSON'S WORDS ARE EVERY MESSAGE OF THE TURN, not the newest one
// ([programsHeard]): a steer typed while the model reads the code names
// nothing, and it does not unsay the program the turn opened by asking for.
//
// ONLY WHERE A `via` COULD BE HONOURED. Inside a task, with no run road, or
// with no program carried, a `via` is refused anyway ([Agent.stageTask]), and a
// bounce there would be a round trip that ends where it began.

import (
	"context"
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

// programAskBounce is the once-per-message refusal of a proposal that left out
// the program the person named, or nil when this proposal is not turned back.
//
// ONCE IS COUNTED PER MESSAGE THAT NAMED THE PROGRAM. [Agent.personSeq]
// numbers what the person has typed, steering included, and the bounce is
// counted against the newest message of the turn that named one
// ([programsHeard]): a new message of theirs that names the program again
// earns one more bounce, a steer that names nothing earns none and does not
// unsay the one before it, and a woken turn, which types nothing, inherits the
// count of the message it is still answering.
//
// AND A PROPOSAL PASSES ONLY ONCE THE MODEL HAS READ THE BOUNCE, which is never
// in the step that made it. A model reads a result in the request after the
// batch that returned it, and every proposal of one message is staged before
// any of their results is read: side by side in the batch, or while the
// message is still arriving. "fix issues #31 and #32 with senior-dev" is two
// proposals in one message, and the second used to pass as though the first
// one's bounce had been read, go up with no `via`, and be admitted to codeaf's
// own worker by its countdown. So the mark carries the step that made it
// ([Agent.stepSeq]), every proposal without a `via` in that step is turned back
// too, and it is a proposal from a later step that passes as it is. The check
// and the mark are made under one lock.
func (a *Agent) programAskBounce(spec taskSpec) *askBounce {
	if spec.via != "" || !a.mayHandToProgram() || len(a.config.Delegates) == 0 {
		return nil
	}
	step := a.stepSeq.Load()
	a.mu.Lock()
	defer a.mu.Unlock()
	heard := a.programsHeard
	if heard.named == "" {
		return nil
	}
	if a.programBounced.seq == heard.seq && a.programBounced.step < step {
		return nil
	}
	bounce := &askBounce{agent: a, name: heard.named, prior: a.programBounced, mark: bounceMark{seq: heard.seq, step: step}}
	a.programBounced = bounce.mark
	return bounce
}

// bounceMark is where a proposal was last turned back for leaving out the
// program the person named: the [Agent.personSeq] of the message that named
// it, and the [Agent.stepSeq] of the request whose proposals were turned back.
type bounceMark struct {
	seq  uint64
	step uint64
}

// askBounce is one proposal turned back by [Agent.programAskBounce], as the
// staged call it is (task.go's [Agent.stageTask]). It is its own [bare.Staged]
// rather than a settled refusal because it leaves something behind: the mark
// that lets the next proposal through.
type askBounce struct {
	agent *Agent
	name  string
	// mark is what this bounce wrote on [Agent.programBounced], and prior is
	// what was there before it.
	mark, prior bounceMark
}

// Commit hands the bounce over as the call's result.
func (b *askBounce) Commit(context.Context) (string, bool, error) {
	return programNamedSentence(b.name), true, nil
}

// Withdraw takes the mark back. A call withdrawn before it went ahead is one
// the model never reads — the reply carrying it was cut, or the turn ended —
// so the bounce it carried was never read either, and the next proposal for
// the message has to be turned back in its place. A sibling from the same
// step wrote the same mark over this one, so the mark is put back only while
// it is still this bounce's own, and the siblings withdrawn in any order leave
// what was there before the first of them.
func (b *askBounce) Withdraw() {
	b.agent.mu.Lock()
	defer b.agent.mu.Unlock()
	if b.agent.programBounced == b.mark {
		b.agent.programBounced = b.prior
	}
}

// text is the sentence the bounce hands over, and "" for no bounce.
func (b *askBounce) text() string {
	if b == nil {
		return ""
	}
	return programNamedSentence(b.name)
}

// askedForProgram says `via` names a program the person's words named in the
// turn they last spoke in, which is the one thing that lifts the spawn floor
// for a proposal ([Agent.refuseProposedTask]). A name this build does not
// carry is never heard ([Agent.hearProgramsLocked]), so it lifts nothing.
func (a *Agent) askedForProgram(via string) bool {
	if via == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Contains(a.programsHeard.asked, via)
}

// programsHeard is what the person's messages of one turn said about the
// programs this build carries.
//
// A TURN'S ASK IS EVERY MESSAGE TYPED INTO IT, not the newest one. A person
// names senior-dev as the turn opens and steers with a detail while the model
// reads the code, which is exactly when a model that has forgotten the name
// proposes without `via`, and the steer names nothing. [Agent.personAsk] is
// that steer by then, and reading it alone sent the work to codeaf's own
// worker and held "fix this file only" on the spawn floor although the turn
// had asked for senior-dev. So this is kept from the message that opens a turn
// to the last one steered into it, and started again by the next message that
// opens a turn. A woken turn types nothing and leaves it where it was, so it
// still answers the ask it was woken under.
type programsHeard struct {
	// turn is the [Agent.turnSeq] the messages were typed into.
	turn uint64
	// named is the program the newest message that named one named, and seq is
	// that message's [Agent.personSeq], which the bounce is counted against.
	named string
	seq   uint64
	// asked is every program a message of the turn named, which is what lifts
	// the floor for a proposal whose `via` is one of them.
	asked []string
}

// hearProgramsLocked reads one message of the person's for the programs this
// build carries, into [Agent.programsHeard]. [Agent.rememberAskLocked] is the
// one caller, after the message is numbered, so every message they type is
// read here exactly once and nothing the session wrote itself is. The caller
// holds a.mu.
func (a *Agent) hearProgramsLocked(text string) {
	if len(a.config.Delegates) == 0 {
		return
	}
	if a.programsHeard.turn != a.turnSeq {
		a.programsHeard = programsHeard{turn: a.turnSeq}
	}
	words := normalizedWords(text)
	for _, name := range a.config.delegateNames() {
		if namesProgram(words, name) && !slices.Contains(a.programsHeard.asked, name) {
			a.programsHeard.asked = append(a.programsHeard.asked, name)
		}
	}
	if name := a.config.programNamedIn(text); name != "" {
		a.programsHeard.named, a.programsHeard.seq = name, a.personSeq
	}
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
