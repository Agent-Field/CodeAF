// Banking: what an attempt that died on the clock hands to the one that takes
// over from it.
//
// The incident this file exists for ran for the better part of an hour. A craft
// job's root leaf implemented four classification algorithms, benchmarked them
// against RBF-SVM on three datasets, posted a progress row at every step, and at
// 20:13:51 said "The comparison writeup for all four algorithms is now pulled
// together into one document". Nine minutes later its first attempt hit its time
// ceiling. At 20:22:06 the retry began — and it began COLD, with an empty
// context, in a workspace already holding the writeup, rebuilding from nothing
// what the journal could have told it in twenty lines.
//
// Nothing was missing except the handover. The graph already knows how to say
// "here is what the last agent got to, do not do it again": OverrunGoal has
// composed exactly that for every re-decomposed leaf since the overrun path was
// built, and its two headers are the whole of the contract. This file makes the
// same two headers reachable from the OTHER two ways an attempt ends and starts
// over — the in-place retry, and the requeue at launch after the process
// carrying the work went away — so a leaf that ran out of clock never loses what
// it had.
//
// The bank is not a new record. Every line of it is already in the journal
// (node-anchored progress rows, board notes) or already on disk (the workspace's
// own files). What was missing was somebody reading them back.
package resident

import (
	"fmt"
	"log"
	"sort"
	"strings"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The two continuation headers, owned here and used by everything that hands
// unfinished work on: OverrunGoal's replan brief, the retry's dependency input,
// and the launch requeue's. They are exported so there is exactly one wording of
// the contract, and so a test can pin the words a retry is actually handed.
const (
	// ContinuationPartialHeader introduces what the previous agent had when it
	// stopped — its text and, when it shared any, the lines it posted as it went.
	ContinuationPartialHeader = "What the previous agent produced before stopping (its partial result arrives as a dependency input; build on it):"
	// ContinuationFilesHeader introduces the files already on disk.
	ContinuationFilesHeader = "Files already produced, to reuse rather than recreate:"
	// ContinuationStateHeader introduces the dead leaf's structured findings:
	// the files it touched, the checks it ran, and the last calls it made. It
	// is general for any task — derived from the leaf's own outcome, not from
	// any domain-specific record — so a continuation resumes from what the dead
	// leaf actually did instead of re-reading everything it already diagnosed.
	// Empty when the leaf left no structured record, which is the ordinary
	// case for a generalist that produced only prose.
	ContinuationStateHeader = "What the previous agent actually did — files it touched, checks it ran, and its last calls. Resume from here; do not re-discover what this already found."
	// ContinuationTranscriptHeader introduces the dead attempt's own turns as
	// they were recorded: what it said, what it ran, and what came back. It is
	// the last block of the continuation because it is the longest and the most
	// specific — the three above are the summary, this is the working.
	ContinuationTranscriptHeader = "Your own turns from the attempt that was interrupted, oldest first — what you said, what you ran, and what came back. This work is already done and already paid for; carry on from the end of it."
)

// BankSharedLead introduces the shared progress lines inside the partial block.
//
// They sit UNDER the partial header rather than under one of their own, because
// they are not a different kind of fact: a row saying "the comparison writeup is
// now pulled together into one document" is the previous agent telling you what
// it produced, in its own words, at the moment it produced it. A second header
// would invite the model to read it as commentary about the work instead of as
// the work.
const BankSharedLead = "What it reported as it went, oldest first — this is work that is already done and must not be done again:"

// BankInputTitle names the bank where a leaf's brief renders its inputs. It is
// the second person on purpose: the leaf reading it is the same node, on its
// next attempt, and telling it otherwise invites it to treat its own output as
// somebody else's claim to be checked.
const BankInputTitle = "your own earlier attempt at this same task"

// Bank is what an attempt left behind: the text it had, the lines it shared, and
// the files it wrote. Every field is optional and an empty one is simply left
// out of the composition — a bank never invents a handover it does not have.
type Bank struct {
	// Partial is whatever text the attempt had produced when it stopped. Empty
	// is the ordinary case for a death on the clock, which is exactly why the
	// other two fields exist.
	Partial string
	// Shared is the progress the attempt posted as it went, oldest first.
	Shared []string
	// Artifacts are absolute paths to files that are still on disk.
	Artifacts []string
	// State is the dead leaf's structured findings: files it touched, edits it
	// made, checks it ran and what they found, and the last calls it made.
	// Derived from the leaf's own outcome by LeafState, general for any task.
	// Empty when the leaf left no structured record, which is the ordinary
	// case for a generalist that produced only prose — and an empty state is
	// simply left out of the composition.
	State string
	// Transcript is the dead attempt's OWN TURNS, read back from the record it
	// wrote as it worked (internal/store/transcript.go): the assistant text, the
	// tool calls and the results they returned, in the order they happened.
	//
	// THIS IS THE FIELD THAT MAKES A RESTART A RESUMPTION. The three above are
	// what the attempt CHOSE to announce — a partial result, some progress rows,
	// a structured summary — and a leaf abandoned mid-turn announced almost
	// nothing, because announcing is what a leaf does when it is finishing. Its
	// work is nonetheless all there: seventy-one runs of one test file, on the
	// happy-dom run of 2026-08-28, every one recorded under the node and every
	// one discarded when the claim reaper started the leaf over.
	//
	// It is expected to be PARTIAL. The recorder flushes on a full batch and on
	// every way out of a leaf, a fault included, so what survives is everything
	// up to the last flush — which is exactly what "resume from where it got to"
	// means, and is why nothing here tries to invent the result of a tool call
	// whose answer never arrived. See [BankedRun] for how one is rendered: an
	// outline of every turn the run took, and then its end verbatim.
	Transcript string
}

// Empty reports that there is nothing to hand on, in which case no caller should
// compose anything: an input announcing an earlier attempt that produced nothing
// is a sentence that costs tokens and teaches the model that the work has
func (b Bank) Empty() bool {
	return strings.TrimSpace(b.Partial) == "" && strings.TrimSpace(b.State) == "" &&
		strings.TrimSpace(b.Transcript) == "" &&
		len(b.trimmedShared()) == 0 && len(b.Artifacts) == 0
}

// Continuation is the bank composed under the continuation headers — the same
// two OverrunGoal writes, in the same order, with the same words.
func (b Bank) Continuation() string {
	var body strings.Builder
	if partial := b.partialBlock(); partial != "" {
		body.WriteString(ContinuationPartialHeader)
		body.WriteString("\n")
		body.WriteString(partial)
	}
	if len(b.Artifacts) > 0 {
		if body.Len() > 0 {
			body.WriteString("\n\n")
		}
		body.WriteString(ContinuationFilesHeader)
		body.WriteString("\n")
		body.WriteString(strings.Join(b.Artifacts, "\n"))
	}
	if state := strings.TrimSpace(b.State); state != "" {
		if body.Len() > 0 {
			body.WriteString("\n\n")
		}
		body.WriteString(ContinuationStateHeader)
		body.WriteString("\n")
		body.WriteString(state)
	}
	// Last, and longest. The three blocks above are the attempt's account of
	// itself; this is the attempt itself, and a reader that ran out of attention
	// before reaching it has already been told the summary.
	if transcript := strings.TrimSpace(b.Transcript); transcript != "" {
		if body.Len() > 0 {
			body.WriteString("\n\n")
		}
		body.WriteString(ContinuationTranscriptHeader)
		body.WriteString("\n")
		body.WriteString(transcript)
	}
	return body.String()
}

// Input is the bank as the retry sees it: one more dependency result, rendered
// by the leaf's own brief under the header that already says results here are
// work you hold and must not gather again.
//
// The paths ride inside Continuation rather than on Input.Artifacts because the
// brief renders that field as its own "(files: … — read them if you need the
// full detail)" line, and a bank that named its files twice would be spending
// the retry's context to say one thing in two voices.
func (b Bank) Input() executor.Input {
	return executor.Input{Title: BankInputTitle, Result: b.Continuation()}
}

// partialBlock is the text and the shared lines under one heading.
func (b Bank) partialBlock() string {
	partial := strings.TrimSpace(b.Partial)
	shared := b.trimmedShared()
	if len(shared) == 0 {
		return partial
	}
	var block strings.Builder
	if partial != "" {
		block.WriteString(partial)
		block.WriteString("\n\n")
	}
	block.WriteString(BankSharedLead)
	for _, line := range shared {
		block.WriteString("\n- ")
		block.WriteString(line)
	}
	return block.String()
}

func (b Bank) trimmedShared() []string {
	lines := make([]string, 0, len(b.Shared))
	for _, line := range b.Shared {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// WithShared returns the bank with more shared lines merged in, oldest first and
// each said once. It is how the two sources of the same fact join: what a leaf
// posted in this process, and what the journal remembers of what it posted in a
// process that is gone.
func (b Bank) WithShared(lines ...string) Bank {
	seen := make(map[string]bool, len(b.Shared)+len(lines))
	merged := make([]string, 0, len(b.Shared)+len(lines))
	for _, line := range append(append([]string(nil), b.Shared...), lines...) {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		merged = append(merged, line)
	}
	b.Shared = merged
	return b
}

// WithArtifacts returns the bank with more paths merged in, deduplicated and in
// a stable order.
func (b Bank) WithArtifacts(paths ...string) Bank {
	seen := make(map[string]bool, len(b.Artifacts)+len(paths))
	merged := make([]string, 0, len(b.Artifacts)+len(paths))
	for _, path := range append(append([]string(nil), b.Artifacts...), paths...) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		merged = append(merged, path)
	}
	sort.Strings(merged)
	b.Artifacts = merged
	return b
}

// NoteMark is the structural marker for a job-board note: written by code, read
// by code, so a board read can never mistake an anchored ask, receipt or
// progress post for a worker's shared line. It is a protocol byte, not a phrase
// the model is asked to produce.
const NoteMark = "⚑ "

// BankedProgressLimit is how many of an attempt's own lines ride into its
// successor. The last ones, because progress is cumulative: a row saying the
// writeup is assembled subsumes the eleven rows about assembling it.
const BankedProgressLimit = 16

// nodeRecord is the one read this file needs of the journal: a node's own
// record, in order.
type nodeRecord interface {
	NodeMessages(nodeID string, afterSeq int64, limit int) ([]store.Message, error)
}

// BankedProgress reads back what a node's earlier attempt said it had reached.
//
// It reads the node's OWN record and nothing else. Both shapes of "where this
// got to" live there and both are taken: the replaceable progress rows a long
// worker posts (their Latest line when they carry one, their phase line
// otherwise) and the board notes a worker shares in its own words. Nothing is
// matched against a list of phrases — a row either declares itself progress in
// its fields or carries the note marker, and everything else on a node's record
// is somebody talking ABOUT the work rather than the work reporting itself.
func BankedProgress(graph nodeRecord, nodeID string) []string {
	if graph == nil || strings.TrimSpace(nodeID) == "" {
		return nil
	}
	messages, err := graph.NodeMessages(nodeID, 0, 400)
	if err != nil {
		return nil
	}
	seen := make(map[string]bool, len(messages))
	lines := make([]string, 0, len(messages))
	for _, message := range messages {
		line := bankedLine(message)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		lines = append(lines, line)
	}
	if len(lines) > BankedProgressLimit {
		lines = lines[len(lines)-BankedProgressLimit:]
	}
	return lines
}

// ── the attempt's own turns ─────────────────────────────────────────────────
//
// THE DEFECT THIS ANSWERS. On the happy-dom run of 2026-08-28 the node
// `task-2-x1-n2` started three times, twenty minutes apart to the minute. Each
// start was a provider call that hung for fifteen minutes, a claim reaper that
// took the node back, and a leaf that began again — with an empty context, in a
// workspace already holding its own edits, having already run the project's test
// file seventy-one times. Every one of those turns was in the store: the leaflog
// recorder writes them under the node and flushes on every way out of a leaf.
// Nothing read them back.
//
// The bank already knew how to hand a dead attempt's work to its successor. What
// it could hand over was only what the attempt had ANNOUNCED — a partial result,
// its progress rows, its structured findings — and an attempt killed mid-turn
// announces almost nothing, because announcing is the last thing a leaf does.
// Its turns are the work.

const (
	// BankedTranscriptTurns is how many of the attempt's most recent turns ride
	// into its successor VERBATIM.
	//
	// THE LAST ONES, for the reason [BankedProgressLimit] takes the last progress
	// rows: work of this kind is cumulative, and the state a resuming leaf needs
	// is where the previous one had GOT TO. The head of a run is what an autopsy
	// wants and the store keeps it (store.MaxTranscriptEntries seals from the
	// front); a continuation wants the other end.
	//
	// IT IS NOT THE WHOLE SEED, and treating it as one was the memory half of
	// the ink run of 2026-08-29: twelve turns out of a hundred and thirty-eight
	// is a window on one file's contents, and the attempt's successor re-read
	// the repository because nothing had told it about the other hundred and
	// twenty-six. The outline above it (see [BankedRun]) carries every turn of
	// the run at one line each, which is what makes this bound affordable
	// instead of lossy.
	BankedTranscriptTurns = 12
	// BankedTranscriptBytes bounds the whole block, because a dozen turns of a
	// leaf that ran a test suite is not a dozen short lines.
	//
	// It is store.MaxTranscriptTextBytes × 8 and not a fresh figure: one entry's
	// bound is what this package already accepts as "enough of one step to tell
	// what happened", and this block is a handful of steps. Reached first, it
	// wins over the turn count and the block is trimmed from the FRONT, so what
	// survives is always the end of the work.
	BankedTranscriptBytes = 8 * store.MaxTranscriptTextBytes
	// bankedResultBytes bounds one tool result inside the block. A test run's
	// output is the largest thing a leaf ever holds and the least of it is worth
	// re-reading: what the resuming leaf needs is that the command ran and how it
	// ended, which is its tail (store.TruncateTranscriptText keeps both ends).
	bankedResultBytes = 1200
)

// transcriptRecord is the one read this needs of the journal.
type transcriptRecord interface {
	TranscriptFor(nodeID string, limit int) ([]store.TranscriptEntry, error)
}

// BankedTranscript renders a node's recorded turns as the block a resuming
// attempt is handed, or "" when this worker left no record.
func BankedTranscript(graph transcriptRecord, nodeID string) string {
	block, _ := BankedRun(graph, nodeID)
	return block
}

// BankedRun renders the LAST RUN in a node's record as the block a resuming
// attempt is handed, and says how many turns that run reached.
//
// EMPTY IS AN HONEST ANSWER AND A COMMON ONE. Not every worker records a
// transcript, and a leaf that died before its first flush recorded nothing. In
// both the bank simply has one fewer block, which is what it already does with
// every other field it does not have.
//
// A TOOL CALL WITH NO RESULT IS SAID TO HAVE NO RESULT. That is the shape a
// partial record takes — the attempt was interrupted between asking and being
// answered — and it is the one fact about it that must not be smoothed over: a
// resuming leaf that believed a command had run and returned nothing would skip
// the command. So the call is rendered with "(interrupted before it answered)"
// and the leaf can decide to run it again.
//
// IT IS ONE RUN AND NOT THE WHOLE TABLE. A node's record is every attempt any
// worker ever made under it, appended, and each attempt numbers its own turns
// from one — so "the last twelve turns" read across the table was not a window
// on anything. On the ink run of 2026-08-29 the third claim's seed was assembled
// from turns 79-90 of the attempt before it INTERLEAVED with turns 34-45 of the
// attempt before that, because both satisfied a turn-number cutoff. A run is
// found by structure instead: the turn counter only ever goes up inside one
// attempt, so where it goes backwards a new attempt began.
//
// AND IT CARRIES AN OUTLINE OF THE WHOLE RUN, not only its tail. The tail is
// where the work got to and the outline is what it decided on the way there, and
// handing over the second without the first is what an eleven-million-token
// re-exploration is made of: twelve turns of one file's contents say nothing
// about the thirty-eight files the attempt had already read and rejected. The
// outline is one line per turn over every turn of the run — what it said and
// what it ran — which is affordable precisely because it is one line.
func BankedRun(graph transcriptRecord, nodeID string) (string, int) {
	if graph == nil || strings.TrimSpace(nodeID) == "" {
		return "", 0
	}
	entries, err := graph.TranscriptFor(nodeID, 0)
	if err != nil || len(entries) == 0 {
		return "", 0
	}
	run := lastRecordedRun(entries)
	if len(run) == 0 {
		return "", 0
	}
	reached := run[len(run)-1].Turn
	answered := make(map[string]bool, len(run))
	for _, entry := range run {
		if entry.Kind == store.TranscriptToolResult && entry.CallID != "" {
			answered[entry.CallID] = true
		}
	}
	// Which turns survive into the verbatim tail, counted from the end of THIS
	// run. The entries carry their own turn number, so this is a read of the
	// record rather than an assumption about how many entries a turn has.
	cutoff := 0
	if reached > BankedTranscriptTurns {
		cutoff = reached - BankedTranscriptTurns
	}
	lines := make([]string, 0, len(run))
	for _, entry := range run {
		if entry.Turn <= cutoff {
			continue
		}
		if line := bankedTranscriptLine(entry, answered); line != "" {
			lines = append(lines, line)
		}
	}
	tail := trimmedFromTheFront(lines, BankedTranscriptBytes)
	outline := bankedRunOutline(run)
	var block strings.Builder
	if outline != "" {
		block.WriteString(BankedRunOutlineLead)
		block.WriteString("\n")
		block.WriteString(outline)
	}
	if tail != "" {
		if block.Len() > 0 {
			block.WriteString("\n\n")
		}
		block.WriteString(BankedRunTailLead)
		block.WriteString("\n")
		block.WriteString(tail)
	}
	if block.Len() == 0 {
		return "", 0
	}
	return block.String(), reached
}

// The two leads inside the transcript block. They are separate because the two
// halves answer different questions and a model handed them under one heading
// reads the outline as a preamble to the tail rather than as the record of
// thirty turns it will otherwise repeat.
const (
	// BankedRunOutlineLead introduces the one-line-per-turn account of the
	// WHOLE run.
	BankedRunOutlineLead = "In outline, every turn that attempt took, oldest first — this is the whole of it, and none of it needs doing again:"
	// BankedRunTailLead introduces the verbatim end of the run.
	BankedRunTailLead = "And the end of it verbatim — what you said, what you ran, and what came back:"
)

// lastRecordedRun is the slice of entries belonging to the most recent attempt.
//
// A run boundary is a turn number that goes DOWN, which is structure rather than
// a guess: within one attempt the counter only ever rises, and a fresh attempt
// numbers from one. Entries on turn zero — the harness's own notes, the elision
// marker, a fault recorded before any turn — carry no boundary information and
// are left attached to whatever they follow.
func lastRecordedRun(entries []store.TranscriptEntry) []store.TranscriptEntry {
	start, previous := 0, 0
	for index, entry := range entries {
		if entry.Turn <= 0 {
			continue
		}
		if previous > 0 && entry.Turn < previous {
			start = index
		}
		previous = entry.Turn
	}
	return entries[start:]
}

// bankedRunOutline is one line per turn of the run: what the turn ran and what
// it said, in that order, because the tools are the shorter half and a reader
// scanning for "did it already try this" is scanning for them.
//
// It is clipped from the MIDDLE rather than the front, which is the opposite of
// what the tail does and correct for the same reason: an outline that keeps only
// its end has thrown away what the attempt set out to do, and the head of a run
// is where that is stated.
func bankedRunOutline(run []store.TranscriptEntry) string {
	lines := make([]string, 0, 32)
	turn, said, tools := 0, "", []string(nil)
	flush := func() {
		if turn <= 0 || (said == "" && len(tools) == 0) {
			return
		}
		line := fmt.Sprintf("turn %d", turn)
		if len(tools) > 0 {
			line += " · ran " + strings.Join(tools, ", ")
		}
		if said != "" {
			line += " · " + said
		}
		lines = append(lines, line)
	}
	for _, entry := range run {
		if entry.Turn != turn {
			flush()
			turn, said, tools = entry.Turn, "", nil
		}
		switch entry.Kind {
		case store.TranscriptAssistant:
			if said == "" {
				said = outlineSentence(entry.Text)
			}
		case store.TranscriptToolCall:
			tool := strings.TrimSpace(entry.Tool)
			if tool == "" {
				tool = "a tool"
			}
			tools = append(tools, tool)
		case store.TranscriptFault, store.TranscriptNote:
			if sentence := outlineSentence(entry.Text); sentence != "" {
				said = sentence
			}
		}
	}
	flush()
	return clipMiddle(strings.Join(lines, "\n"), BankedTranscriptBytes)
}

// outlineSentence is one turn's words as a single clipped line. A model's turn
// opens with what it has decided and continues into how it will do it, so the
// first line is the half worth an outline's budget.
func outlineSentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if cut := strings.IndexByte(text, '\n'); cut >= 0 {
		text = strings.TrimSpace(text[:cut])
	}
	if len(text) > bankedOutlineSentenceBytes {
		text = strings.TrimSpace(text[:bankedOutlineSentenceBytes]) + "…"
	}
	return text
}

// bankedOutlineSentenceBytes bounds one outline line's prose. It is
// store.MaxTranscriptTextBytes / 16 and not a fresh figure: a whole recorded
// step is worth that bound, and one line about a step is worth a sixteenth of
// one — which is about a sentence, which is what this is.
const bankedOutlineSentenceBytes = store.MaxTranscriptTextBytes / 16

// bankedTranscriptLine renders one recorded entry, or nothing for a kind whose
// only reader is an autopsy.
func bankedTranscriptLine(entry store.TranscriptEntry, answered map[string]bool) string {
	text := strings.TrimSpace(entry.Text)
	switch entry.Kind {
	case store.TranscriptAssistant:
		if text == "" {
			return ""
		}
		return "you said: " + text
	case store.TranscriptToolCall:
		line := "you ran " + entry.Tool
		if text != "" {
			line += " with " + text
		}
		if entry.CallID != "" && !answered[entry.CallID] {
			line += " (interrupted before it answered)"
		}
		return line
	case store.TranscriptToolResult:
		outcome := "it returned"
		if entry.Failed {
			outcome = "it failed with"
		}
		if text == "" {
			return outcome + " nothing"
		}
		return outcome + ": " + store.TruncateTranscriptText(clipMiddle(text, bankedResultBytes))
	case store.TranscriptNote, store.TranscriptFault:
		// The harness talking about its own machinery, and how the attempt
		// ended. Both are worth carrying — "the stream was cut and the turn was
		// asked again" explains a gap the turns themselves cannot — and both are
		// marked as the harness speaking so they are not read as the model's.
		if text == "" {
			return ""
		}
		return "(the harness noted: " + text + ")"
	default:
		return ""
	}
}

// clipMiddle keeps a value's head and tail with the gap named, which is what
// store.TruncateTranscriptText does at its own bound; this applies the tighter
// bound a continuation can afford before that one is asked.
func clipMiddle(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	head := limit * 1 / 3
	tail := limit - head
	return text[:head] + "\n… … …\n" + text[len(text)-tail:]
}

// trimmedFromTheFront joins the lines and drops them from the OLD end until the
// block fits. The end of the work is what a continuation is for.
func trimmedFromTheFront(lines []string, limit int) string {
	for len(lines) > 0 {
		block := strings.Join(lines, "\n")
		if len(block) <= limit {
			return block
		}
		lines = lines[1:]
	}
	return ""
}

// bankedLine reads one record row as a progress line, or says it is not one.
func bankedLine(message store.Message) string {
	if message.Progress != nil {
		if latest := strings.TrimSpace(message.Progress.Latest); latest != "" {
			return latest
		}
		return strings.TrimSpace(message.Body)
	}
	if message.Role != store.RoleAgent || message.QuestionSeq != 0 || message.CommandSeq != 0 ||
		message.Brief != nil {
		return ""
	}
	if !strings.HasPrefix(message.Body, NoteMark) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(message.Body, NoteMark))
}

