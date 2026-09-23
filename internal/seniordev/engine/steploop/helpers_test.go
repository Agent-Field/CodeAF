//go:build !windows

package steploop

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

// Shared helpers for the tests in this package.

func jsonString(t *testing.T, value any) string {
	t.Helper()
	raw, err := jsonutil.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
