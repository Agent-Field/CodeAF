package luby

import (
	"math"
	"reflect"
	"testing"
)

// Translation of src/session/luby.test.ts — verbatim subtest names, same
// assertion semantics. `expect(a).toEqual(b)` on arrays is a deep compare
// (reflect.DeepEqual over []float64); `expect(x).toBe(y)` on numbers is
// strict equality.

func TestLubySequence(t *testing.T) {
	t.Run("known prefix (1-indexed): 1,1,2,1,1,2,4,1,1,2,1,1,2,4,8", func(t *testing.T) {
		expected := []float64{1, 1, 2, 1, 1, 2, 4, 1, 1, 2, 1, 1, 2, 4, 8}
		got := make([]float64, len(expected))
		for idx := range expected {
			got[idx] = LubySequence(float64(idx + 1))
		}
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("got %v, want %v", got, expected)
		}
	})

	t.Run("reluctant-doubling: powers of two land at i = 2^k - 1", func(t *testing.T) {
		// i=1 → 1, i=3 → 2, i=7 → 4, i=15 → 8, i=31 → 16
		for _, c := range []struct{ in, want float64 }{
			{1, 1}, {3, 2}, {7, 4}, {15, 8}, {31, 16},
		} {
			if got := LubySequence(c.in); got != c.want {
				t.Fatalf("LubySequence(%v) = %v, want %v", c.in, got, c.want)
			}
		}
	})

	t.Run("values are always powers of two", func(t *testing.T) {
		for i := 1; i <= 64; i++ {
			v := LubySequence(float64(i))
			// power of two ⇒ v & (v-1) === 0 (JS `&` is an int32 coercion)
			if iv := int32(v); iv&(iv-1) != 0 {
				t.Fatalf("LubySequence(%d) = %v is not a power of two", i, v)
			}
			if !(v >= 1) {
				t.Fatalf("LubySequence(%d) = %v, want >= 1", i, v)
			}
		}
	})

	t.Run("clamps non-positive / fractional indices to 1", func(t *testing.T) {
		if got := LubySequence(0); got != 1 {
			t.Fatalf("LubySequence(0) = %v, want 1", got)
		}
		if got := LubySequence(-5); got != 1 {
			t.Fatalf("LubySequence(-5) = %v, want 1", got)
		}
		if got := LubySequence(1.9); got != 1 { // floor(1.9)=1
			t.Fatalf("LubySequence(1.9) = %v, want 1", got)
		}
		if got := LubySequence(3.4); got != 2 { // floor(3.4)=3 → 2
			t.Fatalf("LubySequence(3.4) = %v, want 2", got)
		}
	})
}

func TestLubyBudgets(t *testing.T) {
	t.Run("first n unscaled budgets equal the sequence", func(t *testing.T) {
		want := []float64{1, 1, 2, 1, 1, 2, 4}
		if got := LubyBudgets(7); !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("scales each value by unit", func(t *testing.T) {
		want := []float64{10, 10, 20, 10, 10, 20, 40}
		if got := LubyBudgets(7, 10); !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("n <= 0 yields empty array", func(t *testing.T) {
		if got := LubyBudgets(0); !reflect.DeepEqual(got, []float64{}) {
			t.Fatalf("LubyBudgets(0) = %v, want []", got)
		}
		if got := LubyBudgets(-3); !reflect.DeepEqual(got, []float64{}) {
			t.Fatalf("LubyBudgets(-3) = %v, want []", got)
		}
	})

	t.Run("default unit is 1", func(t *testing.T) {
		if got, want := LubyBudgets(3), LubyBudgets(3, 1); !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
}

func TestLubySequence_FixFlag_NaN(t *testing.T) {
	t.Setenv("CODEAF_GO_FIX_LUBY_GUARDS", "1")
	if got := LubySequence(math.NaN()); got != 1 {
		t.Fatalf("LubySequence(NaN) = %v, want 1 (flag on)", got)
	}
}

func TestLubyBudgets_FixFlag_Inf(t *testing.T) {
	t.Setenv("CODEAF_GO_FIX_LUBY_GUARDS", "1")
	got := LubyBudgets(math.Inf(1))
	if len(got) != 0 {
		t.Fatalf("LubyBudgets(+Inf) = %v, want [] (flag on)", got)
	}
}

func TestLubySequence_FixFlag_Off_NormalValues(t *testing.T) {
	// flag unset: behaviour unchanged for normal inputs
	if got := LubySequence(3); got != 2 {
		t.Fatalf("LubySequence(3) = %v, want 2", got)
	}
	if got := LubySequence(7); got != 4 {
		t.Fatalf("LubySequence(7) = %v, want 4", got)
	}
}
