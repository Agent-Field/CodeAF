package plan

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/profile"
)

// invoiceRecords builds n records of one shape at a fixed price, which is all
// any test here needs: the arithmetic is medians and a minimum.
func invoiceRecords(shape string, count, tokens, turns int, cost float64) []profile.Record {
	records := make([]profile.Record, 0, count)
	for index := 0; index < count; index++ {
		records = append(records, profile.Record{
			Title:  fmt.Sprintf("%s %d", shape, index),
			Size:   shape,
			Turns:  turns,
			Tokens: tokens,
			Cost:   cost,
		})
	}
	return records
}

// The first law of the invoice: it is measurement or it is nothing. A worker
// nobody has watched long enough gets no price list, not a thinner one and not
// a prior dressed as an observation — because the whole value of a figure in
// this block is that it can be read without a qualifier attached.
func TestTheInvoiceRendersNothingWithoutRealMeasurements(t *testing.T) {
	if _, ok := InvoiceFor("linear", nil); ok {
		t.Fatal("an unmeasured worker was priced")
	}
	thin := invoiceRecords(string(SizeAtomic), profile.MinSamples-1, 40_000, 9, 0.04)
	if invoice, ok := InvoiceFor("linear", thin); ok {
		t.Fatalf("a worker below the evidence gate was priced: %+v", invoice)
	}
	if got := RenderInvoice(); got != "" {
		t.Fatalf("rendering no invoices produced %q, want the empty string", got)
	}
	unpriced, _ := InvoiceFor("linear", thin)
	if got := RenderInvoice(unpriced); got != "" {
		t.Fatalf("rendering an unpriced invoice produced %q", got)
	}
}

// Reflex micro-leaves never set a price. They are a different population with
// an envelope that was never allowed to be large, and an invoice that averaged
// them in would tell a planner a piece of work costs a fraction of what a piece
// of work costs.
func TestReflexRecordsAreNotPricedAsWork(t *testing.T) {
	records := invoiceRecords(profile.BucketReflex, 40, 900, 1, 0.001)
	if invoice, ok := InvoiceFor("linear", records); ok {
		t.Fatalf("forty reflex leaves priced a worker: %+v", invoice)
	}
}

// What the block is actually for: the three facts a split decision needs, each
// of them a thing that really happened. The measured medians for the shape, the
// cheapest leaf ever recorded — which is what a new piece costs before it does
// any work — and what a leaf that read earlier results has cost, which is the
// price width adds at the far end.
func TestTheInvoiceCarriesTheThreeFactsADivisionIsPricedOn(t *testing.T) {
	records := invoiceRecords(string(SizeAtomic), profile.MinSamples, 40_000, 9, 0.0412)
	records = append(records, profile.Record{
		Title: "cheapest", Size: string(SizeAtomic), Turns: 2, Tokens: 6_100, Cost: 0.004,
	})
	// Eight joins, each reading three earlier results.
	for index := 0; index < profile.MinSamples; index++ {
		records = append(records, profile.Record{
			Title: fmt.Sprintf("join %d", index), Size: string(SizeOversized),
			Turns: 14, Tokens: 88_000, Cost: 0.11,
			Sources: 3, SourcesKnown: true, FanIn: profile.FanInOf(3),
		})
	}
	invoice, ok := InvoiceFor("linear", records)
	if !ok {
		t.Fatal("a measured worker was not priced")
	}
	if invoice.Floor != 6_100 || !invoice.FloorKnown {
		t.Fatalf("floor = %d (known=%v), want the cheapest leaf ever recorded",
			invoice.Floor, invoice.FloorKnown)
	}
	if invoice.JoinRuns != profile.MinSamples || invoice.JoinTokens != 88_000 || invoice.JoinInputs != 3 {
		t.Fatalf("fan-in tax = %+v, want the eight measured joins", invoice)
	}
	rendered := RenderInvoice(invoice)
	for _, want := range []string{
		"MEASURED HERE",
		"linear, atomic:",
		"40,000 tokens",
		"$0.0412",
		"6,100 tokens",
		"88,000 tokens",
		"median of 3 results",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("the rendered invoice is missing %q:\n%s", want, rendered)
		}
	}
}

// A touch-list is not a fan-in. Every ordinary leaf names two or three things it
// must visit, and for as long as the join price selected on that count it was
// computed over the ordinary population — which is how "a leaf that read earlier
// results" came back at exactly the price of a leaf that read nothing, and the
// one cost a fan-out prompt never sees said reassembly was free.
func TestASourceCountAloneNeverPricesAJoin(t *testing.T) {
	records := invoiceRecords(string(SizeAtomic), profile.MinSamples*2, 40_000, 9, 0.04)
	for index := range records {
		records[index].Sources, records[index].SourcesKnown = 4, true
	}
	invoice, ok := InvoiceFor("linear", records)
	if !ok {
		t.Fatal("a measured worker was not priced")
	}
	if invoice.JoinRuns != 0 {
		t.Fatalf("a fan-in tax was priced from %d leaves that only named a touch-list",
			invoice.JoinRuns)
	}
	if strings.Contains(RenderInvoice(invoice), "read earlier results") {
		t.Fatal("the rendered invoice claims a fan-in price read off the touch-list")
	}
}

