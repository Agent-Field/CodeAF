package main

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/lane"
)

// ── THE SIGHTINGS AS THEY ARRIVED ───────────────────────────────────────────
//
// `~/.codeaf/v3/lanes.log` is internal/lane's own append-only observation
// journal: one NDJSON line per thing the ledger was told, in the order it was
// told it. Its public rows — what the router publishes about a machine's price,
// its limits and its four timing percentiles — are the PRIOR every belief starts
// from, so a replay that skipped them would be judging every candidate on a cold
// start the live build never had.
//
// IT IS COMPACTED, AND THAT IS THE FINDING A READER MUST BE TOLD. The journal is
// drained into `lanes.json` and truncated every few hundred observations
// (journal.go's drain), so what survives on a given machine is minutes rather
// than days. The report says how much of the replay's span the journal actually
// covers, because a prior that arrives for the last two minutes of ten days is a
// prior that changed nothing and must not be reported as though it had.

// seen is one public row as the journal kept it.
type seen struct {
	at     time.Time
	row    lane.Row
	weight float64
}

// entry is one line of the journal.
//
// IT RE-DECLARES A SHAPE internal/lane KEEPS TO ITSELF. The journal's own record
// type is unexported, and the payloads it carries — [lane.Row], [lane.Sighting],
// [lane.Outcome] — are not, so this reads the file through the exported halves
// and names the unexported envelope for itself. That is the whole of the coupling
// and it is one struct; the alternative was an export door on a package another
// lane owns, for a shape nothing in the product reads back.
// AND IT NAMES ONLY THE PUBLISHED ROW. The journal's other kind of line is a
// sighting, and this reader drops every one of them on purpose: `calls.jsonl`
// already carries the same answers, with the shape and the tag the pricing needs,
// so folding both files in would teach every candidate one answer twice and make
// each of them look twice as sure as it was. What the journal is read for is the
// one thing the call log cannot say — what the ledger had already PUBLISHED, and
// with how much weight behind it.
type entry struct {
	At     time.Time `json:"at"`
	Row    *lane.Row `json:"row,omitempty"`
	Weight float64   `json:"k,omitempty"`
}

// readJournal reads every public row the journal kept, in time order. A missing
// journal is an empty one and never an error: this instrument must run on a
// machine whose belief file was compacted an hour ago, and on a log synced from
// somewhere else with no journal beside it at all.
func readJournal(path string) ([]seen, bool) {
	if strings.TrimSpace(path) == "" {
		return nil, true
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, true
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	var kept []seen
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var one entry
		if json.Unmarshal([]byte(line), &one) != nil || one.Row == nil {
			continue
		}
		weight := one.Weight
		if weight <= 0 {
			// A row with no weight on it is a row from a build that had not
			// started writing one. [lane.SheetWeight] is what the ledger itself
			// uses and is the honest stand-in.
			weight = lane.SheetWeight
		}
		kept = append(kept, seen{at: one.At, row: *one.Row, weight: weight})
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].at.Before(kept[j].at) })
	return kept, false
}
