package appwire

import (
	"strings"
	"time"
)

// TASKS.PROGRAM_REPORT: the exact bytes of one report file a specialist left in
// its task's record folder, for the launch the app is looking at, served only
// on the receipt that launch's program issued when it wrote the file (the
// bytes hash to it). The request names a task, a launch and a name
// ([TaskProgram.ReportFiles]); it never carries a path, and nothing in an
// ending's display names ([EndingFacts.Reports]) is a grant. See
// docs/design/app-v3/PROTOCOL.md.

// ProgramReportMax is the largest report served: 2 MiB of raw bytes. A bigger
// file is too_large with no bytes, never a cut one.
const ProgramReportMax = 2 << 20

// ProgramReportParams asks for one report of a task's program.
type ProgramReportParams struct {
	ID     string `json:"id"`
	Launch string `json:"launch"`
	Name   string `json:"name"`
}

// ProgramReportResult is one report, exactly as the file held it. Data is the
// file's bytes in standard padded base64 (a string, never an array of numbers)
// so the reader decodes them itself, strict UTF-8 and any byte-order mark
// included; Bytes is how many there are and SHA256 their lowercase hex digest,
// which is always the digest the launch's receipt names.
type ProgramReportResult struct {
	ID        string    `json:"id"`
	Program   string    `json:"program"`
	Launch    string    `json:"launch"`
	Name      string    `json:"name"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	Bytes     int       `json:"bytes"`
	SHA256    string    `json:"sha256"`
	Data      string    `json:"data"`
}

// ProgramReporter is the optional half of a Worker that serves a program's
// report: tasks.program_report. An engine without it refuses the method as
// unknown, which an app reads as "this engine cannot show the report".
type ProgramReporter interface {
	// ProgramReport answers the report name of the task's program for launch.
	// Failures are the wire's codes: a launch that is no longer the task's, or
	// a file that is not the bytes its receipt names, is a conflict; a launch
	// that issued no receipt for name is not_found; a file over
	// ProgramReportMax too_large; a read that could not be admitted or finished
	// in time busy. It never answers an empty report.
	ProgramReport(id, launch, name string) (ProgramReportResult, error)
}

func init() {
	methods["tasks.program_report"] = typed(func(e Engine, p ProgramReportParams) (*ProgramReportResult, error) {
		reporter, ok := e.(ProgramReporter)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve tasks.program_report")
		}
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Launch) == "" || strings.TrimSpace(p.Name) == "" {
			return nil, Errorf(CodeBadParams, "tasks.program_report needs an id, a launch and a name")
		}
		out, err := reporter.ProgramReport(p.ID, p.Launch, p.Name)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
