package ledgers

// Port of src/session/decision-ledger.ts:1-201 — append-only JSONL for
// scheduler decisions, plus the pure task-outcome scorer.
//
// Fidelity notes (deliberate, do not "fix"):
//   - BuildDecision accepts an injected `now`; only an absent value falls back
//     to Date.now().
//   - LoadDecisions returns the parsed objects themselves. Unknown keys,
//     source key order, and JS array-index-key ordering therefore remain
//     observable through MarshalJSON, using the jsVal layer in jsjson.go.
//   - ScoreDecisions emits decision-kind keys in first-appearance order, as
//     the TS Map accumulator and subsequent object assignments do.

import (
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/logshim"
	"github.com/Agent-Field/swe-pro-go/internal/session/leafoutcome"
)

var decisionLog = logshim.Create(map[string]any{"service": "session.decision-ledger"})

// DecisionKind mirrors the DecisionKind union.
type DecisionKind string

const (
	DecisionCut         DecisionKind = "cut"
	DecisionRootCut     DecisionKind = "root-cut"
	DecisionProbe       DecisionKind = "probe"
	DecisionEscalate    DecisionKind = "escalate"
	DecisionCoalesce    DecisionKind = "coalesce"
	DecisionAuditMode   DecisionKind = "audit-mode"
	DecisionReplanTick  DecisionKind = "replan-tick"
	DecisionTriage      DecisionKind = "triage"
	DecisionConvergence DecisionKind = "convergence"
)

// DecisionKinds mirrors DECISION_KINDS in declaration order.
var DecisionKinds = []DecisionKind{
	DecisionCut,
	DecisionRootCut,
	DecisionProbe,
	DecisionEscalate,
	DecisionCoalesce,
	DecisionAuditMode,
	DecisionReplanTick,
	DecisionTriage,
	DecisionConvergence,
}

var decisionKindSet = func() map[DecisionKind]bool {
	out := make(map[DecisionKind]bool, len(DecisionKinds))
	for _, kind := range DecisionKinds {
		out[kind] = true
	}
	return out
}()

// DecisionRecord mirrors DecisionRecord. Field order is the buildDecision
// object-literal order and therefore the JSONL wire order.
type DecisionRecord struct {
	Ts           float64      `json:"ts"`
	Kind         DecisionKind `json:"kind"`
	TaskID       string       `json:"taskID"`
	Model        string       `json:"model"`
	Band         string       `json:"band"`
	ReliableBand string       `json:"reliableBand"`
	Chosen       string       `json:"chosen"`
	Reason       string       `json:"reason"`
	KnobsHash    string       `json:"knobsHash"`

	raw *jsVal
}

// MarshalJSON preserves raw JSON.parse output for loaded rows and emits new
// records in the exact buildDecision property order.
func (r DecisionRecord) MarshalJSON() ([]byte, error) {
	if r.raw != nil {
		return r.raw.appendJSON(nil), nil
	}
	return decisionRecordVal(r).appendJSON(nil), nil
}

func decisionRecordVal(r DecisionRecord) jsVal {
	o := newJSObj()
	o.set("ts", numberVal(r.Ts))
	o.set("kind", stringVal(string(r.Kind)))
	o.set("taskID", stringVal(r.TaskID))
	o.set("model", stringVal(r.Model))
	o.set("band", stringVal(r.Band))
	o.set("reliableBand", stringVal(r.ReliableBand))
	o.set("chosen", stringVal(r.Chosen))
	o.set("reason", stringVal(r.Reason))
	o.set("knobsHash", stringVal(r.KnobsHash))
	return objectVal(o)
}

// BuildDecisionInput mirrors buildDecision's inline input object.
type BuildDecisionInput struct {
	Kind         DecisionKind       `json:"kind"`
	TaskID       string             `json:"taskID"`
	Model        string             `json:"model"`
	Band         string             `json:"band"`
	ReliableBand string             `json:"reliableBand"`
	Chosen       string             `json:"chosen"`
	Reason       string             `json:"reason"`
	KnobsHash    string             `json:"knobsHash"`
	Now          *jscompat.JSNumber `json:"now"`
}

