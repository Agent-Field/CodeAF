package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strconv"
)

// The bounds of one captured video frame. They are the app's PNG helper's
// (4096 a side, 8 Mpx), plus a byte cap that keeps the frame inside one wire
// request: the app transport refuses a request over 4 MiB, and base64 grows the
// bytes by a third. The number is mirrored by appwire.MediaFrameMaxBytes and a
// test pins them equal.
const (
	MaxFrameBytes  = 3_000_000
	MaxFrameSide   = 4096
	MaxFramePixels = 8_000_000
	// MaxFrameAtMS is a year of milliseconds: a moment far outside it is a bug.
	MaxFrameAtMS int64 = 366 * 24 * 3600 * 1000
)

// ErrMediaFrame means the bytes are not a well-formed PNG within the frame bounds.
var ErrMediaFrame = errors.New("that frame is not a PNG this engine can keep")

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// CheckFramePNG validates a PNG without decoding a pixel: the signature, an
// IHDR that comes first with sane dimensions, every chunk's length and CRC, at
// least one IDAT, and an IEND that is the last byte. The size is bounded before
// the first byte is read, so a hostile caller costs one linear pass at most.
func CheckFramePNG(data []byte) (width, height int, err error) {
	if len(data) == 0 || len(data) > MaxFrameBytes {
		return 0, 0, fmt.Errorf("%w: %d bytes, and a frame is 1 to %d", ErrMediaFrame, len(data), MaxFrameBytes)
	}
	if !bytes.HasPrefix(data, pngSignature) {
		return 0, 0, ErrMediaFrame
	}
	pos := len(pngSignature)
	idat, ended, palette := false, false, false
	var colourType byte
	previous := ""
	for first := true; pos < len(data); first = false {
		if ended || len(data)-pos < 12 {
			return 0, 0, ErrMediaFrame
		}
		length := int(binary.BigEndian.Uint32(data[pos:]))
		if length < 0 || length > len(data)-pos-12 {
			return 0, 0, ErrMediaFrame
		}
		kind := data[pos+4 : pos+8]
		body := data[pos+8 : pos+8+length]
		if crc32.ChecksumIEEE(data[pos+4:pos+8+length]) != binary.BigEndian.Uint32(data[pos+8+length:]) {
			return 0, 0, ErrMediaFrame
		}
		switch string(kind) {
		case "IHDR":
			if !first || length != 13 {
				return 0, 0, ErrMediaFrame
			}
			w, h := binary.BigEndian.Uint32(body), binary.BigEndian.Uint32(body[4:])
			if w == 0 || h == 0 || w > MaxFrameSide || h > MaxFrameSide || uint64(w)*uint64(h) > MaxFramePixels {
				return 0, 0, fmt.Errorf("%w: %dx%d is outside %d a side and %d pixels", ErrMediaFrame, w, h, MaxFrameSide, MaxFramePixels)
			}
			depth, colour := body[8], body[9]
			legal := false
			switch colour {
			case 0:
				legal = depth == 1 || depth == 2 || depth == 4 || depth == 8 || depth == 16
			case 3:
				legal = depth == 1 || depth == 2 || depth == 4 || depth == 8
			case 2, 4, 6:
				legal = depth == 8 || depth == 16
			}
			if !legal || body[10] != 0 || body[11] != 0 || body[12] > 1 {
				return 0, 0, fmt.Errorf("%w: the header is not a legal PNG header", ErrMediaFrame)
			}
			width, height, colourType = int(w), int(h), colour
		case "PLTE":
			if first || idat || length == 0 || length%3 != 0 || length > 768 {
				return 0, 0, ErrMediaFrame
			}
			palette = true
		case "IDAT":
			// The pixel stream is one run of IDAT chunks, never interrupted.
			if first || (idat && previous != "IDAT") || (colourType == 3 && !palette) {
				return 0, 0, ErrMediaFrame
			}
			idat = true
		case "IEND":
			if first || length != 0 || !idat {
				return 0, 0, ErrMediaFrame
			}
			ended = true
		default:
			if first {
				return 0, 0, ErrMediaFrame
			}
		}
		previous = string(kind)
		pos += 12 + length
	}
	if !ended || width == 0 {
		return 0, 0, ErrMediaFrame
	}
	return width, height, nil
}

