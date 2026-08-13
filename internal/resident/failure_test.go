package resident

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The row the incident produced, read back as the thing a person is owed.
//
// Every assertion here is one of the four things that were missing: their own
// words for what was being attempted, one plain clause about why, what happens
// next, and nothing that names the machinery. The transport is still there —
// below the first line, where the room's disclosure grammar folds it — because
// deleting the evidence is the other way to lie about a failure.
func TestAFailedNodeReachesTheRoomComposedWithTheRawErrorBehindTheFold(t *testing.T) {
	node := store.Node{
		ID:    "craft-3799~launch",
		Brief: "Launch the analysis fan",
		Title: "Launch",
		Error: `node craft-3799~launch: after 3 node call attempts: API error (404): ` +
			`{"error":{"message":"No endpoints found that support tool use. Try disabling \"sh\" ` +
			`or choosing a different model.","code":404}}`,
		Provenance: store.Provenance{Intent: "put together a deep dive on the Q3 numbers"},
	}
	body := FailedNode(node, CraftFallbackLine).Room()
	headline, rest, _ := strings.Cut(body, "\n")

	// THE ASK, in their words — never the craft step's brief.
	if !strings.Contains(headline, "put together a deep dive on the Q3 numbers") {
		t.Fatalf("the row does not say what was being attempted: %q", headline)
	}
	if strings.Contains(headline, "Launch the analysis fan") {
		t.Fatalf("a machine-written step brief led the row: %q", headline)
	}
	// THE CAUSE, in the provider's own words and only its first clause.
	if !strings.Contains(headline, "No endpoints found that support tool use.") {
		t.Fatalf("the row does not say why: %q", headline)
	}
	if strings.Contains(headline, "Try disabling") {
		t.Fatalf("the provider's whole paragraph crowded the row: %q", headline)
	}
	// WHAT HAPPENS NEXT.
	if !strings.Contains(headline, CraftFallbackLine) {
		t.Fatalf("the row does not say what happens now: %q", headline)
	}
	// AND NONE OF THE MACHINERY.
	for _, banned := range []string{"{", "}", "craft-3799", "node call attempts", "API error", `\"`} {
		if strings.Contains(headline, banned) {
			t.Fatalf("the row leaked %q: %s", banned, headline)
		}
	}
	// The transport is reachable, whole, behind the fold.
	if !strings.Contains(rest, node.Error) {
		t.Fatalf("the raw error is unreachable:\n%s", body)
	}
	if strings.TrimSpace(strings.SplitN(rest, "\n", 2)[0]) != "" {
		t.Fatalf("the detail is not separated from the headline by a blank row:\n%s", body)
	}
}

// A failure that carries nothing readable is said honestly rather than dressed
// up. Inventing a cause is worse than admitting there is none: an unreadable
// blob is ignored, a wrong explanation is believed.
func TestAFailureWithNothingToSayAdmitsIt(t *testing.T) {
	node := store.Node{ID: "task-4", Title: "audit the billing code"}
	body := FailedNode(node, "").Room()
	if !strings.Contains(body, "audit the billing code") {
		t.Fatalf("the row does not say what failed: %q", body)
	}
	if !strings.Contains(body, "nothing was recorded about why") {
		t.Fatalf("a missing reason was papered over: %q", body)
	}
	if strings.Contains(body, "\n") {
		t.Fatalf("an empty detail still opened a fold: %q", body)
	}
}

// FailureCause is a decoder, not a cleaner: it opens the one envelope this
// system stamps and hands back anything else exactly as it arrived.
func TestFailureCauseOpensTheEnvelopeAndInventsNothing(t *testing.T) {
	for name, probe := range map[string]struct{ raw, want string }{
		"a provider body": {
			raw:  `after 3 node call attempts: API error (404): {"error":{"message":"No endpoints found. Try again."}}`,
			want: "No endpoints found.",
		},
		"a bare message field": {
			raw:  `API error (500): {"message":"upstream is unavailable"}`,
			want: "upstream is unavailable",
		},
		// A body clipped mid-JSON on its way through a bounded field is exactly
		// the case a reader most needs saved from, and the decoder still opens it.
		"a clipped body": {
			raw:  `API error (404): {"error":{"message":"No endpoints found that support tool use."},"cod`,
			want: "No endpoints found that support tool use.",
		},
		// Nothing to open, so nothing is touched. The provider's words are the
		// only diagnosis anyone has.
		"prose with no envelope": {
			raw:  "openrouter: 500 upstream is unavailable",
			want: "openrouter: 500 upstream is unavailable",
		},
		// A blob that will not decode is not forwarded as a sentence; what this
		// system wrapped it in at least names the stage that failed, and the
		// bytes stay whole in the detail.
		"an undecodable blob": {
			raw:  `the coding pipeline crashed: {not json at all`,
			want: "the coding pipeline crashed",
		},
		"nothing at all": {raw: "", want: ""},
	} {
		if got := FailureCause(probe.raw); got != probe.want {
			t.Errorf("%s: FailureCause(%q) = %q, want %q", name, probe.raw, got, probe.want)
		}
	}
}

// The executor stamps its own node id on the front of an error. It is removed
// by handing this function the id — one exact string this system wrote — rather
// than by a pattern, so nothing a provider said can ever be caught by it.
func TestStripNodeStampRemovesOnlyWhatWeWrote(t *testing.T) {
	stamped := "node craft-3799~launch: after 3 node call attempts: it stopped"
	if got := StripNodeStamp("craft-3799~launch", stamped); got != "after 3 node call attempts: it stopped" {
		t.Fatalf("the stamp survived: %q", got)
	}
	// A different node's id is not this node's stamp, and a sentence that
	// happens to mention a node is not a stamp at all.
	if got := StripNodeStamp("task-1", stamped); got != stamped {
		t.Fatalf("a foreign id was stripped: %q", got)
	}
	loose := "the worker said node craft-3799~launch: never started"
	if got := StripNodeStamp("craft-3799~launch", loose); got != loose {
		t.Fatalf("a mention mid-sentence was treated as a stamp: %q", got)
	}
}

// The ask leads even on a leaf, because a craft run's leaves carry the run's
// own provenance — and a step brief is precisely the machine-written phrase
// this row must not lead with.
func TestTheFailureAskPrefersTheProvenanceIntent(t *testing.T) {
	leaf := store.Node{
		ID: "craft-3799~launch", Brief: "Launch the analysis fan", Title: "Launch",
		Provenance: store.Provenance{Intent: "put together a deep dive"},
	}
	if got := failureAsk(leaf); got != "put together a deep dive" {
		t.Fatalf("failureAsk = %q", got)
	}
	// Work with no recorded ask still says something: its own name, then its
	// brief, and only then nothing.
	if got := failureAsk(store.Node{Title: "Launch", Brief: "Launch the analysis fan"}); got != "Launch" {
		t.Fatalf("failureAsk without an intent = %q", got)
	}
	if got := failureAsk(store.Node{Brief: "Launch the analysis fan"}); got != "Launch the analysis fan" {
		t.Fatalf("failureAsk with only a brief = %q", got)
	}
	if got := failureAsk(store.Node{ID: "craft-3799~launch"}); got != "" {
		t.Fatalf("a node with nothing to say named itself: %q", got)
	}
}
