package tally

import "testing"

func TestCellMeanAndVar(t *testing.T) {
	if got := (Cell{}).Mean(); got != 0 {
		t.Fatalf("empty Mean = %v, want 0", got)
	}
	if got := (Cell{}).Var(); got != 0 {
		t.Fatalf("empty Var = %v, want 0", got)
	}
	if got := (Cell{N: 1, Sum: 5, SumSq: 25}).Var(); got != 0 {
		t.Fatalf("single-observation Var = %v, want 0", got)
	}
	c := Cell{N: 3, Sum: 6, SumSq: 14} // observations 1, 2, 3
	if got := c.Mean(); got != 2 {
		t.Fatalf("Mean = %v, want 2", got)
	}
	if got := c.Var(); got != 1 {
		t.Fatalf("Var = %v, want 1", got)
	}
}

func TestCellVarNeverNegative(t *testing.T) {
	// (4 - 3*3/2) / 1 rounds negative; the clamp must hold it at zero.
	if got := (Cell{N: 2, Sum: 3, SumSq: 4}).Var(); got != 0 {
		t.Fatalf("Var = %v, want 0", got)
	}
}
