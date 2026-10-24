package pair

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/manual"
	"github.com/Agent-Field/codeaf/internal/relay"
)

// THE MANUAL LAW, ENFORCED FOR THIS PACKAGE'S OWN SENTENCES.
//
// The chat answers questions about codeaf out of the pages in
// internal/manual/chat, and its training data contains nothing about this
// program — so a sentence a person can be shown that is not in a page is a
// sentence the chat will improvise around or deny. Every line below is
// something somebody can read on their screen, quoted here against the corpus
// so that changing one without changing the page fails the build.
//
// The bar is the same one the other gates set: the page must SAY the sentence,
// word for word, not describe it well.
func TestEverySentenceThisPackageShowsIsInTheManual(t *testing.T) {
	code := &Code{digits: "715302"}
	said := []string{
		// The pairing, in the order a person meets it.
		PairingPreamble("otter-lamp-42"),
		PairingWeight,
		strings.TrimSpace(PairingPrompt("otter-lamp-42")),
		PairedLine("otter-lamp-42"),
		// What `codeaf serve` prints.
		strings.TrimRight(Lines("otter-lamp-42", code), "\n"),
		// The four ways `--at` fails, each naming its own cause.
		NoRelay("otter-lamp-42").Error(),
		Unreachable("https://service.example.com").Error(),
		NotConnected("otter-lamp-42").Error(),
		NotPaired("otter-lamp-42").Error(),
		// And the ones a person meets less often.
		WrongCode("otter-lamp-42").Error(),
		NotThatMachine("otter-lamp-42").Error(),
		NoAnswer("otter-lamp-42").Error(),
		Busy().Error(),
		Stopped(),
		RevokedLine("laptop"),
		// The join words on the machine being opened, and what its own screen
		// says about the attempts it turned away.
		WaitingLine("otter-lamp-42", "amber fox dune"),
		AskMachineLine("laptop", "amber fox dune"),
		AskChoice,
		ErrRefused.Error(),
		"a device asked to pair and was not let in",
		"someone typed a wrong code, so that code is no longer good",
		"a device tried to pair and there was no code to pair with",
	}
	for _, sentence := range said {
		if !manual.Chat().Mentions(sentence) {
			t.Errorf("no chat manual page says %q — add it to internal/manual/chat/reaching-this-machine-without-ssh.md", sentence)
		}
	}
}

// THE PAIRING OF A PERSON'S CHATS HAS ITS OWN PAGE, and every sentence a screen
// shows for it is quoted there whole. The page is named rather than searched
// for, so a sentence that moved to some other page still fails: a person who
// asks how to use codeaf on a second computer is sent to this one.
func TestEverySentenceOfPairingChatsIsOnItsOwnPage(t *testing.T) {
	const name = "use-this-on-another-computer"
	page, ok := manual.Chat().Page(name)
	if !ok {
		t.Fatalf("the page %s is not in the corpus", name)
	}
	page = strings.ToLower(page)
	code := &Code{digits: "715302", plate: "42"}
	said := []string{
		// What each side shows while a person pairs, in the order they meet it.
		strings.TrimRight(ChatLines(code, ""), "\n"),
		strings.TrimRight(ChatLines(code, "https://relay.example.com"), "\n"),
		WaitingChatsLine("amber fox dune"),
		AskChatsLine("laptop", "amber fox dune"),
		AskChoice,
		BurnLine,
		JoinedLine,
		PairedChatsLine("laptop"),
		// What can go wrong, each one sentence naming its own cause.
		ErrCodeShape.Error(),
		ErrCodeDidNotWork.Error(),
		ErrRefused.Error(),
		NothingWaitingUnder("42").Error(),
		ErrStopped.Error(),
		ErrSomeoneElse.Error(),
		ErrDidNotFinish.Error(),
		ErrOffering.Error(),
		ErrBadGrant.Error(),
		ErrSyncOff.Error(),
		ErrNoSync.Error(),
		ErrRelayBusy.Error(),
		ErrRelayTooOld.Error(),
		ErrDifferentChats.Error(),
		ErrAlreadyPaired.Error(),
		CannotReachHost("relay.example.com").Error(),
		TooManyFor(7 * time.Minute).Error(),
	}
	for _, sentence := range said {
		if !strings.Contains(page, strings.ToLower(sentence)) {
			t.Errorf("the page %s does not say %q — add it to internal/manual/chat/%s.md", name, sentence, name)
		}
	}
}

