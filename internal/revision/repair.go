package revision

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// ── what a repair round actually changed, and what that lets it close ────────
//
// A failed gate buys one repair, and there are two kinds. One re-runs the
// worker: it can edit files, run checks, and change what the world says. The
// other — Compose — rewrites the ACCOUNT of work that has already landed and
// runs nothing at all; its own comment says so ("the composition did none of
// it; only the words are new").
//
// The second kind was closing findings the first kind exists for. ink s5 and
// ofetch s5 both failed their gate on the substance of the work — "the missing
// element is the substance of the work: the implemented code, the branch
// created, and the confirmation that it compiles", "the missing element is the
// test results and the confirmation that pnpm test exits 0" — composed a better
// summary, were re-judged on the better summary, and settled whole at 7 of 25
// and 44 of 47 hidden checks (2026-08-29, bench/deepswe;
// docs/design/gate/SETTLEMENT.md §8).
//
// THE LAW: A REPAIR THAT DID NOT MOVE THE TREE MAY NOT CLOSE A FINDING WHOSE
// GROUND IS THE TREE OR A READING. It may still close a finding whose only
// ground is the delivered text — a wrong summary, a missing explanation — which
// is the one thing rewriting the text can honestly fix, and the case Compose was
// built for.
//
// Both halves of that sentence are measured rather than asserted. Whether the
// tree moved is a stamp of the record taken on either side of the repair.
// Whether a finding's ground is the world is read off the record too: what the
// gate settled mechanically, what the run measured, the files the finding names,
// the redness of the project's own checks, and the names the record itself uses
// for the things it ran.

// TreeStamp is the world at one moment, in the one dimension a repair round has
// to be asked about: did anything under the run's own record change?
//
// It is size and modification time per path, over the artifact record the gate
// is held to, folded to a digest. Content hashing every artifact would be the
// same answer at many times the cost — a repository's build output alone is
// megabytes — and size-and-mtime is what every build system in existence trusts
// for this question. A path that has gone missing stamps as missing, which is a
// change and the loudest kind.
//
// An empty record stamps as empty, and two empty stamps compare equal. That is
// the honest reading: a run with nothing in its record has no tree for a repair
// to move, so nothing here can convict it — and the delivered text is then the
// whole of what it produced, which is exactly the case the law leaves alone.
func TreeStamp(artifacts []string) string {
	stamped := make([]string, 0, len(artifacts))
	for _, path := range artifacts {
		if path = strings.TrimSpace(path); path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			stamped = append(stamped, path+"\x00missing")
			continue
		}
		stamped = append(stamped, fmt.Sprintf("%s\x00%d\x00%d",
			path, info.Size(), info.ModTime().UnixNano()))
	}
	// Sorted because the record's own order is the order leaves happened to
	// land in, and an order that changes without a file changing would read as
	// a moved tree.
	sort.Strings(stamped)
	sum := sha256.Sum256([]byte(strings.Join(stamped, "\n")))
	return hex.EncodeToString(sum[:])
}

// GroundedInTheWorld reports whether the thing a finding points at is a thing
// only WORK could change — a file, or a check — as opposed to the account of the
// work, which writing can change.
//
// Every door below reads the RECORD and never a list of words, which is
// FAILSAFE clause 1. In order of what it costs:
//
//  1. The gate already settled it against the world. A mechanical gap asked the
//     disk for a file the plan promised; a sourced finding is a measurement the
//     run took. Neither has an opinion in it.
//  2. It names a file, under NamedFiles' own shape rule — so "docs/memo.md" is a
//     file and "the write-up" is not.
//  3. It names something the record says the run RAN. The vocabulary is the
//     record's: a project whose suite is `cargo nextest run` and one whose suite
//     is `pnpm test` are both recognised without either name appearing here.
//  4. THE RUN LEFT A TREE BEHIND AND NOTHING SAYS THE TREE IS GOOD. This is the
//     door that carries the weight, and it is written on the fail-safe side on
//     purpose. A run that produced files was doing work, not writing an answer;
//     a finding against it is a finding about that work unless the world says
//     otherwise, and the only thing that can say otherwise is the project's own
//     checks, run on the tree being handed over, coming back green. A red
//     reading is the world convicting; an ABSENT reading is nobody having
//     looked, and nobody-looked is not nothing-wrong (see store.DeliveryGate.
//     Unmeasured, which is that same rule written down one seam along).
//
// False — a finding whose only ground is the delivered text, which a rewritten
// account can honestly close — is therefore two cases, and both are ones where
// writing IS the work. A run that left nothing but its message: the message is
// the artifact, and this is §6's line reused rather than a second rule. And a
// run whose change the project's own checks pass, where what is wrong is the
// account of it: "the deliverable is a list of file paths, not the
// implementation itself" over a green suite is exactly what revision.Compose was
// built for.
func GroundedInTheWorld(finding Judgment, evidence Evidence) bool {
	if finding.Mechanical || finding.Sourced {
		return true
	}
	text := strings.TrimSpace(finding.Gaps + " " + joinCitations(finding.Cited()))
	if text == "" {
		// A finding that says nothing points at nothing this can read, and the
		// fail-safe direction for a bound on what a repair may close is the one
		// that leaves the finding standing.
		return true
	}
	if len(NamedFiles(text)) > 0 {
		return true
	}
	if namesSomethingTheRunRan(text, evidence) {
		return true
	}
	if len(evidence.Artifacts) == 0 {
		// Nothing was produced but the message, so the message is the whole of
		// what this run left behind and rewriting it is doing the work.
		return false
	}
	return !readingIsGreen(evidence)
}

