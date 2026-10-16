package main

// ── codeaf app: THE SPENDING CALLS, EVERY MATCHING CALL SAVED AS ONE CSV ────
//
// spend.calls.export (internal/appwire/spend_calls.go) is served from the
// machine's usage ledger by session.EachUsageCall, which reads it as a stream
// and holds one line at a time. This file turns the wire's filter into the same
// query spend.ledger and spend.report use (appLedgerQuery), each call into its
// twelve cells, and puts the file where the person asked for it with the same
// whole-or-not-at-all writer as the ledger's export (appWriteWhole): a sibling
// 0600 temporary file, synced, then linked into place without overwrite or
// renamed with it — never a look-then-rename — and removed on any failure,
// leaving whatever was at the destination exactly as it was.
//
// NOTHING IS JOINED. The agent column is the recorded root conversation, else
// the line's own session; the task and the model are the ids the ledger wrote.
// No title is looked up, so nothing is read from the app's agents, tasks or
// automations and the cost of the file does not depend on them. A crew, a
// provider and an outcome are not recorded, so they are blank on every row, and
// the answer's not_recorded says so.

import (
	"encoding/csv"
	"io"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The app's backend serves spend.calls.export.
var _ appwire.SpendCallsExporter = (*appBackend)(nil)

// appSpendCallsRun is what one pass over the ledger wrote.
type appSpendCallsRun struct {
	rows   int
	totals session.UsageCallTotals
	stats  session.UsageScanStats
}

// appSpendCallsFill is the pass that writes the file's header and one row per
// call to w, recording what it wrote in run. It is the whole of the CSV and is
// kept apart from the file so a test can hand it a writer that fails.
func appSpendCallsFill(ledger string, query session.UsageQuery, zone *time.Location, run *appSpendCallsRun) func(io.Writer) error {
	return func(w io.Writer) error {
		out := csv.NewWriter(w)
		if err := appwire.NewSpendCallsCSV(out); err != nil {
			return err
		}
		var err error
		run.rows = 0
		run.totals, run.stats, err = session.EachUsageCall(ledger, query, func(call session.UsageCall) error {
			run.rows++
			return out.Write(appwire.SpendCallsCSVRecord(appSpendCall(call, zone)))
		})
		if err != nil {
			return err
		}
		out.Flush()
		return out.Error()
	}
}

// appSpendCall is one ledger record as a row of the file. The date and time are
// the call's instant in the zone the filter's days were read in.
func appSpendCall(call session.UsageCall, zone *time.Location) appwire.SpendCall {
	row := appwire.SpendCall{
		Time: call.At.In(zone), Agent: call.Owner, Task: call.Task, Model: call.Model,
		Requests: call.Requests, TokensIn: call.Input, TokensOut: call.Output,
	}
	if call.Priced {
		usd := call.USD
		row.CostUSD = &usd
	}
	return row
}

// ExportSpendCalls writes every call the filter matches to the destination as
// CSV, whole or not at all.
func (b *appBackend) ExportSpendCalls(ask appwire.SpendCallsExportAsk) (appwire.SpendCallsExportResult, error) {
	query, err := appLedgerQuery(ask.Filter)
	if err != nil {
		return appwire.SpendCallsExportResult{}, appLedgerError(err)
	}
	var run appSpendCallsRun
	written, err := appWriteWhole(ask.Destination, ask.Overwrite, appSpendCallsFill(appLedgerPath(), query, appLedgerZone(), &run))
	if err != nil {
		return appwire.SpendCallsExportResult{}, appLedgerError(err)
	}
	t := run.totals
	totals := appwire.SpendCallsTotals{
		Records: t.Records, Calls: t.Requests, TokensIn: t.Input, TokensOut: t.Output,
		UnpricedRecords: t.Unpriced, UnbilledRecords: t.Unbilled,
	}
	if t.Priced() {
		usd := t.USD
		totals.CostUSD = &usd
	}
	return appwire.SpendCallsExportResult{
		Path: ask.Destination, Rows: run.rows, Bytes: written, Totals: totals,
		Skipped: appwire.SpendLedgerSkipped{Malformed: run.stats.Malformed, Oversized: run.stats.Oversized},
	}, nil
}
