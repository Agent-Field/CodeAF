package main

import (
	"strings"
	"testing"
)

func TestPercentileAndMean(t *testing.T) {
	xs := []float64{5, 1, 3, 2, 4, 10, 9, 8, 7, 6}
	if percentile(xs, 50) != 5 || percentile(xs, 90) != 9 || percentile(nil, 50) != 0 || mean([]float64{1, 3}) != 2 {
		t.Fatal("stats")
	}
}

func TestReportShape(t *testing.T) {
	rows := []row{
		{ID: "a", Family: "crm", Road: "calls", RoadUsed: "calls", CheckPass: true, Stages: stageSet{Plan: true, Write: true, Check: true, Preview: true}},
		{ID: "a", Family: "crm", Road: "loop", RoadUsed: "loop+fix", RawErrors: 1, RawSamples: []string{"json: x"}},
	}
	got := report("abc", rows)
	for _, want := range []string{"| calls | 1 | 1/1", "| loop | 1 | 0/1", "loop+fix 1", "| crm | calls | 1 | 1/1", "| crm | loop | 1 | 0/1", "json: x"} {
		if !strings.Contains(got, want) {
			t.Errorf("report lacks %q:\n%s", want, got)
		}
	}
}

func TestCoversAndTrace(t *testing.T) {
	if !covers([]string{"a", "b"}, []string{"a"}) || covers([]string{"a"}, []string{"a", "b"}) {
		t.Fatal("covers")
	}
}

func TestH1AndStageRecords(t *testing.T) {
	r := row{ID: "a", Road: "calls", State: "plan-failed", Error: `prompt: "budget" is not an app file`}
	r.Cause = causeOf(r.Error)
	recs := stageRecords(r)
	if r.Cause != "H1" || len(recs) != 1 || recs[0][0] != "plan" || recs[0][1] != "fail" {
		t.Fatalf("%+v %v", r, recs)
	}
	ok := row{Stages: stageSet{true, true, true, true, true, true}, CheckPass: true}
	if got := stageRecords(ok); len(got) != 7 || got[6][0] != "done" {
		t.Fatalf("%v", got)
	}
}

func TestParseVerdict(t *testing.T) {
	for in, want := range map[string]string{"verdict: THIN - sparse screens": "thin", "useless\nwhy": "useless", "Useful.": "useful", "hmm": "pending"} {
		if got, _ := parseVerdict(in); got != want {
			t.Errorf("%q: %s want %s", in, got, want)
		}
	}
}