// namesSomethingTheRunRan reports that the finding names a command the record
// says was run, or the verification entrypoint the reading used.
//
// It compares whole strings rather than symbols, and that is deliberate: a
// command is usually plain words with a space in it — `pnpm test`, `make check`,
// `go test ./...` — which symbolShaped correctly refuses to treat as a
// distinctively-spelled name. The name here does not have to be distinctive,
// because it is not being used to ground a citation; it is being matched against
// a string the RECORD supplies, so nothing is being inferred from prose.
func namesSomethingTheRunRan(text string, evidence Evidence) bool {
	text = strings.ToLower(text)
	for _, command := range ranCommands(evidence) {
		command = strings.ToLower(strings.TrimSpace(command))
		// Two characters is not a command, and a one-word fragment like "go"
		// would match half the prose in the language.
		if len(command) < 4 || !strings.Contains(command, " ") {
			continue
		}
		if strings.Contains(text, command) {
			return true
		}
	}
	return false
}

// ranCommands is everything the record says this run executed or was going to
// execute to check itself.
func ranCommands(evidence Evidence) []string {
	commands := append([]string(nil), evidence.Ran...)
	reading := evidence.Verification
	for _, entrypoint := range reading.Plan.Entrypoints {
		commands = append(commands, entrypoint.Command)
	}
	return append(commands, reading.Before.Entrypoint.Command, reading.After.Entrypoint.Command)
}

// readingIsGreen says the project's own checks were run on the tree being handed
// over and passed.
//
// It is asked in the affirmative because that is the direction the acquittal
// runs in: an absent reading, a reading whose after half was never taken, and a
// suite killed at its ceiling all answer NO — not because they are evidence of
// failure, but because none of them is evidence of success, and the door this
// guards is one that lets a rewritten sentence close a finding about work.
// A HUNG SUITE NAMES NOTHING, which is the same reading verify.Result.TimedOut
// is written for.
func readingIsGreen(evidence Evidence) bool {
	reading := evidence.Verification
	if !reading.Taken || !reading.AfterTaken {
		return false
	}
	after := reading.After
	if after.TimedOut {
		return false
	}
	return after.Exit == 0 && len(after.Failing) == 0
}

// RepairClosed answers, in one place, whether the single repair round a failed
// gate bought actually closed it.
//
// Three facts settle it, asked in the order of what they cost. The re-judgement
// has to have HAPPENED and to have passed — an unreadable second verdict is a
// gate that did not run, never an abstention, which is the rule Judgment.Fault
// exists for. And then the one this function was extracted for: a round that
// moved nothing may not close a finding the world is the ground of.
//
// It is a function rather than an expression at the wiring seam because it had
// been an expression at the wiring seam, and the rule it now states was not
// visible there to be got wrong. Every reader of "did the repair close it" reads
// this and there is no second copy — see cmd/aforge/chat.go, its only caller,
// and docs/design/gate/SETTLEMENT.md §8.
func RepairClosed(rejudged, finding Judgment, evidence Evidence, unmoved bool) bool {
	if !rejudged.Checked || !rejudged.Pass {
		return false
	}
	if unmoved && !toldToMoveNothing(finding, evidence) && GroundedInTheWorld(finding, evidence) {
		return false
	}
	return true
}

// toldToMoveNothing says the person forbade this run to change anything, which
// makes "the round changed nothing on disk" the opposite of the evidence the
// law above reads it as.
//
// THE STANDSTILL RULE IS WRITTEN FOR RUNS THAT WERE SUPPOSED TO MOVE. A repair
// that rewrote the account and touched no file is a repair that did not do the
// work — unless doing the work meant touching no file, in which case the
// unmoved tree is the run keeping its word. #427's stream said `not repaired:
// the repair rewrote the account and changed nothing on disk` over a run whose
// whole contract was to change nothing, and that reading is one of the three
// rules in this harness that rewarded the violation it is now written against.
//
// It reads the constraint and never the absence of artifacts: a run that
// happened to produce nothing is not a run that was told to produce nothing,
// and only the person's own words can say which of the two this is.
//
// AND IT NEVER EXCUSES A MECHANICAL FINDING, whatever the person said. A
// mechanical gap is a file the plan or the person PROMISED and the disk does not
// hold, and no rule about what a run may not write makes an absent deliverable
// appear: closing that one still takes the disk moving. Without the guard, one
// `no_writes` rule on the record would have made an unmoved repair able to close
// every world-grounded finding the second judge happened to pass — which is §8
// switched off by a sentence about something else.
func toldToMoveNothing(finding Judgment, evidence Evidence) bool {
	if finding.Mechanical {
		return false
	}
	for _, constraint := range evidence.Constraints {
		if constraint.Kind == plan.ConstraintNoWrites {
			return true
		}
	}
	return false
}
