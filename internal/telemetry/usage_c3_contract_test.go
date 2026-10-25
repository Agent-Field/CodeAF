package telemetry

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"regexp"
	"testing"
	"time"
)

func TestK13UsageReceiptEmitsOnlyRelayValidComponents(t *testing.T) {
	testHome(t)
	maxSafe, unsafe, maxInt := int(1<<53-1), int(1<<53), int(^uint(0)>>1)
	zero, one, seven, negative := 0, 1, 7, -1
	cases := []struct {
		name                string
		input, output       int
		d                   UsageDimensions
		status              string
		measured, estimated [2]*int
		total               *int
	}{
		{"reported_zero", 0, 0, UsageDimensions{UsageStatus: "reported"}, "reported", [2]*int{&zero, &zero}, [2]*int{}, nil},
		{"partial_zero", 0, 3, UsageDimensions{UsageStatus: "partial", MissingOutput: true}, "partial", [2]*int{&zero, nil}, [2]*int{}, nil},
		{"mixed_zero", 0, 0, UsageDimensions{UsageStatus: "mixed", MissingOutput: true, EstimatedOutput: &seven}, "mixed", [2]*int{&zero, nil}, [2]*int{nil, &seven}, nil},
		{"estimated_output", 0, 0, UsageDimensions{UsageStatus: "estimated", EstimatedOutput: &seven}, "estimated", [2]*int{}, [2]*int{nil, &seven}, nil},
		{"total_only_zero", 0, 0, UsageDimensions{UsageStatus: "total_only", MissingInput: true, MissingOutput: true, ReportedTotal: &zero}, "total_only", [2]*int{}, [2]*int{}, &zero},
		{"missing", 0, 0, UsageDimensions{UsageStatus: "missing"}, "missing", [2]*int{}, [2]*int{}, nil},
		{"negative_estimate", 0, 0, UsageDimensions{UsageStatus: "estimated", EstimatedOutput: &negative}, "missing", [2]*int{}, [2]*int{}, nil},
		{"negative_measurement", -1, 7, UsageDimensions{UsageStatus: "reported"}, "partial", [2]*int{nil, &seven}, [2]*int{}, nil},
		{"unsafe_estimate", 0, 0, UsageDimensions{UsageStatus: "estimated", EstimatedOutput: &unsafe}, "missing", [2]*int{}, [2]*int{}, nil},
		{"unsafe_measured_sum", maxSafe, 1, UsageDimensions{UsageStatus: "reported"}, "missing", [2]*int{}, [2]*int{}, nil},
		{"unknown_status_partial", 7, 0, UsageDimensions{UsageStatus: "bogus", MissingOutput: true}, "partial", [2]*int{&seven, nil}, [2]*int{}, nil},
		{"unknown_status_estimate", 0, 0, UsageDimensions{UsageStatus: "bogus", MissingInput: true, MissingOutput: true, EstimatedOutput: &seven}, "estimated", [2]*int{}, [2]*int{nil, &seven}, nil},
		{"overflowed_estimates", 0, 0, UsageDimensions{UsageStatus: "estimated", EstimatedInput: &maxInt, EstimatedOutput: &maxInt}, "missing", [2]*int{}, [2]*int{}, nil},
		{"malformed_total_only", 0, 7, UsageDimensions{UsageStatus: "total_only", ReportedTotal: &one}, "reported", [2]*int{&zero, &seven}, [2]*int{}, nil},
		{"estimate_without_receipt", 0, 0, UsageDimensions{UsageStatus: "estimated", EstimatedOutput: &seven}, "missing", [2]*int{}, [2]*int{}, nil},
		{"unsafe_estimated_sum", 0, 0, UsageDimensions{UsageStatus: "estimated", EstimatedInput: &maxSafe, EstimatedOutput: &one}, "missing", [2]*int{}, [2]*int{}, nil},
		{"unsafe_total_only", 0, 0, UsageDimensions{UsageStatus: "total_only", MissingInput: true, MissingOutput: true, ReportedTotal: &unsafe}, "missing", [2]*int{}, [2]*int{}, nil},
		{"safe_boundary", maxSafe, 0, UsageDimensions{UsageStatus: "reported"}, "reported", [2]*int{&maxSafe, &zero}, [2]*int{}, nil},
		{"unsafe_measurement", unsafe, 7, UsageDimensions{UsageStatus: "reported"}, "partial", [2]*int{nil, &seven}, [2]*int{}, nil},
	}
	data, err := os.ReadFile("testdata/usage-accounting.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Events []jsonEvent `json:"events"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Events) != 5+len(cases) {
		t.Fatalf("fixture events=%d", len(fixture.Events))
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.d
			d.ReceiptID = "generation:c3-" + tc.name
			if tc.name == "estimate_without_receipt" {
				d.ReceiptID = ""
			}
			d.EstimateScope = "text_only"
			d.AccountingSource = "provider"
			d.RoutingProvider = "other"
			d.ModelFamily = "qwen"
			at, err := time.Parse(time.RFC3339, fixture.Events[5+i].EventTime)
			if err != nil {
				t.Fatal(err)
			}
			event := UsageReceipt(ModeChat, tc.input, tc.output, "contract", at, d)
			p := event.Props
			if p["usage_status"] != tc.status {
				t.Fatalf("status: %v, want %s", p, tc.status)
			}
			for j, side := range []string{"input_tokens", "output_tokens"} {
				for _, group := range []struct {
					prefix string
					values [2]*int
				}{{"", tc.measured}, {"estimated_", tc.estimated}} {
					key := group.prefix + side
					if group.values[j] == nil {
						if _, ok := p[key]; ok {
							t.Fatalf("invented %s: %v", key, p)
						}
					} else if p[key] != *group.values[j] {
						t.Fatalf("%s: %v", key, p)
					}
				}
			}
			if tc.total != nil && p["total_tokens"] != *tc.total {
				t.Fatal(p)
			}
			if err := relayUsageC3(p); err != nil {
				t.Fatal(err)
			}
			// The shared fixture adds accepted constructor outputs without changing
			// the envelope or the relay fixture test's first estimate/recovery pair.
			if d.ReceiptID == "" {
				event.ID = fixture.Events[5+i].EventID
			}
			event.InstallHash = fixture.Events[5+i].InstallIDHash
			for _, key := range commonPropNames {
				event.Props[key] = fixture.Events[5+i].Props[key]
			}
			got, _ := json.Marshal(event)
			want, _ := json.Marshal(fixture.Events[5+i])
			var g, w any
			json.Unmarshal(got, &g)
			json.Unmarshal(want, &w)
			if !reflect.DeepEqual(g, w) {
				t.Fatalf("fixture: got %s want %s", got, want)
			}
		})
	}
}

