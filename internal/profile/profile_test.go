package profile

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

func TestMeasureReportsObservedSpread(t *testing.T) {
	measured := &Profile{}
	measured.Add(
		Record{Title: "largest", Size: "atomic", Turns: 9, Tokens: 9_000},
		Record{Title: "legacy overrun", Size: "borderline", Turns: 1, Tokens: 1_000, Stop: "budget"},
		Record{Title: "verdict overrun", Size: "oversized", Turns: 5, Tokens: 5_000, Verdict: provider.VerdictTurnCap},
		Record{Title: "direct", Size: BucketDirect, Turns: 100, Tokens: 100_000, Verdict: provider.VerdictBudgetStop},
	)

	got := measured.Measure()
	want := Spread{Samples: 3, MinTurns: 1, Median: 5, MaxTurns: 9, Overran: 2, MedianKb: 5}
	if got != want {
		t.Fatalf("Measure() = %+v, want %+v", got, want)
	}
}

func TestNeedsRecalibrationBranches(t *testing.T) {
	tests := []struct {
		name       string
		records    []Record
		want       bool
		wantReason string
	}{
		{
			name:       "nearly every task overran refuses",
			records:    repeatedRecords(8, 10, provider.VerdictBudgetStop),
			want:       false,
			wantReason: "points at the budget",
		},
		{
			name: "one quarter overran recalibrates",
			records: append(
				repeatedRecords(2, 10, provider.VerdictBudgetStop),
				repeatedRecords(6, 10, provider.VerdictVerifiedSuccess)...,
			),
			want:       true,
			wantReason: "ruler is too generous",
		},
		{
			name: "uniformly small tasks recalibrate",
			records: []Record{
				{Title: "one", Size: "atomic", Turns: 1},
				{Title: "two", Size: "atomic", Turns: 2},
				{Title: "three", Size: "atomic", Turns: 2},
				{Title: "four", Size: "atomic", Turns: 3},
				{Title: "five", Size: "atomic", Turns: 3},
				{Title: "six", Size: "atomic", Turns: 4},
				{Title: "seven", Size: "atomic", Turns: 5},
				{Title: "eight", Size: "atomic", Turns: 6},
			},
			want:       true,
			wantReason: "splitting work that did not need it",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			measured := &Profile{}
			measured.Add(test.records...)
			got, reason := measured.NeedsRecalibration()
			if got != test.want {
				t.Fatalf("NeedsRecalibration() = %v, %q; want %v", got, reason, test.want)
			}
			if !strings.Contains(reason, test.wantReason) {
				t.Fatalf("NeedsRecalibration() reason = %q, want it to contain %q", reason, test.wantReason)
			}
		})
	}
}

