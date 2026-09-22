package tokens

import (
	"math/rand/v2"
	"time"
)

// WorkLogoRandom asks a new activity to choose a study once at its boundary.
const WorkLogoRandom = -1

// Named studies let any caller select a particular motion without copying the
// geometry or depending on a menu's ordering.
const (
	WorkLogoRally = iota
	WorkLogoPingPong
	WorkLogoDribble
	WorkLogoSlingshot
	WorkLogoJuggle
	WorkLogoBackflip
	WorkLogoCradle
	WorkLogoRipple
	WorkLogoAccordion
	WorkLogoInfinity
)

// WorkActivity is reusable presentation state for one operation. It owns no
// timers, goroutines or terminal output. The owner starts it at an operation
// boundary, samples it from its existing clock, and decides when it is visible.
// Its zero value is dormant; callers can keep one per conversation or task.
type WorkActivity struct {
	style   int
	began   time.Time
	chosen  bool
	caption string
	recent  [8]string
	next    int
}

// Start selects once. Random selection avoids immediately repeating the previous
// choice; a named study is honored exactly. Invalid choices use random selection.
func (w *WorkActivity) Start(at time.Time, choice int) {
	if choice < 0 || choice >= WorkLogoCount {
		if w.chosen {
			choice = (w.style + 1 + rand.IntN(WorkLogoCount-1)) % WorkLogoCount
		} else {
			choice = rand.IntN(WorkLogoCount)
		}
	}
	w.style, w.began, w.chosen = choice, at, true
	w.caption = nextWorkCaption(w.recent[:])
	w.recent[w.next] = w.caption
	w.next = (w.next + 1) % len(w.recent)
}

// Started distinguishes a real operation from an uninitialized display value.
func (w WorkActivity) Started() bool { return w.chosen }

// Style reports the stable selected study for tests and callers that name it.
func (w WorkActivity) Style() int { return w.style }

// Frame uses the owner's clock and is read-only, including when called twice
// for layout and paint. Before Start it returns empty cells.
func (w WorkActivity) Frame(at time.Time) [WorkLogoWidth]WorkLogoCell {
	if !w.chosen {
		return [WorkLogoWidth]WorkLogoCell{}
	}
	return WorkLogo(w.style, at.Sub(w.began).Seconds())
}

// Caption is selected once alongside the motion; rendering never rewrites it.
func (w WorkActivity) Caption() string { return w.caption }

// Elapsed lets the owner's existing frame clock sample the caption's light.
func (w WorkActivity) Elapsed(at time.Time) time.Duration { return at.Sub(w.began) }
