package manual

import (
	"strings"
	"testing"
)

// The desktop shimmer and the header's breathing dot have their own page.
// These questions are the words a person uses for them. The shared probe
// table in chat_test.go is left alone.
func TestDesktopShimmerManualAnswersTheLiveMotion(t *testing.T) {
	asked := []struct {
		question string
		page     string
		title    string
		says     []string
	}{
		{
			question: "why is the live step shimmering on the desktop",
			page:     "desktop-shimmer",
			title:    "Why is the live step shimmering on the desktop",
			says:     []string{"2.4 seconds", "quieter ink"},
		},
		{
			question: "what is the breathing dot next to N running",
			page:     "desktop-shimmer",
			title:    "What is the breathing dot next to N running",
			says:     []string{"6px", "2.8 seconds", "4 running"},
		},
		{
			question: "how do I stop the desktop shimmer and the breathing dot",
			page:     "desktop-shimmer",
			title:    "How do I stop the desktop shimmer and the breathing dot",
			says:     []string{"reduced motion", "no halo"},
		},
	}
	for _, ask := range asked {
		t.Run(ask.question, func(t *testing.T) {
			reached := false
			for _, section := range Chat().Search(ask.question, DefaultResults) {
				if section.Page != ask.page || !strings.Contains(section.Title, ask.title) {
					continue
				}
				reached = true
				for _, needle := range ask.says {
					if !strings.Contains(section.Body, needle) {
						t.Errorf("%q reached %q but its body omits %q", ask.question, section.Title, needle)
					}
				}
			}
			if !reached {
				t.Fatalf("%q does not reach %s %q", ask.question, ask.page, ask.title)
			}
		})
	}
}
