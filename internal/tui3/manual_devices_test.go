package tui3

import (
	"fmt"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/manual"
	"github.com/Agent-Field/codeaf/internal/pair"
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
			fmt.Sprintf(approveDenied, "laptop"), pair.ApprovedLine("laptop"),
			strings.Join([]string{devicesMoveKey, devicesNameKey, devicesGoneKey, devicesCloseKey}, " · "), fmt.Sprintf(devicesRemoveAsk, "desk"), fmt.Sprintf(devicesRevoked, "desk"), "seen 3h ago", "offline", approveWorking, approveDeclining, approveSlow, approveUnsure, ErrAlreadyPaired.Error(),
			deviceBring, deviceNothing, devicePickAsk, devicePickLeav,
			chatlist.Device{Name: "desk"}.Mark(false, tokens.Plain.Glyph(tokens.GQueued)), chatlist.RemovedChat,
			devicesWatchKey, devicesTypeKey, devicesAccessBoth, devicesAccessWatch, devicesAccessNone, devicesTypeAskFor("desk"),
			devicesWatchOnFor("desk"), devicesWatchOffFor("desk"), devicesTypeOnFor("desk"), devicesTypeOffFor("desk"), devicesPermitNoSetter,
			strings.Join([]string{devicesMoveKey, devicesNameKey, devicesGoneKey, devicesWatchKey, devicesTypeKey, devicesCloseKey}, " · "),
		},
		"naming-a-device": {devicesNameKeys, devicesNameAsk + "atlas▏", devicesNameOwn},
		"continuing-a-chat-on-another-computer": {
			chatlist.SyncPaused, chatlist.SyncRestored,
			moveAsk, moveStay, moveRetry, moveAlreadyRunning, movingHere, moveFails, moveCheckFails, moveStoreBusy, moveUnreachable, lostRaceWord, chatlist.Unreachable,
			fmt.Sprintf(resumeAskFormat, "tower"), resumeYes, resumeNo,
			chatlist.Moved("tower", 3200e6, true), chatlist.Moved("", 3200e6, false),
			chatlist.ArrivedHead("tower"), chatlist.OfferSetUp, chatlist.OfferNotNow,
			chatlist.OfferGotIt, chatlist.SetupLater, chatlist.Superseded("<device>"),
			chatlist.TestsLine(true, 0), chatlist.TestsLine(false, 2),
			chatlist.TakeoverLine(chatlist.Row{Status: chatlist.Running, Device: "tower", DurableAgo: 4 * time.Second}),
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

// The person-facing words keep the vocabulary law: the pages never speak of the
// machinery behind the devices.
//
// "relay" IS NOT ON THIS LIST ANY MORE. Until 2026-11-23 the pages called the
// server "the sync service" and kept the word relay out. The server is "the
// relay" in every sentence a person reads now, the refusals these pages quote
// verbatim (TestEverySentenceOfTheDeviceScreensIsOnItsPage) say it, and what the
// word may name is fenced by internal/wordlaw instead: the server and its
// storage switch, never a feature or an act.
var banned = regexp.MustCompile(`\b(node|lease|manifest|takeover|take over)\b`)

// oldNamesHeading is the one heading that spells the old names of Move here,
// "take over" among them, so a person who asks in those words reaches the page.
// It is the only place the pages may say them.
const oldNamesHeading = `## Move here: also called "continue here", "take over" or "continue on this machine"`

func TestTheDevicePagesKeepTheVocabularyLaw(t *testing.T) {
	for _, name := range []string{"add-or-remove-a-computer", "continuing-a-chat-on-another-computer", "naming-a-device"} {
		page, _ := manual.Chat().Page(name)
		if word := banned.FindString(strings.ReplaceAll(strings.ToLower(page), strings.ToLower(oldNamesHeading), "")); word != "" {
			t.Errorf("the page %s uses %q", name, word)
		}
	}
}
