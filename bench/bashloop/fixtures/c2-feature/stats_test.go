package stats

import (
	"math"
	"testing"
)

func TestMean(t *testing.T) {
	if got := Mean(nil); got != 0 {
		t.Errorf("Mean(nil) = %v, want 0", got)
	}
	if got := Mean([]float64{2, 4, 6}); got != 4 {
		t.Errorf("Mean = %v, want 4", got)
	}
}

func TestMedian(t *testing.T) {
	if got := Median(nil); got != 0 {
		t.Errorf("Median(nil) = %v, want 0", got)
	}
	if got := Median([]float64{3, 1, 2}); got != 2 {
		t.Errorf("Median odd = %v, want 2", got)
	}
	if got := Median([]float64{4, 1, 2, 3}); got != 2.5 {
		t.Errorf("Median even = %v, want 2.5", got)
	}
	values := []float64{3, 1, 2}
	Median(values)
	if values[0] != 3 {
		t.Fatalf("Median modified its input: %v", values)
	}
	if math.Abs(Median([]float64{10, 20})-15) > 1e-9 {
		t.Fatal("Median even should average the middle two")
	}
}
