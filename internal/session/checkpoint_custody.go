package session

import (
	"strconv"
	"strings"
)

// ── custody ─────────────────────────────────────────────────────────────────
//
// THE LAW: WORK THIS CONVERSATION IS STILL HOLDING NEVER LEAVES IT.
//
// A checkpoint hands over WORK. It cannot hand over CUSTODY, because custody is
// a fact about the graph and not a sentence in a brief: a task admitted by this
// road is a ROOT (route_judge.go's [Agent.launchRouteTask] sets no parent), and
// `tasks` inside a task answers only the children that task itself created
// (tools_tasks.go's [Agent.taskRows]). So a worker told to wait for, read, steer,
// settle or integrate a piece THIS conversation handed out has been given a duty
// it cannot see the object of. Its first `tasks` call answers "No tasks have run
// in this project yet.", and every minute after that is spent guessing at branch
// names.
//
// THE PRIVATE FAMILY BOUNDARY IS CORRECT AND IS NOT WHAT MOVES. The other repair
// — letting a task see its siblings — would erase the one boundary that makes a
// worker's world knowable, to buy a coordinator the conversation is already
// better placed to be: the conversation created those pieces, it is woken by
// their reports, and it is the only reader in the building holding all of them.
// So coordination stays where the pieces are visible, and what is handed out is
// the part of the remainder that stands on its own.
//
// AND A PART THAT COULD NOT BE HANDED TO ANYBODY IS STILL WORK. It is not
// discarded and it is not refused — "wait for 4 and 8, integrate, review, open
// the PR" is exactly what the person asked for, done a little later, by the only
// reader that can see its object. So the withheld parts come back out of
// [checkpointSketch.withoutHeldWork] as a REMAINDER, and the three things that
// have to know about it are told: the journal, the person, and the transcript the
// next turn opens on (checkpoint.go's [Agent.handOverRunningTurn]).
//
// AND IT IS THE ENGINE'S OWN LEDGER THAT DECIDES IT, never a rule about English.
// [Agent.piecesStillOut] is the list of root pieces this conversation handed out
// and has not got back, by the number the surface gave each one and the name it
// goes by, and the two readings below are the only two questions asked of it.
//
// This is #276's law (checkpoint.go's "the conversation's own verbs") carried
// from the SHAPE of a drawing to the FACT of what is out. #304 refused a drawing
// that was coordination throughout and let a mixed one proceed whole; the mixed
// one is the drawing this file takes apart (#567).

// checkpointHeldRestNote is what is added to the line a person reads when part of
// the drawing DID NOT GO WITH THE WORK.
//
// It is a suffix rather than a line of its own for [carryAskOnlyNote]'s reason: it
// is the same event, and how much of their turn moved is part of the sentence
// saying it moved. It keeps that register — lowercase, middle dot, no full stop —
// and it names no machinery: "the pieces already out" is what the manual calls
// them and what the person watched go out.
const checkpointHeldRestNote = " · the rest stays here for when the pieces already out land"

// checkpointHeldWholeNote is the ONE line a person reads when NOTHING was handed
// over, because everything there was to hand over was about work already out.
//
// It is a line of its own and not a suffix, because it is not the same event as
// the ones the notes in checkpoint.go announce: nothing moved, so a sentence
// saying work was moving would be a note left standing over something that did
// not happen. It keeps their register all the same — an observation, a middle
// dot, what the harness did, no full stop ([checkpointCeilingNote]).
const checkpointHeldWholeNote = "this is all about the pieces already out · keeping it here until they land"

// checkpointOwnRemainderHead opens the block the TRANSCRIPT keeps, in the register
// [checkpointSketch.head] uses for the parts that did travel. The two are read by
// different readers — that one by the worker, this one by the next turn of this
// conversation — and spelling them alike is what makes them one grammar.
const checkpointOwnRemainderHead = "WHAT STAYS HERE, FOR WHEN THE PIECES ALREADY OUT LAND: "

// heldRestRecord is the remainder as it goes into the transcript, and NOTHING IS
// WRITTEN FOR AN EMPTY ONE — the emptiness law, in the one place where breaking it
// would put a heading with nothing under it at the top of the next turn's context.
func heldRestRecord(remainder string) string {
	if strings.TrimSpace(remainder) == "" {
		return ""
	}
	return "\n" + checkpointOwnRemainderHead + remainder
}

// heldPiece is one root piece this conversation handed out and has not got back:
// the number the surface gave it (`task 4`, cancel.go's spelling) and the name it
// goes by on the rail.
type heldPiece struct {
	id    uint64
	title string
}

