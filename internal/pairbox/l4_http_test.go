package pairbox

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// L4-H6d: a generic server failure is not a capacity refusal on the code road.
func TestL4PairServerFailure(t *testing.T) {
	for _, status := range []int{500, 503} {
		err := answer(&http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("temporarily unavailable"))}, 200, nil)
		if !errors.Is(err, wireauth.ErrUnavailable) {
			t.Fatal(err)
		}
	}
	err := answer(&http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"err":"full"}`))}, 200, nil)
	if !errors.Is(err, ErrRelayFull) {
		t.Fatal(err)
	}
}
