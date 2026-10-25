package session

import (
	"encoding/json"
	"errors"
	"os"
	"time"
)

// writtenPaths retains offsets, not snapshots or file contents. One incremental
// scan builds the canonical-path index; subsequent lookups read only the tail
// and the latest rows of that path. It shares the leased tracker registry's
// lifetime, so idle indexes can be evicted without retaining historical homes.
// All access is under artifactsMu, including state transitions and appends.
type writtenPaths struct {
	dev, ino uint64
	scan     deliverScan
	paths    map[string]map[writtenIdentity]deliverUpdate
}

type writtenIdentity struct{ session, id string }

// writtenSyncBudget bounds one catch-up attempt, not the recording operation.
var writtenSyncBudget int64 = 64 << 20

func writtenIndex(index string, file *os.File, now mediaCursorBody) (*writtenPaths, func(), error) {
	var tracker *deliverTracker
	var release func()
	var err error
	for {
		tracker, release, err = leaseTracker(index, time.Time{})
		if !errors.Is(err, ErrMediaBusy) {
			break
		}
	}
	if err != nil {
		return nil, nil, err
	}
	p := tracker.written
	if p == nil || p.dev != now.Dev || p.ino != now.Ino || p.scan.pos > now.End {
		p = &writtenPaths{dev: now.Dev, ino: now.Ino, paths: make(map[string]map[writtenIdentity]deliverUpdate)}
		tracker.written = p
	}
	for {
		err = p.sync(file, now.End, writtenDeadline())
		if !errors.Is(err, ErrMediaBusy) {
			break
		}
	}
	if err != nil {
		release()
		return nil, nil, err
	}
	return p, func() {
		// An idle path index retains no partial row. A later append rereads at
		// most one bounded head; overlong-line progress remains intact.
		if p.scan.carry != nil {
			p.scan.pos, p.scan.carry = p.scan.line, nil
		}
		release()
	}, nil
}

// sync always advances at least one bounded chunk. Expiration retains that
// progress; its caller retries synchronously, so slow history delays recording
// instead of reporting a successful mutation with an absent deliverable.
func (p *writtenPaths) sync(file *os.File, end int64, deadline time.Time) error {
	budget := max(writtenSyncBudget, 1)
	for p.scan.pos < end {
		size := min(deliverSyncChunk, budget, end-p.scan.pos)
		buf := make([]byte, size)
		if _, err := file.ReadAt(buf, p.scan.pos); err != nil {
			return err
		}
		writtenRead(len(buf))
		p.scan.feedLines(buf, p.remember)
		budget -= size
		if p.scan.pos < end && (budget == 0 || time.Now().After(deadline)) {
			return ErrMediaBusy
		}
	}
	return nil
}

func (p *writtenPaths) remember(off int64, line []byte) {
	var artifact Artifact
	if json.Unmarshal(line, &artifact) != nil || artifact.Path == "" || artifact.Title == "" {
		return
	}
	row, ok := deliverableRowOf(artifact)
	if !ok {
		return
	}
	bucket := p.paths[artifact.Path]
	if bucket == nil {
		bucket = make(map[writtenIdentity]deliverUpdate)
		p.paths[artifact.Path] = bucket
	}
	bucket[writtenIdentity{artifact.Session, row.ID}] = deliverUpdate{off: off, n: int32(len(line))}
}

func readWrittenRow(file *os.File, at deliverUpdate) (DeliverableRow, error) {
	line := make([]byte, at.n)
	if _, err := file.ReadAt(line, at.off); err != nil {
		return DeliverableRow{}, err
	}
	writtenRead(len(line))
	var artifact Artifact
	if err := json.Unmarshal(line, &artifact); err != nil {
		return DeliverableRow{}, err
	}
	row, ok := deliverableRowOf(artifact)
	if !ok {
		return DeliverableRow{}, ErrMediaCursor
	}
	row.Start, row.Len = at.off, int(at.n)
	return row, nil
}

func (p *writtenPaths) latest(file *os.File, artifact Artifact) (Artifact, error) {
	var found Artifact
	var newest int64 = -1
	for key, at := range p.paths[artifact.Path] {
		if key.session != artifact.Session || at.off <= newest {
			continue
		}
		row, err := readWrittenRow(file, at)
		if err != nil {
			return Artifact{}, err
		}
		if sameWrittenSource(row.Fact, *artifact.Deliver) {
			found, newest = row.Artifact, at.off
			found.Deliver = &row.Fact
		}
	}
	return found, nil
}

// latestDeliverableForUpdate refreshes a caller's possibly stale snapshot at
// the same atomic seam as write upserts. State changes cannot restore old bytes.
func latestDeliverableForUpdate(index string, base DeliverableRow) (DeliverableRow, error) {
	file, now, err := openIndex(index)
	if err != nil {
		return DeliverableRow{}, err
	}
	defer file.Close()
	paths, release, err := writtenIndex(index, file, now)
	if err != nil {
		return DeliverableRow{}, err
	}
	defer release()
	at, ok := paths.paths[base.Artifact.Path][writtenIdentity{base.Artifact.Session, base.ID}]
	if !ok {
		return DeliverableRow{}, ErrMediaNotFound
	}
	return readWrittenRow(file, at)
}
