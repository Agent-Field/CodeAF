package syncsetup_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// THE MANUAL NAMES EVERY CHANNEL'S DEFAULT RELAY. Where sync goes by default
// has one source of truth in internal/syncsetup/relayurl.go. The manual cannot
// import this package (it would be a cycle), so the check lives here: every
// channel constant must appear in the pages that explain the default sync
// service, and HostedRelayURL must be one of them.
func TestTheManualNamesTheHostedRelayTheCodeDefaultsTo(t *testing.T) {
	var corpus strings.Builder
	for _, section := range manual.Chat().Search("where does sync go by default hosted relay channel", 50) {
		corpus.WriteString(section.Body)
	}
	text := corpus.String()
	channels := []string{
		syncsetup.ProductionRelayURL,
		syncsetup.StagingRelayURL,
		syncsetup.DevRelayURL,
	}
	for _, addr := range channels {
		if !strings.Contains(text, addr) {
			t.Errorf("no page about where sync goes by default names %s: update relay-hosted-or-your-own and use-this-on-another-computer", addr)
		}
	}
	if hosted := syncsetup.HostedRelayURL; hosted != "" && !slices.Contains(channels, hosted) {
		t.Errorf("the code's default relay %s is not one of the documented channel relays %v", hosted, channels)
	}
}
