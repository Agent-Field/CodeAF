package session

// mediaheader.go reads two facts out of bytes the engine already holds, with no
// process and no file read: how long a WAV runs, and how large an mp4's picture
// is. Both are pure functions of a byte slice, bounded by its length, and both
// answer "not known" rather than a number the header did not honestly state
// (the emptiness law): a streaming WAV whose size field says "unknown", a
// header that claims more audio than the file holds, a rotated mp4 whose
// stored size is not the size anybody sees.
//
// The length of an mp4 is mp4.go's; this file adds only its frame size.

import (
	"encoding/binary"
	"math"
	"time"
)

// wavLength is the playing time of a PCM or float WAV, from its fmt chunk's
// byte rate and the data chunk's declared size. ok is false for anything else:
// not a RIFF/WAVE, a compressed encoding (its byte rate is an average, not a
// length), a data size of zero or the 32-bit "unknown" sentinel a streaming
// writer leaves, or a data chunk longer than the bytes in hand.
func wavLength(data []byte) (time.Duration, bool) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return 0, false
	}
	byteRate := uint32(0)
	pos := 12
	for pos+8 <= len(data) {
		name := string(data[pos : pos+4])
		size := uint64(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		body := pos + 8
		switch name {
		case "fmt ":
			if size < 16 || uint64(len(data)-body) < 16 {
				return 0, false
			}
			// 1 is integer PCM and 3 is IEEE float: the encodings whose byte rate
			// is exact. WAVE_FORMAT_EXTENSIBLE (0xFFFE) wraps one of them in a
			// sub-format this does not read, so it is not known.
			if format := binary.LittleEndian.Uint16(data[body : body+2]); format != 1 && format != 3 {
				return 0, false
			}
			byteRate = binary.LittleEndian.Uint32(data[body+8 : body+12])
		case "data":
			if byteRate == 0 || size == 0 || size == math.MaxUint32 || size > uint64(len(data)-body) {
				return 0, false
			}
			seconds := float64(size) / float64(byteRate)
			if seconds < mp4LengthFloor || seconds > 24*3600 {
				return 0, false
			}
			return time.Duration(seconds * float64(time.Second)), true
		}
		// Chunks are word-aligned.
		next := uint64(body) + size + size&1
		if next > uint64(len(data)) {
			return 0, false
		}
		pos = int(next)
	}
	return 0, false
}

// mp4FrameSize is the picture size of an mp4's first video track, in pixels,
// from the track header. It answers false when the track is rotated or
// sheared (the stored size is then not the displayed one), when no video
// track is declared, or when the box is too short to state a size.
func mp4FrameSize(data []byte) (width, height int, ok bool) {
	moov, found := mp4FirstBox(data, "moov")
	if !found {
		return 0, 0, false
	}
	mp4EachBox(moov, "trak", func(trak []byte) bool {
		mdia, found := mp4FirstBox(trak, "mdia")
		if !found {
			return true
		}
		hdlr, found := mp4FirstBox(mdia, "hdlr")
		if !found || len(hdlr) < 12 || string(hdlr[8:12]) != "vide" {
			return true
		}
		tkhd, found := mp4FirstBox(trak, "tkhd")
		if !found || len(tkhd) < 1 {
			return false
		}
		// The matrix starts 36 bytes before the two size words; version 1 widens
		// the timestamps and the duration, so its fixed part is 12 bytes longer.
		fixed := 84
		if tkhd[0] == 1 {
			fixed = 96
		}
		if tkhd[0] > 1 || len(tkhd) < fixed {
			return false
		}
		matrix := tkhd[fixed-44 : fixed-8]
		unit := uint32(0x10000)
		if binary.BigEndian.Uint32(matrix[0:4]) != unit || binary.BigEndian.Uint32(matrix[16:20]) != unit ||
			binary.BigEndian.Uint32(matrix[4:8]) != 0 || binary.BigEndian.Uint32(matrix[12:16]) != 0 ||
			binary.BigEndian.Uint32(matrix[8:12]) != 0 || binary.BigEndian.Uint32(matrix[20:24]) != 0 || binary.BigEndian.Uint32(matrix[32:36]) != 1<<30 {
			return false
		}
		w := int(binary.BigEndian.Uint32(tkhd[fixed-8:fixed-4]) >> 16)
		h := int(binary.BigEndian.Uint32(tkhd[fixed-4:fixed]) >> 16)
		if w > 0 && h > 0 {
			width, height, ok = w, h, true
		}
		return false
	})
	return width, height, ok
}
