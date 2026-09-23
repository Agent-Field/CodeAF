package report

import "testing"

func TestSummaryReadsTheWayAccountantsWrite(t *testing.T) {
	got := Summary(523456, 150000)
	want := "In: 5,234.56\nOut: (1,500.00)\n"
	if got != want {
		t.Fatalf("Summary =\n%s\nwant\n%s", got, want)
	}
}
