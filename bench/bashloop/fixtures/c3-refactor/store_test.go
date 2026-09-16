package inventory

import (
	"math"
	"testing"
)

func nearly(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestItemPrice(t *testing.T) {
	s := &Store{Items: []Item{
		{Name: "mug", Price: 10, Discounted: false},
		{Name: "poster", Price: 20, Discounted: true},
	}}
	if got := s.ItemPrice(s.Items[0]); !nearly(got, 10.825) {
		t.Errorf("full-rate item = %v, want 10.825", got)
	}
	if got := s.ItemPrice(s.Items[1]); !nearly(got, 21) {
		t.Errorf("discounted item = %v, want 21", got)
	}
}

func TestOrderTotal(t *testing.T) {
	s := &Store{
		Items: []Item{
			{Name: "mug", Price: 10},
			{Name: "poster", Price: 20, Discounted: true},
		},
		Orders: []Order{{ID: "o1", Items: []int{0, 1}}},
	}
	// 10 * 1.0825 + 20 * 1.05
	if got := s.OrderTotal(s.Orders[0]); !nearly(got, 31.825) {
		t.Errorf("OrderTotal = %v, want 31.825", got)
	}
}

func TestTaxLine(t *testing.T) {
	s := &Store{Items: []Item{
		{Name: "mug", Price: 100},
		{Name: "poster", Price: 200, Discounted: true},
	}}
	// 100 * 0.0825 + 200 * 0.05
	if got := s.TaxLine(); !nearly(got, 18.25) {
		t.Errorf("TaxLine = %v, want 18.25", got)
	}
}