// piecesStillOut is the conversation's own ledger of what it is holding.
//
// A ROOT AND UNSETTLED, and neither half is negotiable. A root is a piece THIS
// conversation created — a nested piece belongs to the worker that commissioned
// it and is already visible to it. Unsettled is the whole of "still out": a piece
// that is done, failed or unchecked has reported, and a drawing that names it is
// naming a fact, not asking anybody to wait for one.
//
// It is nil-safe for [Agent.tasker]'s reason: a session that never groomed a task
// holds nothing, and nothing is the honest answer rather than a graph built to
// answer one question.
func (a *Agent) piecesStillOut() []heldPiece {
	return a.tasker().piecesStillOut()
}

func (g *TaskGraph) piecesStillOut() []heldPiece {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []heldPiece
	for _, id := range g.order {
		node := g.nodes[id]
		if node == nil || node.parent != 0 || node.state.settled() {
			continue
		}
		out = append(out, heldPiece{id: node.id, title: node.spec.title})
	}
	return out
}

// namesHeldWork reports that a piece of text is ABOUT work this conversation is
// still holding, and it is the one reading this file makes of prose.
//
// TWO WAYS A DRAWING NAMES A PIECE, and it folds nothing else:
//
//   - BY ITS NUMBER, STANDING BESIDE THE SURFACE'S OWN NOUN FOR THE THING.
//     `tasks 4 and 8 still out` names two of them. The noun is required because a
//     bare number is a quantity, and `task`/`tasks` is not a word about English
//     but the word this program prints when it starts one (route_judge.go: "this
//     looked like work, so task 9 started").
//   - BY ITS NAME, where the drawing quotes the whole of it. Two words at least:
//     a piece called `parser` would match half the sentences ever written, and
//     the failure direction here is to withhold work that was really handable.
//
// AND NOTHING IS INFERRED FROM A VERB. "integrate", "land", "merge" and their
// neighbours are ordinary work when the object is the worker's own; what makes
// them somebody else's is the object, and the object is what this reads.
//
// AND HERE IS WHAT IT DOES NOT CATCH, said plainly so that nobody assumes it is
// closed: prose that refers to a piece WITHOUT its number and WITHOUT its name.
// `wait for #4 and #8` puts a mark where the noun belongs, and `the other two`
// names nothing at all. On the DRAWING side both are still withheld — a part that
// opens on a wait word is the conversation's own whenever a piece is out, whatever
// it goes on to say ([partHandsBack]) — so the gap is in the reading of a BRIEF,
// and it is why the road does not rest on this reading alone
// (checkpoint.go's [checkpointCeilingHeldWork]).
func namesHeldWork(text string, held []heldPiece) bool {
	if len(held) == 0 || strings.TrimSpace(text) == "" {
		return false
	}
	words := normalizedWords(text)
	numbered := numbersBesideTheNoun(words)
	for _, piece := range held {
		if numbered[piece.id] {
			return true
		}
		if name := normalizedWords(piece.title); len(name) >= heldPieceNameWords &&
			containsRun(words, name) {
			return true
		}
	}
	return false
}

// numbersBesideTheNoun reads the ids a text REFERS TO, and it is positional.
//
// A NUMBER IS A REFERENCE ONLY WHERE IT STANDS BESIDE THE WORD THIS PROGRAM USES
// FOR THE THING; ANYWHERE ELSE IT IS A QUANTITY. A reading that asked only
// whether the noun and the number both occurred somewhere in the same text is
// near enough over one clause of a drawing and is a false-positive generator over
// a two-thousand-character brief: ids are small numbers, and `run the 4 tests` in
// a document that says `task` once would blank a brief nobody could fault and
// drop the carry ladder a rung for nothing.
//
// So the noun anchors, and what is read off it is the RUN of numbers that
// follows: `tasks 4 and 8 still out` is {4, 8}, because a list of ids is written
// with joiners between them. A joiner only continues a run that has already
// produced a number — `the task and 4 tests` opens on a joiner and names nothing
// — and anything that is neither ends the run.
//
// AND THE NOUN WITH ITS ID FUSED ONTO IT IS STILL THE NOUN, which is not a
// generalisation anybody reasoned their way to — it is a spelling a real model
// wrote on this road, on its first attempt, with two pieces out:
//
//	(one: write headers to docs/one two three) | (three: parallel docs content) >
//	(merge task1+2 branches) > (review all) > (open PR)
//
// `task1+2` reaches this as the two tokens `task1` and `2`, because
// [normalizedWords] keeps digits inside a word and reads the plus as a space. A
// reading that only knew the bare token saw no noun at all, read nothing, and let
// a part naming BOTH held pieces travel whole — `kept` came back empty on that
// run, and what caught it was the ask door downstream rather than the reduction.
// So `task1` anchors and yields its own id, and the run then continues from it
// exactly as it does after a spaced noun, which makes `task1+2` the {1, 2} it
// plainly says. THE READING HAS TO SURVIVE THE SPELLINGS A MODEL ACTUALLY USES,
// not the ones a fixture is written with.
//
// AND IT IS THE NOUN AND ITS PLURAL AND NOTHING ELSE. `taskboard` and `tasking`
// begin with the same letters and are not this program's word for a piece of
// work; matching a prefix rather than the whole word would buy a reading that
// withholds work over the name of a screen.
func numbersBesideTheNoun(words []string) map[uint64]bool {
	var ids map[uint64]bool
	remember := func(id uint64) {
		if ids == nil {
			ids = make(map[uint64]bool, 2)
		}
		ids[id] = true
	}
	for index, word := range words {
		fused, isFused := nounWithIDFused(word)
		if !isFused && word != heldPieceNoun && word != heldPieceNounPlural {
			continue
		}
		numbered := false
		if isFused {
			remember(fused)
			numbered = true
		}
		for _, next := range words[index+1:] {
			if heldPieceJoiners[next] {
				if numbered {
					continue
				}
				break
			}
			id, err := strconv.ParseUint(next, 10, 64)
			if err != nil {
				break
			}
			remember(id)
			numbered = true
		}
	}
	return ids
}

