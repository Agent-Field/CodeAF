package session

// mp4.go measures the two facts a landed render's note states — how long the
// clip runs and whether it carries a sound track — from the mp4's own boxes.
//
// The note used to say neither, on the emptiness law's ground that "nothing
// here decodes an mp4 and a guessed number is worse than none". Half of that
// was right: a guess is worse than nothing. But the number is not a guess —
// the file's movie header states its duration and its tracks name their kind,
// and both are a few dozen lines of box-walking away. The facts matter because
// the model plans its NEXT step from them: a stitch of twelve clips is timed
// from their lengths, and whether the joined cut needs its audio carried is
// known from whether the clips had any — a model that learned "silent" from
// the note does not ship a soundtrack bug it could only have found by ear.
//
// The emptiness law still governs the exit: bytes that do not parse as an mp4,
// a header with no timescale, a duration of zero, of a sentinel, or outside
// any length a render could honestly be — all answer ok=false, and the note
// simply omits the clause rather than carrying a number nobody measured.

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// The bounds a measured length must sit inside to be believed. A render is
// seconds to minutes; the ceiling is far above the provider's own ten-minute
// give-up (tools_video.go) and the floor far below any clip a person would be
// handed — a header outside them is lying (a saturated conversion, a 32-bit
// "unknown" sentinel read as a 64-bit number), and a lie is answered with the
// clause's absence, never with a 292-year clip in the note.
const (
	mp4LengthFloor   = 0.1    // seconds
	mp4LengthCeiling = 3600.0 // seconds
)

// mp4Facts answers the note's two questions about a finished render: length
// and sound. ok is false whenever the bytes cannot answer BOTH honestly — the
// sound answer is a claim about EVERY track, so a track list that is damaged,
// truncated or absent refuses the whole measurement rather than letting "no
// sound track seen" masquerade as "without sound". A note that said "without
// sound" about a clip whose audio track was simply unreadable would send the
// model to build a stitch that never maps audio — the exact bug the note
// exists to prevent.
func mp4Facts(data []byte) (length time.Duration, sound bool, ok bool) {
	moov, found := mp4FirstBox(data, "moov")
	if !found {
		return 0, false, false
	}
	mvhd, found := mp4FirstBox(moov, "mvhd")
	if !found {
		return 0, false, false
	}
	length, ok = mp4MovieLength(mvhd)
	if !ok {
		return 0, false, false
	}
	tracks := 0
	damaged := false
	clean := mp4EachBox(moov, "trak", func(trak []byte) bool {
		tracks++
		mdia, found := mp4FirstBox(trak, "mdia")
		if !found {
			damaged = true
			return false
		}
		hdlr, found := mp4FirstBox(mdia, "hdlr")
		if !found || len(hdlr) < 12 {
			damaged = true
			return false
		}
		// hdlr payload: version+flags (4), pre_defined (4), then the handler
		// type — "soun" for an audio track, "vide" for video. That is the ISO
		// layout the provider's files carry; a QuickTime .mov keeps its type
		// four bytes later, and would read as silent here — acceptable while
		// the only bytes this sees are the provider's own .mp4 renders. And it
		// is a DECLARED track, not a measure of audible samples: a track of
		// silence still answers "with sound".
		if string(hdlr[8:12]) == "soun" {
			sound = true
			return false
		}
		return true
	})
	if sound {
		return length, true, true
	}
	if damaged || !clean || tracks == 0 {
		return 0, false, false
	}
	return length, false, true
}

// mp4EachBox walks the boxes laid end to end in data and hands each payload of
// the named kind to visit, stopping early when visit answers false. It reports
// whether the walk was CLEAN — ended deliberately or consumed every byte —
// because a walk that died on malformed framing has not seen the boxes beyond
// the damage, and a caller asserting "none of the boxes is X" needs to know
// the difference between "none" and "none before the walk broke".
func mp4EachBox(data []byte, kind string, visit func(payload []byte) bool) (clean bool) {
	for len(data) >= 8 {
		size := uint64(binary.BigEndian.Uint32(data[:4]))
		name := string(data[4:8])
		header := uint64(8)
		switch size {
		case 0:
			// A size of zero means "to the end of the enclosing space".
			size = uint64(len(data))
		case 1:
			// A size of one means the real size follows as 64 bits.
			if len(data) < 16 {
				return false
			}
			size = binary.BigEndian.Uint64(data[8:16])
			header = 16
		}
		if size < header || size > uint64(len(data)) {
			return false
		}
		if name == kind && !visit(data[header:size]) {
			return true
		}
		data = data[size:]
	}
	return len(data) == 0
}

// mp4FirstBox is mp4EachBox stopped at the first hit — the shape every
// singleton lookup on the moov path wants. A walk that broke before the box
// simply answers not-found, which every caller already treats as the absence
// it is.
func mp4FirstBox(data []byte, kind string) (payload []byte, found bool) {
	mp4EachBox(data, kind, func(inner []byte) bool {
		payload, found = inner, true
		return false
	})
	return payload, found
}

// mp4MovieLength reads the movie header's duration. The layout forks on the
// header's version — version 1 widens the timestamps and the duration to 64
// bits — and both forks keep the timescale at 32. A zero timescale cannot
// divide, a zero duration is a file that claims to be nothing, and the
// all-ones duration is the spec's own "unknown" sentinel: all of them, and
// anything outside the believable bounds above, answer not-ok rather than a
// number the file did not honestly state. The bounds are checked on the float
// BEFORE the Duration conversion, because converting an out-of-range float is
// the step whose result differs by architecture.
func mp4MovieLength(mvhd []byte) (time.Duration, bool) {
	if len(mvhd) < 1 {
		return 0, false
	}
	var timescale, duration uint64
	switch mvhd[0] {
	case 0:
		if len(mvhd) < 20 {
			return 0, false
		}
		timescale = uint64(binary.BigEndian.Uint32(mvhd[12:16]))
		duration = uint64(binary.BigEndian.Uint32(mvhd[16:20]))
		if duration == math.MaxUint32 {
			return 0, false
		}
	case 1:
		if len(mvhd) < 32 {
			return 0, false
		}
		timescale = uint64(binary.BigEndian.Uint32(mvhd[20:24]))
		duration = binary.BigEndian.Uint64(mvhd[24:32])
		if duration == math.MaxUint64 {
			return 0, false
		}
	default:
		return 0, false
	}
	if timescale == 0 || duration == 0 {
		return 0, false
	}
	seconds := float64(duration) / float64(timescale)
	if seconds < mp4LengthFloor || seconds > mp4LengthCeiling {
		return 0, false
	}
	return time.Duration(seconds * float64(time.Second)), true
}

// mediaLength is how a measured clip length reads in a note: tenths of a
// second below a minute, where a render's whole life happens, and
// minutes-and-seconds above it, where tenths are noise. The branch is taken on
// the ROUNDED tenths, so 59.96s reads "1m00s" and never the "60.0s" the
// sub-minute rule forbids.
func mediaLength(length time.Duration) string {
	seconds := length.Seconds()
	tenths := math.Round(seconds*10) / 10
	if tenths < 60 {
		return fmt.Sprintf("%.1fs", tenths)
	}
	whole := int(math.Round(seconds))
	return fmt.Sprintf("%dm%02ds", whole/60, whole%60)
}