// LeafState derives what a dead leaf's worker actually did — the files it
// touched, the commands it issued, what the finished-tree reading found, and
// the last calls it made — from the leaf's own outcome. It is general for any
// task: a worker that owns a verifier contributed its structured account, and
// every worker contributes the bounded tail of what it did. The principle: a
// continuation that knows what the dead leaf already found resumes from
// there instead of re-reading everything it already diagnosed, which is
// how a one-line fix that exhausted 150k tokens spawned a continuation
// that spent 137k fresh tokens re-discovering the same diagnosis.
//
// Empty when the leaf left no structured record — no account and no tool
// calls worth reporting — which is the ordinary case for a generalist that
// produced only prose. An empty state is simply left out of every
// composition that uses it.
func LeafState(outcome *executor.Outcome) string {
	if outcome == nil {
		return ""
	}
	var parts []string
	// The structured account: files changed with sizes, commands the leaf
	// issued, and checks the closing photograph ran. Only a worker that
	// photographs its own change set or finished tree fills this in; for every
	// other leaf it is nil.
	if outcome.Account != nil {
		if report := strings.TrimSpace(outcome.Account.Report()); report != "" {
			parts = append(parts, report)
		}
	}
	// A red the leaf was never asked to settle used to reach nobody, so the next
	// leaf paid to rediscover it. The landing now hands over the finding's fact,
	// without an instruction addressed to the worker that already stopped.
	if len(outcome.Standing) > 0 {
		var lines strings.Builder
		lines.WriteString("What its own reading of the finished tree found, and it landed holding:")
		for _, finding := range outcome.Standing {
			lines.WriteString("\n  - ")
			lines.WriteString(finding.Fact)
		}
		parts = append(parts, lines.String())
	}
	// The bounded tail of what the worker did, in order. Every worker
	// contributes this — it is the flight recorder's own record of the last
	// calls, clipped to keep one pasted file from filling a judge's context.
	if len(outcome.Ran) > 0 {
		var lines strings.Builder
		lines.WriteString("Last calls the worker made, in order:")
		for _, call := range outcome.Ran {
			lines.WriteString("\n  ")
			lines.WriteString(call)
		}
		parts = append(parts, lines.String())
	}
	return strings.Join(parts, "\n\n")
}