// BuildDecision mirrors buildDecision. It is pure when Now is supplied.
func BuildDecision(input BuildDecisionInput) DecisionRecord {
	ts := float64(nowMillis())
	if input.Now != nil {
		ts = float64(*input.Now)
	}
	return DecisionRecord{
		Ts:           ts,
		Kind:         input.Kind,
		TaskID:       input.TaskID,
		Model:        input.Model,
		Band:         input.Band,
		ReliableBand: input.ReliableBand,
		Chosen:       input.Chosen,
		Reason:       input.Reason,
		KnobsHash:    input.KnobsHash,
	}
}

// AppendDecisionArgs mirrors appendDecision's inline argument object.
type AppendDecisionArgs struct {
	Workspace string         `json:"workspace"`
	Decision  DecisionRecord `json:"decision"`
}

const decisionLedgerFile = ".codeaf/decisions.jsonl"

// AppendDecision mirrors appendDecision. It is a best-effort sink and never
// returns an error.
func AppendDecision(args AppendDecisionArgs) {
	line, err := args.Decision.MarshalJSON()
	if err != nil {
		return
	}
	dir := filepath.Join(args.Workspace, ".codeaf")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		logDecisionAppendFailure(args.Decision, err)
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "decisions.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o666)
	if err != nil {
		logDecisionAppendFailure(args.Decision, err)
		return
	}
	_, writeErr := f.Write(append(line, '\n'))
	closeErr := f.Close()
	if writeErr != nil {
		logDecisionAppendFailure(args.Decision, writeErr)
	} else if closeErr != nil {
		logDecisionAppendFailure(args.Decision, closeErr)
	}
}

func logDecisionAppendFailure(decision DecisionRecord, err error) {
	msg := err.Error()
	if utf16Length(msg) > 200 {
		msg = utf16SliceTo(msg, 200)
	}
	decisionLog.Error("decision-ledger jsonl append failed", map[string]any{
		"taskID": decision.TaskID,
		"kind":   decision.Kind,
		"error":  msg,
	})
}

func isUsableDecision(v jsVal) bool {
	if v.kind != jsObject {
		return false
	}
	ts, ok := v.obj.get("ts")
	if !ok || ts.kind != jsNumber || !isFiniteJS(ts.num) {
		return false
	}
	kind, ok := v.obj.get("kind")
	if !ok || kind.kind != jsString || !decisionKindSet[DecisionKind(kind.str)] {
		return false
	}
	taskID, ok := v.obj.get("taskID")
	if !ok || taskID.kind != jsString || utf16Length(taskID.str) == 0 {
		return false
	}
	for _, field := range []string{"model", "band", "reliableBand", "chosen", "reason", "knobsHash"} {
		value, found := v.obj.get(field)
		if !found || value.kind != jsString {
			return false
		}
	}
	return true
}

func decisionFromVal(v jsVal) DecisionRecord {
	el := v
	r := DecisionRecord{raw: &el}
	if x, ok := v.obj.get("ts"); ok {
		r.Ts = x.num
	}
	if x, ok := v.obj.get("kind"); ok {
		r.Kind = DecisionKind(x.str)
	}
	if x, ok := v.obj.get("taskID"); ok {
		r.TaskID = x.str
	}
	if x, ok := v.obj.get("model"); ok {
		r.Model = x.str
	}
	if x, ok := v.obj.get("band"); ok {
		r.Band = x.str
	}
	if x, ok := v.obj.get("reliableBand"); ok {
		r.ReliableBand = x.str
	}
	if x, ok := v.obj.get("chosen"); ok {
		r.Chosen = x.str
	}
	if x, ok := v.obj.get("reason"); ok {
		r.Reason = x.str
	}
	if x, ok := v.obj.get("knobsHash"); ok {
		r.KnobsHash = x.str
	}
	return r
}

// LoadDecisions mirrors loadDecisions. jsonlPath is the file path itself, not
// a workspace. Missing files and malformed/unusable lines are ignored.
func LoadDecisions(jsonlPath string) []DecisionRecord {
	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		return []DecisionRecord{}
	}
	out := []DecisionRecord{}
	for _, line := range strings.Split(decodeUTF8Lossy(data), "\n") {
		trimmed := jscompat.Trim(line)
		if trimmed == "" {
			continue
		}
		parsed, ok := parseJSON(trimmed)
		if ok && isUsableDecision(parsed) {
			out = append(out, decisionFromVal(parsed))
		}
	}
	return out
}

