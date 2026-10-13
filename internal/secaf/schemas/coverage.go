package schemas

import (
	"encoding/json"
	"fmt"
	"strings"
)

// HuntCoverage is measured by the phase, never inferred from a model's answer
// or the number of findings. Each selected strategy invocation is one attempt,
// including repeated strategies; failures stay in selection order.
type HuntCoverage struct {
	Attempted int           `json:"attempted"`
	Failures  []HuntFailure `json:"failures"`
	// Unknown marks unreadable persisted accounting, never a measured count.
	Unknown bool `json:"unknown,omitempty"`
}

type HuntFailure struct {
	Strategy string `json:"strategy"`
	Reason   string `json:"reason"`
}

func (c HuntCoverage) Incomplete() bool { return c.Unknown || len(c.Failures) > 0 }
func (c HuntCoverage) AllFailed() bool  { return c.Attempted > 0 && len(c.Failures) == c.Attempted }

func (c HuntCoverage) Description() string {
	if c.Unknown {
		return "hunt accounting could not be read"
	}
	return fmt.Sprintf("%d of %d hunts failed", len(c.Failures), c.Attempted)
}

// Coverage reads the optional report metadata after either a local run or JSON
// round trip. Malformed accounting cannot become a claim of completed analysis.
func Coverage(metadata map[string]any) HuntCoverage {
	raw, present := metadata["hunt_coverage"]
	if !present {
		return HuntCoverage{}
	}
	data, err := json.Marshal(raw)
	var fields struct {
		Attempted *int           `json:"attempted"`
		Failures  *[]HuntFailure `json:"failures"`
	}
	if err == nil {
		err = json.Unmarshal(data, &fields)
	}
	if err != nil || fields.Attempted == nil || fields.Failures == nil || *fields.Attempted < 1 || len(*fields.Failures) > *fields.Attempted {
		return HuntCoverage{Unknown: true}
	}
	coverage := HuntCoverage{Attempted: *fields.Attempted, Failures: *fields.Failures}
	for _, failure := range coverage.Failures {
		if strings.TrimSpace(failure.Strategy) == "" || strings.TrimSpace(failure.Reason) == "" {
			return HuntCoverage{Unknown: true}
		}
	}
	return coverage
}

// Interruption is a global stop after a partial result was produced. It is
// separate from hunt coverage because a ceiling or refusal can arrive later.
func Interruption(metadata map[string]any) string {
	text, _ := metadata["analysis_incomplete"].(string)
	return text
}
