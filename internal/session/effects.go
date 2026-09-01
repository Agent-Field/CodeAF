package session

// Effect novelty: what an action LEFT BEHIND, and whether the node had already
// produced that exact thing.
//
// ── WHY THE LEASH NEEDED THIS ──
//
// The checkpoint that renews or ends a running node (task_run.go's
// [Agent.taskProgress]) is handed a list of the calls the node made — the tool's
// name and the arguments it was given. That list is a NARRATIVE, and a narrative
// of successful calls reads as work whatever the calls did: measured on
// 2026-08-31, a worker rewrote one file twenty times in twenty-two minutes, every
// call exited cleanly, every call named a real path, and the reader looking at
// that list renewed the leash each time it was asked. Nothing in front of it said
// the file had not changed.
//
// So the harness computes the one fact the reader cannot infer from a tool's exit
// code: DID THIS ACTION PRODUCE ANYTHING THAT WAS NOT ALREADY THERE. It is stated
// on the action's own evidence line, and a run of actions that produced nothing
// new is stated as a sentence with a number in it. The reader still rules; it
// just rules over facts instead of over a story.
//
// ── ONE RULE, AND IT IS NOT ABOUT FILES ──
//
// An action's EFFECT is what it left behind, and there are exactly two places
// that can be: bytes on disk, when the call was one that saves something, or the
// answer it brought back, when it was not. Both are content and both are hashed
// the same way, which is what keeps this domain-neutral — a search run for the
// second time with the same query, an API called again with the same body, a page
// downloaded twice, and a file rewritten with the same bytes are ONE finding here
// and are said in one sentence.
//
// ── AND IT STATES, IT NEVER RULES ──
//
// Nothing in this file ends a node, and no number here decides anything. The run
// length is a count the leash puts in front of the reader; what to do about a run
// of nine is the reader's call and not a constant somebody tuned. That is the
// whole point of the organ: a threshold written here would be a threshold a
// worker with a different rhythm trips for nothing, and the reason the previous
// design fed the reader a narrative was precisely that nobody wanted to write
// that constant down.
//
// It is deliberately NOT the line-novelty estimator beside it (novelty.go). That
// one asks how MUCH of a result is new, answers with a ratio against a judged
// threshold, and feeds a counter. This one asks whether an effect is one already
// produced, answers yes or no, and feeds a sentence. The second question is the
// one a saving call was never asked: [addedSomething] reads a successful write as
// progress without ever looking at what landed.

