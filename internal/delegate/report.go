package delegate

// A PROGRAM'S REPORT, READ BACK BYTE FOR BYTE, ON THE AUTHORITY OF THE RUN
// THAT WROTE IT. A program that lands text leaves its report in the run's
// record folder under a fixed bare name it declared ([Delegate.Reports]); a
// reader that must see exactly what the next step (a post, a fix) will see
// asks for the bytes and decodes them itself.
//
// A NAME ON THE DISK IS NOT A RUN'S REPORT. The folder outlives every launch
// of its task, so a file under a declared name may be an earlier run's: a
// rerun that failed before it wrote leaves the last run's report standing,
// with the rerun's launch on the record. The ONE thing that says a file is
// this launch's report is the receipt the program issued when it wrote it
// ([ReportReceipt]: the name, the byte count and the SHA-256 of exactly the
// bytes it handed the write), carried on its terminal line and kept by its
// worker on the record of that launch ([ProgramRecord.ReportReceipts]). The
// declaration, the ending's display paths ([EndFacts.Reports]), the record's
// times and the file's own clock are each no such proof, and a record written
// before receipts existed has none: its report is unavailable.
//
// WHAT THE READ WILL NOT DO. It does not follow a link as the folder or as the
// file, serve a file with a second name, open a pipe or a device, read past
// [ReportMax], create or change anything, retry, or return some of a file. It
// serves only bytes whose digest is the receipt's, so what it answers is what
// the run wrote. It does not decode, trim or redact.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// ReportMax is the most a served report may hold: 2 MiB.
const ReportMax = 2 << 20

// reportChunk is how much one read step takes, so a deadline is noticed between
// steps and a slow disk is never waited on for 2 MiB at a time.
const reportChunk = 64 << 10

// ReportReceiptsKey is the terminal line's data key a program issues its
// receipts under, and the record's field they are kept in.
const ReportReceiptsKey = "report_receipts"

// reportReceiptsMax bounds the receipts one line or one record may carry; a
// program declares a handful of reports, never more.
const reportReceiptsMax = 16

// Why a report could not be served. Each is a fact; none of them is an empty
// report.
var (
	// ErrReportNoRecord: the folder holds no program record that can be read.
	ErrReportNoRecord = errors.New("there is no program record for that task")
	// ErrReportUndeclared: the record's program declares no such report.
	ErrReportUndeclared = errors.New("that is not a report file of this program")
	// ErrReportMoved: the record's launch is not the one asked for (the task
	// was handed to its program again), or it moved while the file was read.
	ErrReportMoved = errors.New("that report is of another run of the program; read the task again")
	// ErrReportNotFinal: the record's run has not ended.
	ErrReportNotFinal = errors.New("the run has not ended, so its report is not final")
	// ErrReportNotIssued: the run ended without issuing a receipt for that
	// report: it wrote none, or it was written before receipts existed.
	ErrReportNotIssued = errors.New("this run left no such report")
	// ErrReportNotReceipted: the file is not the one the run wrote (its size or
	// its digest is not the receipt's): another writer, or an earlier run.
	ErrReportNotReceipted = errors.New("the report on the disk is not the one this run wrote")
	// ErrReportMissing: nothing is there under that name.
	ErrReportMissing = errors.New("the report is not there")
	// ErrReportUnsafe: the folder or the file is a link, a pipe, a device, a
	// folder, a file with a second name, or otherwise not an ordinary file in
	// the record folder.
	ErrReportUnsafe = errors.New("the report is not an ordinary file in the record folder")
	// ErrReportTooLarge: the file is over [ReportMax]; none of it is read.
	ErrReportTooLarge = errors.New("the report is over 2 MiB")
	// ErrReportChanged: the file, the folder or the record changed while the
	// report was read.
	ErrReportChanged = errors.New("the report changed while it was read")
	// ErrReportUnreadable: the disk refused the read.
	ErrReportUnreadable = errors.New("the report could not be read")
	// ErrReportUnsupported: this platform has no descriptor-relative open
	// without following links, so it serves no report.
	ErrReportUnsupported = errors.New("reports are not served on this platform")
)

