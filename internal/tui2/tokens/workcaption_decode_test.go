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

func TestWorkCaptionRotationIsStableAndDoesNotRepeatDeck(t *testing.T) {
	start := time.Unix(1000, 0)
	var w WorkActivity
	w.Start(start, WorkLogoRally)
	seen := map[string]bool{}
	for i := 0; i < len(w.captions); i++ {
		at := start.Add(time.Duration(i) * WorkCaptionPeriod)
		phrase := w.CaptionAt(at)
		if seen[phrase] {
			t.Fatal("phrase repeated before deck finished")
		}
		seen[phrase] = true
		if w.CaptionAt(at.Add(WorkCaptionPeriod-time.Nanosecond)) != phrase {
			t.Fatal("phrase changed before reading interval ended")
		}
		if w.CaptionAt(at) != phrase {
			t.Fatal("rendering mutated phrase")
		}
	}
	if len(seen) < 60 {
		t.Fatal("caption deck too small")
	}
	if w.CaptionAt(start.Add(time.Duration(len(w.captions))*WorkCaptionPeriod)) != w.Caption() {
		t.Fatal("deck does not loop")
	}
}
