package manual

import (
	"regexp"
	"strings"
	"testing"
)

// The chat manual's words for two things changed on 2026-11-23, and these are
// the contract lines a person can watch hold:
//
//   - The act of carrying a chat from another computer to this one is "Move
//     here". No page offers "Continue here".
//   - The manual still answers somebody who remembers the old label: one
//     heading holds both the new words and the old ones, and a question in the
//     old words reaches it.
//   - "sync service" is gone. The server is "the relay", and "relay" names only
//     that server and its storage switch, never a feature or something a person
//     does.

// oldLabelSection is the one section that may spell the retired label. The
// heading carries both spellings so the page answers by either.
func oldLabelSection(title string) bool {
	lower := strings.ToLower(title)
	return strings.Contains(lower, "continue here") && strings.Contains(lower, "move here")
}

// relayAsAFeature matches "relay" used as a verb or as the name of a feature.
// A server plural ("both relays") and the server's own nouns are fine; a person
// never "relays a chat", and there is no "relay mode".
var relayAsAFeature = regexp.MustCompile(`(?i)\b(relayed|relaying|relay mode|relay feature|relays? (a|the|your) (chat|chats|message|messages|answer|conversation)|relay (a|your) (chat|chats|message|messages))\b`)

func TestChatCorpusKeepsTheRetiredWordsOutOfSight(t *testing.T) {
	retiredLabel := regexp.MustCompile(`(?i)continue[- ]here`)
	for _, section := range Chat().Sections() {
		where := section.Page + " / " + section.Title
		if retiredLabel.MatchString(section.Title+"\n"+section.Body) && !oldLabelSection(section.Title) {
			t.Errorf("%s offers the retired label %q; the act is Move here", where, retiredLabel.FindString(section.Title+"\n"+section.Body))
		}
		for _, text := range []string{section.Title, section.Body} {
			if strings.Contains(strings.ToLower(text), "sync service") {
				t.Errorf("%s says \"sync service\"; the server is the relay", where)
			}
			if found := relayAsAFeature.FindString(text); found != "" {
				t.Errorf("%s uses %q; relay names only the server and its storage switch", where, found)
			}
		}
	}
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
