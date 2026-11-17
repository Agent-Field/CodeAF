package main

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/Agent-Field/codeaf/internal/provider"
)

// A RUN IS TRIED ONCE MORE ONLY AFTER AN ERROR THAT MAY PASS (L6): a busy or
// failing provider, or a dropped connection. A refusal or a bad key is not.
func TestOnlyAModelErrorThatMayPassIsTriedAgain(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("turn: %w", &provider.APIError{Status: 503}), true},
		{&provider.APIError{Status: 429}, true},
		{&provider.APIError{Status: 401}, false},
		{&provider.APIError{Status: 400}, false},
		{fmt.Errorf("read: %w", io.ErrUnexpectedEOF), true},
		{errors.New("the model refused"), false},
	} {
		if got := transientModelError(c.err); got != c.want {
			t.Errorf("transientModelError(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
