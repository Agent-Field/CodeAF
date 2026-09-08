package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func runWhy(args []string) error {
	return runWhyTo(args, os.Stdout, time.Now())
}

func runWhyTo(args []string, output io.Writer, now time.Time) error {
	flags := commandFlags("why")
	database := flags.String("db", defaultChatDB(), storeFlagHelp)
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: aforge why self|<node-id> [--db path]")
	}
	path, err := expandHome(strings.TrimSpace(*database))
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("open receipts: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("open receipts: %s is not a regular database file", path)
	}
	graph, err := store.Open(path)
	if err != nil {
		return err
	}
	defer graph.Close()

	// `why self` is the day's self-spend; `why <node-id>` is one leaf's own
	// account of itself. They are the same question at two scales — what did
	// this cost and what did it buy — which is why they are one command rather
	// than a second verb nobody would think to look for.
	if flags.Arg(0) != "self" {
		return writeNodeTranscript(output, graph, flags.Arg(0))
	}

	local := now.In(time.Local)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	receipts, err := graph.SelfReceipts(start)
	if err != nil {
		return err
	}
	// A COLUMN HEADER IS NEVER PRINTED WITHOUT A ROW UNDER IT. `TRIED COST
	// LEARNED` over nothing is a table claiming rows that are not there, and a
	// person reads it as a reader that failed rather than as a day with no
	// self-spend on it. One short sentence instead, which is what `aforge
	// cache` answers over the same emptiness.
	if len(receipts) == 0 {
		_, err := fmt.Fprintln(output, "nothing was tried on its own account today.")
		return err
	}
	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "TRIED\tCOST\tLEARNED")
	for _, receipt := range receipts {
		fmt.Fprintf(table, "%s\t%s\t%s\n", oneLineReceipt(receipt.Origin),
			formatReceiptDollars(receipt.Cost), receiptLearning(receipt))
	}
	if err := table.Flush(); err != nil {
		return fmt.Errorf("write self receipts: %w", err)
	}
	return nil
}

func receiptLearning(receipt store.SelfReceipt) string {
	parts := make([]string, 0, 3)
	if len(receipt.FactIDs) > 0 {
		parts = append(parts, "facts "+receiptIDs(receipt.FactIDs))
	}
	if len(receipt.SkillIDs) > 0 {
		parts = append(parts, "skills "+receiptIDs(receipt.SkillIDs))
	}
	if receipt.SurpriseDelta != nil && *receipt.SurpriseDelta > 0 {
		parts = append(parts, fmt.Sprintf("surprise down %.0f%%", 100**receipt.SurpriseDelta))
	}
	if len(parts) == 0 {
		parts = append(parts, "nothing")
	}
	if receipt.SurpriseDelta != nil && *receipt.SurpriseDelta < 0 {
		parts = append(parts, fmt.Sprintf("surprise up %.0f%%", -100**receipt.SurpriseDelta))
	}
	return strings.Join(parts, "; ")
}

func receiptIDs(ids []int64) string {
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		values = append(values, "#"+strconv.FormatInt(id, 10))
	}
	return strings.Join(values, ",")
}

func formatReceiptDollars(cost float64) string {
	raw := strconv.FormatFloat(cost, 'f', 4, 64)
	raw = strings.TrimRight(raw, "0")
	if strings.HasSuffix(raw, ".") {
		raw += "00"
	} else if dot := strings.IndexByte(raw, '.'); dot < 0 {
		raw += ".00"
	} else if len(raw)-dot == 2 {
		raw += "0"
	}
	return "$" + raw
}

func oneLineReceipt(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

// writeNodeTranscript prints one leaf's turn-by-turn record: what the model
// said, what it asked its tools for, what came back, and how it ended.
//
// It is the reader for the table internal/store/transcript.go writes, and it
// exists because a record nobody can get at is not a record. Before it, the
// only account of a leaf that had cost fifty cents was the harness's own two
// progress lines.
func writeNodeTranscript(output io.Writer, graph *store.Store, nodeID string) error {
	nodeID = strings.TrimSpace(nodeID)
	entries, err := graph.TranscriptFor(nodeID, 0)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		// Two different silences, said as one sentence, because a person
		// holding an empty answer needs to know which of them they have.
		fmt.Fprintf(output, "%s has no transcript: either nothing has run it yet, or the worker that ran it keeps no record.\n", nodeID)
		// AND A MISS IS NOT A SUCCESS. This returned nil, so a script asking
		// whether an id exists read exit 0 and concluded it existed and was
		// empty — the two states this sentence exists to tell apart, collapsed
		// again the moment anything but a person read it. `aforge logs` took
		// exactly this change over the same emptiness, and `notebook retract`
		// has always got it right. Exit 1 is the rung: nothing ran, because
		// there was nothing here to run (envelope.go).
		return exitCannotRun
	}
	for _, entry := range entries {
		fmt.Fprintln(output, transcriptHeadline(entry))
		if body := strings.TrimRight(entry.Text, "\n"); body != "" {
			for _, line := range strings.Split(body, "\n") {
				fmt.Fprintln(output, "    "+line)
			}
		}
	}
	return nil
}

// transcriptHeadline is the one line above each entry's body. The turn number
// leads every line so a reader can see a turn's shape — one thought, four
// tools, four results — without counting.
func transcriptHeadline(entry store.TranscriptEntry) string {
	turn := fmt.Sprintf("turn %d", entry.Turn)
	switch entry.Kind {
	case store.TranscriptAssistant:
		return turn + " · said"
	case store.TranscriptToolCall:
		return turn + " · " + entry.Tool + " ←"
	case store.TranscriptToolResult:
		line := turn + " · " + entry.Tool + " → " + transcriptDuration(entry.Millis)
		if entry.Failed {
			line += " · error"
		}
		return line
	case store.TranscriptFault:
		return turn + " · stopped"
	case store.TranscriptNote:
		// THE NOTE IS SIGNED WITH THE PRODUCT'S NAME. It read `the harness`,
		// which names neither who wrote the line nor what happened — and
		// "harness" is machinery vocabulary besides, spent in this product on
		// the saved shapes of work a person builds and runs by name. A note in
		// this record is aforge writing about the run rather than the model
		// speaking, and `aforge` says exactly that in a word the reader already
		// knows, because it is what they typed to get here.
		return turn + " · aforge"
	case store.TranscriptElided:
		return turn + " · the record stops here"
	}
	return turn + " · " + string(entry.Kind)
}

// transcriptDuration renders a tool's wall time. Sub-second work says so in
// milliseconds because a tool that took 4ms and one that took 900ms are
// different animals, and everything longer rounds to tenths of a second — past
// a second nobody is counting milliseconds.
func transcriptDuration(millis int64) string {
	if millis < 1000 {
		return fmt.Sprintf("%dms", millis)
	}
	return fmt.Sprintf("%.1fs", float64(millis)/1000)
}
