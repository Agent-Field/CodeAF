package resident

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The last seam a deliverable crosses before a person reads it, proved rather
// than assumed. When the UX suite found final messages that were plans and
// pointers, the announcer was one of four places the answer could have been lost
// — a summariser here, or a receipt about the work, would have made every fix
// upstream invisible. It is neither: what the work said is what the thread says,
// to the byte, with the file list the work wrote riding along inside it.
//
// So the fix for the plan-shaped non-answer belongs upstream, in the leaf
// contract and the delivery gate, and this test is what says so.
func TestTheThreadPostsTheDeliverableItselfAndNotAReceiptAboutIt(t *testing.T) {
	graph := openStore(t)
	spliceOvernightJob(t, graph, "filings", "s1")
	reconciler := New(graph, nil, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.TouchSeen("tui", "s1", store.SeenAttached); err != nil {
		t.Fatal(err)
	}

	// A real deliverable: the answer first, its working named beside it, and the
	// artifact list the executor appends. Every part of it must survive.
	const delivered = "Revenue was $4.1B in the 2023 filing and $3.6B in the 2022 filing.\n\n" +
		"Both figures are the consolidated line, taken from Form 10-K page 44 in each year; the 2022 " +
		"number is restated from the $3.4B originally reported.\n\n" +
		"Files:\n/tmp/job/filings.md"
	landNode(t, graph, "filings", delivered)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	messages, err := graph.Messages("s1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("the delivery is not one message: %+v", messages)
	}
	if messages[0].Body != delivered {
		t.Fatalf("the thread does not carry the deliverable verbatim:\n got %q\nwant %q", messages[0].Body, delivered)
	}
	// Said the other way round, because verbatim is easy to break by addition:
	// the figures lead, and nothing was wrapped around them.
	if !strings.HasPrefix(messages[0].Body, "Revenue was $4.1B") {
		t.Fatalf("something was put in front of the answer: %q", messages[0].Body)
	}
	if !strings.Contains(messages[0].Body, "/tmp/job/filings.md") {
		t.Fatalf("the artifact list did not ride along: %q", messages[0].Body)
	}
}

// The one thing the announcer is allowed to do to a deliverable is stop it
// overflowing the thread, and even then it clips the content rather than
// replacing it with a sentence about where the content is. A pointer posted in
// place of an answer is the failure this whole wave is about, and it must not
// be reintroduced by the length rule.
func TestAnOverlongDeliverableIsClippedAndNeverReplacedByAPointer(t *testing.T) {
	graph := openStore(t)
	spliceOvernightJob(t, graph, "long", "s1")
	reconciler := New(graph, nil, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.TouchSeen("tui", "s1", store.SeenAttached); err != nil {
		t.Fatal(err)
	}

	const opening = "The answer is 41.\n\n"
	landNode(t, graph, "long", opening+strings.Repeat("working. ", store.MaxMessageBytes))
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	messages, err := graph.Messages("s1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("the delivery is not one message: %+v", messages)
	}
	body := messages[0].Body
	if !strings.HasPrefix(body, opening) {
		t.Fatalf("clipping took the answer off the front: %q", body[:min(len(body), 120)])
	}
	if len(body) > store.MaxMessageBytes {
		t.Fatalf("the clip did not bound the message: %d bytes", len(body))
	}
	if !strings.HasSuffix(body, "...") {
		t.Fatalf("a clipped message does not say it was clipped: %q", body[max(0, len(body)-40):])
	}
}
