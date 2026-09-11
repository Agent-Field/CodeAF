package standing

// receipt.go is what aforge last put at each REPORT PATH: the report it
// published there, or the file a person agreed at a card to have replaced.
//
// ── THE RECEIPT IS THE FILE'S, NOT THE ITEM'S (ruling R2, 2026-09-11) ──
//
// "Is this file what aforge last published here" is a question about a file,
// and it used to be answered from the item's own receipts (published.json
// beside its runs). So an item stopped and set up afresh — the only road the
// chat had for "change it" before `stand` could edit — found its successor's
// first report held `report-changed` at the file its predecessor had published,
// and every run after it the same (the chat protocol's lifecycle and move
// cases). Keyed by the path, a successor reads the receipt its predecessor
// wrote, and a file somebody else changed is still held, because its bytes are
// not the receipt's.
//
// ── THE LAWS IT IS WRITTEN UNDER ──
//
//   - FENCED (the scale audit's L1). A receipt is read and replaced only under
//     the path's own lock, with the act it vouches for between the two
//     ([Store.AtReport]): the look at the file, the rename and the new receipt
//     are one step to every other writer of that path.
//   - ONE DURABLE WRITE (L4). A receipt is replaced whole through
//     [writeAtomic]. One that cannot be read is reported to whoever asked and
//     is never written over: a publication against it fails and says why.
//   - STAMPED AND ADDITIVE (L5). Every receipt carries [ReceiptSchema], and one
//     from a newer build is unreadable here rather than guessed at. The item
//     receipts written before this file are READ and never written: an item
//     that last published under them still finds that publication, until its
//     next one moves the receipt to the path ([Store.legacyPublication]).
//   - FANNED OUT (L7). reports/<two hex>/<sha256 of the path>.json: 256
//     folders, so ten thousand paths to each is 2.5 million report paths before
//     any one folder grows past the audit's figure.
//   - RETENTION DECLARED (L9). One receipt, and one lock file, per distinct
//     report path this machine has ever published to or adopted, replaced in
//     place and KEPT FOREVER. The count grows with the files people name, never
//     with runs; and a receipt removed would make aforge's own last report a
//     file it treats as the person's, holding every run after it.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ReceiptSchema is the shape of a report path's receipt. A receipt stamped
// newer than this is one this build cannot vouch for.
const ReceiptSchema = 1

// The classes of receipt: what aforge did at the path to earn it.
const (
	// ReceiptPublished is a report aforge placed there.
	ReceiptPublished = "published"
	// ReceiptAdopted is a file that was already there when a person said yes
	// to a card that said so and said aforge would replace it. Its bytes are
	// the ones the card described, and the first report replaces exactly those.
	ReceiptAdopted = "adopted"
)

// Receipt is what aforge last put at one report path, kept under the path.
type Receipt struct {
	Schema int `json:"schema"`
	// Path is the report's absolute path, as the publisher resolved it.
	Path  string `json:"path"`
	Class string `json:"class"`
	// Item is the standing item that earned the receipt: provenance for a
	// person reading the file, never a condition of the compare.
	Item   string    `json:"item,omitempty"`
	SHA256 string    `json:"sha256"`
	Bytes  int       `json:"bytes"`
	At     time.Time `json:"at"`
}

// receiptsDir is the folder report paths' receipts fan out under.
const receiptsDir = "reports"

// receiptFile is where path's receipt lives. Its lock is beside it with the
// same name and .lock, so neither can be mistaken for the other.
func (s *Store) receiptFile(path string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	name := hex.EncodeToString(sum[:])
	return filepath.Join(s.root, receiptsDir, name[:2], name+".json")
}

