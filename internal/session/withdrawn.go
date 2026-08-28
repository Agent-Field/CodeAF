package session

// Withdrawal: what a call to a hand the HARNESS TOOK AWAY is answered with, and
// how a step the harness itself failed is told apart from a step the model got
// wrong.
//
// ── THE MEASURED FAILURE ──
//
// SWE-Marathon s4, task 1, 00:37Z–00:40Z. A worker building a Java language
// server in Rust had been editing, building and testing for half an hour. Then:
// one edit that replaced text with itself ("No changes made … the replacement
// produced identical content"), four greps with a malformed pattern, each
// answered "No matches found". Five steps that taught nothing, and the
// no-progress ratchet fired ([taskLimits.noProgress], task_run.go): LAND NOW,
// and the belt was narrowed to the hands that save a file ([landingBelt]).
//
// What the model was told about that narrowing, by the harness, eight times,
// was eighteen bytes:
//
//	Unknown tool: read
//	Unknown tool: bash
//
// "Unknown" is a sentence about the MODEL — you named a verb nobody has. It was
// a lie: the harness had the tool a second earlier and took it. It names no
// reason, no surviving set, and nothing to do instead, so the only rational
// move left is to try again — the call was legal thirty seconds ago and nothing
// said otherwise. The model retried `read` twice, the same grep three times,
// the same `cargo build` three times. The stuck watch (looped.go) then injected
// three [stuck] notes scolding it for the repetition the harness had just
// manufactured. At 00:40:22 the model worked it out for itself, from nothing
// but the shape of the failures: "I can't build or run tests — bash is not in
// my tool list … No bash, no read, no grep, no find."
//
// Meanwhile it had saved real edits to parser.rs and analysis.rs at 00:39:50
// and 00:40:02 — after the build hand was gone. The task landed with those
// edits NEVER COMPILED, reported as work, and the chat had to discover and
// build them five minutes later.
//
// ── THE LAW ──
//
// A capability the harness WITHDRAWS is reported as a withdrawal, never as an
// unknown verb. The result says WHY it is gone, WHAT REMAINS by name, and WHAT
// TO DO with what remains. A name nobody ever had is still "Unknown tool" —
// that one really is a sentence about the model, and the two must not be told
// with the same words (loop.go's [Agent.executeTool]).
//
// And a step whose failure the harness produced is the HARNESS'S step, not the
// model's: it never advances a stuck counter and never feeds the loop watch
// ([toolResult.harness] — set once at the chokepoint, carried out to the
// runner on [Event.HarnessMade]).

