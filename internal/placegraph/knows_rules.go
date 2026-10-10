package placegraph

import (
	"strings"
	"time"
	"unicode"
)

// The Knows rules are pure functions over Line values. They decide nothing on
// their own schedule and touch no store: a caller hands in lines and a clock and
// gets back what should change, so every rule can be proved with a fixed time.

const (
	// StrikeAfter is how long a replaced line stays visible, struck through,
	// before it is purged.
	StrikeAfter = 7 * 24 * time.Hour
	// StillTrueAfter is how long a line may go unused before the place asks
	// whether it is still true.
	StillTrueAfter = 60 * 24 * time.Hour
)

// AskOnce is returned instead of a replacement when two lines came from the
// person on the same day: neither is plainly newer, so the person is asked once
// which one holds.
type AskOnce struct {
	A Line `json:"a"`
	B Line `json:"b"`
}

// fromPerson reports whether the person, rather than the agent or a file, is the
// origin of the line.
func fromPerson(l Line) bool {
	return !l.EditedAt.IsZero() || l.Source.Kind == LineYouWrote || l.Source.Kind == LineSaidInChat
}

// learnedAt is when a line came to be known, preferring its own timestamp over
// the source's.
func learnedAt(l Line) time.Time {
	if !l.EditedAt.IsZero() {
		return l.EditedAt
	}
	if !l.CreatedAt.IsZero() {
		return l.CreatedAt
	}
	return l.Source.At
}

func sameDay(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return false
	}
	ay, am, ad := a.Date()
	by, bm, bd := b.In(a.Location()).Date()
	return ay == by && am == bm && ad == bd
}

// Supersede applies "the newer line wins". It returns the older line marked
// replacedBy and replacedAt. When both lines are the person's own words from the
// same day, it returns an AskOnce and leaves the older line untouched, because
// guessing which of two same-day statements the person meant would be inventing.
func Supersede(older, newer Line, now time.Time) (Line, *AskOnce) {
	if older.ID == newer.ID || older.ReplacedBy != "" {
		return older, nil
	}
	if fromPerson(older) && fromPerson(newer) && sameDay(learnedAt(older), learnedAt(newer)) {
		return older, &AskOnce{A: older, B: newer}
	}
	older.ReplacedBy = newer.ID
	older.ReplacedAt = now
	return older, nil
}

// Purge drops lines whose strike-through has lasted StrikeAfter. A replaced line
// still named by a surviving line's ReplacedBy is kept so the chain stays valid.
func Purge(lines []Line, now time.Time) []Line {
	expired := func(l Line) bool {
		return l.ReplacedBy != "" && !now.Before(l.ReplacedAt.Add(StrikeAfter))
	}
	kept := append([]Line(nil), lines...)
	for {
		referenced := map[string]bool{}
		for _, l := range kept {
			if l.ReplacedBy != "" && !expired(l) {
				referenced[l.ReplacedBy] = true
			}
		}
		next := kept[:0:0]
		for _, l := range kept {
			if expired(l) && !referenced[l.ID] {
				continue
			}
			next = append(next, l)
		}
		if len(next) == len(kept) {
			return next
		}
		kept = next
	}
}

// StillTrueDue lists the live lines unused for StillTrueAfter that have not been
// asked about within that same window, in stored order.
func StillTrueDue(lines []Line, now time.Time) []Line {
	var due []Line
	for _, l := range lines {
		if l.ReplacedBy != "" {
			continue
		}
		last := l.LastUsedAt
		if last.IsZero() {
			last = learnedAt(l)
		}
		if last.IsZero() || now.Sub(last) < StillTrueAfter {
			continue
		}
		if !l.AskedStillTrueAt.IsZero() && now.Sub(l.AskedStillTrueAt) < StillTrueAfter {
			continue
		}
		due = append(due, l)
	}
	return due
}

// normalizeLineText folds case, whitespace and edge punctuation so that
// "Use tabs." and "use  tabs" count as one fact.
func normalizeLineText(s string) string {
	f := strings.FieldsFunc(strings.ToLower(s), unicode.IsSpace)
	return strings.TrimFunc(strings.Join(f, " "), func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSpace(r)
	})
}

// MergeDuplicates folds live near-duplicate lines of one place into the older
// one, which keeps its source and id; it also inherits the later last-use time so
// merging never makes a used fact look stale. It returns the surviving lines in
// stored order and the ids that were folded away.
func MergeDuplicates(lines []Line) (merged []Line, removed []string) {
	winner := map[string]int{} // place + normalised text -> index in merged
	for _, l := range lines {
		key := normalizeLineText(l.Text)
		if l.ReplacedBy != "" || key == "" {
			merged = append(merged, l)
			continue
		}
		key = l.PlaceID + "\x00" + key
		i, seen := winner[key]
		if !seen {
			winner[key] = len(merged)
			merged = append(merged, l)
			continue
		}
		keep, drop := merged[i], l
		if a, b := learnedAt(drop), learnedAt(keep); !a.IsZero() && (b.IsZero() || a.Before(b)) {
			keep, drop = drop, keep
		}
		if drop.LastUsedAt.After(keep.LastUsedAt) {
			keep.LastUsedAt = drop.LastUsedAt
		}
		merged[i] = keep
		removed = append(removed, drop.ID)
	}
	return merged, removed
}
