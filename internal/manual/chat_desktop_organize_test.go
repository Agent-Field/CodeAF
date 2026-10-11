package manual

import (
	"os"
	"strings"
	"testing"
)

// These questions must find the shipped controls and their limits using the
// words a person asks, so a complete page cannot silently become unreachable.
func TestDesktopOrganizingQuestionsReachTheirAnswers(t *testing.T) {
	probes := []struct{ question, page, says string }{
		{"how do I organize my old chats into places", "desktop-organize", "Organize now"},
		{"what is Add to what's there versus Start fresh", "desktop-organize", "their chats stay together"},
		{"how do I review the plan and leave out a change", "desktop-organize", "uncheck any"},
		{"can I cancel organizing or undo the accepted plan", "desktop-organize", "whole accepted plan"},
		{"what is the suggested place arrow beside my draft", "desktop-live-guess", "without asking a model"},
		{"how do I change the suggested place before sending", "desktop-live-guess", "until the first reply has settled"},
		{"what are Views on a place Home and how do I clear a view", "desktop-place-views", "Press the same view again"},
		{"why does my chat say also in another place", "desktop-place-views", "no fixed cap"},
		{"how do I organize this place by month or by project", "desktop-steer-a-place", "Organize this by…"},
		{"what if codeaf does not understand how I want this place arranged", "desktop-steer-a-place", "I did not understand that; try 'by month' or 'by project'"},
	}
	for _, probe := range probes {
		t.Run(probe.question, func(t *testing.T) {
			sections := Chat().Search(probe.question, DefaultResults)
			for _, section := range sections {
				if section.Page == probe.page && strings.Contains(section.Body, probe.says) {
					return
				}
			}
			t.Fatalf("%q did not reach %s with %q; found %v", probe.question, probe.page, probe.says, sections)
		})
	}
}

// The organizing pages teach actions and visible consequences rather than the
// internal stages; the cost page separately names the actual billed model.
func TestDesktopOrganizingPagesUsePersonFacingWords(t *testing.T) {
	for _, page := range []string{"desktop-organize", "desktop-live-guess", "desktop-place-views", "desktop-steer-a-place"} {
		body, err := os.ReadFile("chat/" + page + ".md")
		if err != nil {
			t.Fatal(err)
		}
		for _, word := range []string{"auditor", "cluster", "embedding", "fallback"} {
			if strings.Contains(strings.ToLower(string(body)), word) {
				t.Errorf("%s teaches the internal word %q", page, word)
			}
		}
	}
}
