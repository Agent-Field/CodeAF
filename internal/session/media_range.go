package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
)

// The bounds of a recorded audio or video range snapshot.
const (
	// MaxMediaRangeSourceBytes is the largest recorded file a range snapshot
	// copies. The whole file is kept byte for byte, so this is the disk one staged
	// range may hold; ten may be staged for a conversation. It is checked from the
	// open descriptor before the first byte is copied.
	MaxMediaRangeSourceBytes = 32 << 20
	// MaxMediaRangeMS is a year of milliseconds: a time far outside it is a bug.
	MaxMediaRangeMS int64 = 366 * 24 * 3600 * 1000
)

// ErrMediaRange means a range cannot be kept: the span is not inside the source's
// recorded length, or that length was never recorded.
var ErrMediaRange = errors.New("that range cannot be kept")

// SnapshotMediaRange keeps an exact copy of a recorded audio or video as an
// engine-owned file whose name carries the span. The source is authority: it
// must be a recorded audio or video at the recorded version, still the file the
// engine recorded, inside the session's scope, and its RECORDED duration must
// be known and contain the span. Nothing is probed or guessed. The bytes are
// copied whole and unchanged: the range is provenance for the message, never a
// cut, a render or model input. NO PATH FROM THE CALLER IS EVER OPENED.
// Admission and ownership belong to the caller; Remove on the result discards
// only the new file.
func SnapshotMediaRange(ctx context.Context, row MediaRow, roots []string, version string, fromMS, toMS int64, destination string) (MediaReferenceCopy, error) {
	if fromMS < 0 || toMS <= fromMS || toMS > MaxMediaRangeMS {
		return MediaReferenceCopy{}, fmt.Errorf("%w: a range is 0 <= from < to, in milliseconds", ErrMediaRange)
	}
	if !row.Recorded || (row.Kind != "audio" && row.Kind != "video") {
		return MediaReferenceCopy{}, fmt.Errorf("%w: only a recorded audio or video has a range", ErrMediaReference)
	}
	if row.Fact.DurationMS <= 0 {
		return MediaReferenceCopy{}, fmt.Errorf("%w: its length was not recorded, so a range of it cannot be checked", ErrMediaRange)
	}
	if toMS > row.Fact.DurationMS {
		return MediaReferenceCopy{}, fmt.Errorf("%w: the range ends past the %d ms recorded", ErrMediaRange, row.Fact.DurationMS)
	}
	if version == "" || row.Version() != version {
		return MediaReferenceCopy{}, ErrMediaReference
	}
	live := ResolveMedia(row, roots)
	if live.State != MediaAvailable {
		return MediaReferenceCopy{}, ErrMediaReference
	}
	file, before, err := openLiveMedia(ctx, live, MaxMediaRangeSourceBytes,
		func(head []byte) bool { return mediaKindOf(sniffMedia(head)) == row.Kind },
		func() error {
			return fmt.Errorf("%w: the recording is outside 1 byte to %s", ErrMediaRange, byteLimit(MaxMediaRangeSourceBytes))
		})
	if err != nil {
		return MediaReferenceCopy{}, err
	}
	defer file.Close()
	return streamOwnedCopy(ctx, row, roots, file, before, live, destination, rangeName(row.Fact.Canonical, fromMS, toMS))
}

// rangeName is the owned file's name: the recording's stem made safe, the span,
// and the original extension, so the words that name the file name the span.
func rangeName(source string, fromMS, toMS int64) string {
	ext := filepath.Ext(source)
	if len(ext) > 12 || !safeExtension(ext) {
		ext = ""
	}
	stem := frameName(source, 0)
	stem = stem[:len(stem)-len(".frame-0.png")]
	return stem + ".range-" + strconv.FormatInt(fromMS, 10) + "-" + strconv.FormatInt(toMS, 10) + ext
}

func safeExtension(ext string) bool {
	for _, r := range ext[min(1, len(ext)):] {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}
