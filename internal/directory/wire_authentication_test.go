package directory

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

func TestAnAuthenticationRefusalKeepsItsUnauthorizedWireStatus(t *testing.T) {
	got := classify(fmt.Errorf("authentication: %w", wireauth.ErrUnauthorized))
	if got.status != http.StatusUnauthorized || got.code != "unauthorized" {
		t.Fatalf("authentication refusal became %d %q", got.status, got.code)
	}
}
