package session

// THE PROGRAMS, PUT IN THE MODEL'S HANDS — BEHIND A CARD.
//
// A subharness is a saved PROGRAM for a shape of work this project does again
// and again: typed input, typed output, its own tools, its own bounds
// (docs/SUBHARNESS-PRD.md). The whole value of having one is that it gets USED,
// and the way a thing on a list stops being used is that nobody remembers the
// list exists. So the conversation itself offers one when the work in front of
// it is that shape — which is the same argument harness.go makes about the turn
// matching a saved harness, made one layer up where the judgement can be good.
//
// THE DECIDING IS THE MODEL'S, exactly as tools_harness.go's header argues at
// length. A regular expression cannot tell "triage this flaky test" from "we
// should write something that triages flaky tests one day", and a table lookup
// that tried would be wrong on somebody's turn. A tool costs nothing until the
// model reaches for it, so the judgement is made once, by the thing in this
// program that can make it, with the whole conversation in front of it.
//
// ── AND THEN IT IS GATED TWICE MORE, BECAUSE A PROPOSAL IS NOT A LAUNCH ──
//
// THE LEXICAL GATE. Phase 1 matches by CUES and by NAME and by nothing else: the
// program's own trigger vocabulary, appearing in what has actually been said. It
// is deliberately dull — no index, no scores, no thresholds machinery, all of
// which is Phase 2 — and its only job is to make the confidence rule real:
// a match raises the card, and a weak match raises NOTHING. Silence over noise.
// A conversation that has to swat away a suggestion is worse off than one that
// never got it.
//
// THE CARD. Nothing runs because chat proposed it. The person is shown the
// program, chat's reason, and the intake card with the form already filled from
// this conversation — and what they answer is what happens. THERE IS NO CLOCK
// THAT APPROVES: propose_task's countdown ends in a yes because the countdown is
// a window to redirect ordinary work, and this one may not, because "never
// silent auto-execution" is the law this whole path is built around (PRD §9).
// The clock that does exist ends in NOTHING RUNNING, and it exists only because
// a tool that blocks without a bound of its own is the one way left to break the
// turn's batch law (loop.go's runToolsWarm).
//
// ── THE FOUR VERBS ARE DISJOINT ──
//
// propose_subharness runs a program that ALREADY EXISTS for this exact shape.
// build_harness designs a new repeatable recipe. run_adaptive takes a many-part
// goal with no known shape. propose_task hands out one self-contained piece of
// ordinary work. A model that has three of the four reaches for the wrong one,
// which is why each description names the other three.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// subharnessCardWindow is how long a raised card holds the tool call open.
//
// IT IS NOT A DEADLINE ON A PERSON and its expiry is a NO. A card is a question
// on somebody's screen and they are entitled to take as long as they like; what
// this bounds is the TOOL CALL, because a tool that waits on something outside
// this process and does not bound its own waiting is the one way left to hang a
// turn's batch forever (loop.go says so, and names every tool that keeps this
// rule). When it fires, nothing has run and the model is told exactly that.
const subharnessCardWindow = 15 * time.Minute

var proposeSubharnessDescription = "Offer to run a SAVED PROGRAM that already does this exact shape of work. A subharness is a named procedure somebody kept — it takes typed input, produces a typed answer, uses only the tools it declared, and runs beside this conversation as its own task with a room, a number and a stop. Call list_subharnesses first: you can only propose one that exists, by its exact name. PROPOSE ONLY WHEN THE WORK IN FRONT OF YOU IS THAT SHAPE — the program's purpose is what the person is actually asking for, not merely adjacent to it. If you are reaching, say nothing: a wrong offer costs the person a decision they did not want to make, and nothing is lost by leaving it, because they can always ask. The reason is the whole of what they read before deciding, so write what MATCHED in their own terms — \"the brief and a failing test name are both here\" — and not a description of the program, which they can see. Nothing runs when you call this. The person is shown the program, your reason, and its form with whatever this conversation already answers already filled in; they change what is wrong, fill what is missing, and confirm — or they do not, and nothing happens. You are told which. Use it for work a saved program does; use build_harness to DESIGN a program for a shape that has none, run_adaptive for a large goal with many parts and no known shape, and propose_task for one self-contained piece of ordinary work."

