package manual

import (
	"strings"
	"testing"
)

func TestIdleOpen_C6_C7_IndexedAnswersDistinguishFreeAndHeld(t *testing.T) {
	for _, probe := range []struct{ question, phrase string }{
		{"a row on home and the verb it offers", "There is no Continue-here card"},
		{"I reopened an old local chat after the other computer let go", "latest saved history here automatically"},
		{"Continue a chat here that another machine left", "released or lapsed chat opens directly"},
		{"Why a chat says held on build-box or build-box offline", "released or lapsed chat opens here directly"},
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
