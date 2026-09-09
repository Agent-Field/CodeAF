package session

// A node's REPORT is the card: the worker's last message cut to three lines of
// three hundred characters ([taskReport]), composed with what the check said
// about it. Its RESULT is what the work actually produced, kept whole up to one
// cap with a pointer to the rest of it.
//
// The two are separate because the readers are. A row on the roster wants the
// card; the dependent that has to build on the answer, the parent folding a
// piece back into the whole, the conversation writing the person's reply and
// the continuation telling a second attempt what the first one produced all
// want the work. Every one of them used to read the card, so a task asked for a
// command and answering with three lines of preamble delivered the preamble.
//
// The result is written on the checkpoint beside the report, so it survives the
// process the same way. A checkpoint from before it existed has none, and every
// reader below falls back to the report — which is what it read before.
//
// Nothing here is ever composed with a landing's own sentences. An audit's lead
// line, a settle and a branch outcome are written into the report; the result is
// the work's half alone, so a verdict landing an hour later rewrites the card
// without touching what the work produced.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// taskResultLimit is the one cap on what a node's record keeps: a long
	// research answer or a short document. The checkpoint is rewritten on every
	// transition, so a graph of thirty nodes each holding an unbounded
	// transcript would be a file this session writes hundreds of times. Past the
	// cap the whole text goes to a file of its own and the record keeps the
	// pointer, so the cap bounds what is carried rather than what is kept.
	taskResultLimit = 16000

	// taskResultCarry is how much of the answer one landing note, one
	// continuation's finding or one notice hands a reader. It is smaller than
	// what is kept because these ride in a model's context — a parent folding
	// five pieces back reads five results — and the pointer beside them is what
	// makes the rest reachable.
	taskResultCarry = 4000

	// resultWholeLead and resultPartLead open the block that carries the answer,
	// and which of them is used is a statement of fact: the reader either has all
	// of it or has the beginning of it. Saying "in full" over an excerpt is the
	// defect this pair exists to prevent.
	resultWholeLead = "what it produced, in full"
	resultPartLead  = "what it produced, the first part of it"

	// resultWholeAt names where the rest can be read: the file an overflowing
	// result was written to, or the node's own transcript, which holds the
	// message whole. resultWholeGone is the honest alternative when neither
	// exists — a node with no transcript whose sidecar could not be written.
	resultWholeAt   = " — the whole of it is at "
	resultWholeGone = " — the rest of it was not kept"

	// resultHeldLead is what a landing that turned the work back says instead of
	// the answer, and resultHeldOnRecord is its tail when there is no file and no
	// transcript to name: the answer is still on the task's own record, which is
	// what its page and its room read.
	resultHeldLead     = "what it produced was not accepted"
	resultHeldOnRecord = " — it is on the task's own record"

	// taskResultSuffix names the file an oversized result is written to. It sits
	// beside the node's transcript, under the session's journal directory,
	// because that outlives the working copy: a worktree is deleted when the
	// work merges. The transcript name is minted per run, so a second attempt
	// writes its own file rather than overwriting the pointer an earlier landing
	// handed out; a repair round inside one run does replace it, before anything
	// has been handed out at all.
	taskResultSuffix = "-result.txt"
)

// taskResult is what one node produced, as the record keeps it. text is bounded
// by [taskResultLimit] and marked by [clip] when it was cut; bytes is the size
// of what the worker actually said, before any cut; source is the node's
// transcript; overflow is the file this harness wrote when the answer was longer
// than any reader is handed, and "" when it wrote none.
type taskResult struct {
	text     string
	bytes    int
	source   string
	overflow string
}

// TaskReportAccount removes only the exact answer suffix the runner composed
// into a report. Expanded readers already show that answer in full; retaining
// its bounded preview would repeat it above the answer in diagnostic ink.
// Older records used a plain three-line preview, which remains recognizable.
// Unmatched reports remain intact, including accounts rewritten after landing.
func TaskReportAccount(report, result string) string {
	report, result = strings.TrimSpace(report), strings.TrimSpace(result)
	if result == "" {
		return report
	}
	for _, answer := range []string{result, composeTaskReport(result), firstLines(result, taskReportLines)} {
		if report == answer {
			return ""
		}
		if strings.HasSuffix(report, "\n"+answer) {
			return strings.TrimSpace(strings.TrimSuffix(report, "\n"+answer))
		}
	}
	return report
}