// Receipt reads what aforge last put at path, or nil when it has put nothing
// there. It takes no lock: it is for a card or a guard that says what it sees,
// and every act that relies on a receipt reads it again under
// [Store.AtReport].
func (s *Store) Receipt(path string) (*Receipt, error) {
	raw, err := os.ReadFile(s.receiptFile(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var receipt Receipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return nil, fmt.Errorf("standing: the receipt for %s is unreadable: %w", path, err)
	}
	if receipt.Schema > ReceiptSchema {
		return nil, fmt.Errorf("standing: the receipt for %s was written by a newer aforge", path)
	}
	return &receipt, nil
}

// AtReport holds path's lock while act reads what aforge last put there and
// does whatever that allows, and keeps the receipt act answers — nil keeps the
// one there. id and report name the item acting and its report as the item
// spells it, so an item whose receipt predates the path's is still found
// ([Store.legacyPublication]); a caller that is no item passes "".
//
// A RECEIPT THAT COULD NOT BE KEPT IS STILL ACT'S OWN ANSWER: act's error and
// the write's are both returned, and [ErrReceiptNotKept] says which, because a
// report already placed is placed and the caller has to say so.
// NO MODEL OR NETWORK CALL MAY RUN INSIDE act.
func (s *Store) AtReport(path, id, report string, act func(last *Receipt) (*Receipt, error)) error {
	file := s.receiptFile(path)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	return underLock(file[:len(file)-len(".json")]+".lock", func() error {
		last, err := s.Receipt(path)
		if err != nil {
			return err
		}
		if last == nil && id != "" {
			if last, err = s.legacyPublication(id, report, path); err != nil {
				return err
			}
		}
		keep, err := act(last)
		if keep == nil {
			return err
		}
		if unkept := keepReceipt(file, path, *keep); unkept != nil {
			return errors.Join(err, fmt.Errorf("%w: %w", ErrReceiptNotKept, unkept))
		}
		return err
	})
}

// keepReceipt stamps receipt for path and replaces the receipt file whole.
func keepReceipt(file, path string, receipt Receipt) error {
	receipt.Schema, receipt.Path = ReceiptSchema, filepath.Clean(path)
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(file, append(data, '\n'))
}

// ErrReceiptNotKept is a receipt [Store.AtReport] could not write after its act
// had happened. The next act at that path then finds a file aforge cannot vouch
// for, and holds rather than writes over it.
var ErrReceiptNotKept = errors.New("standing: the receipt could not be kept")

// ── the receipts items kept before paths kept their own ──────────────────────

// publicationsFile is where an item kept the receipt of the last report it
// published to each path before receipts were kept by path. It is read, never
// written.
const publicationsFile = "published.json"

// publicationWindow is how many of an item's newest runs [Store.legacyPublication]
// reads when the item kept no receipt file — the same window the pass looks
// back through for a finished occurrence ([Store.finishedOccurrence]).
const publicationWindow = 64

// legacyPublication answers, as a receipt at path, the last report item id
// published to report under the receipts that came before this file: its
// published.json, else its newest [publicationWindow] run records, found by
// number from its own run counter rather than by listing the folder (the third
// review's B5). A receipt older than that is not found, and the file it
// describes is then treated as one aforge never wrote — held, never written over.
func (s *Store) legacyPublication(id, report, path string) (*Receipt, error) {
	if err := checkID(id); err != nil {
		return nil, ErrNotFound
	}
	found, err := s.keptPublication(id, report)
	if err == nil && found == nil {
		found, err = s.recordedPublication(id, report)
	}
	if err != nil || found == nil {
		return nil, err
	}
	return &Receipt{Path: filepath.Clean(path), Class: ReceiptPublished, Item: id, SHA256: found.SHA256, Bytes: found.Bytes, At: found.At}, nil
}

// keptPublication reads report's receipt from the item's published.json.
func (s *Store) keptPublication(id, report string) (*Publication, error) {
	raw, err := os.ReadFile(filepath.Join(s.ItemDir(id), publicationsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var kept map[string]Publication
	if err := json.Unmarshal(raw, &kept); err != nil {
		return nil, fmt.Errorf("standing: the publication receipts of %s are unreadable: %w", id, err)
	}
	if receipt, ok := kept[filepath.Clean(report)]; ok {
		return &receipt, nil
	}
	return nil, nil
}

// recordedPublication looks for report's receipt in the item's newest run
// records.
func (s *Store) recordedPublication(id, report string) (*Publication, error) {
	last, err := s.lastMinted(id)
	if err != nil {
		return nil, err
	}
	for n := last; n > 0 && n > last-publicationWindow; n-- {
		for _, name := range []string{RunName(n), fmt.Sprintf("%04d", n)} {
			record, err := ReadOccurrence(filepath.Join(s.RunsDir(id), name))
			if err != nil {
				continue
			}
			if record.Published != nil && filepath.Clean(record.Published.Path) == filepath.Clean(report) {
				return record.Published, nil
			}
			break
		}
	}
	return nil, nil
}
