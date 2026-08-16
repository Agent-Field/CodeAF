#!/usr/bin/env bash
# pipelines — the Go fixture the dynamic cell triages.
#
# Three pipelines over one deterministic dataset. Two are linear and finish in
# milliseconds; dedupe rescans the whole accumulated result for every input row,
# so it is quadratic and takes seconds. Nothing in the code says "slow" and no
# test fails — the only evidence is the clock, which is what makes this a
# triage rather than a bug report.
#
# The dataset is generated in-process from a fixed seed, so `pipebench` is
# reproducible and needs no network, no fixtures on disk, and no build tags.
# The suite pins dedupe's *semantics* (order-preserving uniqueness), so a fix
# that is merely fast and wrong is caught by `go test ./...`.
#
# Usage: pipelines.sh <dir>
set -euo pipefail

DIR="${1:?usage: pipelines.sh <dir>}"

rm -rf "$DIR"
mkdir -p "$DIR/dataset" "$DIR/pipeline/ingest" "$DIR/pipeline/dedupe" "$DIR/pipeline/rollup" "$DIR/cmd/pipebench"

cat > "$DIR/go.mod" <<'EOF'
module pipelines

go 1.21
EOF

cat > "$DIR/dataset/dataset.go" <<'EOF'
// Package dataset generates the input every pipeline runs over. It is a fixed
// seed and a fixed size so two runs on two machines see the same rows.
package dataset

import (
	"fmt"
	"math/rand"
)

// Rows is the standard input size for the benchmark. It is sized so that the
// linear stages finish in single-digit milliseconds and the quadratic one takes
// seconds: the gap has to be obvious on one run, without a profiler.
const Rows = 80000

// Generate builds Rows event lines. Roughly one row in ten is a deliberate
// duplicate of an earlier row, so the deduplicated result is large — which is
// what makes a linear rescan per row expensive rather than merely wasteful.
func Generate() []string {
	random := rand.New(rand.NewSource(20260813))
	rows := make([]string, 0, Rows)
	for i := 0; i < Rows; i++ {
		if i > 0 && random.Intn(10) == 0 {
			rows = append(rows, rows[random.Intn(len(rows))])
			continue
		}
		rows = append(rows, fmt.Sprintf("evt-%06d|region-%02d|%d", i, random.Intn(24), 100+random.Intn(900)))
	}
	return rows
}
EOF

cat > "$DIR/pipeline/ingest/ingest.go" <<'EOF'
// Package ingest parses raw event lines into records.
package ingest

import (
	"strconv"
	"strings"
)

// Record is one parsed event.
type Record struct {
	ID     string
	Region string
	Amount int
}

// Run parses every line. Malformed lines are dropped rather than fatal.
func Run(rows []string) []Record {
	out := make([]Record, 0, len(rows))
	for _, row := range rows {
		parts := strings.Split(row, "|")
		if len(parts) != 3 {
			continue
		}
		amount, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}
		out = append(out, Record{ID: parts[0], Region: parts[1], Amount: amount})
	}
	return out
}
EOF

cat > "$DIR/pipeline/dedupe/dedupe.go" <<'EOF'
// Package dedupe removes repeated event ids, keeping first occurrence order.
package dedupe

import "pipelines/pipeline/ingest"

// Run returns the records with duplicate IDs removed, preserving the order in
// which each ID was first seen.
func Run(records []ingest.Record) []ingest.Record {
	out := make([]ingest.Record, 0, len(records))
	for _, record := range records {
		seen := false
		for _, kept := range out {
			if kept.ID == record.ID {
				seen = true
				break
			}
		}
		if !seen {
			out = append(out, record)
		}
	}
	return out
}
EOF

cat > "$DIR/pipeline/rollup/rollup.go" <<'EOF'
// Package rollup totals amounts by region.
package rollup

import (
	"sort"

	"pipelines/pipeline/ingest"
)

// Total is one region's summed amount.
type Total struct {
	Region string
	Amount int
	Count  int
}

// Run totals by region and returns the totals sorted by region name.
func Run(records []ingest.Record) []Total {
	sums := map[string]*Total{}
	for _, record := range records {
		total, ok := sums[record.Region]
		if !ok {
			total = &Total{Region: record.Region}
			sums[record.Region] = total
		}
		total.Amount += record.Amount
		total.Count++
	}
	out := make([]Total, 0, len(sums))
	for _, total := range sums {
		out = append(out, *total)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Region < out[j].Region })
	return out
}
EOF

