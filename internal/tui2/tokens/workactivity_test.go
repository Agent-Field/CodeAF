package tokens

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"
)

func TestWorkActivitySelectionAndClock(t *testing.T) {
	at := time.Unix(1234, 0)
	var activity WorkActivity
	if activity.Started() {
		t.Fatal("zero activity claims work")
	}
	for style := 0; style < WorkLogoCount; style++ {
		activity.Start(at, style)
		if activity.Style() != style {
			t.Fatal("explicit selection ignored")
		}
		first := activity.Frame(at)
		later := activity.Frame(at.Add(700 * time.Millisecond))
		if first == later {
			t.Fatalf("study %d does not move", style)
		}
		if first != activity.Frame(at) {
			t.Fatal("rendering changed the selection")
		}
		if first != activity.Frame(at.Add(2800*time.Millisecond)) {
			t.Fatalf("study %d jumps at the loop seam", style)
		}
	}
	for range 100 {
		last := activity.Style()
		activity.Start(at, WorkLogoRandom)
		if activity.Style() == last {
			t.Fatal("random selection repeated immediately")
		}
	}
}

func TestWorkLogoCoverageAndBounds(t *testing.T) {
	for style, frames := range workMotions {
		for _, frame := range frames {
			if ansi.StringWidth(frame) > WorkLogoWidth || strings.ContainsAny(frame, "\n\r\x1b") {
				t.Fatalf("study %d is not a single line: %q", style, frame)
			}
		}
		for i := 0; i < 84; i++ {
			var text strings.Builder
			for _, cell := range WorkLogo(style, float64(i)/30) {
				text.WriteRune(cell.Glyph)
			}
			if ansi.StringWidth(text.String()) != WorkLogoWidth {
				t.Fatal("animation moves its label")
			}
		}
	}
}

func BenchmarkWorkActivityFrame(b *testing.B) {
	var a WorkActivity
	a.Start(time.Unix(0, 0), WorkLogoInfinity)
	for i := 0; i < b.N; i++ {
		_ = a.Frame(time.Unix(0, int64(i)*33000000))
	}
}
