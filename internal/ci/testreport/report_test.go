package testreport

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReadReportsTestsPackagesCacheAndIncompleteWork(t *testing.T) {
	in := strings.Join([]string{
		`{"Action":"start","Package":"example/a"}`,
		`{"Action":"run","Package":"example/a","Test":"TestSlow"}`,
		`{"Action":"pass","Package":"example/a","Test":"TestSlow","Elapsed":1.25}`,
		`{"Action":"output","Package":"example/a","Output":"ok  \texample/a\t(cached)\n"}`,
		`{"Action":"pass","Package":"example/a","Elapsed":1.5}`,
		`{"Action":"start","Package":"example/b"}`,
	}, "\n") + "\n"
	var progress strings.Builder
	report, err := Read(strings.NewReader(in), &progress, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if report.TestsPassed != 1 || len(report.Packages) != 1 || !report.Packages[0].Cached {
		t.Fatalf("report = %#v", report)
	}
	if report.Schema != 2 {
		t.Fatalf("schema = %d, want 2", report.Schema)
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

func TestReadCountsFailuresAndSortsSlowestFirst(t *testing.T) {
	in := strings.Join([]string{
		`{"Action":"start","Package":"example/fail"}`,
		`{"Action":"fail","Package":"example/fail","Test":"TestFast","Elapsed":0.1}`,
		`{"Action":"skip","Package":"example/fail","Test":"TestSlow","Elapsed":2.0}`,
		`{"Action":"fail","Package":"example/fail","Elapsed":2.2}`,
	}, "\n") + "\n"
	report, err := Read(strings.NewReader(in), &strings.Builder{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if report.TestsFailed != 1 || report.TestsSkipped != 1 || report.PackageFailures != 1 {
		t.Fatalf("failure counts = %#v", report)
	}
	if got := report.Tests[0].Name; got != "TestSlow" {
		t.Fatalf("slowest test = %q", got)
	}
}

func TestAPackageThatRanIsNotCalledCachedBecauseATestLoggedTheWord(t *testing.T) {
	in := strings.Join([]string{
		`{"Action":"start","Package":"example/a"}`,
		`{"Action":"output","Package":"example/a","Test":"TestOne","Output":"see (cached) here\n"}`,
		`{"Action":"pass","Package":"example/a","Test":"TestOne","Elapsed":0.3}`,
		`{"Action":"output","Package":"example/a","Output":"ok  \texample/a\t0.310s\n"}`,
		`{"Action":"pass","Package":"example/a","Elapsed":0.31}`,
	}, "\n") + "\n"
	report, err := Read(strings.NewReader(in), &strings.Builder{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Packages) != 1 || report.Packages[0].Cached {
		t.Fatalf("packages = %#v", report.Packages)
	}
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"cached"`) {
		t.Fatalf("report JSON says cached: %s", b)
	}
}

func TestGoPackageSummaryIsTheWitnessThatAResultWasCached(t *testing.T) {
	for _, summary := range []string{
		"ok  \texample/a\t(cached)\n",
		"ok  \texample/a\t(cached)\tcoverage: 61.0% of statements\n",
	} {
		t.Run(summary, func(t *testing.T) {
			in := strings.Join([]string{
				`{"Action":"start","Package":"example/a"}`,
				`{"Action":"output","Package":"example/a","Output":` + strconv.Quote(summary) + `}`,
				`{"Action":"pass","Package":"example/a","Elapsed":0.01}`,
			}, "\n") + "\n"
			report, err := Read(strings.NewReader(in), &strings.Builder{}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Packages) != 1 || !report.Packages[0].Cached {
				t.Fatalf("packages = %#v", report.Packages)
			}
		})
	}
}

func TestAStreamCutMidLineStillReportsEverythingKnownToBeIncomplete(t *testing.T) {
	complete := strings.Join([]string{
		`{"Action":"start","Package":"example/cut"}`,
		`{"Action":"run","Package":"example/cut","Test":"TestOne"}`,
		`{"Action":"pass","Package":"example/cut","Test":"TestOne","Elapsed":0.3}`,
	}, "\n") + "\n"
	last := `{"Action":"pass","Package":"example/cut","Elapsed":0.4}`
	in := complete + last[:len(last)/2]
	report, err := Read(strings.NewReader(in), &strings.Builder{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Truncated {
		t.Fatalf("Truncated = false; report = %#v", report)
	}
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"truncated":true`) {
		t.Fatalf("report JSON does not say it was truncated: %s", b)
	}
	if report.Events != 3 || report.TestsPassed != 1 {
		t.Fatalf("events and passed = %d, %d; report = %#v", report.Events, report.TestsPassed, report)
	}
	if len(report.Incomplete) != 1 || report.Incomplete[0] != "example/cut" {
		t.Fatalf("incomplete = %v", report.Incomplete)
	}
	// AND A RUN THAT ENDED TIDILY IS NOT CALLED CUT. go test's last line ends in a
	// newline, and a stream that trails a blank one has still said everything it
	// had to say; only half an event means somebody killed the run.
	whole, err := Read(strings.NewReader(complete+"\n"), &strings.Builder{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if whole.Truncated {
		t.Fatalf("a stream that ended on a line boundary was called cut: %#v", whole)
	}
}
