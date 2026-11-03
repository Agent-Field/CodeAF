package cellindex

import (
	"bufio"
 "bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/session"
)

// usageIndex is this session's share of the machine-wide usage ledger: one row
// per model call the receipt chain sealed, so a rewind never drops spend. A cell
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
	lines, err := usageLines(c, d)
	return err == nil && len(unrecordedUsage(session.UsageLedgerPath(), lines, c.ID, d.ID)) > 0
}

// Build appends only saved calls absent from both ledger aliases.
func (usageIndex) Build(c cell.Cell, d session.Digest) error {
	lines, err := usageLines(c, d)
	if err != nil {
		return err
	}
	path := session.UsageLedgerPath()
	for _, line := range unrecordedUsage(path, lines, c.ID, d.ID) {
		session.RecordUsage(path, line)
	}
	session.FlushUsage()
	return nil
}

// usageLines is what the session spent: the receipt chain's model calls, which
// no rewind removes; and, for a cell sealed before receipts carried them, the
// transcript's usage lines.
func usageLines(c cell.Cell, d session.Digest) ([]session.UsageLine, error) {
	sealed, err := cellstore.ModelCalls(c)
	if err != nil || len(sealed) == 0 {
		return d.Usage, err
	}
	lines := make([]session.UsageLine, len(sealed))
	for i, s := range sealed {
		lines[i] = usageOfCall(c, d, s)
	}
	return lines, nil
}

func usageOfCall(c cell.Cell, d session.Digest, s cellstore.SealedCall) session.UsageLine {
	return session.UsageLine{
		At: time.UnixMilli(s.SealedAtMs), Model: s.Model, Role: s.Role, Calls: 1,
		Input: int(s.TokensIn), Output: int(s.TokensOut), USD: float64(s.CostMicroUSD) / 1e6,
		Session: usageOwner(c, d), Workspace: d.Workspace,
	}
}

// unrecordedUsage matches each saved call to at most one row per alias. A
// live row precedes its receipt's seal; a rebuilt row carries the seal time.
// Match in call order, retaining distinct equal-cost calls and ignoring repeated
// ledger IDs. Receipt costs are rounded to microdollars, unlike live costs.
func unrecordedUsage(path string, lines []session.UsageLine, sessions ...string) []session.UsageLine {
 f, err := os.Open(path)
 if errors.Is(err, fs.ErrNotExist) { return lines }
 if err != nil { return nil }
 defer f.Close()
 used := map[string][]bool{}
 for _, id := range sessions { if id != "" { used[id] = make([]bool,len(lines)) } }
 seenIDs := map[string]bool{}
 held := make([]bool,len(lines))
 r := bufio.NewReaderSize(f,1<<20)
 for {
  data, err := r.ReadBytes('\n')
  var row session.UsageLine
  if json.Unmarshal(data,&row) == nil && used[row.Session] != nil && (row.ID == "" || !seenIDs[row.ID]) {
   if row.ID != "" { seenIDs[row.ID] = true }
   match := -1
   for i, call := range lines {
    if used[row.Session][i] || row.Model != call.Model || row.Role != call.Role || row.Input != call.Input || row.Output != call.Output || math.Round(row.USD*1e6) != math.Round(call.USD*1e6) { continue }
    if match < 0 { match = i }
    if row.At.UnixMilli() <= call.At.UnixMilli() { match = i; break }
   }
   if match >= 0 { used[row.Session][match] = true; held[match] = true }
  }
  if err != nil { break }
 }
 var missing []session.UsageLine
 for i, line := range lines { if !held[i] { missing = append(missing,line) } }
 return missing
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
