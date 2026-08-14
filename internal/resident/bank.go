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
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/provider"
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
}

// Empty reports that there is nothing to hand on, in which case no caller should
// compose anything: an input announcing an earlier attempt that produced nothing
// is a sentence that costs tokens and teaches the model that the work has
func (b Bank) Empty() bool {
	return strings.TrimSpace(b.Partial) == "" && strings.TrimSpace(b.State) == "" &&
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

// ClockDeath reports that an attempt ended because time ran out, and for no
// other reason.
//
// It reads shapes, never sentences. The executor records how its own loop ended
// (StopDeadline, and Exhausted for a loop ordered to land because the clock was
// close); a context that expired is context.DeadlineExceeded; a transport that
// timed out satisfies the standard Timeout() interface net.Error and the node
// watchdog both speak; and a provider that answered with a timeout status says
// so in provider.APIError.Status. A string match would be a fifth answer that
// could disagree with the four.
func ClockDeath(outcome *executor.Outcome, err error) bool {
	if outcome != nil && (outcome.Stop == executor.StopDeadline || outcome.Exhausted == executor.StopDeadline) {
		return true
	}
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return true
	}
	var api *provider.APIError
	if errors.As(err, &api) && api != nil {
		switch api.Status {
		case http.StatusRequestTimeout, http.StatusGatewayTimeout:
			return true
		}
	}
	return false
}

// MayReclassify reports whether the way an attempt ended is evidence about what
// KIND of worker this assignment needs.
//
// The rule it enforces, and the reason it exists: a leaf that dies on its time
// ceiling was WORKING. It ran out of clock, which is a fact about the clock. In
// the incident this file was written for, a writeup-and-benchmark leaf that died
// nine minutes after announcing its finished document was recalibrated onto a
// repository-shaped coding worker for its retry, and the retry opened by
// "preparing the repository" for a comparison document. Nothing about the ending
// said the worker was the wrong kind; only that the hour was up.
//
// The taxonomy is not a new one. provider.Verdict already separates what may be
// learned from — a budget stop, a turn cap, an empty response, a reply that did
// not parse or was wrong — from what may not: a provider failure, which is
// weather, and an unverified success, which is silence. Verdict.Escalates is
// that question already asked, and a class change is the same question about a
// different rung, so it gets the same answer.
//
// An attempt that produced no outcome at all and failed for a reason that is not
// the clock is capability evidence by elimination: something refused before any
// work happened, and who to ask instead is exactly the open question.
func MayReclassify(outcome *executor.Outcome, err error) bool {
	if ClockDeath(outcome, err) {
		return false
	}
	if outcome != nil && outcome.Verdict != "" {
		return outcome.Verdict.Escalates()
	}
	return err != nil
}

// LeafState derives what a dead leaf's worker actually did — the files it
// touched, the checks it ran, and the last calls it made — from the leaf's
// own outcome. It is general for any task: a worker that owns a verifier
// contributed its structured account (files changed, checks run), and every
// worker contributes the bounded tail of what it did. The principle: a
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
	// The structured account: files changed with sizes, checks run with
	// verdicts. Only a worker that photographs its own change set and runs
	// its own verifier fills this in; for every other leaf it is nil.
	if outcome.Account != nil {
		if report := strings.TrimSpace(outcome.Account.Report()); report != "" {
			parts = append(parts, report)
		}
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
