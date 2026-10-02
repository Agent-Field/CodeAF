package tui3

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/manual"
)

// THE SENTENCES OF THE DEVICE SCREENS ARE IN THEIR PAGES, WORD FOR WORD. A
// person who is shown one of these lines asks the chat what it means, and the
// chat answers from the page: a line the page does not quote is a line the chat
// improvises around. Each sentence is the constant the screen draws from, so
// respelling one without its page fails here.
func TestEverySentenceOfTheDeviceScreensIsOnItsPage(t *testing.T) {
	pages := map[string][]string{
		"add-or-remove-a-computer": {
			addMachineHeading, addMachinePitch, addMachineStepRun, addMachineStepPut, addMachineShow,
			approveAsk, fmt.Sprintf(approveCheckWord, "4821"), approveKeys, approveGone,
			fmt.Sprintf(approveDenied, "laptop"), fmt.Sprintf(joinedFleetWord, "laptop"),
			devicesKeys, devicesNone, fmt.Sprintf(devicesRevoked, "dumb"), "seen 3h ago",
			deviceBring, deviceNothing, devicePickAsk, devicePickLeav,
			chatlist.Device{Name: "dumb"}.Mark(false), chatlist.Removed,
		},
		"naming-a-device": {devicesNameKeys, devicesNameAsk + "atlas▏", devicesNameOwn},
		"continuing-a-chat-on-another-computer": {
			continueAsk, continueStay, continueFails, lostRaceWord, chatlist.Unreachable,
			fmt.Sprintf(resumeAskFormat, "spark"), resumeYes, resumeNo,
			chatlist.Moved("spark", 3200e6, true), chatlist.Moved("", 3200e6, false),
			chatlist.ArrivedHead("spark"), chatlist.OfferSetUp, chatlist.OfferNotNow,
			chatlist.OfferGotIt, chatlist.SetupLater, chatlist.Superseded("<device>"),
			chatlist.TestsLine(true, 0), chatlist.TestsLine(false, 2),
			chatlist.TakeoverLine(chatlist.Row{Status: chatlist.Running, Device: "spark", DurableAgo: 4 * time.Second}),
		},
	}
	for name, said := range pages {
		page, ok := manual.Chat().Page(name)
		if !ok {
			t.Fatalf("the page %s is not in the corpus", name)
		}
		for _, sentence := range said {
			if !strings.Contains(page, sentence) {
				t.Errorf("the page %s does not say %q - add it to internal/manual/chat/%s.md", name, sentence, name)
			}
		}
	}
}

// The person-facing words keep the vocabulary law: the two pages never speak
// of the machinery behind the devices.
var banned = regexp.MustCompile(`\b(relay|node|lease|manifest|takeover|take over)\b`)

func TestTheDevicePagesKeepTheVocabularyLaw(t *testing.T) {
	for _, name := range []string{"add-or-remove-a-computer", "continuing-a-chat-on-another-computer", "naming-a-device"} {
		page, _ := manual.Chat().Page(name)
		if word := banned.FindString(strings.ToLower(page)); word != "" {
			t.Errorf("the page %s uses %q", name, word)
		}
	}
}
