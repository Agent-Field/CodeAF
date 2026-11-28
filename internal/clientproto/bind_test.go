package clientproto

import (
	"bytes"
	"testing"
)

// P2, P9: a MAC computed under the link key changes with either key and with the link key,
// so a relay that substitutes the phone's keys cannot reproduce it without the link key.
func TestBindChangesWithEveryInput(t *testing.T) {
	link := bytes.Repeat([]byte{5}, ClientLinkKeyBytes)
	sign, box := bytes.Repeat([]byte{6}, 32), bytes.Repeat([]byte{7}, 32)
	base := BindMAC(link, sign, box)
	for name, other := range map[string][32]byte{
		"another sign key": BindMAC(link, bytes.Repeat([]byte{8}, 32), box),
		"another box key":  BindMAC(link, sign, bytes.Repeat([]byte{8}, 32)),
		"another link key": BindMAC(bytes.Repeat([]byte{9}, ClientLinkKeyBytes), sign, box),
		"keys swapped":     BindMAC(link, box, sign),
	} {
		if other == base {
			t.Errorf("%s must change the MAC", name)
		}
	}
}

// P2: the four digits are four decimal digits, and are not the first bytes the bind carries.
func TestCheckDigitsAreFourDigitsFromTheBytesAfterTheBind(t *testing.T) {
	mac := BindMAC(bytes.Repeat([]byte{5}, ClientLinkKeyBytes), bytes.Repeat([]byte{6}, 32), bytes.Repeat([]byte{7}, 32))
	if got := CheckDigits(mac); len(got) != 4 {
		t.Fatalf("check digits %q", got)
	}
	if len(Bind(mac)) != ClientLinkBindLen {
		t.Fatalf("bind is %d bytes", len(Bind(mac)))
	}
	var zero [32]byte
	zero[16], zero[17] = 0x00, 0x07
	if CheckDigits(zero) != "0007" {
		t.Fatalf("digits are zero padded: %s", CheckDigits(zero))
	}
}
