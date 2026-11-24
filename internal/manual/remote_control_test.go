package manual

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// REMOTE CONTROL IS A FEATURE THE CHAT CANNOT GUESS. A person who sees "tower went
// offline" or "watch, not type" on a live view asks the chat what it means, and the
// chat answers from this page alone: its training data has never met the feature.
// So every sentence the view can say is on the page word for word, the page says
// the feature is off until it is turned on, and the questions people ask reach it.

const remoteControlPage = "remote-control"

// V13: every sentence a live view can say is on the remote control page, spelled as
// the code spells it, so a person who asks about one is answered with that sentence.
func TestEverySentenceOfALiveViewIsOnTheRemoteControlPage(t *testing.T) {
	page, ok := Chat().Page(remoteControlPage)
	if !ok {
		t.Fatalf("the page %s is not in the chat corpus", remoteControlPage)
	}
	const host = "tower"
	said := []string{
		chatlist.LiveMoving,
		chatlist.LiveIdle,
		chatlist.LiveBudget,
		chatlist.LiveRemoved,
		chatlist.LiveWentOffline(host),
		chatlist.LiveOfflineSeen(host, "14:02"),
		chatlist.LiveNotSentOffline(host),
		chatlist.LiveWatchOnly(host),
		chatlist.LiveTypingFrom(host),
		chatlist.LiveNotAllowedHere(host),
		chatlist.LiveNeedsItsOwnOK(host),
	}
	// The close reasons are a closed set; the page lists the ones a person meets.
	for _, reason := range []string{
		"not-running", "not-allowed", "holder-gone", "holder-old", "removed",
		"permit-changed", "lagging", "frame-too-large", "budget", "idle", "host-closing", "too-many", "remote-control-off",
	} {
		said = append(said, chatlist.LiveSaid(reason, host))
	}
	for _, sentence := range said {
		if !strings.Contains(page, sentence) {
			t.Errorf("the page %s does not say %q: add it to internal/manual/chat/%s.md", remoteControlPage, sentence, remoteControlPage)
		}
	}
}

// V13: three questions are pinned to the SECTION that answers them, not only to the
// page, because the page holds a dozen sections and these are the ones a
// person asks with a sentence on the screen in front of them.
func TestTheQuestionsAboutAWordOnALiveViewReachTheSectionThatExplainsIt(t *testing.T) {
	for _, tc := range []struct {
		question string
		title    string
		says     string
	}{
		{"what does watch only mean", "watch only", "lets this computer watch, not type"},
		{"it says tower went offline", "went offline", "last seen"},
		{"how do I turn remote control on", "Remote control is off", "CODEAF_REMOTE_CONTROL=1"},
		{"it says tower turned remote control off", "Why did my live view end", "tower turned remote control off."},
	} {
		found := false
		for _, section := range Chat().Search(tc.question, DefaultResults) {
			if section.Page == remoteControlPage && strings.Contains(section.Title, tc.title) && strings.Contains(section.Body, tc.says) {
				found = true
			}
		}
		if !found {
			t.Errorf("%q did not reach the %q section of %s", tc.question, tc.title, remoteControlPage)
		}
	}
}

// The page says what is true on the day it ships: remote control is off until it
// is turned on, and a computer that has not been allowed can only watch.
func TestTheRemoteControlPageSaysItIsOffUntilTurnedOnAndHowToTurnItOn(t *testing.T) {
	page, _ := Chat().Page(remoteControlPage)
	for _, needle := range []string{
		"off until you turn it on",
		"CODEAF_REMOTE_CONTROL=1",
		"`remote control`",
		"codeaf devices permit",
		"never starts a chat",
		"codeaf identity rotate",
	} {
		if !strings.Contains(page, needle) {
			t.Errorf("the page does not say %q", needle)
		}
	}
}

// The page keeps the vocabulary law of the device pages: it never speaks of the
// machinery, and the word relay names only the server and its storage.
func TestTheRemoteControlPageKeepsTheVocabularyLaw(t *testing.T) {
	page, _ := Chat().Page(remoteControlPage)
	low := strings.ToLower(page)
	for _, banned := range []string{"fleet", "enroll", "enrol", "lease", "directory", "vault", "blob", "manifest", "takeover", "take over", "continue here"} {
		if strings.Contains(low, banned) {
			t.Errorf("the remote control page says %q", banned)
		}
	}
	for _, banned := range []string{"the board", "the self page", "standing watch", "resident employee", "front desk"} {
		if strings.Contains(low, banned) {
			t.Errorf("the remote control page speaks of the resident: %q", banned)
		}
	}
}

// Each section is retrieved alone, so each stays under the cap and says what it is
// about in its own words.
func TestEverySectionOfTheRemoteControlPageIsSelfContainedAndShort(t *testing.T) {
	n := 0
	for _, section := range Chat().Sections() {
		if section.Page != remoteControlPage {
			continue
		}
		n++
		if size := utf8.RuneCountInString(section.Title) + utf8.RuneCountInString(section.Body); size > 2000 {
			t.Errorf("section %q is %d characters, over the 2000 a retrieved section may be", section.Title, size)
		}
	}
	if n < 13 {
		t.Errorf("the page has %d sections; the outline has at least 13", n)
	}
}
