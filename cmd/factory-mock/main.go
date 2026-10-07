// factory-mock is a HAND-DRIVEN MOCK of the factory surface: a terminal you can
// sit in front of, with a generated fleet of repos, a stream of arriving work,
// benches that run it, questions that surface, proof sheets that land, and a
// clock you can speed up or sleep through. Nothing here talks to GitHub, a
// model, or ~/.codeaf; it exists so the UX can be felt before it is built.
//
// It is its own main package for the reason cmd/codeaf-demo-home is: the
// shipped binary is on a byte budget and a mock must not spend it.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
)

func getenv(n string) string { return os.Getenv(n) }

func main() {
	seed := flag.Int64("seed", 7, "world seed; the same seed draws the same floor")
	repos := flag.Int("repos", 12, "connected repos (max 16)")
	issues := flag.Int("issues", 400, "open items generated across the fleet")
	benches := flag.Int("benches", 6, "streams that may run at once")
	running := flag.Int("running", 5, "streams already on the benches at start")
	speed := flag.Duration("speed", 30*time.Second, "sim time per 200ms tick")
	flag.Parse()

	start := time.Date(2026, 10, 7, 6, 40, 0, 0, time.Local)
	w := newWorld(*seed, *repos, *issues, *benches, start.Add(-9*time.Hour))
	w.Speed = *speed
	// Let the world run for a night so there is a shift to report on, with a
	// few streams launched at the start of it.
	n := 0
	for _, it := range w.Items {
		if n >= *running {
			break
		}
		if it.State == StNew && it.Tier != TierStranger && it.Kind != KindCI {
			// The night shift runs on banked habits: the first few self-ship,
			// the rest land for a sign-off in the morning.
			it.Order.Gate = GateShip
			if n < 2 {
				it.Order.Gate = GateNone
			}
			w.Launch(it)
			n++
		}
	}
	w.Sleep(9 * time.Hour)
	// A few fresh launches so the benches have something young on them too.
	n = 0
	for _, it := range w.Items {
		if n >= 2 {
			break
		}
		if it.State == StNew && it.Tier == TierOwner && it.Kind == KindIssue {
			w.Launch(it)
			n++
		}
	}

	p := tea.NewProgram(newApp(w))
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "factory-mock:", err)
		os.Exit(1)
	}
}