cat > "$DIR/cmd/pipebench/main.go" <<'EOF'
// Command pipebench times one pipeline stage over the standard dataset.
//
// Output is one machine-readable line per stage:
//
//	pipeline=dedupe in=30000 out=27013 ms=3412
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"pipelines/dataset"
	"pipelines/pipeline/dedupe"
	"pipelines/pipeline/ingest"
	"pipelines/pipeline/rollup"
)

func main() {
	which := flag.String("pipeline", "all", "which pipeline to time: ingest, dedupe, rollup, or all")
	flag.Parse()

	rows := dataset.Generate()
	records := ingest.Run(rows)

	run := func(name string) {
		start := time.Now()
		in, out := 0, 0
		switch name {
		case "ingest":
			in = len(rows)
			out = len(ingest.Run(rows))
		case "dedupe":
			in = len(records)
			out = len(dedupe.Run(records))
		case "rollup":
			in = len(records)
			out = len(rollup.Run(records))
		default:
			fmt.Fprintf(os.Stderr, "unknown pipeline %q\n", name)
			os.Exit(2)
		}
		fmt.Printf("pipeline=%s in=%d out=%d ms=%d\n", name, in, out, time.Since(start).Milliseconds())
	}

	if *which == "all" {
		for _, name := range []string{"ingest", "dedupe", "rollup"} {
			run(name)
		}
		return
	}
	run(*which)
}
EOF

cat > "$DIR/pipeline/dedupe/dedupe_test.go" <<'EOF'
package dedupe

import (
	"testing"

	"pipelines/pipeline/ingest"
)

// The contract a faster dedupe still has to honour: every ID appears once, and
// the survivors are in the order their IDs were first seen.
func TestRunKeepsFirstOccurrenceOrder(t *testing.T) {
	records := []ingest.Record{
		{ID: "a", Region: "r1", Amount: 1},
		{ID: "b", Region: "r2", Amount: 2},
		{ID: "a", Region: "r3", Amount: 9},
		{ID: "c", Region: "r1", Amount: 3},
		{ID: "b", Region: "r9", Amount: 8},
	}
	got := Run(records)
	if len(got) != 3 {
		t.Fatalf("got %d records, want 3", len(got))
	}
	wantIDs := []string{"a", "b", "c"}
	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Errorf("position %d = %q, want %q", i, got[i].ID, want)
		}
	}
	if got[0].Amount != 1 || got[1].Amount != 2 {
		t.Error("dedupe kept a later record instead of the first occurrence")
	}
}

func TestRunEmptyAndAllUnique(t *testing.T) {
	if len(Run(nil)) != 0 {
		t.Error("empty input produced records")
	}
	records := []ingest.Record{{ID: "x"}, {ID: "y"}, {ID: "z"}}
	if len(Run(records)) != 3 {
		t.Error("all-unique input lost records")
	}
}
EOF

cat > "$DIR/pipeline/rollup/rollup_test.go" <<'EOF'
package rollup

import (
	"testing"

	"pipelines/pipeline/ingest"
)

func TestRunTotalsByRegion(t *testing.T) {
	got := Run([]ingest.Record{
		{ID: "a", Region: "east", Amount: 10},
		{ID: "b", Region: "west", Amount: 5},
		{ID: "c", Region: "east", Amount: 7},
	})
	if len(got) != 2 {
		t.Fatalf("got %d regions, want 2", len(got))
	}
	if got[0].Region != "east" || got[0].Amount != 17 || got[0].Count != 2 {
		t.Errorf("east total = %+v", got[0])
	}
	if got[1].Region != "west" || got[1].Amount != 5 {
		t.Errorf("west total = %+v", got[1])
	}
}
EOF

cat > "$DIR/pipeline/ingest/ingest_test.go" <<'EOF'
package ingest

import "testing"

func TestRunParsesAndSkipsMalformed(t *testing.T) {
	got := Run([]string{"evt-1|region-01|100", "rubbish", "evt-2|region-02|notanumber", "evt-3|region-03|250"})
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
	if got[0].ID != "evt-1" || got[0].Region != "region-01" || got[0].Amount != 100 {
		t.Errorf("first record = %+v", got[0])
	}
	if got[1].Amount != 250 {
		t.Errorf("second amount = %d", got[1].Amount)
	}
}
EOF

echo "$DIR"
