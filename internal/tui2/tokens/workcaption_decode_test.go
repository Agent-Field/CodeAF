package tokens

import (
	"testing"
	"time"
)

func TestWorkCaptionDecodeReadabilityAndRest(t *testing.T) {
	for _, text := range []string{"Debugging reality", "Consulting rubber ducks", "é界", ""} {
		changed := false
		for elapsed := time.Duration(0); elapsed < 16*time.Second; elapsed += 20 * time.Millisecond {
			got := DecodeWorkCaption(text, elapsed)
			if len(got) != len(text) {
				t.Fatal("caption width changed")
			}
			differences := 0
			for i := range len(text) {
				if got[i] != text[i] {
					differences++
					if !captionLetter(text[i]) {
						t.Fatal("non-letter changed")
					}
				}
			}
			if differences > 1 {
				t.Fatal("more than one letter scrambled")
			}
			changed = changed || differences == 1
			if elapsed < 1800*time.Millisecond && got != text {
				t.Fatal("initial pause missing")
			}
		}
		if len(text) > 8 && !changed {
			t.Fatal("no decoding pass")
		}
		if DecodeWorkCaption(text, -time.Second) != text {
			t.Fatal("negative time scrambled")
		}
	}
	text := "Debugging reality"
	period := 1800*time.Millisecond + 16*240*time.Millisecond
	for _, elapsed := range []time.Duration{1960 * time.Millisecond, period, period + time.Second} {
		if DecodeWorkCaption(text, elapsed) != text {
			t.Fatal("resolved pause missing")
		}
	}
}
