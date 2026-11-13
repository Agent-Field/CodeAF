package main

// ── codeaf app: THE SPENDING LEDGER, READ A PAGE AT A TIME AND SAVED WHOLE ──
//
// spend.ledger and spend.ledger.export (internal/appwire/spend_ledger.go) are
// served from the machine's usage ledger by session.QueryUsageLedger and
// session.EachUsageLedgerEntry, which read it as a bounded stream; this file
// adds only what the app knows and the ledger does not — the titles of the
// things the money was spent on — and the one safe way to put a file where the
// person asked for it.
//
// A NAME IS JOINED, NEVER INFERRED. A conversation's title is the one the app
// holds for that agent; a task's is the task with that id IN THAT
// CONVERSATION as the open conversation last said it (an id alone is not
// unique, so a line with no root names no task); an automation's is the
// store's. Anything none of them holds has no name, and the client — or the
// CSV — falls back to the id the ledger wrote.
//
// A NAME IS LOOKED UP FOR THE ROWS BEING ANSWERED, AND NOTHING ELSE IS READ. A
// page asks for the names of its own rows and an export for each row as it is
// written: a map lookup in what the app already holds, and one store read per
// automation, remembered in a cache that stops growing at
// [appLedgerNameCache]. No conversation's journal is opened and no list of
// every conversation is walked, so the cost of naming does not grow with the
// history — only with the rows the question answers.
//
// THE FILE IS WHOLE OR IT IS NOT THERE. It is written beside the destination
// under a temporary name, synced, and only then moved into place; any failure
// removes the temporary file and leaves whatever was at the destination exactly
// as it was. An existing file is replaced only when the caller said overwrite.

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The app's backend serves spend.ledger and spend.ledger.export.
var _ appwire.SpendLedgerer = (*appBackend)(nil)

// appLedgerPath and appLedgerZone are the ledger the app reads and the zone
// its calendar days are in; a test points them elsewhere.
var (
	appLedgerPath = session.UsageLedgerPath
	appLedgerZone = func() *time.Location { return time.Local }
)

// appLedgerQuery is the wire's filter as the ledger asks it.
func appLedgerQuery(f appwire.SpendLedgerFilter) (session.UsageQuery, error) {
	from, to, err := session.UsageDayRange(f.From, f.To, appLedgerZone())
	if err != nil {
		return session.UsageQuery{}, err
	}
	kind := f.Kind
	if kind == appwire.SpendKindAgent {
		kind = session.SubjectConversation
	}
	return session.UsageQuery{
		From: from, To: to, Model: f.Model, Kind: kind, Subject: f.Subject,
		Session: f.Session, Root: f.Root, Task: f.Task, Automation: f.Automation, Run: f.Run, Workspace: f.Workspace, Agent: f.Agent,
	}, nil
}

// appLedgerError is a ledger refusal in the wire's words: what the caller can
// fix is bad_params, too many readers at once is busy.
func appLedgerError(err error) error {
	switch {
	case errors.Is(err, session.ErrUsageScanBusy):
		return appwire.Errorf(appwire.CodeBusy, "%s", err.Error())
	case errors.Is(err, session.ErrUsageQuery), errors.Is(err, session.ErrUsageCursor), errors.Is(err, session.ErrUsageLedgerTooLarge),
		errors.Is(err, session.ErrUsageLedgerChanged), errors.Is(err, session.ErrUsageLedgerFigures):
		return appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
	}
	return appEngineError(err)
}

// SpendLedger answers one page of the ledger.
func (b *appBackend) SpendLedger(ask appwire.SpendLedgerAsk) (appwire.SpendLedgerResult, error) {
	query, err := appLedgerQuery(ask.Filter)
	if err != nil {
		return appwire.SpendLedgerResult{}, appLedgerError(err)
	}
	page, err := session.QueryUsageLedger(appLedgerPath(), query, ask.Cursor, ask.Limit, session.DefaultUsageLedgerBudget)
	if err != nil {
		return appwire.SpendLedgerResult{}, appLedgerError(err)
	}
	names := b.ledgerNamer()
	zone := appLedgerZone()
	rows := make([]appwire.SpendLedgerRow, 0, len(page.Entries))
	for _, entry := range page.Entries {
		rows = append(rows, appLedgerRow(entry, names, zone))
	}
	return appwire.SpendLedgerResult{
		From: ask.Filter.From, To: ask.Filter.To,
		Totals: appLedgerTotals(page.Totals), Rows: rows, Next: page.Next,
		Skipped: appwire.SpendLedgerSkipped{Malformed: page.Stats.Malformed, Oversized: page.Stats.Oversized},
	}, nil
}

