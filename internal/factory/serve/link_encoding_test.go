package serve

import (
	"net/url"
	"testing"
)

func TestAccessSecretKeepsEncodedTokenCharacters(t *testing.T) {
	const origin = "https://factory.example"
	for _, secret := range []string{"a+b", "a b", "a%b", "a&b"} {
		link := "codeaf://factory/join?" + url.Values{"url": {origin}}.Encode() + "#token=" + url.QueryEscape(secret)
		got, err := accessSecret(link, origin)
		if err != nil || got != secret {
			t.Fatalf("encoded token changed: got %q, want %q, error %v", got, secret, err)
		}
	}
}