// SnapshotMediaFrame keeps one captured frame of an exact recorded video as an
// engine-owned image. The video is authority: it must be a recorded video, at
// the recorded version, still the file the engine recorded and inside the
// session's scope. The bytes are the caller's, so they are checked as a PNG and
// hashed here; NO PATH FROM THE CALLER IS EVER OPENED, which is why a direct
// client can supply a picture but never read a file. Admission and ownership
// belong to the caller; Remove on the result discards only this new file.
func SnapshotMediaFrame(ctx context.Context, row MediaRow, roots []string, version string, atMS int64, data []byte, destination string) (MediaReferenceCopy, error) {
	if atMS < 0 || atMS > MaxFrameAtMS {
		return MediaReferenceCopy{}, fmt.Errorf("%w: the moment is outside the clip", ErrMediaFrame)
	}
	if row.Fact.DurationMS > 0 && atMS > row.Fact.DurationMS {
		return MediaReferenceCopy{}, fmt.Errorf("%w: the moment is past the end of the clip", ErrMediaFrame)
	}
	if _, _, err := CheckFramePNG(data); err != nil {
		return MediaReferenceCopy{}, err
	}
	if err := ctx.Err(); err != nil {
		return MediaReferenceCopy{}, err
	}
	if !row.Recorded || row.Kind != "video" || version == "" || row.Version() != version {
		return MediaReferenceCopy{}, ErrMediaReference
	}
	if live := ResolveMedia(row, roots); live.State != MediaAvailable {
		return MediaReferenceCopy{}, ErrMediaReference
	}
	dir, err := os.MkdirTemp(destination, ".reference-")
	if err != nil {
		return MediaReferenceCopy{}, err
	}
	name := frameName(row.Fact.Canonical, atMS)
	copy := MediaReferenceCopy{Path: filepath.Join(dir, name), dir: dir, name: name}
	out, err := os.OpenFile(copy.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = os.Remove(dir)
		return MediaReferenceCopy{}, err
	}
	keep := false
	defer func() {
		_ = out.Close()
		if !keep {
			_ = copy.Remove()
		}
	}()
	// Remove needs the creation record, which a failed Stat cannot give; this file
	// and directory were just made by this call, so name them directly.
	if copy.owned, err = statCreated(out); err != nil {
		_ = out.Close()
		_ = os.Remove(copy.Path)
		_ = os.Remove(dir)
		keep = true // nothing is left for the deferred cleanup
		return MediaReferenceCopy{}, err
	}
	if _, err := out.Write(data); err != nil {
		return MediaReferenceCopy{}, err
	}
	if err := out.Close(); err != nil {
		return MediaReferenceCopy{}, err
	}
	// The clip must still be the recorded one after the write: a swap while the
	// frame was being kept would otherwise bless a frame of a different file.
	if live := ResolveMedia(row, roots); live.State != MediaAvailable {
		return MediaReferenceCopy{}, ErrMediaReference
	}
	if err := ctx.Err(); err != nil {
		return MediaReferenceCopy{}, err
	}
	sum := sha256.Sum256(data)
	copy.SHA256 = hex.EncodeToString(sum[:])
	keep = true
	return copy, nil
}

// frameName is the owned file's name: the clip's stem, made safe, and the moment.
func frameName(video string, atMS int64) string {
	stem := filepath.Base(video)
	stem = stem[:len(stem)-len(filepath.Ext(stem))]
	clean := make([]rune, 0, 80)
	for _, r := range stem {
		if len(clean) == 80 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			clean = append(clean, r)
		default:
			clean = append(clean, '_')
		}
	}
	if len(clean) == 0 {
		clean = []rune("frame")
	}
	return string(clean) + ".frame-" + strconv.FormatInt(atMS, 10) + ".png"
}