import (
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// effectMemory is how many recent effect fingerprints one node keeps.
//
// IT BOUNDS A MEMORY AND NOT A DECISION. Nothing is ended by reaching it; it
// only says how far back "already produced" reaches, which is what makes a file
// oscillating between two states — write A, write B, write A again — read as the
// repetition it is rather than as two novelties taking turns. A node at its step
// limit takes hundreds of actions, and a set that grew with every one of them
// would be a leak sized by how much work the node happens to do.
const effectMemory = 32

// effectSample is how much of a produced file the fingerprint reads.
//
// The same reasoning [freshAnswer] gives for comparing capped output: two files
// that agree for a megabyte and agree in length are the same file as far as
// anybody looking at this can tell, and the sample is the same on every call so
// it can never make two genuinely different files look alike more than one
// truncated page deep. What it buys is a bound on a node whose deliverable is a
// video: the fingerprint costs a megabyte of reading and not a gigabyte.
const effectSample = 1 << 20

// repeatedEffectClause is what is said about an action whose effect was one the
// node had already produced. It is written once because it is said twice — on
// the action's own evidence line and in the sentence that counts a run of them —
// and two spellings of one finding would read as two findings.
const repeatedEffectClause = "produced byte-identical content"

// effectPrint is one action's effect: the hand that took it and the fingerprint
// of what it left behind.
//
// THE TOOL IS PART OF THE RECORD rather than part of the key, which is the
// opposite of what line novelty does (novelty.go's [lineNovelty.remember]) and
// deliberately so. There, two hands answering the same bytes are two ways of
// learning one thing and are counted separately. Here the question is whether
// anything in the world is different, and a file written by `write` that is
// byte-for-byte what `edit` left a step ago is the same nothing however it was
// spelled. The name is kept so the sentence can say WHICH hand was repeating
// itself when every action in a run used one.
type effectPrint struct {
	tool string
	sum  uint64
}

// effectLedger is one node's memory of what it has produced, and the run of
// steps at the end of it that counted as progress and produced nothing new.
type effectLedger struct {
	recent []effectPrint
	run    []string
}

func newEffectLedger() *effectLedger { return &effectLedger{} }

// saw records one finished action's effect and answers whether that exact effect
// is one of the last [effectMemory] this node produced.
//
// AN ACTION THAT PRODUCED NOTHING IS NOT REMEMBERED. A call the harness itself
// answered, and a call whose result was empty and whose file could not be read,
// left no trace in the world — so nothing about it may make a later real effect
// look like something already produced (withdrawn.go).
func (l *effectLedger) saw(tool string, sum uint64, produced bool) bool {
	if l == nil || !produced {
		return false
	}
	repeat := false
	for _, print := range l.recent {
		if print.sum == sum {
			repeat = true
			break
		}
	}
	l.recent = append(l.recent, effectPrint{tool: tool, sum: sum})
	if len(l.recent) > effectMemory {
		l.recent = l.recent[len(l.recent)-effectMemory:]
	}
	return repeat
}

// counted folds one step into the run, and the rule is narrow on purpose.
//
// ── THE RUN IS THE PROGRESS THAT WASN'T ──
//
// It grows only on a step the no-progress counter READ AS PROGRESS and whose
// effect was one already produced, and ANY other step ends it. That is not a
// convenience; it is what keeps this organ from standing in front of the one
// beside it. The counter already stops the ordinary spin — the same question
// asked again, the same directory listed again — and it is deliberately the more
// forgiving of the two (a reading taken over work that has just changed is
// information by construction, novelty.go's [progressLedger]). A run that grew
// on those steps as well would be strictly ahead of the counter on every shape
// of work and would answer for all of them, which would replace a threshold that
// stops for free with one that costs a model call.
//
// So what is left underneath, and the only thing this counts, is the shape the
// counter cannot see: a step that RESET IT while changing nothing. A successful
// save does exactly that, and one file rewritten with the same bytes is a
// counter at zero for as long as the money lasts.
func (l *effectLedger) counted(tool string, progress, repeat bool) {
	if l == nil {
		return
	}
	if progress && repeat {
		l.run = append(l.run, tool)
		return
	}
	l.run = nil
}

// sameEffectRun is how many steps in a row counted as progress and produced an
// effect this node had already produced. It is a COUNT HANDED TO A READER and
// never a threshold: the leash uses it to decide when to ASK, and what a run of
// that length means is answered by whoever is asked.
func (l *effectLedger) sameEffectRun() int {
	if l == nil {
		return 0
	}
	return len(l.run)
}

// pardon ends the run because it has been LOOKED AT and allowed to continue.
//
// The memory of what has been produced is deliberately untouched: those effects
// really were produced, and an action repeating one of them later is still
// repeating it. What starts again from zero is only how many in a row have gone
// unread — so a node whose repetition was ruled harmless is not asked about
// again on its very next step.
func (l *effectLedger) pardon() {
	if l != nil {
		l.run = nil
	}
}

// sameEffectLine states the run in one sentence, or says nothing.
//
// A RUN IS TWO OR MORE, which is grammar rather than a threshold: a single
// repeat is already written on its own evidence line, and this sentence exists
// to say HOW MANY IN A ROW — which a run of one has nothing to count.
//
// The hand is named when every action in the run used the same one, because
// "the last nine write calls" is a sharper fact than "the last nine calls" and
// the reader is entitled to the sharper one. A mixed run says the general thing,
// which is still true.
func (l *effectLedger) sameEffectLine() string {
	if l == nil {
		return ""
	}
	switch len(l.run) {
	case 0, 1:
		return ""
	}
	tool := l.run[0]
	for _, name := range l.run {
		if name != tool {
			tool = ""
			break
		}
	}
	if tool == "" {
		return fmt.Sprintf("the last %d calls %s", len(l.run), repeatedEffectClause)
	}
	return fmt.Sprintf("the last %d %s calls %s", len(l.run), tool, repeatedEffectClause)
}

// effectPrintOf fingerprints what one finished action left behind, and there is
// a LADDER of three answers because the most specific evidence of an effect is
// the one that tells the truth about it.
//
//   - THE FILE A SAVING CALL WROTE, where there is one, because it IS the effect
//     and the answer is only a sentence about it. A `write` says "wrote 412 bytes
//     to x.go", and that sentence changes when the byte count does — so a node
//     rewriting one file with the same content and a different comment length
//     would look novel by its answers and identical by its bytes.
//   - THE STATE OF THE TREE, when the step left it different from how it found
//     it. A command that produces files says nothing at all: `printf … > note.txt`
//     answers with the same empty output every time while writing a different
//     file on every call. Judging that by its answer would read a whole landing
//     phase as one long repetition, which is the opposite of what happened —
//     measured, a worker's twenty-six-command commit-and-push phase.
//   - THE ANSWER IT BROUGHT BACK, for everything that left no trace on disk. A
//     search, a fetch, a reading: its effect is what it told the node, and asking
//     the same question twice brings back the same bytes.
//
// A HARNESS-MADE RESULT PRODUCED NOTHING, and it returns before anything is
// recorded. A withdrawn hand and a refused call never reached the world, and
// those bytes must not be able to make a later real effect look like something
// already produced (withdrawn.go's [Event.HarnessMade]).
func effectPrintOf(event Event, dir, path string, saved, moved bool, dirt string) (uint64, bool) {
	if event.HarnessMade {
		return 0, false
	}
	if saved && path != "" {
		if sum, ok := fileEffectPrint(dir, path); ok {
			return sum, true
		}
	}
	if moved && dirt != "" {
		digest := fnv.New64a()
		_, _ = digest.Write([]byte(dirt))
		return digest.Sum64(), true
	}
	// THE JOB FOOTER COMES OFF FIRST, for [freshAnswer]'s reason: every result
	// carries the state of every outstanding job at its foot (jobfooter.go) and
	// that line holds an elapsed time, so a fingerprint taken over it would
	// report a novel effect for an answer that had not changed a byte.
	text := strings.TrimSpace(stripJobFooter(event.Output))
	if text == "" {
		return 0, false
	}
	digest := fnv.New64a()
	_, _ = digest.Write([]byte(text))
	return digest.Sum64(), true
}

// fileEffectPrint fingerprints the file a saving call left: WHICH FILE now holds
// WHICH BYTES, reading at most [effectSample] of them. Both halves are the
// effect and neither stands alone — the same words saved to a second path are a
// second file and something that was not there before, and the same path holding
// different words is a change. The LENGTH goes in with the bytes, so two files
// that agree for the whole sample and differ after it are still two files here.
//
// A file that cannot be read answers "nothing was produced", which is the safe
// direction: a fingerprint the harness could not take must never be able to
// stand as evidence of a repeat.
func fileEffectPrint(dir, name string) (uint64, bool) {
	path := filepath.FromSlash(name)
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer func() { _ = file.Close() }()
	digest := fnv.New64a()
	_, _ = digest.Write([]byte(filepath.ToSlash(path)))
	_, _ = digest.Write([]byte{0})
	read, err := io.Copy(digest, io.LimitReader(file, effectSample))
	if err != nil {
		return 0, false
	}
	if info, err := file.Stat(); err == nil {
		_, _ = fmt.Fprintf(digest, "\x00%d", info.Size())
	} else {
		_, _ = fmt.Fprintf(digest, "\x00%d", read)
	}
	return digest.Sum64(), true
}

// effectEvidence is one action's line in the journal the checkpoint reads: the
// hand, what it was asked to do, and — when there is one to state — the fact
// that what it produced was already there.
func effectEvidence(event Event, repeat bool) string {
	line := event.Tool + " " + strings.TrimSpace(event.Args)
	if repeat {
		line += " · " + repeatedEffectClause
	}
	return strings.TrimSpace(line)
}