// The numbers a person is told are the numbers the code uses. A limit quoted in
// a page and applied in a constant is a limit that drifts, so the page is
// checked against the constants rather than against a memory of them.
func TestTheManualQuotesTheLimitsThisPackageActuallyApplies(t *testing.T) {
	page, ok := manual.Chat().Page("reaching-this-machine-without-ssh")
	if !ok {
		t.Fatal("the page reaching-this-machine-without-ssh is not in the corpus")
	}
	for _, quoted := range []string{
		"10 minutes",              // CodeValidFor
		"1 attempt",               // CodeAttempts
		"up to 16",                // relay.MaxStreams
		"30 connections a minute", // relay.DialsPerMinute
		"~/.codeaf/v3/remote/device.key",
	} {
		if !strings.Contains(page, quoted) {
			t.Errorf("the page does not quote %q", quoted)
		}
	}
	if relay.MaxStreams != 16 {
		t.Fatalf("relay.MaxStreams is %d and the page says 16", relay.MaxStreams)
	}
	if relay.DialsPerMinute != 30 {
		t.Fatalf("relay.DialsPerMinute is %d and the page says 30", relay.DialsPerMinute)
	}
	if int(CodeValidFor.Minutes()) != 10 {
		t.Fatalf("CodeValidFor is %v and the page says 10 minutes", CodeValidFor)
	}
	if CodeAttempts != 1 {
		t.Fatalf("CodeAttempts is %d and the page says 1", CodeAttempts)
	}
}

// The page must not describe the keychain or a fingerprint as something that
// works, because neither is built. A page that promised one would be the worst
// possible failure of this lane: somebody choosing to pair a device because
// they believed the key was behind a fingerprint.
func TestTheManualDoesNotPromiseAKeychainThisBuildDoesNotHave(t *testing.T) {
	page, ok := manual.Chat().Page("reaching-this-machine-without-ssh")
	if !ok {
		t.Fatal("the page reaching-this-machine-without-ssh is not in the corpus")
	}
	lower := strings.ToLower(page)
	if !strings.Contains(lower, "not built") {
		t.Error("the page does not say plainly that the keychain is not built")
	}
	if !strings.Contains(lower, "there is no touch id") && !strings.Contains(lower, "no touch id or fingerprint unlock") {
		t.Error("the page does not say plainly that there is no Touch ID in this build")
	}
	// And the seam itself must keep saying what it really is.
	if strings.Contains(strings.ToLower(OpenKeeper().Where()), "keychain") {
		t.Error("the keeper describes itself as a keychain, and this build has none")
	}
}

// THE SENTENCES OF NAMING A COMPUTER ARE ON THE PAGE THAT TELLS HOW. A person
// who is asked what to call this computer, or told what it is called now, asks
// the chat what that means, and the chat answers from the page.
func TestEverySentenceOfNamingAComputerIsOnItsPage(t *testing.T) {
	const name = "naming-a-device"
	page, ok := manual.Chat().Page(name)
	if !ok {
		t.Fatalf("the page %s is not in the corpus", name)
	}
	for _, sentence := range []string{
		NamePrompt("spark"),
		RenamedLine("atlas"),
		RenamedHereLine("atlas"),
		RenamedAloneLine("atlas"),
		devname.ErrEmpty.Error(),
		devname.ErrTooLong.Error(),
		devname.ErrControl.Error(),
	} {
		if !strings.Contains(page, sentence) {
			t.Errorf("the page %s does not say %q - add it to internal/manual/chat/%s.md", name, sentence, name)
		}
	}
}
