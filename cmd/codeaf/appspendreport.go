package main

// ── codeaf app: THE SPENDING REPORT, THE LEDGER ADDED UP BY DAY AND MODEL ───
//
// spend.report (internal/appwire/spend_report.go) is served from the machine's
// usage ledger by session.QueryUsageReport. Unlike the ledger's rows, a cell
// has no title to join: nothing here asks the app what an agent, task or
// automation is called now, and nothing infers a crew, a provider or an outcome
// — the ledger does not hold them, and the wire says so. This file only turns
// the filter into a query, and the cells into the wire's words.

import (
	"errors"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The app's backend serves spend.report.
var _ appwire.SpendReporter = (*appBackend)(nil)

// appReportError is a report refusal in the wire's words: a range too heavy to
// report is the caller's to narrow, as every other ledger refusal is.
func appReportError(err error) error {
	if errors.Is(err, session.ErrUsageReportTooLarge) {
		return appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
	}
	return appLedgerError(err)
}

// SpendReport answers the ledger added up by stored local day, model and
// subject.
func (b *appBackend) SpendReport(ask appwire.SpendReportAsk) (appwire.SpendReportResult, error) {
	query, err := appLedgerQuery(ask.Filter)
	if err != nil {
		return appwire.SpendReportResult{}, appReportError(err)
	}
	report, err := session.QueryUsageReport(appLedgerPath(), query, appLedgerZone(), session.UsageReportBudget{
		MaxCells: appwire.SpendReportCellsMax, MaxBytes: session.UsageReportMaxBytes,
	})
	if err != nil {
		return appwire.SpendReportResult{}, appReportError(err)
	}
	return appReportResult(ask.Filter, report, session.UsageDrops()), nil
}

// appReportResult is the report on the wire. A figure no line carried is
// absent rather than zero, and a cell's kind is the wire's word for it.
func appReportResult(f appwire.SpendLedgerFilter, report session.UsageReport, dropped int64) appwire.SpendReportResult {
	figure := func(usd float64, priced bool) *float64 {
		if !priced {
			return nil
		}
		return &usd
	}
	t := report.Totals
	cells := make([]appwire.SpendReportCell, 0, len(report.Cells))
	for _, c := range report.Cells {
		cells = append(cells, appwire.SpendReportCell{
			Day: c.Day, Model: c.Model, Kind: appLedgerKind(c.Kind), Subject: c.Subject, Root: c.Root, Session: c.Session, Agent: c.Agent,
			Records: c.Records, Calls: c.Calls, TokensIn: c.Input, TokensOut: c.Output,
			CostUSD: figure(c.USD, c.Priced()), UnpricedRecords: c.Unpriced, UnbilledRecords: c.Unbilled,
		})
	}
	return appwire.SpendReportResult{
		From: f.From, To: f.To,
		Totals: appwire.SpendReportTotals{
			Cells: len(cells), Records: t.Records, Calls: t.Calls, TokensIn: t.Input, TokensOut: t.Output,
			CostUSD:        figure(t.USD, t.Records > t.Unpriced),
			RunlessRecords: t.UnknownRunRecords, UnpricedRecords: t.Unpriced, UnbilledRecords: t.Unbilled,
		},
		Cells: cells, Models: append([]string{}, report.Models...),
		Gaps: appwire.SpendReportGaps{
			DroppedSinceStart: dropped, Malformed: report.Stats.Malformed, Oversized: report.Stats.Oversized,
			DaysDerived: report.DaysDerived,
		},
	}
}
