package revision

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// A REVIEW THAT NAMES FIVE THINGS THE PERSON ASKED FOR IS GROUNDED FIVE TIMES,
// NOT ZERO. The citation invariant held a gap to being one verbatim span of the
// ask, and a delivery gate that listed the five files a request had itself
// listed — comma-joined, as anyone would write them — was refused as "not in
// the request". The repair round was never bought, and a leaf that had written
// nothing was delivered as done with a note explaining that the review had
// overreached. The review had not.
func TestEveryCitationTheAskItselfContainsIsGrounded(t *testing.T) {
	intent := "Grow spacing.go, then fix home.go and homebridge.go, and update designlanguage_test.go."
	for name, citations := range map[string][]string{
		"the whole list the mechanical gate emits": {"spacing.go", "home.go", "homebridge.go", "designlanguage_test.go"},
		"a shorter list": {"spacing.go", "home.go"},
		"one citation is judged exactly as before": {"home.go"},
		"a span of prose, not a file":              {"then fix home.go"},
	} {
		t.Run(name, func(t *testing.T) {
			if refusal := admitGapCitations(citations, intent); refusal != "" {
				t.Errorf("%q: refused as %q, but every citation is the person's own words", citations, refusal)
			}
		})
	}
}

// AND ONE INVENTED CITATION POISONS THE LIST. Four real paths do not launder a
// fifth the person never typed: the round that list would buy is a round
// against a standard the run wrote for itself, and that is the thing the
// invariant exists to refuse.
func TestAListWithAnInventedCitationIsRefusedWhole(t *testing.T) {
	intent := "Grow spacing.go, then fix home.go."
	if refusal := admitGapCitations([]string{"spacing.go", "home.go", "verification of the previous round"}, intent); refusal == "" {
		t.Fatal("a list carrying an invented requirement was admitted")
	}
	// A file nobody named is the same invention wearing a filename. This is the
	// half of the rule the file-identity door must not open.
	if refusal := admitGapCitations([]string{"spacing.go", "internal/tui3/rendercache.go"}, intent); refusal == "" {
		t.Fatal("a file only the plan named bought a round")
	}
	if refusal := admitGapCitations(nil, intent); !strings.Contains(refusal, "could not point") {
		t.Fatalf("an empty list should still be the empty-citation refusal, got %q", refusal)
	}
	if refusal := admitGapCitations([]string{"   ", ""}, intent); !strings.Contains(refusal, "could not point") {
		t.Fatalf("a list of blanks grounded itself against anything, got %q", refusal)
	}
}

// THE SECOND DOOR: A FILE IS THE FILE THE PERSON ASKED FOR UNDER EITHER
// SPELLING. The person writes "there is a breakpoints_test.go, update it" and
// the plan resolves that to internal/tui3/breakpoints_test.go. The file the
// review is missing is the file the person asked for; only the spelling moved,
// and a rule comparing the two letter by letter called it an invention.
func TestAPlanResolvedPathIsGroundedByTheBareNameTheAskUsed(t *testing.T) {
	intent := "There is a breakpoints_test.go asserting today's derivations; update it."
	if refusal := admitGapCitations([]string{"internal/tui3/breakpoints_test.go"}, intent); refusal != "" {
		t.Fatalf("the resolved path of a file the person named was refused as %q", refusal)
	}
	// It opens for a citation that IS a file name, never for a sentence holding
	// one: inferring a deliverable from prose is what neither half may do.
	if refusal := admitGapCitations([]string{"a rewrite of internal/tui3/breakpoints_test.go"}, intent); refusal == "" {
		t.Fatal("a sentence mentioning a file was grounded as though it named one")
	}
	// And a different directory is a different file, which is exactly what the
	// person meant when they wrote one.
	deep := "Update internal/tui3/breakpoints_test.go."
	if refusal := admitGapCitations([]string{"internal/tui/breakpoints_test.go"}, deep); refusal == "" {
		t.Fatal("a file at another address was grounded against the one the person named")
	}
}

