package session

// Resolve a reference only to a source already recorded for its owner. The
// supplied path is a search key, never a filesystem capability or a new fact.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/skills"
)

const MediaReferencePathMax = 4096
const mediaReferenceOwnerMax = 128
const mediaReferenceTime = 8 * time.Second

var ErrMediaReferencePath = errors.New("media reference needs an absolute canonical path of at most 4096 bytes")

func ValidMediaReferencePath(path string) bool {
	return path != "" && len(path) <= MediaReferencePathMax && !strings.ContainsRune(path, 0) && filepath.IsAbs(path) && filepath.Clean(path) == path
}

// FindMediaByPath searches newest first, across the complete recorded index.
// Partial searches are Busy, not absence. It shares the existing global lookup
// slots/cache and starts no goroutine; concurrent identical requests share work.
func FindMediaByPath(ctx context.Context, index, owner, path string, deadline time.Time) (found MediaRow, err error) {
	if !ValidMediaReferencePath(path) {
		return MediaRow{}, ErrMediaReferencePath
	}
	if index == "" || len(index) > MediaReferencePathMax || owner == "" || len(owner) > mediaReferenceOwnerMax {
		return MediaRow{}, ErrMediaReferencePath
	}
	if limit := time.Now().Add(mediaReferenceTime); deadline.IsZero() || deadline.After(limit) {
		deadline = limit
	}
	if at, ok := ctx.Deadline(); ok && at.Before(deadline) {
		deadline = at
	}
	if ctx.Err() != nil || !time.Now().Before(deadline) {
		return MediaRow{}, ErrMediaBusy
	}
	// The prefix cannot collide with a valid source ID. Clone the bounded key so
	// the shared flight cannot retain a much larger request backing allocation.
	key := mediaCacheKey{index: strings.Clone(index), session: strings.Clone(owner), id: "resolve:" + path}
	flight, run, err := joinMediaFlight(key)
	if err != nil {
		return MediaRow{}, err
	}
	if !run {
		return waitMediaReference(ctx, flight, deadline)
	}
	index, owner, path = key.index, key.session, strings.TrimPrefix(key.id, "resolve:")
	defer func() {
		// A recovered outer panic must not leave waiters or a lookup slot stranded.
		if found.Fact.ID == "" && err == nil {
			err = ErrMediaBusy
		}
		finishMediaFlight(key, flight, cloneMediaRow(found), err)
	}()
	// The shared regular-file opener refuses FIFOs/devices before open and checks
	// the resulting descriptor too; an index replacement must not park a reader.
	file, err := skills.OpenRegular(index)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return MediaRow{}, ErrMediaNotFound
		}
		return MediaRow{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return MediaRow{}, err
	}
	_, dev, ino := fileIdentity(info)
	needle := mediaNeedle(owner)
	end := info.Size()
	mediaIndexScans.Add(1)
	active := mediaScansActive.Add(1)
	defer mediaScansActive.Add(-1)
	for peak := mediaScansPeak.Load(); active > peak && !mediaScansPeak.CompareAndSwap(peak, active); peak = mediaScansPeak.Load() {
	}
	for end > 0 {
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			return MediaRow{}, ErrMediaBusy
		}
		hit := false
		boundary, scanErr := scanBackward(file, end, mediaScanBudget, deadline, func(start int64, line []byte) bool {
			if ctx.Err() != nil {
				return false
			}
			if !bytes.Contains(line, needle) {
				return true
			}
			var artifact Artifact
			if json.Unmarshal(line, &artifact) != nil || artifact.Session != owner || strings.TrimSpace(artifact.Path) == "" || strings.TrimSpace(artifact.Title) == "" {
				return true
			}
			row, ok := mediaRowOf(artifact)
			if !ok {
				return true
			}
			row.Start, row.Len = start, len(line)
			cacheMediaRow(index, mediaCacheKey{dev: dev, ino: ino}, row, line)
			if row.Fact.Canonical == path {
				found, hit = row, true
				return false
			}
			return true
		})
		if scanErr != nil {
			return MediaRow{}, scanErr
		}
		if ctx.Err() != nil {
			return MediaRow{}, ErrMediaBusy
		}
		if hit {
			return cloneMediaRow(found), nil
		}
		if boundary == 0 {
			return MediaRow{}, ErrMediaNotFound
		}
		if boundary >= end || !time.Now().Before(deadline) {
			return MediaRow{}, ErrMediaBusy
		}
		end = boundary
	}
	return MediaRow{}, ErrMediaNotFound
}

func waitMediaReference(ctx context.Context, flight *mediaFlight, deadline time.Time) (MediaRow, error) {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return MediaRow{}, ErrMediaBusy
	case <-timer.C:
		return MediaRow{}, ErrMediaBusy
	case <-flight.done:
		if ctx.Err() != nil {
			return MediaRow{}, ErrMediaBusy
		}
		if flight.err != nil {
			return MediaRow{}, flight.err
		}
		return cloneMediaRow(flight.row), nil
	}
}