// DecisionKindScore mirrors DecisionKindScore.
type DecisionKindScore struct {
	N                jscompat.JSNumber `json:"n"`
	SuccessRate      jscompat.JSNumber `json:"successRate"`
	MeanCostUsd      jscompat.JSNumber `json:"meanCostUsd"`
	MeanWallMs       jscompat.JSNumber `json:"meanWallMs"`
	MeanRepairRounds jscompat.JSNumber `json:"meanRepairRounds"`
}

// DecisionScores is the insertion-ordered Go representation of
// Partial<Record<DecisionKind, DecisionKindScore>>.
type DecisionScores struct {
	byKind *jscompat.OrderedMap[DecisionKind, DecisionKindScore]
}

// Get returns the score for kind.
func (s DecisionScores) Get(kind DecisionKind) (DecisionKindScore, bool) {
	if s.byKind == nil {
		return DecisionKindScore{}, false
	}
	return s.byKind.Get(kind)
}

// Len returns the number of scored kinds.
func (s DecisionScores) Len() int {
	if s.byKind == nil {
		return 0
	}
	return s.byKind.Len()
}

// Entries returns a snapshot in first-appearance order.
func (s DecisionScores) Entries() []jscompat.Entry[DecisionKind, DecisionKindScore] {
	if s.byKind == nil {
		return []jscompat.Entry[DecisionKind, DecisionKindScore]{}
	}
	return s.byKind.Entries()
}

// MarshalJSON preserves the property insertion order of the TS result object.
func (s DecisionScores) MarshalJSON() ([]byte, error) {
	dst := []byte{'{'}
	for i, entry := range s.Entries() {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = appendJSQuoted(dst, string(entry.Key))
		dst = append(dst, ':')
		dst = appendDecisionScoreJSON(dst, entry.Val)
	}
	return append(dst, '}'), nil
}

func appendDecisionScoreJSON(dst []byte, score DecisionKindScore) []byte {
	o := newJSObj()
	o.set("n", numberVal(float64(score.N)))
	o.set("successRate", numberVal(float64(score.SuccessRate)))
	o.set("meanCostUsd", numberVal(float64(score.MeanCostUsd)))
	o.set("meanWallMs", numberVal(float64(score.MeanWallMs)))
	o.set("meanRepairRounds", numberVal(float64(score.MeanRepairRounds)))
	return objectVal(o).appendJSON(dst)
}

type decisionScoreAccumulator struct {
	n         float64
	successes float64
	cost      float64
	wall      float64
	repairs   float64
}

// ScoreDecisions mirrors scoreDecisions. Nil outcomes model the optional
// `o?.taskID` guard reachable from type-violating JS callers.
func ScoreDecisions(decisions []DecisionRecord, outcomes []*leafoutcome.LeafOutcome) DecisionScores {
	byTask := map[string]*leafoutcome.LeafOutcome{}
	for _, outcome := range outcomes {
		if outcome != nil && outcome.TaskID != "" {
			byTask[outcome.TaskID] = outcome
		}
	}

	acc := jscompat.NewOrderedMap[DecisionKind, *decisionScoreAccumulator]()
	for _, decision := range decisions {
		if !decisionKindSet[decision.Kind] {
			continue
		}
		outcome := byTask[decision.TaskID]
		if outcome == nil {
			continue
		}
		a, ok := acc.Get(decision.Kind)
		if !ok {
			a = &decisionScoreAccumulator{}
			acc.Set(decision.Kind, a)
		}
		a.n++
		if outcome.Verdict == leafoutcome.VerdictPass || outcome.Verdict == leafoutcome.VerdictPassAfterRepair {
			a.successes++
		}
		a.cost += finiteOrZero(float64(outcome.CostUsd))
		a.wall += finiteOrZero(float64(outcome.WallMs))
		a.repairs += finiteOrZero(float64(outcome.RepairRounds))
	}

	out := DecisionScores{byKind: jscompat.NewOrderedMap[DecisionKind, DecisionKindScore]()}
	for _, entry := range acc.Entries() {
		a := entry.Val
		out.byKind.Set(entry.Key, DecisionKindScore{
			N:                jscompat.JSNumber(a.n),
			SuccessRate:      jscompat.JSNumber(a.successes / a.n),
			MeanCostUsd:      jscompat.JSNumber(a.cost / a.n),
			MeanWallMs:       jscompat.JSNumber(a.wall / a.n),
			MeanRepairRounds: jscompat.JSNumber(a.repairs / a.n),
		})
	}
	return out
}

func finiteOrZero(n float64) float64 {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0
	}
	return n
}
