//go:build !linux && !darwin

package delegate

import "context"

// reportFolder opens nothing here: this build has no descriptor-relative open
// it has been proved on, so it cannot promise a link is not followed, and it
// serves no report.
type reportFolder struct{}

func openReportFolder(string) (*reportFolder, error) { return nil, ErrReportUnsupported }

func (*reportFolder) close() {}
func (*reportFolder) record(context.Context) (ProgramRecord, error) {
	return ProgramRecord{}, ErrReportUnsupported
}
func (*reportFolder) readReport(context.Context, string, int64) ([]byte, error) {
	return nil, ErrReportUnsupported
}
func (*reportFolder) still(string) error { return ErrReportUnsupported }
