package plan

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/profile"
)

// The floor is what a piece costs before it does any work, and it is quoted to
// every planning call that weighs a division. An unlabelled record is not a
// shape — it is what a surface that did not say wrote, including a one-turn
// no-op — and one of them held the floor of a real profile at 7,215 tokens.
func TestTheFloorIsTakenOverNamedShapesOnly(t *testing.T) {
	records := invoiceRecords(string(SizeAtomic), profile.MinSamples, 40_000, 9, 0.0412)
	records = append(records, profile.Record{
		Title: "a one-turn no-op nobody sized", Size: "", Turns: 1, Tokens: 7_215, Cost: 0.008,
	})
	invoice, ok := InvoiceFor("linear", records)
	if !ok {
		t.Fatal("a measured worker was not priced")
	}
	if invoice.Floor != 40_000 {
		t.Fatalf("floor = %d, want 40,000 — an unlabelled record set the advertised price of existing as a leaf",
			invoice.Floor)
	}
	if strings.Contains(RenderInvoice(invoice), "7,215") {
		t.Fatal("the unlabelled record's tokens were advertised as the floor")
	}
}

// The same record, once its shape has a name, is a legitimate floor again: the
// exclusion is about shapes nobody named, not about cheap leaves.
func TestTheWholeBucketAccruesItsOwnRowAndMayHoldTheFloor(t *testing.T) {
	records := invoiceRecords(string(SizeAtomic), profile.MinSamples, 40_000, 9, 0.0412)
	records = append(records, invoiceRecords(profile.BucketWhole, profile.MinSamples, 128_000, 22, 0.31)...)
	records = append(records, profile.Record{
		Title: "a small undivided job", Size: profile.BucketWhole, Turns: 2, Tokens: 6_100, Cost: 0.004,
	})
	invoice, ok := InvoiceFor("linear", records)
	if !ok {
		t.Fatal("a measured worker was not priced")
	}
	if invoice.Floor != 6_100 {
		t.Fatalf("floor = %d, want the cheapest named leaf", invoice.Floor)
	}
	var whole *InvoiceShape
	for index := range invoice.Shapes {
		if invoice.Shapes[index].Shape == profile.BucketWhole {
			whole = &invoice.Shapes[index]
		}
	}
	if whole == nil {
		t.Fatalf("the whole bucket has no row: %+v", invoice.Shapes)
	}
	if whole.Tokens != 128_000 || whole.Runs != profile.MinSamples+1 {
		t.Fatalf("whole row = %+v, want the undivided shape's own medians", *whole)
	}
	if !strings.Contains(RenderInvoice(invoice), "linear, whole:") {
		t.Fatalf("the undivided shape is not rendered:\n%s", RenderInvoice(invoice))
	}
}

// Unlabelled records still reach no shape row, which they never did — the
// difference is that nothing writes one any more, and the ones already on disk
// no longer set the floor.
func TestUnlabelledRecordsPriceNothing(t *testing.T) {
	records := invoiceRecords("", profile.MinSamples*3, 7_215, 1, 0.008)
	if invoice, ok := InvoiceFor("linear", records); ok {
		t.Fatalf("records nobody labelled priced a worker: %+v", invoice)
	}
}