// ValidReportName answers whether name is a bare file name a program may
// declare: 1 to 64 bytes of letters, digits, dot, underscore and hyphen, not
// starting with a dot. No separator, no "..", no space, no control character.
func ValidReportName(name string) bool {
	if name == "" || len(name) > 64 || name[0] == '.' {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// DeclaresReport answers whether the program declares name as a report.
func (d Delegate) DeclaresReport(name string) bool {
	if !ValidReportName(name) {
		return false
	}
	for _, declared := range d.Reports {
		if declared == name {
			return true
		}
	}
	return false
}

// ReportReceipt is a program's word, at the moment it wrote a report, for
// exactly what it wrote: the bare name, how many bytes and their SHA-256 in
// lowercase hex.
type ReportReceipt struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// IssueReportReceipt is the receipt for body written under name. A program
// calls it with the very bytes it handed the write, and only once the write
// has succeeded.
func IssueReportReceipt(name string, body []byte) ReportReceipt {
	sum := sha256.Sum256(body)
	return ReportReceipt{Name: name, Bytes: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
}

// valid answers whether the receipt has the shape one can: a bare name, a
// count within [ReportMax], and 64 lowercase hex digits.
func (r ReportReceipt) valid() bool {
	if !ValidReportName(r.Name) || r.Bytes < 0 || r.Bytes > ReportMax || len(r.SHA256) != sha256.Size*2 {
		return false
	}
	for i := 0; i < len(r.SHA256); i++ {
		if c := r.SHA256[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// IssuedReceipts are the receipts the program's terminal line issued for the
// reports it declares: each of valid shape, the first per name, in the order
// the line gave them. A nil line, a line without the key, or a key that is not
// a list of receipts issues none. The worker keeps these, and only these, on
// the record of the launch whose line it was.
func (d Delegate) IssuedReceipts(t *Terminal) []ReportReceipt {
	if t == nil {
		return nil
	}
	raw, ok := t.Data[ReportReceiptsKey]
	if !ok {
		return nil
	}
	var issued []ReportReceipt
	if json.Unmarshal(raw, &issued) != nil || len(issued) > reportReceiptsMax {
		return nil
	}
	var kept []ReportReceipt
	seen := map[string]bool{}
	for _, receipt := range issued {
		if !receipt.valid() || !d.DeclaresReport(receipt.Name) || seen[receipt.Name] {
			continue
		}
		seen[receipt.Name] = true
		kept = append(kept, receipt)
	}
	return kept
}

// Receipt is the record's receipt for name, when its launch issued one of a
// valid shape.
func (r ProgramRecord) Receipt(name string) (ReportReceipt, bool) {
	if len(r.ReportReceipts) > reportReceiptsMax {
		return ReportReceipt{}, false
	}
	for _, receipt := range r.ReportReceipts {
		if receipt.Name == name && receipt.valid() {
			return receipt, true
		}
	}
	return ReportReceipt{}, false
}

// IssuedReport is one report as its run wrote it: the record of the launch
// that issued it, its receipt, and the bytes, which hash to the receipt.
type IssuedReport struct {
	Program            string
	Launch             string
	StartedAt, EndedAt time.Time
	Receipt            ReportReceipt
	Data               []byte
}

// reportReadHook runs after the bytes are read and before anything is checked
// again; a test uses it to change the disk underneath the read.
var reportReadHook func()

// SetReportReadHookForTest sets [reportReadHook] for a test in another package
// and answers nothing; nil removes it.
func SetReportReadHookForTest(hook func()) { reportReadHook = hook }

// ReadIssuedReport reads the report name from the record folder dir, which
// codeaf chose, for the launch asked about, on the authority of that launch's
// receipt. declares answers whether the record's program declares name. Every
// step after the folder is opened goes through that one descriptor
// (platform's openReportFolder), so the record that authorises the read and
// the file that is read are in the same folder.
func ReadIssuedReport(ctx context.Context, dir, launch, name string, declares func(program string) bool) (IssuedReport, error) {
	if !ValidReportName(name) || dir == "" {
		return IssuedReport{}, ErrReportUnsafe
	}
	if !ValidLaunch(launch) {
		return IssuedReport{}, ErrReportMoved
	}
	if err := ctx.Err(); err != nil {
		return IssuedReport{}, err
	}
	folder, err := openReportFolder(dir)
	if errors.Is(err, ErrReportMissing) {
		return IssuedReport{}, ErrReportNoRecord
	}
	if err != nil {
		return IssuedReport{}, err
	}
	defer folder.close()
	record, err := folder.record(ctx)
	if err != nil {
		return IssuedReport{}, err
	}
	if !declares(record.Name) {
		return IssuedReport{}, ErrReportUndeclared
	}
	if record.Launch != launch {
		return IssuedReport{}, ErrReportMoved
	}
	if record.StartedAt.IsZero() || record.EndedAt.IsZero() {
		return IssuedReport{}, ErrReportNotFinal
	}
	receipt, ok := record.Receipt(name)
	if !ok {
		return IssuedReport{}, ErrReportNotIssued
	}
	data, err := folder.readReport(ctx, name, receipt.Bytes)
	if err != nil {
		return IssuedReport{}, err
	}
	if reportReadHook != nil {
		reportReadHook()
	}
	if err := ctx.Err(); err != nil {
		return IssuedReport{}, err
	}
	// THE BYTES ARE THE RECEIPT'S OR THEY ARE NOT SERVED: an earlier run's file
	// of the same size, or one rewritten in place, does not hash to it.
	if IssueReportReceipt(name, data) != receipt {
		return IssuedReport{}, ErrReportNotReceipted
	}
	if err := folder.still(name); err != nil {
		return IssuedReport{}, err
	}
	// THE RECORD IS READ AGAIN AFTER THE FILE, THROUGH THE SAME FOLDER: a
	// relaunch replaces it before its process starts, so a record that still
	// names this launch, this run's clock and this receipt says no other run
	// began underneath the read.
	again, err := folder.record(ctx)
	if errors.Is(err, ErrReportNoRecord) || (err == nil && again.Launch != launch) {
		return IssuedReport{}, ErrReportMoved
	}
	if err != nil {
		return IssuedReport{}, err
	}
	if kept, ok := again.Receipt(name); !ok || kept != receipt || !again.StartedAt.Equal(record.StartedAt) || !again.EndedAt.Equal(record.EndedAt) {
		return IssuedReport{}, ErrReportChanged
	}
	return IssuedReport{Program: record.Name, Launch: launch, StartedAt: record.StartedAt, EndedAt: record.EndedAt,
		Receipt: receipt, Data: data}, nil
}
