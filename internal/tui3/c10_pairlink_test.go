package tui3

import "testing"

// C1 and C10 intercept a self-hosted link as pairing, so pasting its key can
// never submit it as an ordinary message to a model.
func TestC10SelfHostedPairLinkIsIntercepted(t *testing.T) {
	for _, link := range []string{
		"http://127.0.0.1:8787/p/k7m2q9xd#AAAAAAAAAAAAAAAAAAAAAA",
		"https://sync.example/fabric/p/k7m2q9xd#AAAAAAAAAAAAAAAAAAAAAA",
		"codeaf://pair?code=k7m2q9xd&via=http://127.0.0.1:8787#AAAAAAAAAAAAAAAAAAAAAA",
	} {
		if !isPairLink(link) {
			t.Errorf("custom pairing link would reach the model: %s", link)
		}
	}
}
