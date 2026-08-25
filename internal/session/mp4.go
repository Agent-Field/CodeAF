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
// a header with no timescale, a duration of zero or of the unknown sentinel —
// all answer ok=false, and the note simply omits the clause rather than
// carrying a number nobody measured.

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// mp4Facts answers the note's two questions about a finished render: length
// and sound. ok is false whenever the bytes cannot answer BOTH honestly — a
// clip whose duration is readable but whose track list is truncated could
// state a length, but a note that says nothing about sound reads as "no sound"
// to a model that has seen the full sentence, so half an answer is no answer.
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
	mp4EachBox(moov, "trak", func(trak []byte) bool {
		if mdia, found := mp4FirstBox(trak, "mdia"); found {
			if hdlr, found := mp4FirstBox(mdia, "hdlr"); found {
				// hdlr payload: version+flags (4), pre_defined (4), then the
				// handler type — "soun" for an audio track, "vide" for video.
				if len(hdlr) >= 12 && string(hdlr[8:12]) == "soun" {
					sound = true
					return false
				}
			}
		}
		return true
	})
	return length, sound, true
}

// mp4EachBox walks the boxes laid end to end in data and hands each payload of
// the named kind to visit, stopping early when visit answers false. Malformed
// framing — a size smaller than its own header, or larger than what remains —
// ends the walk silently: the caller's ok-paths already treat "not found" and
// "not parseable" as the same absence.
func mp4EachBox(data []byte, kind string, visit func(payload []byte) bool) {
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
				return
			}
			size = binary.BigEndian.Uint64(data[8:16])
			header = 16
		}
		if size < header || size > uint64(len(data)) {
			return
		}
		if name == kind && !visit(data[header:size]) {
			return
		}
		data = data[size:]
	}
}

// mp4FirstBox is mp4EachBox stopped at the first hit — the shape every
// singleton lookup on the moov path wants.
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
// all-ones duration is the spec's own "unknown" sentinel: all three answer
// not-ok rather than a number the file did not actually state.
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
	return time.Duration(float64(duration) / float64(timescale) * float64(time.Second)), true
}

// mediaLength is how a measured clip length reads in a note: tenths of a
// second below a minute, where a render's whole life happens, and
// minutes-and-seconds above it, where tenths are noise.
func mediaLength(length time.Duration) string {
	seconds := length.Seconds()
	if seconds < 60 {
		return fmt.Sprintf("%.1fs", seconds)
	}
	whole := int(seconds + 0.5)
	return fmt.Sprintf("%dm%02ds", whole/60, whole%60)
}