const proposeSubharnessSchemaJSON = `{"type":"object","properties":{` +
	`"name":{"type":"string","description":"The exact name of a saved subharness, as list_subharnesses spells it."},` +
	`"reason":{"type":"string","description":"Why this matched, in one line and in the person's own terms: what is here that this program takes. Not a description of the program."}` +
	`},"required":["name","reason"],"additionalProperties":false}`

const listSubharnessesDescription = "List the saved programs this machine can run: each one's name, what it is for, and where it came from. Call it before propose_subharness — you can only offer a program that exists, by its exact name — and whenever the person asks what shapes of work are already built here. Nothing runs by listing; propose_subharness is what offers one, and only the person's answer to that card starts it."

const listSubharnessesSchemaJSON = `{"type":"object","properties":{},"additionalProperties":false}`

// subharnessTools are the model's hand on the saved programs, and the list that
// says which ones there are.
//
// THEY COME AS A PAIR for [Agent.listHarnessesTool]'s reason, and the pairing is
// load-bearing rather than tidy: the propose verb takes an EXACT NAME out of a
// registry that changes under the process, so a model given the verb and no way
// to read the registry would guess names, and every guess is a refusal the
// person paid a turn for.
//
// THE FAMILY IS ABSENT WHERE IT CANNOT WORK, which is this belt's law (tools.go)
// and matters most here: a model told it can run a saved program plans around
// that ability for the rest of the conversation, long after the first refusal.
// So the gates are the three things a proposal actually needs — programs to
// offer, and somebody watching who can answer the card — and a build that fails
// one of them simply does not have the verbs.
func (a *Agent) subharnessTools() []bare.Tool {
	if !a.canProposeSubharness() {
		return nil
	}
	return []bare.Tool{a.proposeSubharnessTool(), a.listSubharnessesTool()}
}

// canProposeSubharness is the gate: a registry with at least one program on it,
// and somebody watching who can answer the card.
//
// AN EMPTY REGISTRY IS NO VERB. A registry holding only the baseline is a build
// with nothing to offer — `linear` is what you get when you pick nothing, never
// something anybody proposes — and a model handed a propose verb over an empty
// list would offer programs it invented.
func (a *Agent) canProposeSubharness() bool {
	return a.config.AskConsent && len(a.SubharnessList()) > 0
}

// proposeSubharnessTool raises one card and waits for its answer.
func (a *Agent) proposeSubharnessTool() bare.Tool {
	return bare.Tool{
		Name:        "propose_subharness",
		Description: proposeSubharnessDescription,
		Schema:      json.RawMessage(proposeSubharnessSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Name   string `json:"name"`
				Reason string `json:"reason"`
			}
			if len(args) > 0 {
				if err := json.Unmarshal(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
			}
			name := strings.TrimSpace(parsed.Name)
			reason := strings.TrimSpace(parsed.Reason)
			if name == "" || reason == "" {
				return "Invalid arguments: propose_subharness needs both the program's exact name and one line saying why it matched — the reason is what the person reads before deciding.", true, nil
			}
			card, err := a.SubharnessIntake(name)
			if err != nil {
				return "There is no saved program called " + strconv.Quote(name) + " here. Call list_subharnesses for the names this machine actually has.", true, nil
			}
			// THE LEXICAL GATE, and it is the whole of Phase 1's confidence rule.
			// A program whose own cues and whose own name appear nowhere in what
			// has been said is not a match, however good the sentence the model
			// wrote about it — and the answer to a weak match is silence, not a
			// quieter card.
			if !a.subharnessMatches(card.Manifest) {
				return "Nothing was raised: nothing in this conversation matches what " + card.Manifest.Name + " is for, so offering it would be noise. Do the work here, or say in a line that the program exists if you think they would want it.", false, nil
			}
			card.Why = "this looks like " + card.Manifest.Name + ": " + reason
			id, answer, err := a.askSubharnessCard(ctx, card)
			if err != nil {
				return "The card came down before it was answered, so nothing ran.", false, nil
			}
			if !answer.run {
				return "The person did not run " + card.Manifest.Name + ". Nothing started; carry on with the work in front of you, and do not offer it again unless they bring it up.", false, nil
			}
			task, title, err := a.startSubharnessRun(ctx, card.Manifest.Name, answer.input, card.Why)
			if err != nil {
				return card.Manifest.Name + " could not be started: " + err.Error(), true, nil
			}
			_ = id
			return fmt.Sprintf("task %d is running %s: %s", task, card.Manifest.Name, title) +
				"\nIt runs as that task, beside this conversation: the person can open task " +
				strconv.FormatUint(task, 10) +
				" to watch it work, answer it if it asks them something, and stop it. What it produces arrives here when it lands. Carry on with the work in front of you rather than waiting.", false, nil
		},
	}
}

