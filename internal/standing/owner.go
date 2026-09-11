package standing

// owner.go is who keeps each REPORT PATH: one live item, and only one.
//
// ── ONE LIVE OWNER PER REPORT PATH (the ruling on the review of 417fa43a3) ──
//
// R2 made a report's receipt the path's, so a successor publishes where its
// stopped predecessor did ([Store.AtReport]). It also made two LIVE items at
// one path take turns: each found the file matching the path's receipt — the
// other's — and replaced it on every run, for ever. So "aforge last published
// here" clears a publication only when this item, or an item that has been
// stopped, earned the receipt; and two live items on one path is a state the
// store refuses at the write, at both doors ([Store.Create], [Store.Revise]),
// under the path's own lock — not only where the chat draws its card.
//
// ── THE OWNER IS KEPT BY PATH, SO ONE WRITE READS ONE RECORD (L6) ──
//
// The chat's write guard asks "is this file the report of something that
// stands?" on every write, and it used to answer by reading every item on the
// machine. The owner record is kept beside the path's receipt, under the same
// hash, so the answer is two reads — the record and its item — however many
// items there are.
//
// ── THE LAWS IT IS WRITTEN UNDER ──
//
//   - FENCED (L1). An owner record is read, and a claim decided and written,
//     only under the path's lock, the one the receipt is kept under; the item
//     document is written inside it. A path's lock is taken before an item's.
//   - ONE DURABLE WRITE (L4). A record is replaced whole through [writeAtomic].
//     The claim is written BEFORE the item it names and put back if that write
//     fails, so no crash leaves live work at a path with no owner on record.
//   - STAMPED AND ADDITIVE (L5). Every record carries [OwnerSchema]; one from a
//     newer build is not trusted to clear anything.
//   - FANNED OUT (L7). reports/<two hex>/<sha256 of the path>.owner.json, the
//     receipts' own layout.
//   - RETENTION DECLARED (L9). One record per report path that has a live
//     owner. It is removed when that owner is stopped or moves its report
//     elsewhere; a stopped owner's record left behind by a crash is ignored,
//     because an owner is only an owner while its item is not stopped.
//   - ONE-TIME COST DECLARED. The first store that meets this file reads every
//     item ONCE ([Store.ensureOwners]) to record the owners of the paths that
//     already had live items, and to move a stopped predecessor's receipt to its
//     path, so its successor publishes where it did. A marker then makes every
//     later call one stat.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// OwnerSchema is the shape of a report path's owner record.
const OwnerSchema = 1

// ReportOwner is the live item that keeps one report path.
type ReportOwner struct {
	Schema int       `json:"schema"`
	Path   string    `json:"path"`
	Item   string    `json:"item"`
	At     time.Time `json:"at"`
}

// ErrReportOwned is a report path another live item already keeps.
var ErrReportOwned = errors.New("standing: the report path has a live owner")

// ReportOwnedError names who keeps the path, in the words both doors print.
type ReportOwnedError struct {
	Report string
	Owner  Item
}

func (e *ReportOwnedError) Error() string {
	return e.Report + " is already the report of " + strconv.Quote(e.Owner.Words) + " (" + e.Owner.ID +
		"), which has not been stopped — two orders cannot keep one file: edit that one, or stop it first"
}

// Is makes every ReportOwnedError an [ErrReportOwned].
func (e *ReportOwnedError) Is(target error) bool { return target == ErrReportOwned }

// ReportPath is item's report as the one path every record about it is kept
// under, or "" for an item that keeps no report.
func ReportPath(item Item) string {
	if strings.TrimSpace(item.Does.Report) == "" || strings.TrimSpace(item.Workspace) == "" {
		return ""
	}
	return CanonicalPath(filepath.Join(filepath.Clean(item.Workspace), filepath.Clean(item.Does.Report)))
}

// CanonicalPath is path with its folder resolved through the filesystem as it
// is now — the deepest part of it that exists — so one file reached by two
// spellings, through a link or not, is one key. The file itself is not
// followed: a report that is a link is refused where it is written.
func CanonicalPath(path string) string {
	path = filepath.Clean(path)
	dir, existing := filepath.Dir(path), filepath.Dir(path)
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		existing = parent
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return path
	}
	rest, err := filepath.Rel(existing, dir)
	if err != nil {
		return path
	}
	return filepath.Join(real, rest, filepath.Base(path))
}

// pathRecord is where a record about path lives, by the kind of record: its
// receipt (.json), its lock (.lock) and its owner (.owner.json).
func (s *Store) pathRecord(path, kind string) string {
	sum := sha256.Sum256([]byte(CanonicalPath(path)))
	name := hex.EncodeToString(sum[:])
	return filepath.Join(s.root, receiptsDir, name[:2], name+kind)
}

// underPathLock holds path's lock for the length of act.
func (s *Store) underPathLock(path string, act func() error) error {
	lock := s.pathRecord(path, ".lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o700); err != nil {
		return err
	}
	return underLock(lock, act)
}