import (
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// landingWithdrawal is why the landing turn's belt is narrow, in the words the
// model is given. It is the model's own frame — its work is being brought home
// — and not the machinery's ("the no-progress threshold fired"), for the reason
// task_run.go's vocabulary law states: what the model reads it says back.
const landingWithdrawal = "the work is being brought home"

// toolWithdrawal is the record of a belt DELIBERATELY narrowed: which hands
// went, why, and which ones the model still holds.
//
// It exists because the belt itself cannot answer the question. A tool that is
// gone from the belt and a tool that was never on it are the same absence to
// [Agent.executeTool] — one map lookup, one miss — and the whole difference
// between the two answers is a fact only the code that did the narrowing knows.
// So the narrowing records it, at the moment it happens, and the dispatcher
// reads it back.
type toolWithdrawal struct {
	// reason is why the hands are gone, in the model's terms.
	reason string
	// gone is the set of names that were on the belt and are not any more.
	gone map[string]bool
	// kept is the surviving set, in belt order — the exact list the model is
	// told it still has, read off the same slice the request carries, so the
	// sentence and the belt can never disagree.
	kept []string
}

// newToolWithdrawal records what the narrowing from `before` to `after` took.
func newToolWithdrawal(before, after []bare.Tool, reason string) *toolWithdrawal {
	surviving := make(map[string]bool, len(after))
	kept := make([]string, 0, len(after))
	for _, tool := range after {
		surviving[tool.Name] = true
		kept = append(kept, tool.Name)
	}
	gone := make(map[string]bool, len(before))
	for _, tool := range before {
		if !surviving[tool.Name] {
			gone[tool.Name] = true
		}
	}
	return &toolWithdrawal{reason: reason, gone: gone, kept: kept}
}

// notice is what the model reads when it reaches for a hand that was taken, and
// reports false for every other name — including a name nobody ever had, which
// is somebody else's answer to give.
//
// THREE FACTS AND NO MORE. Why it is gone, what is left, what to do with what
// is left. The third is the one the eighteen-byte answer was missing and the
// one that stops the retry: a model told only that a call failed will make it
// again, because a failure with no reason is indistinguishable from a failure
// that might not repeat.
func (w *toolWithdrawal) notice(name string) (string, bool) {
	if w == nil || !w.gone[name] {
		return "", false
	}
	return name + " was withdrawn from your tools: " + w.reason + ". " +
		"You still have " + englishList(w.kept) + ". " +
		"Calling " + name + " again cannot bring it back and will fail the same way. " +
		"Finish what you are saving with the tools above and stop — there is nothing left to run and nothing left to poll.", true
}

// withdrawTools narrows this agent's belt with `narrow`, records the withdrawal
// so a call to a taken hand can be answered as one, and reports the restore.
//
// IT IS THE ONE DOOR, AND IT TAKES THE NARROWING RATHER THAN THE RESULT. The
// landing pass used to swap the two slice headers by hand; the belt it is
// narrowing, the belt it becomes and the record of the difference all have to be
// read and written under ONE hold of armMu, or a call racing the swap reads a
// belt that has already lost a tool against a record that does not yet say so —
// which is the eighteen-byte answer again, reached by a different road.
//
// Restoring puts back the belt, its wire form and any OUTER withdrawal that was
// in force, so nesting cannot leave a stale record standing over a full belt.
func (a *Agent) withdrawTools(narrow func([]bare.Tool) []bare.Tool, reason string) func() {
	a.armMu.Lock()
	defer a.armMu.Unlock()
	tools, definitions, outer := a.tools, a.definitions, a.withdrawn
	kept := narrow(tools)
	a.tools = kept
	a.definitions, _ = toolDefinitions(kept)
	a.withdrawn = newToolWithdrawal(tools, kept, reason)
	return func() {
		a.armMu.Lock()
		defer a.armMu.Unlock()
		a.tools, a.definitions, a.withdrawn = tools, definitions, outer
	}
}

// withdrawalNotice answers a name against the withdrawal in force, if any.
func (a *Agent) withdrawalNotice(name string) (string, bool) {
	a.armMu.Lock()
	withdrawn := a.withdrawn
	a.armMu.Unlock()
	return withdrawn.notice(name)
}

// unverifiedEdits is what a landing says about files a node saved AFTER the
// harness took its checking hand away.
//
// IT BORROWS THE VOCABULARY A LANDING ALREADY HAS ([incompleteLead]) rather
// than inventing a state. Nothing new happened to the work: it is the ordinary
// "somebody should look at this" ending, reached because the one party that
// could have looked — the node itself, with its build — was disarmed one turn
// before it wrote the files. And it says only what is knowable: not that the
// edits are wrong, only that nothing checked them.
const unverifiedEdits = incompleteLead +
	"nothing checked the files it saved after its tools were withdrawn — they were never built or run"

// landingLostTheCheck reports whether the withdrawal in force took away a hand
// this agent had been checking its work with.
func (a *Agent) landingLostTheCheck() bool {
	a.armMu.Lock()
	withdrawn := a.withdrawn
	a.armMu.Unlock()
	return withdrawn.withdrewChecking()
}

// withdrewChecking reports whether the narrowing took away a hand this node had
// been CHECKING its work with — the reason a file saved afterwards is unverified.
//
// [checkingTools] is the question asked, and it is asked of the withdrawal
// rather than of the belt because both halves matter: the node has to have had
// the hand and to have lost it.
func (w *toolWithdrawal) withdrewChecking() bool {
	if w == nil {
		return false
	}
	for name := range w.gone {
		if checkingTools[name] {
			return true
		}
	}
	return false
}

// checkingTools are the hands a node RUNS ITS WORK WITH: the build, the test,
// the script that says whether what was just written is right. There is one of
// them, and naming it once here is what lets the landing say "unverified" about
// a general fact — the node had a check and lost it — with no idea what
// language the work is in and no list of build commands anywhere.
//
// It is deliberately not [knowledgeTools]: reading a file back is not checking
// it. A node that lost `read` lost a convenience; a node that lost `bash` lost
// the only thing that could have told it its edits compile.
var checkingTools = map[string]bool{"bash": true}
