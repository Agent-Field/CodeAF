package plan

import (
	"fmt"
	"strings"
)

// ProgressUpdate is the user-facing shape of planning movement. The planner's
// pass names stay behind this boundary; surfaces receive only calm language,
// an optional honest count, and real generated content when one just landed.
type ProgressUpdate struct {
	Phase  string `json:"phase"`
	Done   int    `json:"done"`
	Total  int    `json:"total"`
	Latest string `json:"latest"`
}

// Progress receives one replaceable snapshot of planning progress.
type Progress func(ProgressUpdate)

func emitProgress(callback Progress, stage, detail, latest string) {
	if callback == nil {
		return
	}
	callback(userProgress(stage, detail, latest))
}

// userProgress is the single vocabulary boundary between planner telemetry
// and a person watching a plan take shape.
func userProgress(stage, detail, latest string) ProgressUpdate {
	update := ProgressUpdate{Latest: strings.TrimSpace(latest)}
	switch stage {
	case "grounding", "grounded":
		update.Phase = "reading the request"
	case "spine":
		if done, total, ok := sampleCount(detail); ok {
			update.Phase, update.Done, update.Total = "exploring approaches", done, total
		} else {
			update.Phase = "choosing the shape"
		}
	case "ensemble", "fan-out", "sizing", "audit", "expand":
		update.Phase = "choosing the shape"
	case "steps":
		update.Phase = "breaking it into steps"
		if count := strings.TrimSpace(detail); count != "" {
			update.Phase += " — " + count
		}
	case "briefs":
		update.Phase = "writing the plan"
		update.Done, update.Total, _ = progressCount(detail)
	case "contracts":
		update.Phase = "setting working standards"
		update.Done, update.Total, _ = progressCount(detail)
	default:
		// Unknown planner passes still must not leak implementation vocabulary.
		update.Phase = "working out the plan"
	}
	return update
}

func sampleCount(detail string) (int, int, bool) {
	var done, total int
	if _, err := fmt.Sscanf(strings.TrimSpace(detail), "sample %d/%d", &done, &total); err != nil {
		return 0, 0, false
	}
	return validProgressCount(done, total)
}

func progressCount(detail string) (int, int, bool) {
	var done, total int
	if _, err := fmt.Sscanf(strings.TrimSpace(detail), "%d/%d", &done, &total); err != nil {
		return 0, 0, false
	}
	return validProgressCount(done, total)
}

func validProgressCount(done, total int) (int, int, bool) {
	if done < 0 || total < 0 || done > total {
		return 0, 0, false
	}
	return done, total, true
}

func nodeProgressTitle(node Node) string {
	if title := strings.TrimSpace(node.Title); title != "" {
		return title
	}
	text := strings.TrimSpace(node.Summary)
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		text = text[:index]
	}
	return strings.TrimSpace(text)
}
