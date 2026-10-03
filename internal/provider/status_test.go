package provider

import (
	"errors"
	"fmt"
	"testing"
)

func TestExpiredKeyRecognitionKeepsStructuredAndFlattenedRefusalsEquivalent(t *testing.T) {
	for _, tc := range []struct {
		status  int
		message string
		want    bool
	}{
		{401, "API key expired.", true}, {401, "token EXPIRED", true},
		{401, "invalid key", false}, {403, "token expired", false},
		{429, "expired quota", false}, {500, "expired upstream", false},
	} {
		t.Run(fmt.Sprintf("%d/%s", tc.status, tc.message), func(t *testing.T) {
			err := &APIError{Status: tc.status, Message: tc.message}
			for _, got := range []error{err, errors.New(err.Error()), fmt.Errorf("turn failed: %w", err)} {
				if KeyExpiredFrom(got) != tc.want {
					t.Errorf("%q expired=%v, want %v", got, KeyExpiredFrom(got), tc.want)
				}
			}
		})
	}
	for _, err := range []error{nil, errors.New("connection expired"), errors.New("API error (4010): expired")} {
		if KeyExpiredFrom(err) {
			t.Errorf("%v became an expired key refusal", err)
		}
	}
}