// nounWithIDFused reads one token that is this program's word for a piece of work
// with an id written straight onto it — `task1`, and `tasks1` for the model that
// writes the plural — and answers the id it names.
//
// THE WHOLE WORD AND NOT A PREFIX. What is left after the noun has to be digits
// and nothing else, so `taskboard` and `tasking` are words about other things and
// answer nothing at all.
func nounWithIDFused(word string) (uint64, bool) {
	for _, noun := range [2]string{heldPieceNoun, heldPieceNounPlural} {
		digits, cut := strings.CutPrefix(word, noun)
		if !cut || digits == "" {
			continue
		}
		if id, err := strconv.ParseUint(digits, 10, 64); err == nil {
			return id, true
		}
	}
	return 0, false
}

const (
	// heldPieceNoun is what this program calls a piece of work it started, and it
	// is the anchor a number is read beside. It is spelled once because a second
	// spelling would be a second law.
	heldPieceNoun       = "task"
	heldPieceNounPlural = "tasks"

	// heldPieceNameWords is how much of a name a drawing has to quote before the
	// quote means anything. One word is a coincidence; two is a reference.
	heldPieceNameWords = 2
)

// heldPieceJoiners are the words that hold a LIST of ids together. Punctuation is
// already gone by the time this is read (title.go's [normalizedWords] reads every
// mark that is not a letter or a digit as a space), so `tasks 4, 8 and 9` and
// `tasks 4 and 8` arrive as the same sequence of words.
var heldPieceJoiners = map[string]bool{
	"and": true,
	"to":  true,
	"or":  true,
}

