package tui

import "testing"

// TestTheTurnRuleMatchesWhateverTelemetryTheRecorderAdds pins the one property
// that kept quietly failing: the feed's turn rule is a regex over a line the
// flight recorder owns, and the recorder grows fields.
//
// cached= was added to that line when the prefix-cache discipline needed a way
// to be falsified, and hit= after it. Neither change touched this file, and
// neither had to: the pattern simply stopped matching, and every turn boundary
// in the node page silently degraded from a rule to an ordinary muted line. The
// fixtures in the older tests still used the pre-cached format, so nothing went
// red. This one uses the format the recorder actually writes today, and the
// last case uses a field that does not exist yet — which is the point.
func TestTheTurnRuleMatchesWhateverTelemetryTheRecorderAdds(t *testing.T) {
	tests := []struct {
		name, line, turn, out, note string
	}{
		{
			name: "the format before the cache work",
			line: "── turn 1  finish=stop  in=1372 out=45 ──",
			turn: "1", out: "45",
		},
		{
			name: "with the cache counts the recorder writes today",
			line: "── turn 2  finish=tool_calls  in=2007 out=58 cached=1856 hit=92% ──",
			turn: "2", out: "58",
		},
		{
			name: "telemetry and a note together",
			line: "── turn 3  finish=tool_calls  in=9000 out=12 cached=8600 hit=95%  [nudge] ──",
			turn: "3", out: "12", note: "nudge",
		},
		{
			name: "a field this feed has never heard of",
			line: "── turn 4  finish=stop  in=10 out=20 something=else ──",
			turn: "4", out: "20",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parts := feedTurnRule.FindStringSubmatch(test.line)
			if parts == nil {
				t.Fatalf("the turn rule did not match %q, so the feed draws it as an ordinary line", test.line)
			}
			if parts[1] != test.turn || parts[4] != test.out || parts[5] != test.note {
				t.Fatalf("matched %q as turn=%q out=%q note=%q, want turn=%q out=%q note=%q",
					test.line, parts[1], parts[4], parts[5], test.turn, test.out, test.note)
			}
		})
	}
}