// listSubharnessesTool is the registry in three columns.
//
// It reads [Agent.SubharnessList] and nothing else, so a program from a store
// and a program compiled into this binary arrive here indistinguishable — which
// is the whole contract in one line, and why the provenance mark is a DIM note
// rather than a column that sorts.
func (a *Agent) listSubharnessesTool() bare.Tool {
	return bare.Tool{
		Name:        "list_subharnesses",
		Description: listSubharnessesDescription,
		Schema:      json.RawMessage(listSubharnessesSchemaJSON),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			rows := a.SubharnessList()
			if len(rows) == 0 {
				return "No saved programs are on this machine. build_harness designs a repeatable procedure when the work is a shape worth keeping.", false, nil
			}
			var out strings.Builder
			for _, row := range rows {
				out.WriteString(row.Manifest.Name)
				if purpose := strings.TrimSpace(row.Manifest.Purpose); purpose != "" {
					out.WriteString(" · " + purpose)
				}
				if from := strings.TrimSpace(string(row.Manifest.Provenance)); from != "" {
					out.WriteString(" · " + from)
				}
				// The last-run note where there is one, and nothing where there is
				// not: a program nobody has run says nothing about runs, never
				// "0 runs" (the emptiness law).
				if last := strings.TrimSpace(row.LastRun); last != "" {
					out.WriteString(" · " + last)
				}
				out.WriteString("\n")
			}
			out.WriteString("\npropose_subharness offers one of these to the person, with the form already filled from this conversation. Nothing runs until they confirm it.")
			return out.String(), false, nil
		},
	}
}

// ── THE LEXICAL GATE ────────────────────────────────────────────────────────

// subharnessMatches reports whether this conversation supports offering this
// program at all.
//
// IT IS A TABLE LOOKUP AND IT IS MEANT TO BE DULL. The scoring machinery, the
// index and the thresholds are Phase 2 (PRD §9); what Phase 1 owes is a gate
// that turns "confidence" into behaviour a person can predict, and two signals
// do that:
//
//   - THE NAME, SAID. Somebody or the model naming a program is the strongest
//     signal there is, and it is the only one a program with no cues has —
//     the same law harness.go states about a harness page with no trigger
//     vocabulary.
//   - A CUE, SAID. The program's own trigger words, written by whoever saved
//     it, appearing in what has actually been said.
//
// A cue is matched as a WHOLE PHRASE against word boundaries rather than as a
// substring, because a substring match on a short cue matches everything: "test"
// inside "latest" would offer a testing program to a conversation about release
// notes.
func (a *Agent) subharnessMatches(manifest exec.Manifest) bool {
	material := strings.ToLower(a.intakeMaterial())
	if material == "" {
		return false
	}
	if containsPhrase(material, strings.ToLower(strings.TrimSpace(manifest.Name))) {
		return true
	}
	for _, cue := range manifest.Cues {
		if containsPhrase(material, strings.ToLower(strings.TrimSpace(cue))) {
			return true
		}
	}
	return false
}

// containsPhrase reports whether one phrase appears in the text on both its own
// word boundaries. An empty phrase matches nothing: a cue nobody wrote is not a
// cue every conversation satisfies.
func containsPhrase(text, phrase string) bool {
	if phrase == "" || text == "" {
		return false
	}
	for offset := 0; ; {
		index := strings.Index(text[offset:], phrase)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(phrase)
		if boundaryAt(text, start-1) && boundaryAt(text, end) {
			return true
		}
		offset = start + 1
	}
}