// containsRun reports that one sequence of words appears whole inside another.
func containsRun(words, run []string) bool {
	if len(run) == 0 || len(run) > len(words) {
		return false
	}
	for start := 0; start+len(run) <= len(words); start++ {
		matched := true
		for offset, word := range run {
			if words[start+offset] != word {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// withoutHeldWork is one drawing with the conversation's own parts taken out of
// it, and it is what a mark, a ceiling and the write seam all read instead of
// what the sidecar drew. It answers the reduced drawing and, beside it, THE
// REMAINDER THAT STAYS HERE — the withheld parts written back out in the order
// the sidecar drew them, empty when nothing was withheld.
//
// A PART IS THE CONVERSATION'S OWN WHEN IT NAMES A PIECE STILL OUT, or when it is
// coordination by its shape and there is a piece out for it to be about. The
// second half is where #304's all-or-none rule stops applying: one wait beside
// real work is a division worth making when this conversation holds nothing, and
// is a duty nobody can take when it holds two pieces. So the rule is not that a
// mixed drawing is refused — the real part is still worth handing out, which is
// the whole reason the road exists — it is that the part about work already out
// stays where that work is visible.
//
// A DRAWING THAT IS ALL THE CONVERSATION'S OWN COMES BACK EMPTY, which reads as a
// hand-back everywhere downstream ([checkpointSketch.split] is false and
// [checkpointSketch.head] adds nothing), exactly as `(waiting)` does. The
// remainder still comes back beside it: nothing was handed out, and everything
// that was drawn is still owed.
//
// THE STAGES THAT WAIT BEHIND EVERY PART GO WHERE THE PARTS WENT. `(A | B) > land
// them` is two jobs and a gathering step, and a gathering step gathers ALL of
// them — so the moment one part is withheld the step is over work this
// conversation is holding, and it is this conversation's. Where no part was
// withheld a stage is read on its own terms, so `(A | B) > integrate task 4`
// still hands out A and B and keeps the integration.
//
// AND THE STAGES ARE A CHAIN, SO A STAGE BEHIND A WITHHELD ONE IS WITHHELD TOO.
// That is the same law one level down, and it is not tidiness: in
// `(A | B) > integrate task 4 > open the PR`, keeping `open the PR` while
// withholding the integration in front of it hands a worker the END of a sequence
// whose middle this conversation has not done — the pull request opened over
// branches nobody has merged. An arrow is an order, and an order with a hole in it
// is not the drawing anybody read.
//
// AND A CONVERSATION HOLDING NOTHING IS UNTOUCHED, byte for byte. This is the
// whole of the condition: without a piece out there is no custody to protect, and
// a drawing rewritten for no reason is a harness editing what it did not draw.
func (s checkpointSketch) withoutHeldWork(held []heldPiece) (checkpointSketch, string) {
	if len(held) == 0 || !s.drawn() {
		return s, ""
	}
	reading := readShape(s.shape)
	kept, mine := shapeReading{}, shapeReading{}
	for _, part := range reading.parts {
		if namesHeldWork(part, held) || partHandsBack(part) {
			mine.parts = append(mine.parts, part)
			continue
		}
		kept.parts = append(kept.parts, part)
	}
	for _, stage := range reading.after {
		if len(mine.parts) > 0 || len(mine.after) > 0 || namesHeldWork(stage, held) {
			mine.after = append(mine.after, stage)
			continue
		}
		kept.after = append(kept.after, stage)
	}
	if len(mine.parts) == 0 && len(mine.after) == 0 {
		return s, ""
	}
	remainder := mine.render()
	shape := kept.render()
	if shape == "" {
		// NOTHING LEFT TO HAND ANYBODY. The legend goes with the shape it named:
		// a sentence saying what letters that are no longer there stood for is a
		// paragraph of machinery in front of a worker, and the emptiness law says
		// what an answer with nothing in it renders as.
		//
		// AND THE REMAINDER IS THE DRAWING ITSELF, VERBATIM, which is the one case
		// a re-render could lose something in. [readShape] reports the stages
		// behind a SINGLE part as no part at all — `(tasks 1 and 2 still out) >
		// integrate their branches > review` reads as one part and the two steps
		// behind it are not in the reading — so writing that reading back out
		// would drop the integration and the review, which are precisely the work
		// this conversation is being left with. Nothing travelled, so nothing has
		// to be subtracted, and the honest remainder is what the sidecar drew.
		return checkpointSketch{handsBack: true}, s.shape
	}
	return checkpointSketch{
		shape:  shape,
		legend: s.legend,
		parts:  topLevelParts(shape),
		// THE COORDINATION READING IS TAKEN AGAIN over the shape that is actually
		// travelling, and not carried over from the one that was drawn — a bit
		// that described parts which are no longer there would be the harness
		// holding a count without the answer that says what the count is worth.
		handsBack: checkpointHandBack(shape),
	}, remainder
}

// render writes a reading back out as a shape, in the grammar [readShape] reads:
// parts separated by ` | `, and the stages that wait behind all of them joined on
// with ` > `.
//
// THE BRACKETS COME BACK WHERE THEY CARRY MEANING — around two or more parts with
// a stage behind them, which is the one shape in which a trailing stage waits on
// every part rather than on one of them — and nowhere else. Each part is written
// exactly as the sidecar wrote it: the arrows and brackets inside one part are
// that part's own internal order, and tidying them would be rewriting a drawing
// this harness did not make.
//
// AND IT IS IDEMPOTENT THROUGH [readShape] FOR A READING [readShape] PRODUCED,
// which is the property everything downstream rests on: what this writes is read
// back as the same reading, so a count, a hand-back and a division taken off a
// reduced drawing all agree with the reduction that produced it.
//
// AND THE CLAIM STOPS THERE, because there is one reading nobody ever drew:
// stages with NO part in front of them. `integrate task 4 > open the PR` renders
// as bare text and reads back as a PART, not as an after-only reading. That is not
// a bug and it is not a shape — it is only ever produced for the conversation's
// OWN remainder, which is a sentence for a person and a line in a file, and
// nothing reads it back as a drawing.
func (r shapeReading) render() string {
	stages := r.after
	if len(r.parts) > 0 {
		head := strings.Join(r.parts, " | ")
		if len(r.parts) > 1 && len(r.after) > 0 {
			head = "(" + head + ")"
		}
		stages = append([]string{head}, r.after...)
	}
	return strings.Join(stages, " > ")
}