// keepResult records what the worker said, whole, beside the report the card is
// cut from. It clears nothing, exactly as [TaskNode.keepClaim] does not: a
// repair round that came back with nothing to say leaves the last real answer
// standing.
//
// The sidecar is written whenever the answer is longer than one reader is
// handed, so that a partial view always has a clean full text to point at, and
// it is written before the lock is taken — the graph's lock is not something to
// hold across a file write.
func (n *TaskNode) keepResult(said string) {
	if n == nil || n.graph == nil {
		return
	}
	said = strings.TrimSpace(said)
	if said == "" {
		return
	}
	journal := n.journalPath()
	result := taskResult{text: said, bytes: len(said), source: taskURI(journal)}
	if len(said) > taskResultLimit {
		result.text = clip(said, taskResultLimit)
	}
	if len(said) > taskResultCarry {
		result.overflow = writeResultWhole(journal, said)
	}
	n.graph.mu.Lock()
	n.produced = result
	n.graph.mu.Unlock()
}

// keepResultNoting is [TaskNode.keepResult] with a line in the job log when the
// answer was long enough to be written beside the transcript: the path is the
// one thing a reader of that log could not work out for themselves. It is a
// function of its own because the road that calls it is at its ledgered length
// (complexity_test.go).
func (n *TaskNode) keepResultNoting(said string, log io.Writer) {
	n.keepResult(said)
	kept := n.result()
	if kept.overflow == "" || log == nil {
		return
	}
	fmt.Fprintf(log, "its answer ran to %s: the whole of it is at %s\n", resultSizeWord(kept), kept.overflow)
}

// writeResultWhole puts the whole answer on disk beside the node's transcript
// and answers the path it wrote, or "" when it could not — including when the
// node has no transcript to sit beside.
//
// It writes through a temporary file in the same directory and renames, so a
// reader following the pointer never opens a half-written answer, and a failure
// leaves no partial file claiming to be the whole. A failure is silence: the
// body is still kept, the transcript still holds the message, and [taskResult]
// simply has no overflow to point at.
func writeResultWhole(journal, said string) string {
	journal = strings.TrimSpace(journal)
	if journal == "" {
		return ""
	}
	path := strings.TrimSuffix(journal, filepath.Ext(journal)) + taskResultSuffix
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return ""
	}
	temporary, err := os.CreateTemp(directory, filepath.Base(path)+".*")
	if err != nil {
		return ""
	}
	if _, err := temporary.WriteString(said); err != nil {
		temporary.Close()
		os.Remove(temporary.Name())
		return ""
	}
	if err := temporary.Close(); err != nil {
		os.Remove(temporary.Name())
		return ""
	}
	if err := os.Chmod(temporary.Name(), 0o600); err != nil {
		os.Remove(temporary.Name())
		return ""
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		os.Remove(temporary.Name())
		return ""
	}
	return path
}