// ExportSpendLedger writes every row the filter matches to the destination as
// CSV, whole or not at all.
func (b *appBackend) ExportSpendLedger(ask appwire.SpendLedgerExportAsk) (appwire.SpendLedgerExportResult, error) {
	query, err := appLedgerQuery(ask.Filter)
	if err != nil {
		return appwire.SpendLedgerExportResult{}, appLedgerError(err)
	}
	names := b.ledgerNamer()
	zone := appLedgerZone()
	var (
		totals session.UsageLedgerTotals
		stats  session.UsageScanStats
		rows   int
	)
	written, err := appWriteWhole(ask.Destination, ask.Overwrite, func(w io.Writer) error {
		out := csv.NewWriter(w)
		if err := appwire.NewSpendLedgerCSV(out); err != nil {
			return err
		}
		var err error
		totals, stats, err = session.EachUsageLedgerEntry(appLedgerPath(), query, session.DefaultUsageLedgerBudget, func(entry session.UsageEntry) error {
			rows++
			if err := out.Write(appwire.SpendLedgerCSVRecord(appLedgerRow(entry, names, zone))); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return err
		}
		out.Flush()
		return out.Error()
	})
	if err != nil {
		return appwire.SpendLedgerExportResult{}, appLedgerError(err)
	}
	return appwire.SpendLedgerExportResult{
		Path: ask.Destination, Rows: rows, Bytes: written, Totals: appLedgerTotals(totals),
		Skipped: appwire.SpendLedgerSkipped{Malformed: stats.Malformed, Oversized: stats.Oversized},
	}, nil
}

// appLedgerRow is one ledger row on the wire.
func appLedgerRow(entry session.UsageEntry, names *appLedgerNamer, zone *time.Location) appwire.SpendLedgerRow {
	row := appwire.SpendLedgerRow{
		Grain: entry.Grain, Key: entry.Key, Run: entry.Run,
		Time: entry.Start.In(zone), End: entry.End.In(zone),
		Kind: appLedgerKind(entry.Kind), Subject: entry.Subject,
		Session: entry.Session, Root: entry.Root, Task: entry.Task, Automation: entry.Automation, Workspace: entry.Workspace,
		Models: append([]string{}, entry.Models...), UnnamedModelRecords: entry.UnnamedModel,
		Records: entry.Records, Calls: entry.Calls, TokensIn: entry.Input, TokensOut: entry.Output,
		UnpricedRecords: entry.Unpriced, UnbilledRecords: entry.Unbilled,
	}
	if entry.Priced() {
		usd := entry.USD
		row.CostUSD = &usd
	}
	row.Name = names.of(entry)
	return row
}

// appLedgerKind is the ledger's subject word in the wire's.
func appLedgerKind(kind string) string {
	switch kind {
	case session.SubjectConversation:
		return appwire.SpendKindAgent
	case session.SubjectTask:
		return appwire.SpendKindTask
	case session.SubjectAutomation:
		return appwire.SpendKindAutomation
	}
	return ""
}

// appLedgerTotals is the totals on the wire; a figure no record carried is
// absent rather than zero.
func appLedgerTotals(t session.UsageLedgerTotals) appwire.SpendLedgerTotals {
	figure := func(usd float64, priced bool) *float64 {
		if !priced {
			return nil
		}
		return &usd
	}
	return appwire.SpendLedgerTotals{
		Rows: t.Entries, Runs: t.Runs, UnknownRunRecords: t.UnknownRunRecords,
		Records: t.Records, Calls: t.Calls, TokensIn: t.Input, TokensOut: t.Output,
		CostUSD:           figure(t.USD, t.Records > t.Unpriced),
		RunCostUSD:        figure(t.RunUSD, t.RunUSD > 0),
		UnknownRunCostUSD: figure(t.UnknownRunUSD, t.UnknownRunUSD > 0),
		UnpricedRecords:   t.Unpriced, UnbilledRecords: t.Unbilled,
	}
}

// appLedgerNameCache is the most automation titles one answer remembers.
// Past it each further one is read from the store when its row is met and not
// kept, so a range of very many automations costs reads, never memory.
const appLedgerNameCache = 256

// Titles are optional display metadata, not billing evidence. Omit an oversized
// title rather than copying it into every page/CSV row; the actual subject id
// remains. Clone small names so whitespace trim cannot retain a huge backing string.
const appLedgerTitleMax = 4096

func appLedgerTitle(title string) string {
	if len(title) > appLedgerTitleMax {
		return ""
	}
	return strings.Clone(strings.TrimSpace(title))
}

// appLedgerNamer names one answer's rows from what the app already holds.
type appLedgerNamer struct {
	// conversation and task answer a held agent's title and a held task's,
	// by the agent's id and by the conversation and the task's id there.
	conversation func(id string) string
	task         func(conversation, id string) string
	// automation reads one automation's title from the store.
	automation  func(id string) string
	automations map[string]string
}

// ledgerNamer is the namer over this backend's agents and automations.
func (b *appBackend) ledgerNamer() *appLedgerNamer {
	n := &appLedgerNamer{
		conversation: func(id string) (title string) {
			b.locked(func() {
				if a := b.agents[id]; a != nil {
					title = a.view.Title
				}
			})
			return title
		},
		task: func(conversation, id string) (title string) {
			n, err := strconv.ParseUint(id, 10, 64)
			if err != nil {
				return ""
			}
			b.locked(func() {
				if a := b.agents[conversation]; a != nil && a.live != nil {
					title = a.live.tasks[appTaskID(conversation, n)].Title
				}
			})
			return title
		},
		automation:  func(string) string { return "" },
		automations: map[string]string{},
	}
	if b.orderStore != nil {
		n.automation = func(id string) string {
			store := b.orderStore()
			if store == nil {
				return ""
			}
			a, err := store.Get(id)
			if err != nil {
				return ""
			}
			return a.Title
		}
	}
	return n
}

// of is the title of what the row's money was spent on, or "".
func (n *appLedgerNamer) of(entry session.UsageEntry) string {
	if entry.Subject == "" {
		return ""
	}
	switch entry.Kind {
	case session.SubjectConversation:
		return appLedgerTitle(n.conversation(entry.Subject))
	case session.SubjectAutomation:
		if title, known := n.automations[entry.Subject]; known {
			return title
		}
		title := appLedgerTitle(n.automation(entry.Subject))
		if len(n.automations) < appLedgerNameCache {
			n.automations[entry.Subject] = title
		}
		return title
	case session.SubjectTask:
		if entry.Root == "" {
			return ""
		}
		return appLedgerTitle(n.task(entry.Root, entry.Subject))
	}
	return ""
}

// appLinkFile and appRemoveFile are the two filesystem steps that place an
// export without overwrite; a test replaces them.
var (
	appLinkFile   = os.Link
	appRemoveFile = os.Remove
)

// appWriteWhole writes what fill writes to path, whole or not at all, and
// answers the bytes written.
//
// THE TEMPORARY FILE IS A SIBLING, so the last step is a rename within one
// directory, which a reader sees as the old file or the new one and never part
// of either. It is made 0600, written, synced and closed before it moves.
//
// WITHOUT OVERWRITE THE MOVE IS A HARD LINK AND NOTHING ELSE. A link refuses an
// existing destination atomically, so a file someone else wrote there a moment
// ago is not replaced. A folder that cannot take a link (some network and
// removable filesystems) is REFUSED, with what to do instead: the only other
// way would be to look first and rename after, and a file created between the
// look and the rename would be destroyed without a word.
//
// THE TEMPORARY FILE IS ALWAYS REMOVED unless it became the destination by
// rename — after a failure, and after a link has placed it (the link is the
// destination's own name for the same bytes).
func appWriteWhole(path string, overwrite bool, fill func(io.Writer) error) (int64, error) {
	dir := filepath.Dir(path)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return 0, appwire.Errorf(appwire.CodeBadParams, "the destination's folder does not exist")
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return 0, appwire.Errorf(appwire.CodeBadParams, "the destination is not a file")
		}
		if !overwrite {
			return 0, appwire.Errorf(appwire.CodeBadParams, "the destination already exists; pass overwrite to replace it")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return 0, appEngineError(err)
	}
	temp, err := os.CreateTemp(dir, ".codeaf-ledger-*.tmp")
	if err != nil {
		return 0, appEngineError(err)
	}
	tempPath := temp.Name()
	renamed := false
	defer func() {
		_ = temp.Close()
		if !renamed {
			_ = appRemoveFile(tempPath)
		}
	}()
	if err := temp.Chmod(appStateFileMode); err != nil {
		return 0, err
	}
	counter := &appCountingWriter{w: bufio.NewWriterSize(temp, 64<<10)}
	if err := fill(counter); err != nil {
		return 0, err
	}
	if err := counter.w.Flush(); err != nil {
		return 0, err
	}
	if err := temp.Sync(); err != nil {
		return 0, err
	}
	if err := temp.Close(); err != nil {
		return 0, err
	}
	if overwrite {
		if err := fsreplace.Rename(tempPath, path); err != nil {
			return 0, fmt.Errorf("the export could not be put in place: %w", err)
		}
		renamed = true
	} else if err := appLinkFile(tempPath, path); err != nil {
		switch {
		case errors.Is(err, fs.ErrExist):
			return 0, appwire.Errorf(appwire.CodeBadParams, "the destination already exists; pass overwrite to replace it")
		case errors.Is(err, syscall.EPERM), errors.Is(err, syscall.ENOTSUP), errors.Is(err, syscall.EOPNOTSUPP),
			errors.Is(err, syscall.EXDEV), errors.Is(err, syscall.EMLINK), errors.Is(err, syscall.ENOSYS):
			return 0, appwire.Errorf(appwire.CodeBadParams, "this folder cannot place a new file without the risk of replacing one created there at the same moment; choose another folder, or pass overwrite to replace whatever is at the destination")
		}
		return 0, fmt.Errorf("the export could not be put in place: %w", err)
	}
	if handle, err := os.Open(dir); err == nil {
		_ = handle.Sync()
		_ = handle.Close()
	}
	return counter.n, nil
}

// appCountingWriter counts what passes through it.
type appCountingWriter struct {
	w *bufio.Writer
	n int64
}

func (c *appCountingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