// relayUsageC3 mirrors validateTokenUsage and its scalar schemas in
// /workspace/woi-82/site/src/lib/telemetry-usage.ts, including safe sums,
// presence-based status, disjoint components, and estimate receipt metadata.
func relayUsageC3(p map[string]any) error {
	fail := func(s string) error { return fmt.Errorf("%s: %v", s, p) }
	number := func(key string) (float64, bool) {
		v, ok := p[key]
		if !ok {
			return 0, false
		}
		switch v := v.(type) {
		case int:
			return float64(v), true
		case float64:
			return v, true
		default:
			return math.NaN(), true
		}
	}
	for _, key := range []string{"input_tokens", "output_tokens", "total_tokens", "estimated_input_tokens", "estimated_output_tokens", "estimated_total_tokens"} {
		if v, ok := number(key); ok && (math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1<<53-1 || math.Trunc(v) != v) {
			return fail("unsafe count")
		}
	}
	measured, estimated := 0, 0
	for _, side := range []string{"input_tokens", "output_tokens"} {
		_, m := p[side]
		_, e := p["estimated_"+side]
		if m && e {
			return fail("overlap")
		}
		if m {
			measured++
		}
		if e {
			estimated++
		}
	}
	status := p["usage_status"]
	_, hasTotal := p["total_tokens"]
	totalOnly := status == "total_only" && measured == 0 && estimated == 0 && hasTotal
	for _, prefix := range []string{"", "estimated_"} {
		if prefix == "" && totalOnly {
			continue
		}
		a, ak := number(prefix + "input_tokens")
		b, bk := number(prefix + "output_tokens")
		total, tk := number(prefix + "total_tokens")
		if !ak && !bk {
			if tk {
				return fail("orphan total")
			}
		} else if !tk || a+b > 1<<53-1 || total != a+b {
			return fail("inconsistent total")
		}
	}
	valid := false
	switch status {
	case "total_only":
		valid = totalOnly
	case "reported":
		valid = measured == 2 && estimated == 0
	case "estimated":
		valid = measured == 0 && estimated > 0
	case "mixed":
		valid = measured > 0 && estimated > 0
	case "partial":
		valid = measured == 1 && estimated == 0
	case "missing":
		valid = measured == 0 && estimated == 0
	}
	if !valid {
		return fail("status disagrees")
	}
	if receipt, exists := p["usage_receipt"]; exists {
		s, ok := receipt.(string)
		if !ok || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(s) {
			return fail("bad receipt")
		}
	}
	if estimated > 0 {
		if p["usage_receipt"] == nil || p["estimate_method"] != "utf8_bytes_4_v1" {
			return fail("missing estimate metadata")
		}
		switch p["estimate_scope"] {
		case "text_only", "multimodal_input", "observed_partial", "unknown":
		default:
			return fail("bad scope")
		}
	} else if p["estimate_method"] != nil || p["estimate_scope"] != nil {
		return fail("orphan metadata")
	}
	return nil
}
