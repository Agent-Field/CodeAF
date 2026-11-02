package manual

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/packed"
)

func TestRelayDefectAnswersSurvivePacking(t *testing.T) {
	archive, err := packed.Pack("chat")
	if err != nil {
		t.Fatal(err)
	}
	if err := packed.Verify(archive, "chat"); err != nil {
		t.Fatal(err)
	}
	compressed := newCorpus(packed.New(archive), "chat/*.md")
	for _, corpus := range []*Corpus{Chat(), compressed} {
		for _, probe := range []struct{ query, page, words string }{
			{"my chosen chat while a startup chat is still arriving", "home", "continuing <Y> next; <X> is still on its way here"},
			{"slow chat listing timeout waiting for a service host", "continuing-a-chat-on-another-computer", "<host> did not answer in 25s; still trying"},
			{"my typed message after continue here is kept does it send by itself", "continuing-a-chat-on-another-computer", "your message is still in the box; press enter to send it"},
			{"continue here another device continuing first picked this chat up again", "continuing-a-chat-on-another-computer", "<holder> picked this chat up again; press enter to continue it here"},
			{"relay trust proxy registration dial pairing X-Forwarded-For", "relay-hosted-or-your-own", "registration, dial and pairing limits"},
		} {
			found := false
			for _, section := range corpus.Search(probe.query, DefaultResults) {
				if section.Page == probe.page && strings.Contains(section.Body, probe.words) {
					found = true
				}
			}
			if !found {
				t.Errorf("%q did not reach %s saying %q", probe.query, probe.page, probe.words)
			}
		}
		page, _ := corpus.Page("continuing-a-chat-on-another-computer")
		for _, part := range strings.Split(page, "\n## ")[1:] {
			heading, _, _ := strings.Cut(part, "\n")
			if strings.HasPrefix(heading, "my choice while") || strings.HasPrefix(heading, "My typed message") || strings.HasPrefix(heading, "slow chat listing") || strings.HasPrefix(heading, "Continue here does nothing") || strings.HasPrefix(heading, "why continue here failed") {
				if len([]rune(part)) > 2000 {
					t.Errorf("section exceeds 2000 characters: %s", heading)
				}
			}
		}
	}
}
