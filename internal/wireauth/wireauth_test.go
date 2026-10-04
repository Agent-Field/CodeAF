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
		{ErrGone, ErrGone},
		{fmt.Errorf("wrapped: %w", ErrGone), ErrGone},
		{fmt.Errorf("wrapped: %w", ErrRevoked), ErrRevoked},
		{ErrUnauthorized, ErrUnauthorized},
		{other, ErrUnauthorized},
	} {
		if got := Narrow(tc.in); got != tc.want {
			t.Errorf("Narrow(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// Endpoint puts a route after a base with or without its own path, and with or
// without a trailing slash, and leaves the route's escaping and query alone.
func TestEndpointJoinsARouteToABaseWithAPath(t *testing.T) {
	for _, tc := range []struct{ base, route, want string }{
		{"http://h:1", "/v1/dir/list", "http://h:1/v1/dir/list"},
		{"http://h:1/", "/v1/dir/list", "http://h:1/v1/dir/list"},
		{"https://h/fabric", "/v1/dir/list", "https://h/fabric/v1/dir/list"},
		{"https://h/fabric/", "/v1/dir/list", "https://h/fabric/v1/dir/list"},
		{"https://h/fabric", "/v1/dir/cells/a%2Fb?x=1", "https://h/fabric/v1/dir/cells/a%2Fb?x=1"},
	} {
		if got := Endpoint(tc.base, tc.route); got != tc.want {
			t.Errorf("Endpoint(%q, %q) = %q, want %q", tc.base, tc.route, got, tc.want)
		}
	}
}
