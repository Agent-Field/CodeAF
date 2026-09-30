package wireauth

import (
	"errors"
	"fmt"
	"testing"
)

// Narrow keeps the two refusals a person can act on and folds every other one
// into unauthorized, however deeply it was wrapped.
func TestNarrowNamesOnlyWhatAPersonCanAct(t *testing.T) {
	other := errors.New("bad cert")
	for _, tc := range []struct{ in, want error }{
		{ErrSkew, ErrSkew},
		{ErrRevoked, ErrRevoked},
		{fmt.Errorf("wrapped: %w", ErrRevoked), ErrRevoked},
		{ErrUnauthorized, ErrUnauthorized},
		{other, ErrUnauthorized},
	} {
		if got := Narrow(tc.in); got != tc.want {
			t.Errorf("Narrow(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
