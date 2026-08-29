package revision

// The acceptance settlement: the half of the gate that asks whether anything
// CHECKS what the person asked for.
//
// The settlement lane fixed the refusal path — a gate that names a gap and is
// overruled by a rule. The sweep that followed it proved the fix and uncovered
// this: two of five graded runs ended at exit 0, reward 0, because the gate had
// said YES. ofetch passed a deliverable opening "All 56 tests pass" at 41 of 47
// hidden tests; happy-dom passed "All tests pass (31/31)" at 13 of 14. In both
// the gate accepted THE WORKER'S CLAIM ABOUT TESTS THE WORKER WROTE ITSELF,
// which is FAILSAFE clause 2 broken in the one place the whole verdict is
// decided. See docs/design/gate/ACCEPTANCE.md.
//
// Everything here answers one question — is there a check that exercises this
// behaviour — and it answers it from two sources, neither of which is prose: the
// check declarations in the worker's own diff, and the identities the project's
// own test runner printed. The deliverable's sentence about its tests is read by
// nothing in this file.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/aforge-v2/internal/shaped"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Held is the checklist the gate may actually hold somebody to: the points whose
// quotation is grounded in what this run promised before it began working.
//
// It is the SAME invariant, read by the SAME code, that decides whether a
// review's finding may buy a repair round — citationGrounded, through the three
// doors grounding.go opens. A point the request does not carry is a requirement
// this system wrote for itself after reading its own prompt, and holding a
// worker to one is the failure the whole grounding rule exists to prevent.
//
// It is applied where the checklist is USED and not only where it was written.
// A list that reached the gate down any other route — a rehydrated plan, a
// spliced repair, a future caller nobody has written yet — is still weighed
// against the person's own words before it can convict anything.
func Held(points []plan.Point, grounds Grounds) []plan.Point {
	if len(points) == 0 || grounds.Empty() {
		return nil
	}
	index := grounds.index()
	held := make([]plan.Point, 0, len(points))
	for _, point := range points {
		if point.Empty() || !citationGrounded(point.Quote, index) {
			continue
		}
		held = append(held, point)
	}
	if len(held) == 0 {
		return nil
	}
	return held
}

// CheckEvidence is every check identity this run can prove exists, and it is
// deliberately assembled from the world rather than from the account of it.
//
// Two sources, in the order of what they cost. The worker's own diff declares
// checks by shape and costs a scan of a string the gate already holds; the
// project's own runner names identities in its output and costs nothing extra,
// because the reading was taken anyway. A DELIVERABLE'S PROSE IS NOT A SOURCE:
// "All 56 tests pass" is a sentence about tests the same worker wrote, and
// weighing it is the exact defect this file exists to close.
//
// Empty means nothing can be concluded about coverage. That is a real answer and
// it is the honest one for a project that declares no verification and a worker
// that derived no diff — a capability that cannot work is ABSENT, not broken.
func CheckEvidence(evidence Evidence) []string {
	var checks []string
	if patch := evidence.patchSource(); patch != "" {
		added, _ := verify.PatchChecks(patch)
		checks = append(checks, added...)
	}
	reading := evidence.Verification
	roster := reading.After.Reported
	if !reading.AfterTaken {
		// The before roster is the fallback and not a substitute: it names the
		// checks that existed when the work STARTED, so it can say a behaviour
		// was already covered and can never say a new one is. That asymmetry is
		// right — a point already exercised by the repository's own suite is
		// exercised — and it is why this is read at all rather than skipped.
		roster = reading.Before.Reported
	}
	if reading.Taken {
		checks = append(checks, roster...)
	}
	return verify.Subtract(checks, nil)
}

// mapPrompt asks one question and takes no position on the answer.
//
// It is written to make the NEGATIVE cheap to say. A model asked to match things
// up will match everything up, so the instruction that carries the weight is the
// one that names the failure mode: a check whose name is about a neighbouring
// behaviour is not a check for this one, and the whole finding this call feeds
// is the difference between "counts hook errors as failures" and "does not retry
// when a hook throws".
const mapPrompt = `You are given a list of behaviours a request asked for, and a list of the checks
that exist in a project. For each behaviour, say which check exercises it.

A check exercises a behaviour when running that check would FAIL if the behaviour
were absent or wrong. A check whose name is about something adjacent does not
count: "counts hook errors as failures" does not exercise "hook failures are not
retried", and "keyed by origin" does not exercise "two origins are tracked
independently". If you are not sure a check would catch the behaviour breaking,
say there is none.

Answer with one bare JSON object and nothing else — no code fence around it and
no sentence before or after it. Give one entry per behaviour, in the order they
were given, with the check's exact name or an empty string when no check
exercises it:
{"mapped": [{"point": "<the behaviour, copied>", "check": "<the check's name, or empty>"}]}`

var mapSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "mapped": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "point": {"type": "string"},
          "check": {"type": "string"}
        },
        "required": ["point", "check"],
        "additionalProperties": false
      }
    }
  },
  "required": ["mapped"],
  "additionalProperties": false
}`)

// MapChecks asks which check exercises which behaviour, and returns the mapping
// in the order the points were given.
//
// A call that cannot be made, cannot be read, or comes back short answers with
// NOTHING MAPPED rather than with everything mapped. That is the fail-safe
// direction for this particular question and it is the opposite of the gate's
// own: an unanswerable gate must not hold a finished deliverable hostage, but an
// unanswerable coverage question that resolved to "all covered" would silently
// restore exactly the behaviour this mechanism replaces. Unmapped buys a repair
// round; it never ships a wrong answer as a right one.
func MapChecks(ctx context.Context, settings config.Config, client *pool.Client,
	node store.Node, points []plan.Point, checks []string, workerModel string,
) []store.ExercisedPoint {
	mapping := make([]store.ExercisedPoint, 0, len(points))
	for _, point := range points {
		mapping = append(mapping, store.ExercisedPoint{Point: point.Behaviour})
	}
	if client == nil || len(points) == 0 || len(checks) == 0 {
		return mapping
	}
	var body strings.Builder
	body.WriteString("The behaviours the request asked for:\n")
	for index, point := range points {
		fmt.Fprintf(&body, "%d. %s\n", index+1, point.Behaviour)
	}
	body.WriteString("\nThe checks that exist:\n")
	for _, check := range checks {
		body.WriteString(check + "\n")
	}
	mapCtx := settings.Context(router.WithAvoidModel(ctx, workerModel), "gate")
	mapCtx = provider.WithCall(mapCtx, provider.ClassPlanAudit)
	// The same tag the gate's own verdict carries, because this is the gate
	// answering half of its own question and a reader of the model-call log
	// wants the two beside each other.
	mapCtx = provider.WithCallTag(mapCtx, "gate")
	mapCtx = pool.WithSpendNode(mapCtx, node.ID)
	var answer struct {
		Mapped []store.ExercisedPoint `json:"mapped"`
	}
	// The seam owns the wire: the schema travels only where a router carries
	// it, a cut answer is continued and a prose one re-asked once, and what
	// comes back is either the object or a typed refusal — the mapping never
	// reads the model's text itself.
	response, err := askVerdict(mapCtx, client, []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: mapPrompt}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: body.String()}}},
	}, mapSchema, &answer)
	if err != nil || response == nil {
		if shaped.Unreadable(err) {
			provider.Report(mapCtx, provider.VerdictFormatFailure)
		} else {
			provider.Report(mapCtx, provider.VerdictProviderFailure)
		}
		return mapping
	}
	provider.Report(mapCtx, provider.VerdictVerifiedSuccess)
	// Merged by position, and only where the answer named a check that actually
	// exists. A model that invents a check name has answered about a project it
	// imagined, and admitting it would let a hallucinated test satisfy a real
	// behaviour.
	known := make(map[string]string, len(checks))
	for _, check := range checks {
		known[strings.ToLower(strings.TrimSpace(check))] = check
	}
	for index, row := range answer.Mapped {
		if index >= len(mapping) {
			break
		}
		if named, ok := known[strings.ToLower(strings.TrimSpace(row.Check))]; ok {
			mapping[index].Check = named
		}
	}
	return mapping
}

// Unexercised is the acceptance finding: the behaviours the request stated that
// nothing in the project's own verification touches.
//
// SOURCED, NOT MECHANICAL, for the reason Regressions is. There is no citation
// to weigh here — the person asked for the behaviour, and whether a check exists
// for it is a measurement of the repository rather than a reading of the words.
// Grounding it would refuse it every time, which is the shape of the two runs
// that shipped a deliverable claiming every test passed while a whole family of
// stated behaviours was exercised by nothing at all.
//
// The list is bounded the way its sibling is: eight named and the rest counted,
// which is regressionsNamed, stated once in this package and read here rather
// than restated.
//
// ok is false when every point is exercised, when there were no points, or when
// nothing could be measured — and the caller then judges exactly as it did
// before this existed.
func Unexercised(mapping []store.ExercisedPoint) (judgment Judgment, ok bool) {
	var missing []string
	for _, row := range mapping {
		point := strings.TrimSpace(row.Point)
		if point != "" && strings.TrimSpace(row.Check) == "" {
			missing = append(missing, point)
		}
	}
	if len(missing) == 0 {
		return Judgment{}, false
	}
	named := missing
	if len(named) > regressionsNamed {
		named = named[:regressionsNamed]
	}
	var gap strings.Builder
	gap.WriteString("The request asks for behaviours that no check exercises. " +
		"Nothing in this project's own verification would fail if each of these were " +
		"absent or wrong, so nothing that has been run says whether the work does them:\n")
	for _, point := range named {
		gap.WriteString("no check exercises: " + point + "\n")
	}
	if len(missing) > len(named) {
		fmt.Fprintf(&gap, "And %d more.\n", len(missing)-len(named))
	}
	gap.WriteString("Write the check for each, and make it pass.")
	return Judgment{
		Pass: false, Gaps: strings.TrimSpace(gap.String()), Quote: joinCitations(named),
		Citations: named, Sourced: true, Checked: true, Exercises: mapping,
	}, true
}

// WeakenedChecks is the third mechanism: a check that STOPPED EXISTING between
// the two photographs.
//
// The verification photograph SETTLEMENT §4 built sees a check that turned red.
// It cannot see one that was deleted, renamed or skipped — and taking out the
// test that was failing is the cheapest way there is to make a suite green, so
// the one thing a coverage rule must not do is leave that door open while
// closing every other one.
//
// Two sources, either of which convicts. A check declaration on a REMOVED line
// of the worker's own diff is direct evidence and needs no run at all; a name
// the runner reported before the work and did not report after it is the same
// fact measured. Both are already subtractions that cancel a move — see
// verify.PatchChecks and verify.Reading.Vanished — so a check that changed file
// or was merely re-indented is not here.
//
// Sourced, for the same reason as its two siblings: nobody has to ask for their
// tests to keep existing.
func WeakenedChecks(removed, vanished []string) (judgment Judgment, ok bool) {
	gone := verify.Subtract(append(append([]string{}, removed...), vanished...), nil)
	if len(gone) == 0 {
		return Judgment{}, false
	}
	named := gone
	if len(named) > regressionsNamed {
		named = named[:regressionsNamed]
	}
	gap := "This work removed checks that existed before it: " + joinCitations(named) + "."
	if len(gone) > len(named) {
		gap += fmt.Sprintf(" And %d more.", len(gone)-len(named))
	}
	gap += " A check that was there and is not was deleted, renamed or skipped; " +
		"whatever it was holding is now held by nothing."
	return Judgment{
		Pass: false, Gaps: gap, Quote: joinCitations(named),
		Citations: named, Sourced: true, Checked: true,
	}, true
}

// settleAcceptance is the whole of this mechanism as the gate reaches it, and it
// reaches it at ONE moment: after the model judge has said the deliverable is
// whole. A gate that is already failing the work buys the repair round anyway,
// so asking the coverage question there would spend a call to reach a conclusion
// that is already true — and the run this exists for is precisely the one that
// was about to be called finished.
//
// It returns pass unchanged when there is nothing to settle: no checklist, no
// evidence about checks, or every point exercised.
func settleAcceptance(ctx context.Context, settings config.Config, client *pool.Client,
	node store.Node, evidence Evidence, grounds Grounds, workerModel string, pass Judgment,
) Judgment {
	points := Held(evidence.Accept, grounds)
	if len(points) == 0 {
		return pass
	}
	checks := CheckEvidence(evidence)
	if len(checks) == 0 {
		// NOBODY LOOKED IS NOT NOTHING WRONG, and it is not a finding either.
		// A project that declares no verification and a worker that derived no
		// diff leave this question unanswerable, and a gate that failed every
		// such delivery would fail every piece of prose this program writes.
		// The pass says so, so a reader of the record can tell an unchecked
		// delivery from a checked one.
		pass.Unmeasured = "nothing in this project's verification could be read, so no check " +
			"could be matched to what the request asked for"
		return pass
	}
	mapping := MapChecks(ctx, settings, client, node, points, checks, workerModel)
	finding, unexercised := Unexercised(mapping)
	if !unexercised {
		pass.Exercises = mapping
		return pass
	}
	finding.Grounds = grounds
	return finding
}
