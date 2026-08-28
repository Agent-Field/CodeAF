package pair

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/aforge-v2/internal/relay"
)

// THE MANUAL LAW, ENFORCED FOR THIS PACKAGE'S OWN SENTENCES.
//
// The chat answers questions about aforge out of the pages in
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
		// What `aforge serve` prints.
		strings.TrimRight(Lines("otter-lamp-42", code), "\n"),
		// The four ways `--at` fails, each naming its own cause.
		NoRelay("otter-lamp-42").Error(),
		Unreachable("https://relay.example.com").Error(),
		NotConnected("otter-lamp-42").Error(),
		NotPaired("otter-lamp-42").Error(),
		// And the ones a person meets less often.
		WrongCode("otter-lamp-42").Error(),
		NotThatMachine("otter-lamp-42").Error(),
		NoAnswer("otter-lamp-42").Error(),
		Busy().Error(),
		Stopped(),
		RevokedLine("laptop"),
	}
	for _, sentence := range said {
		if !manual.Chat().Mentions(sentence) {
			t.Errorf("no chat manual page says %q — add it to internal/manual/chat/reaching-this-machine-without-ssh.md", sentence)
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
		"5 attempts",              // CodeAttempts
		"up to 16",                // relay.MaxStreams
		"30 connections a minute", // relay.DialsPerMinute
		"~/.aforge/v3/remote/device.key",
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
	if CodeAttempts != 5 {
		t.Fatalf("CodeAttempts is %d and the page says 5", CodeAttempts)
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
