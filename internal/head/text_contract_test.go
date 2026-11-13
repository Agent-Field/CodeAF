package head

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Text contract C5: the resident can read a predecessor's native path, with
// either drive-letter case, through the existing recorded-artifact boundary.
func TestTextContractReadNativeSummaryPath(t *testing.T) {
	graph := openHeadStore(t)
	node, _ := seedAssessmentJob(t, graph, assessmentVerdict)
	head := New(nil, graph)
	paths := resultFiles(node, deepFileCap)
	if len(paths) != 1 {
		t.Fatalf("recorded paths = %v", paths)
	}
	argument := paths[0]
	if runtime.GOOS == "windows" {
		argument = strings.ToLower(filepath.VolumeName(argument)) + filepath.ToSlash(argument[len(filepath.VolumeName(argument)):])
	}
	got, err := head.readArtifact(node, argument)
	if err != nil || !strings.Contains(got, assessmentVerdict) {
		t.Fatalf("read = %q, %v", got, err)
	}
}
