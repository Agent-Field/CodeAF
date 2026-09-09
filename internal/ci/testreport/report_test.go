package testreport

import (
	"strings"
	"testing"
	"time"
)

func TestReadReportsTestsPackagesCacheAndIncompleteWork(t *testing.T) {
	in := strings.Join([]string{
		`{"Action":"start","Package":"example/a"}`,
		`{"Action":"run","Package":"example/a","Test":"TestSlow"}`,
		`{"Action":"pass","Package":"example/a","Test":"TestSlow","Elapsed":1.25}`,
		`{"Action":"output","Package":"example/a","Output":"ok example/a (cached)\n"}`,
		`{"Action":"pass","Package":"example/a","Elapsed":1.5}`,
		`{"Action":"start","Package":"example/b"}`,
	}, "\n")
	var progress strings.Builder
	report, err := Read(strings.NewReader(in), &progress, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if report.TestsPassed != 1 || len(report.Packages) != 1 || !report.Packages[0].Cached {
		t.Fatalf("report = %#v", report)
	}
	if len(report.Incomplete) != 1 || report.Incomplete[0] != "example/b" {
		t.Fatalf("incomplete = %v", report.Incomplete)
	}
	if !strings.Contains(progress.String(), "TestSlow (1.250s)") {
		t.Fatalf("progress = %q", progress.String())
	}
}

func TestReadRejectsMalformedAndEmptyStreams(t *testing.T) {
	for _, input := range []string{"", "not-json\n"} {
		if _, err := Read(strings.NewReader(input), &strings.Builder{}, time.Now()); err == nil {
			t.Fatalf("Read(%q) succeeded", input)
		}
	}
}