// boundaryAt says whether the byte at this position ends a word. Off either end
// of the string is a boundary, which is what makes a phrase at the very start or
// the very end of the material match.
func boundaryAt(text string, at int) bool {
	if at < 0 || at >= len(text) {
		return true
	}
	r := rune(text[at])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// ── THE CARD ────────────────────────────────────────────────────────────────

// subharnessConsent is one answer travelling from the surface back to the
// blocked proposal.
type subharnessConsent struct {
	run bool
	// input is the form as the person LEFT it, which is not necessarily the form
	// they were shown: the card is editable, and what launches has to be what
	// they confirmed rather than what was inferred for them. An empty answer on
	// a yes is the card exactly as it was raised.
	input json.RawMessage
}

// askSubharnessCard raises one intake card and waits for the person.
//
// It is [Agent.askHarness] with the payload changed and the clock's meaning
// inverted, and the inversion is the point: a harness offer's clock is the
// turn's own context, and this one has a window whose expiry is a NO. Both end
// with nothing having run, which is the only ending this door is allowed to
// reach without an answer.
func (a *Agent) askSubharnessCard(ctx context.Context, card SubharnessCard) (uint64, subharnessConsent, error) {
	answers := make(chan subharnessConsent, 1)
	carried := card
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return 0, subharnessConsent{}, errAgentClosed
	}
	a.subharnessSeq++
	id := a.subharnessSeq
	if a.subharnessOffers == nil {
		a.subharnessOffers = make(map[uint64]chan subharnessConsent, 1)
	}
	a.subharnessOffers[id] = answers
	a.mu.Unlock()
	defer a.forgetSubharnessOffer(id)

	a.emitHarness(Event{
		Kind:       EventSubharnessProposal,
		ID:         id,
		Text:       card.Manifest.Name,
		Hint:       strings.TrimSpace(card.Manifest.Purpose),
		Subharness: &carried,
	})

	timer := time.NewTimer(subharnessCardWindow)
	defer timer.Stop()
	select {
	case answer := <-answers:
		if answer.run && len(answer.input) == 0 {
			// They confirmed the card as it stood. The form is built here rather
			// than in the surface so that a surface which only knows how to say
			// yes launches the same object as one that lets them edit
			// (subharness_intake.go's SubharnessInput is the one place a card
			// becomes an input).
			answer.input = SubharnessInput(card)
		}
		return id, answer, nil
	case <-timer.C:
		// NOTHING RAN, and that is the whole of what the clock decides. It is not
		// a decline the person made and it is not recorded as one; it is the tool
		// call letting the turn go.
		return id, subharnessConsent{}, nil
	case <-ctx.Done():
		return id, subharnessConsent{}, ctx.Err()
	}
}

// ResolveSubharness answers one EventSubharnessProposal: whether to run the
// program, and the form as the person left it.
//
// TRUE RUNS IT AND NOTHING ELSE DOES. There is no other path from a proposal to
// a launch — no clock that approves, no default, no "they did not object" — which
// is what makes "never silent auto-execution" a fact about this code rather than
// a promise about it.
//
// The input may be nil, and nil means "as it was raised": a surface that draws
// the card read-only and offers one key is answering the same question as one
// that lets every field be edited, and neither has to know how the other spells
// the form.
//
// An id nobody is waiting on — a card whose turn was interrupted, a second press
// — is ignored rather than reported, exactly as [Agent.ResolveConsent] ignores a
// late answer.
func (a *Agent) ResolveSubharness(id uint64, run bool, input json.RawMessage) {
	a.mu.Lock()
	answers := a.subharnessOffers[id]
	delete(a.subharnessOffers, id)
	a.mu.Unlock()
	if answers == nil {
		return
	}
	answers <- subharnessConsent{run: run, input: input}
}

func (a *Agent) forgetSubharnessOffer(id uint64) {
	a.mu.Lock()
	delete(a.subharnessOffers, id)
	a.mu.Unlock()
}
