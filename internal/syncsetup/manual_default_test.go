package syncsetup_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// hostedAddress is the address the chat's own manual gives for the hosted relay.
const hostedAddress = "https://codeaf.agentfield.ai/fabric"

// THE MANUAL NAMES THE DEFAULT RELAY THE CODE HAS. Where sync goes by default has
// one source of truth, [syncsetup.HostedRelayURL]. The manual cannot import this
// package (it would be a cycle), so the check lives here: when the constant is
// set it must be the address the pages name, and when the default flips this
// test fails naming the page that has to change with it.
func TestTheManualNamesTheHostedRelayTheCodeDefaultsTo(t *testing.T) {
	var corpus strings.Builder
	for _, section := range manual.Chat().Search("where does sync go by default hosted relay", 50) {
		corpus.WriteString(section.Body)
	}
	if !strings.Contains(corpus.String(), hostedAddress) {
		t.Fatalf("no page about where sync goes by default names %s", hostedAddress)
	}
	if hosted := syncsetup.HostedRelayURL; hosted != "" && hosted != hostedAddress {
		t.Errorf("the code's default relay is %s but the manual says %s: update relay-hosted-or-your-own and use-this-on-another-computer", hosted, hostedAddress)
	}
}