// A record that never said how many results it read is not a leaf that read
// none. Counting it as zero would price a join from leaves that never made one,
// which is exactly the kind of quiet fabrication this block must not contain.
func TestAnUnknownFanInNeverPricesAJoin(t *testing.T) {
	records := invoiceRecords(string(SizeAtomic), profile.MinSamples*2, 40_000, 9, 0.04)
	invoice, ok := InvoiceFor("linear", records)
	if !ok {
		t.Fatal("a measured worker was not priced")
	}
	if invoice.JoinRuns != 0 {
		t.Fatalf("a fan-in tax was priced from %d records that never named a source count",
			invoice.JoinRuns)
	}
	if strings.Contains(RenderInvoice(invoice), "read earlier results") {
		t.Fatal("the rendered invoice claims a fan-in price it does not have")
	}
}

// Cache shape, as a test. The invoice is volatile — it moves every time a leaf
// lands — so it must never appear in a shared prefix. The sizing pass's system
// prompt is the same bytes for every stage of every job on this machine, and it
// stays that way; the golden in testdata is the byte-level statement of that
// and this is the reason it still passes.
func TestTheInvoiceNeverEntersTheSizingPromptsSharedPrefix(t *testing.T) {
	invoice := RenderInvoice(mustPrice(t))
	if invoice == "" {
		t.Fatal("the fixture priced nothing")
	}
	if strings.Contains(sizePromptWith(Anchors()), "MEASURED HERE") {
		t.Fatal("the invoice reached the sizing system prompt, which is a shared prefix")
	}
	if strings.Contains(expandBurden, "MEASURED HERE") {
		t.Fatal("the invoice reached the expansion burden, which is a constant")
	}
	// Where it does belong: the very tail of a per-request message.
	tail := withInvoice("Judge the size of each of these stage 1 nodes:\n1. Thing — a thing\n", invoice)
	if !strings.HasSuffix(tail, invoice) {
		t.Fatalf("the invoice is not the last thing in the message:\n%s", tail)
	}
	if !strings.HasPrefix(tail, "Judge the size") {
		t.Fatalf("the invoice displaced the message it was appended to:\n%s", tail)
	}
	// And an absent invoice leaves the message it was offered to untouched,
	// byte for byte, which is what every prompt on a fresh machine gets.
	before := "Judge the size of each of these stage 1 nodes:\n1. Thing — a thing\n"
	if got := withInvoice(before, ""); got != before {
		t.Fatalf("an empty invoice changed the message: %q", got)
	}
}

// The expansion path is the second decision that divides work, and it reads the
// prices off the document rather than off a caller's options — because a
// sub-planner is reached through the graph alone.
func TestTheExpansionScopeCarriesTheInvoiceLast(t *testing.T) {
	invoice := RenderInvoice(mustPrice(t))
	graph := &Graph{Goal: "a goal", NextID: 1, Invoice: invoice}
	graph.Add(Node{Kind: KindWork, Stage: 1, Title: "Part", Summary: "one part"})
	node := graph.Node(1)
	rendered := scopeFor(graph, node).render(node)
	if !strings.HasSuffix(rendered, invoice) {
		t.Fatalf("the expansion scope does not end with the invoice:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Break down only this piece of it") {
		t.Fatalf("the expansion scope lost its own body:\n%s", rendered)
	}
	// An unmeasured machine renders exactly what it always did.
	graph.Invoice = ""
	if strings.Contains(scopeFor(graph, node).render(node), "MEASURED HERE") {
		t.Fatal("an unpriced graph still rendered an invoice block")
	}
}

func mustPrice(t *testing.T) Invoice {
	t.Helper()
	invoice, ok := InvoiceFor("linear",
		invoiceRecords(string(SizeAtomic), profile.MinSamples, 40_000, 9, 0.0412))
	if !ok {
		t.Fatal("the fixture was not priced")
	}
	return invoice
}

// The golden is the byte-level guarantee that the shared prefix did not move.
// It is read here as well as in subharness_test.go because this wave is the one
// that could have moved it and did not.
func TestTheSizingPromptStillMatchesItsGoldenAfterTheInvoice(t *testing.T) {
	want, err := os.ReadFile("testdata/size_prompt_baseline.golden")
	if err != nil {
		t.Fatal(err)
	}
	if got := sizePromptWith(Anchors()); got != string(want) {
		t.Fatal("the sizing prompt moved; the invoice belongs in the per-request tail")
	}
}
