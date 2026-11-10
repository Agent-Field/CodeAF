package manual

import (
	"strings"
	"testing"
)

func TestIdleOpen_C6_C7_IndexedAnswersDistinguishFreeAndHeld(t *testing.T) {
	for _, probe := range []struct{ question, phrase string }{
		{"a row on home and the verb it offers", "There is no Continue-here card"},
		{"I reopened an old local chat after the other computer let go", "latest saved history here automatically"},
		{"Continue a chat here that another machine left", "released chat opens directly"},
		{"Why a chat says held on build-box or build-box offline", "released chat opens here directly"},
	} {
		found := false
		for _, section := range Chat().Search(probe.question, DefaultResults) {
			if strings.Contains(section.Body, probe.phrase) && strings.Contains(section.Body, "live hold") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q must retrieve the free-open and live-hold distinction", probe.question)
		}
	}
}

func TestIdleOpen_R6_ManualExplainsLapsedConsentAndOfflineDraft(t *testing.T) {
	for _, question := range []string{
		"Idle chats release their hold",
		"opening a free chat from Home after restarting",
		"what does held on tower or working on tower mean",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if strings.Contains(section.Body, "lapsed") && strings.Contains(section.Body, "working") && strings.Contains(section.Body, "card") {
				found = true
			}
		}
		if !found {
			t.Errorf("%q must explain consent for a lapsed working holder", question)
		}
	}
	found := false
	for _, section := range Chat().Search("opening a saved chat offline asking again keeping unsent words", DefaultResults) {
		found = found || strings.Contains(section.Body, "your words are still in the box") && strings.Contains(section.Body, "calls no model")
	}
	if !found {
		t.Error("offline opening and send must explain why the draft is kept")
	}
}
