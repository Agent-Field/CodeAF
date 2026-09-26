package modelapi

import "testing"

func TestChatURLJoinsTheBaseAndTheRouteWithOneSlash(t *testing.T) {
	for _, base := range []string{"http://127.0.0.1:9/v1", "http://127.0.0.1:9/v1/", " http://127.0.0.1:9/v1 "} {
		if got := ChatURL(base); got != "http://127.0.0.1:9/v1/chat/completions" {
			t.Fatalf("ChatURL(%q) = %q", base, got)
		}
	}
}
