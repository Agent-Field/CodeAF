package cellindex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/session"
)

// usageIndex is this session's share of the machine-wide usage ledger: one row
// per ordinary sealed call and mirrored remote event. A rewind never drops
// sealed ordinary spend. A cell
// sealed before receipts carried model calls falls back to the usage lines the
// transcript journaled.
//
// The ledger row a live call writes also carries lane timings, seat and task
// ids that no receipt holds; a rebuilt row leaves them absent, which the ledger
// reads as "nobody said". Totals match; a receipt keeps cost in whole micro
// dollars, so a rebuilt row is the live one rounded to that unit.
type usageIndex struct{}

func (usageIndex) Name() string { return "usage.jsonl" }

// Present reports whether the ledger already names the session in any row.
func (usageIndex) Present(c cell.Cell) bool {
	path := session.UsageLedgerPath()
	if ledgerRows(path, c.ID) > 0 {
		return true
	}
	return ledgerRows(path, usageJournalID(c)) > 0
}

// Behind reports whether the chat spent on calls the ledger has no row for: the
// copy this machine held was older than the one a take just brought.
func (usageIndex) Behind(c cell.Cell, d session.Digest) bool {
	lines, receipted, err := usageLines(c, d)
	if err != nil {
		return false
	}
	missing, err := missingUsageForCell(session.UsageLedgerPath(), lines, receipted, c.ID, d.ID)
	return err == nil && len(missing) > 0
}

// Reconstruction compares call identities across the whole machine ledger.
// A parent's row count cannot say whether its workers already paid for a call.
var usageRebuildMu sync.Mutex

// Build appends saved calls whose live ledger identity is absent.
func (usageIndex) Build(c cell.Cell, d session.Digest) error {
	usageRebuildMu.Lock()
	defer usageRebuildMu.Unlock()
	session.FlushUsage()
	lines, receipted, err := usageLines(c, d)
	if err != nil {
		return err
	}
	path := session.UsageLedgerPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+session.UsageLedgerLockSuffix, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer filelock.Unlock(lock)
	lines, err = missingUsageForCell(path, lines, receipted, c.ID, d.ID)
	if err != nil {
		return err
	}
	for _, line := range lines {
		session.RecordImportedUsage(path, line)
	}
	if !session.FlushUsage() {
		return os.ErrDeadlineExceeded
	}
	return nil
}

// missingUsage applies one rule to every actual call: keep it only if the
// ledger lacks its identity. Fold summaries never enter the call stream.
// Older receipts lacking identity retain their session-prefix compatibility.
func missingUsage(path string, lines []session.UsageLine, owners ...string) ([]session.UsageLine, error) {
	return missingUsageForCell(path, lines, true, owners...)
}

func missingUsageForCell(path string, lines []session.UsageLine, receipted bool, owners ...string) ([]session.UsageLine, error) {
	restoreRemoteUsageIDs(path, lines)
	paid, err := session.ReadUsage(path, time.Time{})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	savedExplicit := usageExplicitIDs(lines)
	explicitSeen := make(map[string]bool, len(paid)*2)
	fingerprintCount := make(map[string]int, len(paid))
	legacyFingerprintCount := make(map[string]int, len(paid))
	for _, row := range paid {
		if id := session.UsageExplicitID(row); id != "" {
			explicitSeen[id] = true
		}
		if row.CallID != "" {
			explicitSeen[row.CallID] = true
		}
		if row.ID != "" {
			explicitSeen[row.ID] = true
		}
		if row.RemoteEvent != "" {
			explicitSeen["remote:"+row.RemoteEvent] = true
		}
		fp := session.UsageFingerprint(row)
		if fp != "" {
			explicitSeen[fp] = true
		}
		if !matchedExplicit(row, fp, savedExplicit) && fp != "" {
			fingerprintCount[fp]++
			if countsAsLegacyPrefix(row, receipted) {
				legacyFingerprintCount[fp]++
			}
		}
	}
	skip := 0
	for _, owner := range owners {
		skip = max(skip, legacyUsageRows(paid, savedExplicit, owner, receipted))
	}
	if len(owners) == 0 {
		for _, line := range lines {
			skip = max(skip, legacyUsageRows(paid, savedExplicit, line.Session, receipted))
		}
	}
	var missing []session.UsageLine
	for _, line := range lines {
		if id := session.UsageExplicitID(line); id != "" {
			if explicitSeen[id] || (line.RemoteEvent != "" && explicitSeen["remote:"+line.RemoteEvent]) {
				continue
			}
			explicitSeen[id] = true
			if line.RemoteEvent != "" {
				explicitSeen["remote:"+line.RemoteEvent] = true
			}
			missing = append(missing, line)
			continue
		}
		// A LINE WITH NO IDENTITY IS STILL COUNTED, NEVER BOOKED BLINDLY. A
		// transcript line whose timestamp did not parse has no fingerprint, and
		// both rules this one unified cover such a line by the session's uncovered
		// rows rather than booking it again on every reopen. "No identity, no
		// dedupe" is about rows: a row without one is never another call's
		// duplicate by identity, which the count below does not claim.
		fp := session.UsageFingerprint(line)
		if fp != "" && fingerprintCount[fp] > 0 {
			fingerprintCount[fp]--
			if legacyFingerprintCount[fp] > 0 {
				legacyFingerprintCount[fp]--
				if skip > 0 {
					skip--
				}
			}
			continue
		}
		if skip > 0 {
			skip--
			continue
		}
		missing = append(missing, line)
	}
	return missing, nil
}

