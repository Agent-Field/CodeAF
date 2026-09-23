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
	style    int
	began    time.Time
	chosen   bool
	caption  string
	captions []string
	recent   [8]string
	next     int
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
	w.captions = []string{w.caption}
	for _, recipe := range workCaptionRecipes {
		for _, object := range recipe.objects {
			phrase := recipe.action + " " + object
			if phrase != w.caption {
				w.captions = append(w.captions, phrase)
			}
		}
	}
	rand.Shuffle(len(w.captions)-1, func(i, j int) { w.captions[i+1], w.captions[j+1] = w.captions[j+1], w.captions[i+1] })
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

// WorkCaptionPeriod gives each phrase a full decoding pass and reading time.
const WorkCaptionPeriod = 10 * time.Second

// CaptionAt samples a deck shuffled once at Start. Every phrase appears before
// the deck repeats, with no random work or mutation during layout and paint.
func (w WorkActivity) CaptionAt(at time.Time) string {
	if len(w.captions) == 0 || w.Elapsed(at) < 0 {
		return w.caption
	}
	return w.captions[int(w.Elapsed(at)/WorkCaptionPeriod)%len(w.captions)]
}

// Elapsed lets the owner's existing frame clock sample the caption motion.
func (w WorkActivity) Elapsed(at time.Time) time.Duration { return at.Sub(w.began) }
