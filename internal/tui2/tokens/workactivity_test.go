package tokens

import (
	"math"
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
	for style := 0; style < WorkLogoCount; style++ {
		for frame := 0; frame < 84; frame++ {
			seconds := float64(frame) / 30
			picture := WorkLogo(style, seconds)
			for _, line := range picture {
				for _, cell := range line {
					for _, half := range []WorkLogoInk{cell.Top, cell.Bottom} {
						if math.IsNaN(half.Ink) || math.IsNaN(half.Gold) || half.Ink < 0 || half.Gold < 0 || half.Ink+half.Gold > 1.000001 {
							t.Fatal("invalid raster coverage")
						}
					}
				}
			}
			for _, p := range motionScene(seconds, style).items {
				if p.kind == 0 && p.alpha > .5 && (p.a.x-p.rx < 15 || p.a.x+p.rx > 85 || p.a.y-p.ry < 20 || p.a.y+p.ry > 80) {
					t.Fatalf("study %d clips its ball", style)
				}
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