func usageExplicitIDs(lines []session.UsageLine) map[string]bool {
	ids := make(map[string]bool)
	for _, line := range lines {
		if id := session.UsageExplicitID(line); id != "" {
			ids[id] = true
		}
		if line.RemoteEvent != "" {
			ids["remote:"+line.RemoteEvent] = true
		}
	}
	return ids
}

func matchedExplicit(row session.UsageLine, fp string, saved map[string]bool) bool {
	if id := session.UsageExplicitID(row); id != "" && saved[id] {
		return true
	}
	if row.CallID != "" && saved[row.CallID] {
		return true
	}
	if row.ID != "" && saved[row.ID] {
		return true
	}
	if row.RemoteEvent != "" && saved["remote:"+row.RemoteEvent] {
		return true
	}
	return fp != "" && saved[fp]
}

// A receipted cell shares modern ledger row IDs with its sealed receipts, so a
// SharedID row absent from the receipt chain is an unsealed call. A receiptless
// cell falls back to transcript usage lines, which never carry row IDs.
func countsAsLegacyPrefix(row session.UsageLine, receipted bool) bool {
	if row.RemoteEvent != "" || row.Unbilled {
		return false
	}
	if receipted {
		return !row.SharedID
	}
	return true
}

func legacyUsageRows(rows []session.UsageLine, saved map[string]bool, owner string, receipted bool) int {
	if owner == "" {
		return 0
	}
	n := 0
	for _, row := range rows {
		fp := session.UsageFingerprint(row)
		if row.Session == owner && countsAsLegacyPrefix(row, receipted) && !matchedExplicit(row, fp, saved) {
			n++
		}
	}
	return n
}

// usageLines combines ordinary sealed calls with transcript remote events.
// With no ordinary receipts, the transcript supplies the ordinary fallback too.
func usageLines(c cell.Cell, d session.Digest) ([]session.UsageLine, bool, error) {
	sealed, err := cellstore.ModelCalls(c)
	if err != nil || len(sealed) == 0 {
		return d.Usage, false, err
	}
	lines := make([]session.UsageLine, len(sealed))
	for i, s := range sealed {
		lines[i] = usageOfCall(c, d, s)
	}
	// Mirrored charges bypass the local recorder; their identities live in the
	// carried transcript alongside the local receipt chain.
	for _, line := range d.Usage {
		if line.RemoteEvent != "" {
			lines = append(lines, line)
		}
	}
	return lines, true, nil
}

func usageOfCall(c cell.Cell, d session.Digest, s cellstore.SealedCall) session.UsageLine {
	id := s.ExplicitID()
	return session.UsageLine{
		ID: id, SharedID: id != "", CallID: id,
		At: time.UnixMilli(s.SealedAtMs), Model: s.Model, Role: s.Role, Calls: 1,
		Input: int(s.TokensIn), Output: int(s.TokensOut), USD: float64(s.CostMicroUSD) / 1e6,
		Session: usageOwner(c, d), Workspace: d.Workspace,
	}
}

// ledgerRows counts the ledger rows that name the session, without parsing any
// row. An unreadable ledger answers as if it were full: it is not ours to append to.
func ledgerRows(path string, sessions ...string) int {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0
	}
	if err != nil {
		return math.MaxInt
	}
	defer f.Close()
	markers := make([][]byte, 0, len(sessions))
	for _, id := range sessions {
		if id != "" {
			markers = append(markers, []byte(`"session":"`+id+`"`))
		}
	}
	n := 0
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		for _, marker := range markers {
			if bytes.Contains(line, marker) {
				n++
				break
			}
		}
		if err != nil {
			return n
		}
	}
}

// Seeded cells from older builds can have a journal identity different from
// their folder. Live calls use the journal identity; recognize both names so
// reopening never books the receipt again under its folder's name.
func usageJournalID(c cell.Cell) string {
	f, err := os.Open((session.Place{Dir: c.Root}).Transcript())
	if err != nil {
		return ""
	}
	defer f.Close()
	var header struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(f).Decode(&header) != nil {
		return ""
	}
	return strings.TrimSpace(header.ID)
}

func usageOwner(c cell.Cell, d session.Digest) string {
	if d.ID != "" {
		return d.ID
	}
	return c.ID
}
