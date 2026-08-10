// Package stalereaper ports src/session/stale-reaper.ts:1-113 from swe-pro
// commit 3b25a1a. LegacyReapSelection additionally preserves and isolates the
// dead packed-timestamp path at src/session/plandb-scheduler.ts:798-830 that
// the bundle requires kept bug-for-bug.
package stalereaper

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// QuietStaleReleaseMS is the quiet-wait orphan deadline (2.5 minutes).
const QuietStaleReleaseMS = 150_000

var activeStatuses = map[string]bool{"running": true, "claimed": true}

// ReapTaskRow is the subset of a PlanDB task row read by the quiet reaper.
// Timestamp fields stay as any because PlanStampToMS deliberately rejects
// malformed non-number values at runtime, just as the TS helper does.
type ReapTaskRow struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	StartedAt any    `json:"started_at"`
	ClaimedAt any    `json:"claimed_at"`
	UpdatedAt any    `json:"updated_at"`
}

// StaleReapInput is SelectStaleActiveTasks' input.
type StaleReapInput struct {
	Tasks       []*ReapTaskRow
	NowMS       float64
	ThresholdMS *float64
	LiveTaskIDs map[string]struct{}
}

func jsNumber(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

// PlanStampToMS recovers epoch milliseconds from a PlanDB packed timestamp.
// The bool is false for TS undefined: any non-number, non-finite, or
// non-positive value.
func PlanStampToMS(stamp any) (float64, bool) {
	n, ok := jsNumber(stamp)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 {
		return 0, false
	}
	return math.Floor(n / 4096), true
}

// LastActivityMS returns the greatest decodable activity timestamp.
func LastActivityMS(task *ReapTaskRow) (float64, bool) {
	if task == nil {
		return 0, false
	}
	var latest float64
	found := false
	for _, raw := range []any{task.UpdatedAt, task.StartedAt, task.ClaimedAt} {
		ms, ok := PlanStampToMS(raw)
		if !ok {
			continue
		}
		if !found {
			latest, found = ms, true
		} else {
			latest = math.Max(latest, ms)
		}
	}
	return latest, found
}

// SelectStaleActiveTasks returns stale running/claimed IDs in input order.
func SelectStaleActiveTasks(input StaleReapInput) []string {
	threshold := float64(QuietStaleReleaseMS)
	if input.ThresholdMS != nil {
		threshold = *input.ThresholdMS
	}
	out := []string{}
	for _, task := range input.Tasks {
		if task == nil || task.ID == "" {
			continue
		}
		if !activeStatuses[task.Status] {
			continue
		}
		if input.LiveTaskIDs != nil {
			if _, live := input.LiveTaskIDs[task.ID]; live {
				continue
			}
		}
		last, ok := LastActivityMS(task)
		if ok && input.NowMS-last < threshold {
			continue
		}
		out = append(out, task.ID)
	}
	return out
}

// LegacyReapTaskRow is the timestamp subset read by the old in-cycle reaper.
type LegacyReapTaskRow struct {
	ID            string `json:"id"`
	LastHeartbeat any    `json:"last_heartbeat"`
	StartedAt     any    `json:"started_at"`
	ClaimedAt     any    `json:"claimed_at"`
}

func jsTruthy(value any) bool {
	if value == nil {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v != ""
	default:
		if n, ok := jsNumber(value); ok {
			return n != 0 && !math.IsNaN(n)
		}
		return true
	}
}

func legacyStamp(row *LegacyReapTaskRow) any {
	if row == nil {
		return nil
	}
	if jsTruthy(row.LastHeartbeat) {
		return row.LastHeartbeat
	}
	if jsTruthy(row.StartedAt) {
		return row.StartedAt
	}
	return row.ClaimedAt
}

// legacyDateParse is the narrow Date.parse surface the legacy reaper reaches.
// Crucially, a packed numeric PlanDB stamp is first coerced with JS Number
// formatting and fails every date grammar, returning NaN.
func legacyDateParse(value any) float64 {
	var text string
	switch v := value.(type) {
	case string:
		text = v
	default:
		n, ok := jsNumber(value)
		if !ok {
			return math.NaN()
		}
		text = jscompat.FormatNumber(n)
	}
	for _, layout := range []string{time.RFC3339Nano, http.TimeFormat} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return float64(parsed.UnixMilli())
		}
	}
	// Date.parse also accepts a large legacy grammar. Numeric packed stamps do
	// not enter it successfully; keep a conservative numeric rejection before
	// the small date-only compatibility case below.
	if _, err := strconv.ParseFloat(text, 64); err == nil {
		return math.NaN()
	}
	if parsed, err := time.Parse("2006-01-02", text); err == nil {
		return float64(parsed.UnixMilli())
	}
	return math.NaN()
}

// LegacyReapSelection is the pure decision core of reapStaleRunningTasks.
// The finite check intentionally makes realistic packed PlanDB timestamps a
// no-op because legacyDateParse returns NaN. Do not decode packed stamps here.
func LegacyReapSelection(tasks []*LegacyReapTaskRow, nowMS, stallAfterMS float64) []string {
	out := []string{}
	for _, task := range tasks {
		if task == nil || task.ID == "" {
			continue
		}
		stamp := legacyStamp(task)
		if !jsTruthy(stamp) {
			continue
		}
		stampMS := legacyDateParse(stamp)
		if math.IsNaN(stampMS) || math.IsInf(stampMS, 0) {
			continue
		}
		if nowMS-stampMS < stallAfterMS {
			continue
		}
		out = append(out, task.ID)
	}
	return out
}
