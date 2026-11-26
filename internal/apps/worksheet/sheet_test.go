package worksheet_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/worksheet"
)

func TestWorksheetCalculatesReferencesAndContainsErrors(t *testing.T) {
	sheet, err := worksheet.Open("Research\t3\t=B1*2\nDelivery\t4\t=B2+1\nTotals\t=SUM(B1:B2)\t=SUM(C1:C2)\n=A4\t=1/0")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		row, col int
		want     any
	}{
		{0, 2, float64(6)},
		{1, 2, float64(5)},
		{2, 1, float64(7)},
		{2, 2, float64(11)},
	} {
		got := sheet.Cell(tc.row, tc.col)
		if got.Value != tc.want || got.Error != "" || got.Formula == "" {
			t.Fatalf("cell %d,%d = %+v, want %v", tc.row, tc.col, got, tc.want)
		}
	}
	for _, col := range []int{0, 1} {
		if got := sheet.Cell(3, col); got.Error == "" || got.Value != nil {
			t.Fatalf("invalid formula escaped its cell: %+v", got)
		}
	}
}
