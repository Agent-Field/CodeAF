package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The live sentence, verbatim: "When it finishes and the plots are written to
// disk, I'll open them for you." Nothing on the belt opens anything, so the
// third clause was a claim about a hand the head does not have — and the prompt
// forbidding it was enforced nowhere.
func TestAPromiseAboutTheirScreenIsNeverKeepable(t *testing.T) {
	said := "I've put that in hand. When it finishes and the plots are written to disk, I'll open them for you."
	for name, commissioned := range map[string]bool{
		"with work in hand": true, "with nothing in hand": false,
	} {
		if unkeepablePromise(said, commissioned, commissioned) == "" {
			t.Fatalf("%s: the reply passed with a promise to open a file", name)
		}
	}
	for _, promise := range []string{
		"I'll keep an eye on it and tell you.",
		"I'll email you the summary when it lands.",
		"I'll pull up the chart once it's written.",
	} {
		if unkeepablePromise(promise, true, true) == "" {
			t.Fatalf("an unbacked promise passed: %q", promise)
		}
	}
}

// The line moved when the wake landed, and that is the whole point of checking
// rather than banning: coming back with what the work FINDS is now true, and a
// rule that still forbade it would be making the head lie in the other
// direction.
func TestComingBackWithWhatWorkFindsIsBackedOnlyByWorkInHand(t *testing.T) {
	said := "That's in hand now — I'll come back to you with what it finds."
	if offending := unkeepablePromise(said, true, true); offending != "" {
		t.Fatalf("a promise the wake keeps was refused: %q", offending)
	}
	if unkeepablePromise(said, false, false) == "" {
		t.Fatal("a report-back with nothing commissioned passed: nothing will ever wake")
	}
	// An ordinary answer with no promise in it is untouched either way.
	plain := "North grew fastest at 14%. The full read is in regions.md."
	if unkeepablePromise(plain, false, false) != "" || unkeepablePromise(plain, true, true) != "" {
		t.Fatalf("an ordinary reply was read as a promise: %q", plain)
	}
}

// The repair is a re-ask, so the replacement sentence is the model's own words
// rather than a canned line in a fourth voice.
func TestAnUnkeepablePromiseIsRewrittenBeforeItIsPosted(t *testing.T) {
	graph := openHeadStore(t)
	client := &fakeClient{model: "test/model", responses: []string{
		"The three series are in series.csv. When it finishes I'll open the plots for you.",
		"The three series are in series.csv. The plots are written beside it.",
	}}
	user := postUser(t, graph, "room", "plot the three series and show me")
	head := New(client, graph)
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatalf("answer: %v", err)
	}
	reply := waitForAgentReply(t, graph, "room", user.Seq)
	if strings.Contains(strings.ToLower(reply.Body), "open the plots") {
		t.Fatalf("the unkeepable promise was posted: %q", reply.Body)
	}
	if !strings.Contains(reply.Body, "series.csv") {
		t.Fatalf("the correction lost the reply: %q", reply.Body)
	}
}

// A model that will not take the correction does not get to post the claim
// anyway. The sentence is cut, which is worse writing and a true reply.
func TestAModelThatKeepsPromisingLosesTheSentence(t *testing.T) {
	stubborn := "Right, that's running. I'll open them for you as soon as they land. The data is in series.csv."
	stripped := stripSentences(stubborn, true, true)
	if strings.Contains(strings.ToLower(stripped), "open them for you") {
		t.Fatalf("the promise survived the strip: %q", stripped)
	}
	for _, kept := range []string{"that's running", "series.csv"} {
		if !strings.Contains(stripped, kept) {
			t.Fatalf("the strip took the answer with it: %q", stripped)
		}
	}
	// Bulleted promises are one bullet, not one paragraph.
	bulleted := "Two things:\n- the run is in hand\n- I'll email you when it lands"
	if left := stripSentences(bulleted, true, true); strings.Contains(left, "email you") {
		t.Fatalf("a bulleted promise survived: %q", left)
	}
}

// The enforcement costs nothing on the replies that do not promise anything,
// which is nearly all of them.
func TestACleanReplyCostsNoSecondCall(t *testing.T) {
	graph := openHeadStore(t)
	client := &fakeClient{responses: []string{"The graph is ready and waiting for work."}}
	user := postUser(t, graph, "room", "what is happening?")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("a clean reply cost %d calls", calls)
	}
	if _, err := graph.Messages("room", user.Seq, 0); err != nil {
		t.Fatal(err)
	}
	_ = store.RoleAgent
}

// The live failure, verbatim. The head diagnosed a broken brochure PDF across
// its whole belt, commissioned nothing, and wrote a receipt for work that did
// not exist: the person then waited on a repair nobody had started until they
// typed "start it" a message later.
func TestAReceiptForWorkNobodyStartedIsNeverPosted(t *testing.T) {
	said := "I found the problem: page 1 is nearly empty while all the content is on page 2. " +
		"I've commissioned a fix to the build script, and the corrected PDF will land here when it's done."
	offending := unkeepablePromise(said, false, false)
	if offending == "" {
		t.Fatal("a receipt for work that was never commissioned passed")
	}
	if strings.Contains(offending, "I found the problem") {
		t.Fatalf("the finding was read as the claim: %q", offending)
	}
	// The same words are true of a turn that actually put the work in hand, and
	// of one that steered work already running. Neither may be rewritten.
	if offending := unkeepablePromise(said, true, true); offending != "" {
		t.Fatalf("a real receipt was refused: %q", offending)
	}
	if offending := unkeepablePromise(said, false, true); offending != "" {
		t.Fatalf("a turn that changed live work may describe it: %q", offending)
	}
	// And the finding survives the strip when the model will not rewrite: what is
	// cut is the claim, never what was actually found.
	stripped := stripSentences(said, false, false)
	if !strings.Contains(stripped, "page 1 is nearly empty") {
		t.Fatalf("the strip took the finding with it: %q", stripped)
	}
	if strings.Contains(strings.ToLower(stripped), "commissioned") {
		t.Fatalf("the false receipt survived: %q", stripped)
	}
}
