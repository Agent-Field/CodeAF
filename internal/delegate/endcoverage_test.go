package delegate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/secaf/schemas"
)

// endLineAndPage is an ending's `end` line as the file holds it and its Shown
// as the page carries it, through the program's real wire.
func endLineAndPage(t *testing.T, name string, end Ending) string {
	t.Helper()
	dir := t.TempDir()
	if err := AppendAction(dir, EndAction(endAt, terminalOf(t, end))); err != nil {
		t.Fatal(err)
	}
	line, err := os.ReadFile(filepath.Join(dir, ActionsFile))
	if err != nil {
		t.Fatal(err)
	}
	logged, _ := ReadActions(dir, 0)
	shown, ok := Delegate{Name: name}.Reader()(logged[0])
	if !ok {
		t.Fatal("the ending was not shown")
	}
	page, _ := json.Marshal(shown)
	return string(line) + string(page) + "\n"
}

func matchGolden(t *testing.T, name, got string) {
	t.Helper()
	want, err := os.ReadFile(filepath.Join("testdata", "end-"+name+".golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("the wire changed.\n got: %s\nwant: %s", got, want)
	}
}

// SEC'S REAL "COULD NOT READ" ENDING KEEPS ITS COVERAGE. sec writes the value
// of schemas.HuntCoverage{Unknown: true} into its ending, which marshals as
// {"attempted":0,"failures":null,"unknown":true}. The copy used to see the null
// failures and drop the whole account, so the line said nothing about coverage
// beside four counts — which reads as an audit whose every hunt finished. It is
// now kept as unknown, and only as unknown: the zero attempts are not a count.
func TestSecsRealUnknownCoverageIsGoldenAsUnknownOnly(t *testing.T) {
	end := Ending{Status: StatusPass, Message: "nothing it could show exploitable", Extra: map[string]any{
		"status": "pass", "reports": []string{"/records/sec-report.md", "/records/sec-report.json"}, "sessions": 6, "calls": 40,
		"confirmed": 0, "likely": 0, "inconclusive": 1, "ruled_out": 3,
		"hunt_coverage": schemas.HuntCoverage{Unknown: true}}}
	got := endLineAndPage(t, "sec", end)
	matchGolden(t, "sec-unknown", got)
	if strings.Contains(got, `"attempted"`) || strings.Contains(got, `"failures"`) {
		t.Fatalf("unknown coverage kept a count: %s", got)
	}
}

// THE COPY SAYS WHAT SEC'S OWN READER SAYS, OR LESS — NEVER MORE. Every case
// here is read by schemas.Coverage, the reader sec itself trusts, and by the
// `end` line's copy. The copy may only say unknown where sec reads a whole
// account when the spelling is one sec's own marshal never writes (a number
// with a point or an exponent, a key in another case); it may never read a
// whole account where sec reads unknown, and a coverage that is present is
// never absent.
func TestEndCoverageFollowsSecsOwnReader(t *testing.T) {
	failure := func(n int) string {
		rows := make([]string, n)
		for i := range rows {
			rows[i] = `{"strategy":"injection","reason":"session failed"}`
		}
		return "[" + strings.Join(rows, ",") + "]"
	}
	cases := []struct {
		name, raw string
		// stricter marks a spelling sec's marshal never writes, where the copy
		// says unknown and sec reads a count.
		stricter bool
	}{
		{"real unknown", `{"attempted":0,"failures":null,"unknown":true}`, false},
		{"attempted only", `{"attempted":4}`, false},
		{"more failures than attempts", `{"attempted":1,"failures":` + failure(2) + `}`, false},
		{"zero attempted", `{"attempted":0,"failures":[]}`, false},
		{"unknown beside a count", `{"attempted":4,"failures":[],"unknown":true}`, false},
		{"unknown false", `{"attempted":4,"failures":` + failure(1) + `,"unknown":false}`, false},
		{"unknown null", `{"attempted":4,"failures":` + failure(1) + `,"unknown":null}`, false},
		{"unknown a string", `{"attempted":4,"failures":[],"unknown":"yes"}`, false},
		{"null failures", `{"attempted":4,"failures":null}`, false},
		{"empty failures", `{"attempted":4,"failures":[]}`, false},
		{"one of four", `{"attempted":4,"failures":` + failure(1) + `}`, false},
		{"all failed", `{"attempted":3,"failures":` + failure(3) + `}`, false},
		{"sixteen of sixteen", `{"attempted":16,"failures":` + failure(16) + `}`, false},
		{"seventeen failures", `{"attempted":20,"failures":` + failure(17) + `}`, true},
		{"negative", `{"attempted":-1,"failures":[]}`, false},
		{"blank strategy", `{"attempted":2,"failures":[{"strategy":" ","reason":"x"}]}`, false},
		{"numeric reason", `{"attempted":2,"failures":[{"strategy":"a","reason":7}]}`, false},
		{"row a string", `{"attempted":2,"failures":["a"]}`, false},
		{"null row", `{"attempted":2,"failures":[null]}`, false},
		{"failures an object", `{"attempted":2,"failures":{}}`, false},
		{"not an object", `[1]`, false},
		{"a string", `"complete"`, false},
		{"null", `null`, false},
		{"case-variant unknown", `{"attempted":4,"failures":[],"Unknown":true}`, false},
		{"case-variant attempted", `{"Attempted":4,"failures":[]}`, true},
		{"case-variant row", `{"attempted":4,"failures":[{"Strategy":"a","reason":"b"}]}`, true},
		{"point", `{"attempted":4.0,"failures":[]}`, true},
		{"exponent", `{"attempted":4e0,"failures":[]}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var metadata map[string]any
			if err := json.Unmarshal([]byte(`{"hunt_coverage":`+tc.raw+`}`), &metadata); err != nil {
				t.Fatal(err)
			}
			sec := schemas.Coverage(metadata)
			facts := EndAction(endAt, Terminal{Status: StatusPass, Data: map[string]json.RawMessage{
				"hunt_coverage": json.RawMessage(tc.raw), "confirmed": json.RawMessage(`0`)}}).Ending()
			if facts == nil || facts.HuntCoverage == nil {
				t.Fatalf("a present coverage is absent; sec reads %+v", sec)
			}
			ours := *facts.HuntCoverage
			if ours.Unknown {
				if ours.Attempted != nil || ours.Failures != nil {
					t.Fatalf("unknown kept a count: %+v", ours)
				}
				if !sec.Unknown && !tc.stricter {
					t.Fatalf("the copy says unknown where sec reads %+v", sec)
				}
				return
			}
			if sec.Unknown {
				t.Fatalf("the copy reads %+v where sec reads unknown", ours)
			}
			if tc.stricter {
				t.Fatalf("a spelling sec never writes read as %+v", ours)
			}
			want := make([]HuntFailure, len(sec.Failures))
			for i, f := range sec.Failures {
				want[i] = HuntFailure{Strategy: f.Strategy, Reason: f.Reason}
			}
			if ours.Attempted == nil || *ours.Attempted != sec.Attempted || len(ours.Failures) != len(want) ||
				(len(want) > 0 && !reflect.DeepEqual(ours.Failures, want)) {
				t.Fatalf("the copy reads %+v, sec reads %+v", ours, sec)
			}
		})
	}
}

// A REPORT NAME IS A PATH AND IS NEVER REWRITTEN. sec names its reports by
// absolute path, so a long records folder is a real path past the cap; the
// copy used to cut it, or fold its spaces, into a path naming a different
// file. Now one such name drops the whole list.
func TestAReportPathIsKeptExactlyOrTheListIsDropped(t *testing.T) {
	long := "/" + strings.Repeat("r", 300) + "/sec-report.md"
	exact := "/" + strings.Repeat("r", endReportCap-1)
	for name, list := range map[string][]string{
		"too long":       {"/records/sec-report.md", long},
		"doubled space":  {"/records/my  run/sec-report.md"},
		"edge space":     {" /records/sec-report.md"},
		"newline":        {"/records/sec\nreport.md"},
		"tab":            {"/records/sec\treport.md"},
		"empty":          {"/records/sec-report.md", ""},
		"seventeen kept": make17(),
	} {
		raw, _ := json.Marshal(list)
		facts := EndAction(endAt, Terminal{Status: StatusPass, Data: map[string]json.RawMessage{
			"reports": raw, "confirmed": json.RawMessage(`1`)}}).Ending()
		if facts == nil || facts.Reports != nil {
			t.Errorf("%s: reports kept as %q", name, facts.Reports)
		}
	}
	kept := []string{"/records/my run/sec-report.md", exact}
	raw, _ := json.Marshal(kept)
	facts := EndAction(endAt, Terminal{Status: StatusPass, Data: map[string]json.RawMessage{"reports": raw}}).Ending()
	if facts == nil || !reflect.DeepEqual(facts.Reports, kept) {
		t.Fatalf("a good list read as %+v", facts)
	}
}

func make17() []string {
	names := make([]string, endReportsMax+1)
	for i := range names {
		names[i] = "/records/r.md"
	}
	return names
}

// A COVERAGE IS BLURRED, NOT DROPPED, TO HOLD THE CAP: counts with no coverage
// beside them would read as a whole audit, so a line too big to keep its
// account keeps unknown instead.
func TestTheCapBlursACoverageAndNeverDropsIt(t *testing.T) {
	loud := mustJSON(t, strings.Repeat("\x01", endTextCap))
	data := map[string]json.RawMessage{
		"claim": loud, "observed": loud, "analysis_incomplete": loud, "submission_reason": loud,
		"confirmed":     json.RawMessage(`2`),
		"hunt_coverage": json.RawMessage(`{"attempted":3,"failures":[{"strategy":"injection","reason":"` + strings.Repeat("why ", 50) + `"}]}`),
	}
	action := EndAction(endAt, Terminal{Status: StatusPass, Data: data})
	facts := action.Ending()
	if len(action.Data) > EndDataCap || facts == nil || !reflect.DeepEqual(facts.HuntCoverage, &HuntCoverage{Unknown: true}) {
		t.Fatalf("%d bytes, facts %+v", len(action.Data), facts)
	}
}
