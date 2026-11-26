package manual

import (
	"strings"
	"testing"
)

// The chat manual's words for two things changed on 2026-11-23. That no page
// offers the old words is internal/wordlaw's law (it reads the pages as text);
// what is tested here is the other half of the contract, that the manual still
// ANSWERS somebody who remembers them: one heading holds both the new words and
// the old ones, and a question in the old words reaches it.

// oldLabelSection is the one section that spells the retired label, in its
// heading, so the page answers by either spelling.
func oldLabelSection(title string) bool {
	lower := strings.ToLower(title)
	return strings.Contains(lower, "continue here") && strings.Contains(lower, "move here")
}

func TestTheOldLabelStillReachesTheMovePage(t *testing.T) {
	const page = "continuing-a-chat-on-another-computer"
	for _, asked := range []string{
		"what happened to the Continue here button",
		"where did Continue here go",
		"I used to click Continue here on home, what is it called now",
		"Continue here is now Move here",
	} {
		found := false
		for _, section := range Chat().Search(asked, DefaultResults) {
			if section.Page == page && oldLabelSection(section.Title) && strings.Contains(section.Body, "**Move here** is the one verb") {
				found = true
			}
		}
		if !found {
			t.Errorf("%q did not reach the section that answers by the old label", asked)
		}
	}
}

// The storage switch is "relay storage" now. Contract: a person who remembers
// "sync storage" still reaches the sections that answer for it, and the name
// they find says what it is called now.
func TestTheOldStorageNameStillReachesRelayStorage(t *testing.T) {
	for _, probe := range []struct{ asked, page string }{
		{"what happened to sync storage", "relay-what-it-stores-and-sees"},
		{"how do I turn on sync storage", "relay-what-it-stores-and-sees"},
		{"where is the sync storage row in settings", "settings"},
	} {
		found := false
		for _, section := range Chat().Search(probe.asked, DefaultResults) {
			lower := strings.ToLower(section.Title)
			if section.Page == probe.page && strings.Contains(lower, "relay storage") && strings.Contains(lower, "sync storage") {
				found = true
			}
		}
		if !found {
			t.Errorf("%q did not reach %s under a heading that names both spellings", probe.asked, probe.page)
		}
	}
}