func TestOverranUsesStopOnlyForLegacyRecords(t *testing.T) {
	tests := []struct {
		name   string
		record Record
		want   bool
	}{
		{name: "legacy budget", record: Record{Stop: "budget"}, want: true},
		{name: "legacy turn cap", record: Record{Stop: "turn-cap"}, want: true},
		{name: "legacy completion", record: Record{Stop: "done"}, want: false},
		{name: "verdict overrun", record: Record{Verdict: provider.VerdictBudgetStop}, want: true},
		{
			name:   "verdict overrides stale stop",
			record: Record{Stop: "budget", Verdict: provider.VerdictVerifiedSuccess},
			want:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.record.Overran(); got != test.want {
				t.Fatalf("Overran() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestDirectBucketDoesNotBecomeRulerEvidence(t *testing.T) {
	measured := &Profile{}
	measured.Add(repeatedRecords(8, 40, provider.VerdictBudgetStop)...)
	for index := range measured.Records {
		measured.Records[index].Size = BucketDirect
	}
	// Atomic is the historical label for both planned leaves and old direct
	// jobs, so it remains valid evidence for backward compatibility.
	measured.Add(Record{Title: "legacy atomic", Size: "atomic", Turns: 7, Tokens: 2_000})

	spread := measured.Measure()
	if spread.Samples != 1 || spread.Median != 7 || spread.Overran != 0 {
		t.Fatalf("Measure() = %+v, want only the legacy atomic record", spread)
	}
	needed, reason := measured.NeedsRecalibration()
	if needed || !strings.Contains(reason, "1 samples") {
		t.Fatalf("NeedsRecalibration() = %v, %q; direct records must not meet the sample floor", needed, reason)
	}
	small, middle, large := measured.Evidence(3)
	if len(small) != 1 || len(middle) != 1 || len(large) != 0 {
		t.Fatalf("Evidence() lengths = %d, %d, %d; want 1, 1, 0", len(small), len(middle), len(large))
	}
	if small[0].Title != "legacy atomic" || middle[0].Title != "legacy atomic" {
		t.Fatalf("Evidence() included a direct record: small=%+v middle=%+v", small, middle)
	}
}

func TestSourceCountCompatibility(t *testing.T) {
	tests := []struct {
		name string
		json string
		want bool
	}{
		{name: "old omitted zero is unknown", json: `{"title":"old","sources":0}`, want: false},
		{name: "old nonzero remains known", json: `{"title":"old","sources":3}`, want: true},
		{name: "new explicit zero is known", json: `{"title":"new","sources":0,"sources_known":true}`, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var record Record
			if err := json.Unmarshal([]byte(test.json), &record); err != nil {
				t.Fatal(err)
			}
			if got := record.HasSourceCount(); got != test.want {
				t.Fatalf("HasSourceCount() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestAddRecordsSurpriseAgainstPriorBucketMedian(t *testing.T) {
	measured := &Profile{}
	baseline := make([]Record, MinSamples)
	for index := range baseline {
		baseline[index] = Record{Title: "baseline", Size: "atomic", Turns: 10, Tokens: 1_000}
	}
	for index, record := range measured.Add(baseline...) {
		if record.ExpectedTurns != nil || record.ExpectedTokens != nil || record.Surprise != nil {
			t.Fatalf("baseline record %d has an expectation before %d prior samples: %+v", index, MinSamples, record)
		}
	}

	landed := measured.Add(Record{Title: "miss", Size: "atomic", Turns: 30, Tokens: 4_000})[0]
	if landed.ExpectedTurns == nil || *landed.ExpectedTurns != 10 ||
		landed.ExpectedTokens == nil || *landed.ExpectedTokens != 1_000 {
		t.Fatalf("expectation = turns %v tokens %v, want 10/1000", landed.ExpectedTurns, landed.ExpectedTokens)
	}
	// Turn error is 200%, token error is 300%; their normalized mean is 250%.
	if landed.Surprise == nil || *landed.Surprise != 2.5 {
		t.Fatalf("surprise = %v, want 2.5", landed.Surprise)
	}

	exact := measured.Add(Record{Title: "exact", Size: "atomic", Turns: 10, Tokens: 1_000})[0]
	if exact.Surprise == nil || *exact.Surprise != 0 {
		t.Fatalf("exact surprise = %v, want defined zero", exact.Surprise)
	}
	other := measured.Add(Record{Title: "new bucket", Size: "borderline", Turns: 10, Tokens: 1_000})[0]
	if other.Surprise != nil {
		t.Fatalf("new bucket surprise = %v, want absent despite global history", *other.Surprise)
	}

	capped := measured.Add(Record{Title: "pathological", Size: "atomic", Turns: 1_000, Tokens: 100_000})[0]
	if capped.Surprise == nil || *capped.Surprise != maxSurprise {
		t.Fatalf("capped surprise = %v, want %.0f", capped.Surprise, maxSurprise)
	}
}

func TestSurpriseJSONIsBackwardCompatible(t *testing.T) {
	var old Record
	if err := json.Unmarshal([]byte(`{"title":"old","size":"atomic","turns":2,"tokens":200}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.ExpectedTurns != nil || old.ExpectedTokens != nil || old.Surprise != nil {
		t.Fatalf("legacy record gained a defined surprise: %+v", old)
	}

	encoded, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"expected_turns", "expected_tokens", "surprise"} {
		if strings.Contains(string(encoded), field) {
			t.Fatalf("absent legacy field %q was emitted: %s", field, encoded)
		}
	}

	var exact Record
	if err := json.Unmarshal([]byte(`{"expected_turns":2,"expected_tokens":200,"surprise":0}`), &exact); err != nil {
		t.Fatal(err)
	}
	if exact.Surprise == nil || *exact.Surprise != 0 {
		t.Fatalf("defined zero surprise did not survive JSON: %+v", exact)
	}
}

func repeatedRecords(count, turns int, verdict provider.Verdict) []Record {
	records := make([]Record, count)
	for index := range records {
		records[index] = Record{
			Title:   "task",
			Size:    "atomic",
			Turns:   turns,
			Verdict: verdict,
		}
	}
	return records
}

func TestReflexBucketRecordsSuccessPromotionAndCost(t *testing.T) {
	measured := &Profile{}
	measured.Add(
		Record{Title: "clean", Size: BucketReflex, Turns: 1, Tokens: 100, Cost: 0.01, Verdict: provider.VerdictVerifiedSuccess},
		Record{Title: "quick", Size: BucketReflex, Turns: 2, Tokens: 200, Cost: 0.02, Verdict: provider.VerdictUnverifiedSuccess},
		Record{Title: "promoted", Size: BucketReflex, Turns: 4, Tokens: 400, Cost: 0.04, Promoted: true, Verdict: provider.VerdictBudgetStop},
	)
	stats := measured.MeasureReflex()
	if stats.Samples != 3 || stats.Successes != 2 || stats.Promotions != 1 ||
		stats.MedianTurns != 2 || stats.MedianTokens != 200 ||
		stats.AverageCost < 0.0233 || stats.AverageCost > 0.0234 {
		t.Fatalf("MeasureReflex() = %+v", stats)
	}
	if spread := measured.Measure(); spread.Samples != 0 {
		t.Fatalf("reflex records leaked into planner ruler: %+v", spread)
	}
}