// readOwner reads path's owner record, or nil when it has none.
func (s *Store) readOwner(path string) (*ReportOwner, error) {
	raw, err := os.ReadFile(s.pathRecord(path, ".owner.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var owner ReportOwner
	if err := json.Unmarshal(raw, &owner); err != nil {
		return nil, fmt.Errorf("standing: the owner record for %s is unreadable: %w", path, err)
	}
	if owner.Schema > OwnerSchema {
		return nil, fmt.Errorf("standing: the owner record for %s was written by a newer aforge", path)
	}
	return &owner, nil
}

// keepOwner records id as path's owner, or removes the record for "".
func (s *Store) keepOwner(path, id string) error {
	file := s.pathRecord(path, ".owner.json")
	if id == "" {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := json.MarshalIndent(ReportOwner{Schema: OwnerSchema, Path: filepath.Clean(path), Item: id, At: s.now()}, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(file, append(data, '\n'))
}

// liveOwner is the item other than except that keeps path while it is not
// stopped: the one on its owner record, else the one that earned last, the
// path's receipt. An item that cannot be read is taken as live, since whether
// it was stopped is unknown; one that is not there is not.
func (s *Store) liveOwner(path, except string, last *Receipt) (Item, bool) {
	var ids []string
	if owner, err := s.readOwner(path); err == nil && owner != nil {
		ids = append(ids, owner.Item)
	}
	if last != nil {
		ids = append(ids, last.Item)
	}
	for _, id := range ids {
		if id == "" || id == except {
			continue
		}
		item, err := s.Get(id)
		switch {
		case errors.Is(err, ErrNotFound):
			continue
		case err != nil:
			return Item{ID: id}, true
		case item.Status != StatusRetired:
			return item, true
		}
	}
	return Item{}, false
}

// LiveOwner is the item that keeps the report file at path while it is not
// stopped. It reads one record and one item, however many items there are.
func (s *Store) LiveOwner(path string) (Item, bool) {
	s.ensureOwners()
	return s.liveOwner(path, "", nil)
}

// OtherLiveOwner is [Store.LiveOwner] for a publication by item id, asked
// inside [Store.AtReport] with the receipt it read there: a receipt earned by
// another item that has not been stopped does not clear this one.
func (s *Store) OtherLiveOwner(path, id string, last *Receipt) (Item, bool) {
	return s.liveOwner(path, id, last)
}

// claimReport writes item through write while no other live item keeps the
// report path it will have, and records it as that path's owner. A claim
// refused writes nothing. An item that keeps no report is written as it is.
func (s *Store) claimReport(item Item, write func() error) error {
	path := ReportPath(item)
	if path == "" {
		return write()
	}
	s.ensureOwners()
	return s.underPathLock(path, func() error {
		last, _ := s.Receipt(path)
		if owner, live := s.liveOwner(path, item.ID, last); live {
			return &ReportOwnedError{Report: item.Does.Report, Owner: owner}
		}
		before, err := s.readOwner(path)
		if err != nil {
			return err
		}
		if err := s.keepOwner(path, item.ID); err != nil {
			return err
		}
		if err := write(); err != nil {
			previous := ""
			if before != nil {
				previous = before.Item
			}
			_ = s.keepOwner(path, previous)
			return err
		}
		return nil
	})
}

// releaseReport removes item's claim on path when it is still item's: it was
// stopped, or its report moved elsewhere.
func (s *Store) releaseReport(path, id string) {
	if path == "" {
		return
	}
	_ = s.underPathLock(path, func() error {
		owner, err := s.readOwner(path)
		if err != nil || owner == nil || owner.Item != id {
			return nil
		}
		return s.keepOwner(path, "")
	})
}

// ownersMarker says this store's owners were recorded once from its items.
const ownersMarker = "owners.v1"

// ensureOwners records, once per store, the owners of the report paths items
// already kept before owners were recorded, and moves a predecessor's
// receipt to its path. It is called before any path's lock is taken, never
// under one. Every call after the first is one stat.
func (s *Store) ensureOwners() {
	marker := filepath.Join(s.root, receiptsDir, ownersMarker)
	if _, err := os.Stat(marker); err == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
		return
	}
	_ = underLock(marker+".lock", func() error {
		if _, err := os.Stat(marker); err == nil {
			return nil
		}
		items, err := s.List()
		if err != nil {
			return err
		}
		keepers := map[string][]Item{}
		for at := len(items) - 1; at >= 0; at-- { // oldest first
			if path := ReportPath(items[at]); path != "" {
				keepers[path] = append(keepers[path], items[at])
			}
		}
		for path, holders := range keepers {
			if err := s.recordOwner(path, holders); err != nil {
				return err
			}
		}
		return writeAtomic(marker, []byte("1\n"))
	})
}

// recordOwner is one path's share of [Store.ensureOwners]: the newest receipt
// any of its items kept before paths kept their own becomes the path's, and
// the owner is the live item that earned the receipt, else the live one that
// stood first. A second live item at the path is left to be held when it
// publishes ([Store.OtherLiveOwner]).
func (s *Store) recordOwner(path string, holders []Item) error {
	return s.underPathLock(path, func() error {
		last, err := s.Receipt(path)
		if err != nil {
			return err
		}
		if last == nil {
			for _, item := range holders {
				found, err := s.legacyPublication(item.ID, item.Does.Report, path)
				if err == nil && found != nil && (last == nil || found.At.After(last.At)) {
					last = found
				}
			}
			if last != nil {
				if err := keepReceipt(s.receiptFile(path), path, *last); err != nil {
					return err
				}
			}
		}
		if owner, err := s.readOwner(path); err != nil || owner != nil {
			return err
		}
		chosen := ""
		for _, item := range holders {
			if item.Status == StatusRetired {
				continue
			}
			if last != nil && item.ID == last.Item {
				chosen = item.ID
				break
			}
			if chosen == "" {
				chosen = item.ID
			}
		}
		return s.keepOwner(path, chosen)
	})
}
