package money

import "testing"

func TestFormatReadsTheWayAccountantsWrite(t *testing.T) {
	for cents, want := range map[int64]string{
		0:          "0.00",
		5:          "0.05",
		99999:      "999.99",
		123456:     "1,234.56",
		100000000:  "1,000,000.00",
		-5:         "(0.05)",
		-123456:    "(1,234.56)",
		-100000000: "(1,000,000.00)",
	} {
		if got := Format(cents); got != want {
			t.Errorf("Format(%d) = %q, want %q", cents, got, want)
		}
	}
}