// result is the node's kept answer, read under the graph's lock like every
// other leaving beside it.
func (n *TaskNode) result() taskResult {
	if n == nil || n.graph == nil {
		return taskResult{}
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.resultLocked()
}

// resultLocked is [TaskNode.result] for a caller already inside the graph's
// lock — the brief assembly and the notice builder both are.
func (n *TaskNode) resultLocked() taskResult { return n.produced }

// carried is the body a reader's context is handed and whether that is all of
// the answer. It is cut either because the record itself was cut at
// [taskResultLimit] or because the carry is smaller than what the record kept.
func (r taskResult) carried() (string, bool) {
	body := strings.TrimSpace(r.text)
	if body == "" {
		return "", false
	}
	cut := r.bytes > len(r.text)
	if len(body) > taskResultCarry {
		body, cut = clip(body, taskResultCarry), true
	}
	return body, cut
}

// whereWhole is the address of the whole answer: the file this harness wrote,
// and the node's own transcript otherwise. Empty when there is neither, and the
// block then says the rest was not kept rather than pointing at nothing.
func (r taskResult) whereWhole() string {
	if r.overflow != "" {
		return taskURI(r.overflow)
	}
	return r.source
}

// resultDelivery is what one reader is handed about what a node produced: the
// body, where the whole of it can be read, whether the body is only the
// beginning of it, and whether the answer is being named rather than handed on.
type resultDelivery struct {
	body  string
	where string
	cut   bool
	held  bool
}

// resultBlock is the section a reader gets under a node's report.
//
// A held delivery is one line: what the work produced was not accepted, and
// where it can be read. It is never the body, because a landing that turned the
// work back has already answered the claim and recycling it would hand a reader
// a rejected account as though it stood — and it is never silence either, since
// the output still exists and a person or a model may want to look at it.
//
// The address rides the lead rather than the foot because a fit downstream cuts
// the tail, and a pointer written last would be the first thing to go.
func resultBlock(delivery resultDelivery) string {
	where := strings.TrimSpace(delivery.where)
	if delivery.held {
		if where == "" {
			return resultHeldLead + resultHeldOnRecord
		}
		return resultHeldLead + resultWholeAt + where
	}
	body := strings.TrimSpace(delivery.body)
	if body == "" {
		return ""
	}
	if !delivery.cut {
		return resultWholeLead + ":\n" + body
	}
	whole := resultWholeGone
	if where != "" {
		whole = resultWholeAt + where
	}
	return resultPartLead + whole + ":\n" + body
}

// carriedResultLocked is what this node hands a reader, and it holds the rules
// about how an answer travels. They are asked of the node's STATE and ENDING —
// facts the record carries — and never of the report's wording, which a late
// verdict, an accept or a re-check may rewrite long after the answer was kept.
//
//   - Nothing has landed yet: nothing is handed over. A node still working has
//     no outcome to qualify its answer with.
//   - The check did not accept the work ([TaskEndingRefused], the one outcome
//     that means somebody looked and said no): the answer is NAMED, not handed
//     on. What is missing is the news, and the work's own account of a job that
//     was turned back must not be recycled as though it stood — but it is still
//     there to read, and where is said out loud.
//   - Everything else that has landed — done, needing a look, halted, stopped —
//     hands the answer over. Those landings are about how far the work got, not
//     about the answer being wrong, and their reports already stand over the
//     work's own account.
//
// The one thing the report is asked is whether it already contains the answer
// EXACTLY: an ordinary two-line landing says everything it has to say in its own
// three lines, and a block repeating it would be the same sentence twice. That
// test can only omit an answer the reader is already holding, so a report that
// has been rewritten simply fails it and the answer is handed over.
func (n *TaskNode) carriedResultLocked() resultDelivery {
	body, cut := n.produced.carried()
	if body == "" || !n.state.settled() {
		return resultDelivery{}
	}
	where := n.produced.whereWhole()
	if n.state == TaskFailed && n.endingLocked() == TaskEndingRefused {
		return resultDelivery{where: where, held: true}
	}
	if !cut && strings.Contains(n.report, body) {
		return resultDelivery{}
	}
	return resultDelivery{body: body, where: where, cut: cut}
}

// deliveredLocked is this node's report with what it produced under it: what the
// dependent's brief and the continuation's finding are built from, both
// assembled inside the graph's lock. The report leads because a landing's own
// sentences — what the check said, what is still missing, where the branch went
// — are what qualify the answer under them.
func (n *TaskNode) deliveredLocked() string {
	delivery := n.carriedResultLocked()
	// The pointer is dropped from a body the caller will fit again and named on
	// the header instead ([TaskNode.resultPointerLocked]); a held delivery keeps
	// its own, because its whole block is one short line no pot will clip.
	if !delivery.held {
		delivery.where = ""
	}
	return withReport(n.report, resultBlock(delivery))
}

// resultPointerLocked is the clause naming where the whole answer is, for a
// caller that puts it somewhere no fit can reach. It is empty when the reader is
// getting all of the answer anyway.
func (n *TaskNode) resultPointerLocked() string {
	delivery := n.carriedResultLocked()
	switch {
	case delivery.held || !delivery.cut:
		return ""
	case strings.TrimSpace(delivery.where) == "":
		return resultWholeGone
	default:
		return resultWholeAt + delivery.where
	}
}

// taskResultRecord is the result as the checkpoint carries it (task_store.go).
// It is a shape of its own rather than four fields on [taskRecord] so that a
// record written before results existed decodes to nil and the node it rebuilds
// falls back to its report.
type taskResultRecord struct {
	// Text is the kept body, bounded by [taskResultLimit] when it was written.
	Text string `json:"text,omitempty"`
	// Bytes is what the worker actually said, before any cut.
	Bytes int `json:"bytes,omitempty"`
	// Source is the node's transcript URI and Whole the overflow file's path.
	Source string `json:"source,omitempty"`
	Whole  string `json:"whole,omitempty"`
}

// resultRecordOf and resultFromRecord are the two halves of the checkpoint
// seam, written next to each other so a field added to one is hard to forget in
// the other.
func resultRecordOf(r taskResult) *taskResultRecord {
	if strings.TrimSpace(r.text) == "" {
		return nil
	}
	return &taskResultRecord{Text: r.text, Bytes: r.bytes, Source: r.source, Whole: r.overflow}
}

func resultFromRecord(record *taskResultRecord) taskResult {
	if record == nil {
		return taskResult{}
	}
	kept := taskResult{text: record.Text, bytes: record.Bytes, source: record.Source, overflow: record.Whole}
	// A record that never learned its own size is not a result that was cut:
	// reading an absent Bytes as zero would make every restored answer look
	// shorter than its own text, and every reader would then be told it had only
	// part of what it has all of.
	if kept.bytes < len(kept.text) {
		kept.bytes = len(kept.text)
	}
	return kept
}

// resultSizeWord is how a log line says how much a node produced. It is a log
// and not a card, so a count is the useful thing to say.
func resultSizeWord(r taskResult) string {
	if r.bytes == 0 {
		return ""
	}
	return fmt.Sprintf("%d bytes", r.bytes)
}