// THE LEDGER KEYS PER CITATION. A round that names one file already worked on
// beside one nobody has touched must still buy its round for the fresh one, and
// a round naming only spent citations must not. That is what keeps the loop
// falling by at least one citation per round rather than either spiralling or
// abandoning real work.
func TestTheSpentLedgerKeysPerCitation(t *testing.T) {
	const intent = "Grow spacing.go, then fix home.go and homebridge.go."
	spent := []string{"spacing.go"}
	if refusal := AdmitGapCitation(intent, []string{"spacing.go", "home.go"}, spent); refusal != "" {
		t.Fatalf("a fresh citation beside a spent one was refused: %q", refusal)
	}
	if refusal := AdmitGapCitation(intent, []string{"spacing.go"}, spent); refusal == "" {
		t.Fatal("a citation already worked on bought a second round")
	}
	if refusal := AdmitGapCitation(intent, []string{"spacing.go", "home.go"},
		[]string{"spacing.go", "home.go"}); refusal == "" {
		t.Fatal("a list every citation of which was spent bought another round")
	}
	// Spelling does not refresh a file. A round bought for the bare name is a
	// round bought for the path it resolves to, or one file buys two rounds.
	if refusal := AdmitGapCitation("Fix home.go.", []string{"internal/tui3/home.go"},
		[]string{"home.go"}); refusal == "" {
		t.Fatal("resolving a path in between bought the same file a second round")
	}
}

// THE ACTUAL VERDICT, AGAINST THE ACTUAL REQUEST. The gate's citations, the
// line it recorded and the request text are the ones from the run that surfaced
// this, kept verbatim so the test fails the day either half of the fix
// regresses on real input. The five paths below are the plan's own Produces
// entries; the recorded quote is read out of the journal event rather than
// retyped, so the two cannot drift apart in this file.
func TestTheSpacingLadderGateIsGroundedInItsOwnRequest(t *testing.T) {
	produces := []string{
		"internal/tui3/spacing.go",
		"internal/tui3/home.go",
		"internal/tui3/homebridge.go",
		"internal/tui3/designlanguage_test.go",
		"internal/tui3/breakpoints_test.go",
	}
	intent, err := os.ReadFile("testdata/spacing-ladder-request.txt")
	if err != nil {
		t.Fatalf("the request fixture is the acceptance criterion: %v", err)
	}
	// The recorded event, copied verbatim out of docs/design/gate/muse-evidence.json
	// (which holds the surrounding plan and node events too, and was captured
	// truncated). Reading the line rather than retyping it is what keeps this
	// test about the run that happened.
	recorded, err := os.ReadFile("testdata/muse-delivery-gate.json")
	if err != nil {
		t.Fatalf("the journal fixture is the acceptance criterion: %v", err)
	}
	var evidence struct {
		DeliveryGate struct {
			Quote   string `json:"quote"`
			Refused string `json:"refused"`
		} `json:"delivery_gate"`
	}
	if err := json.Unmarshal(recorded, &evidence); err != nil {
		t.Fatal(err)
	}

	// Nothing was produced, which is the whole of what went wrong: the gate was
	// right, and it is the run's own recorded verdict.
	gate, fired := MissingProduces(plan.Done{Produces: produces}, nil)
	if !fired {
		t.Fatal("the mechanical gate did not fire on a plan whose every promised file is absent")
	}
	if gate.Quote != evidence.DeliveryGate.Quote {
		t.Fatalf("the gate's line is not the one the journal recorded:\n got %q\nwant %q",
			gate.Quote, evidence.DeliveryGate.Quote)
	}
	if len(gate.Citations) != len(produces) {
		t.Fatalf("citations = %q, want one per promised file", gate.Citations)
	}
	if !gate.Mechanical {
		t.Fatal("a gap about files on disk was not marked mechanical")
	}
	if refusal := AdmitGapRevision(string(intent), "", gate.Cited()); refusal != "" {
		t.Fatalf("the gate's own citations were refused as %q against the request that names every file in them", refusal)
	}
	if refusal := AdmitGapCitation(string(intent), gate.Cited(), nil); refusal != "" {
		t.Fatalf("the extension refused the same citations as %q", refusal)
	}
	// The refusal the run actually recorded is the one this whole change exists
	// to stop being possible on grounded input.
	if want := "what the review asked for next is not in the request"; evidence.DeliveryGate.Refused != want {
		t.Fatalf("the fixture no longer records the refusal under repair: %q", evidence.DeliveryGate.Refused)
	}
}