// LineageBank is what a whole job's recorded work hands to the next node
// spliced under it.
//
// THE RECORDED RUNS ARE A LINEAGE PROPERTY, NOT A NODE'S. BankedRun answers
// "what did THIS node do", which serves the two paths where the id stays the
// same — the in-place retry and the requeue after a claim comes back. It cannot
// serve the path a growing job actually takes: a round splices FRESH IDS
// (`task-2` → `task-2-x1-n2`), so every child asks its own empty record and
// starts cold beside a workspace full of its predecessors' work.
//
// The textual run of 2026-08-29 (s9) is the measurement. `task-2` ran to 350
// recorded rows over 80 turns and exhausted; thirteen minutes later a gap round
// spliced five fresh children under it, and `task-2-x1-n2`'s first recorded row
// is turn 1, "Let me start by examining the existing codebase", followed by
// `find /app`. The record it needed was in the same store, under the id one
// character away, and nothing looked.
//
// It is composed newest-first and bounded once, not per node: what a resuming
// worker most needs is where the job GOT TO, and a bound spent on the oldest
// attempt is a bound not spent on the newest. The sink is skipped because it is
// the node being seeded — a brief that quotes the reader back to itself is a
// brief that has said nothing.
func LineageBank(graph *store.Store, lineage, sink string) (string, int) {
	if graph == nil || strings.TrimSpace(lineage) == "" {
		return "", 0
	}
	nodes, err := graph.LineageNodes(lineage)
	if err != nil {
		// AN UNREADABLE LINEAGE IS NOT AN EMPTY ONE, and the difference has to
		// be audible. Returning nothing on an error is indistinguishable from a
		// job that genuinely has no record, which is the reading that starts
		// every successor cold — the exact silence this function exists to end.
		// It is still not allowed to fail the work: a seed is an account of the
		// job, never a part of it.
		log.Printf("note: could not read the lineage of %s to seed its next piece: %v", lineage, err)
		return "", 0
	}
	if len(nodes) == 0 {
		return "", 0
	}
	var block strings.Builder
	resumed := 0
	// Newest first: LineageNodes is oldest-first admission order, and the last
	// thing the job did is the first thing its successor needs.
	for index := len(nodes) - 1; index >= 0; index-- {
		node := nodes[index]
		if node.ID == sink {
			continue
		}
		run, turns := BankedRun(graph, node.ID)
		if turns == 0 || strings.TrimSpace(run) == "" {
			continue
		}
		if block.Len()+len(run) > BankedTranscriptBytes {
			break
		}
		if block.Len() > 0 {
			block.WriteString("\n\n")
		}
		// Each run says whose it was. A composition of three attempts read as
		// one continuous transcript would have the reader believe a decision
		// taken by one worker was taken by another.
		block.WriteString(lineageRunLead(node))
		block.WriteString("\n")
		block.WriteString(run)
		resumed += turns
	}
	return block.String(), resumed
}

// lineageRunLead names one recorded run inside a composed bank.
func lineageRunLead(node store.Node) string {
	title := strings.TrimSpace(node.Title)
	if title == "" {
		title = strings.TrimSpace(firstLineOf(node.Brief))
	}
	if title == "" {
		return "From an earlier piece of this job (" + node.ID + "):"
	}
	return "From an earlier piece of this job — " + title + " (" + node.ID + "):"
}

// firstLineOf is the first line of a brief, for a one-line label.
func firstLineOf(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}
