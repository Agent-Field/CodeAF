//go:build replay

package lane

// A replay bench: replay ~/.aforge/logs/calls.jsonl through the real
// ledger and chooser and score every pick against what the lanes measurably did
// around that moment. Not part of any suite; built only with -tags replay.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"testing"
	"time"
)

type replayRow struct {
	TS         string `json:"ts"`
	ID         string `json:"id"`
	Phase      string `json:"phase"`
	Model      string `json:"model"`
	Lane       string `json:"lane"`
	Served     string `json:"served"`
	Status     int    `json:"status"`
	MS         int    `json:"ms"`
	TTFT       int    `json:"ttft_ms"`
	Prompt     int    `json:"prompt_tokens"`
	Cached     int    `json:"cached_tokens"`
	Completion int    `json:"completion_tokens"`
	Reasoning  int    `json:"reasoning_tokens"`
	Tools      int    `json:"tools"`
	Tag        string `json:"tag"`
	Finish     string `json:"finish"`
	Error      string `json:"error"`
	at         time.Time
}

type sighting struct {
	at   time.Time
	ttft float64 // ms
	rate float64 // tok/s
}

var viaRE = regexp.MustCompile(`via ([A-Za-z0-9 .\-]+?)[:)]`)

func median(v []float64) float64 {
	if len(v) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[len(s)/2]
}

func TestReplayLog(t *testing.T) {
	path := os.Getenv("REPLAY_LOG")
	since := os.Getenv("REPLAY_SINCE")
	if path == "" {
		t.Skip("REPLAY_LOG unset")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []*replayRow
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var r replayRow
		if json.Unmarshal(sc.Bytes(), &r) != nil || r.TS < since {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, r.TS)
		if err != nil {
			continue
		}
		r.at = at
		rows = append(rows, &r)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].at.Before(rows[j].at) })
	ends := map[string]*replayRow{}
	sights := map[ID][]sighting{}
	for _, r := range rows {
		if r.Phase == "" && r.ID != "" {
			ends[r.ID] = r
		}
		if r.Phase == "" && r.Status == 200 && r.Served != "" && r.TTFT > 0 && r.Completion >= 32 && r.MS-r.TTFT > 300 {
			id := ID{Model: BareModel(r.Model), Lane: r.Served}
			sights[id] = append(sights[id], sighting{at: r.at, ttft: float64(r.TTFT), rate: float64(r.Completion) / (float64(r.MS-r.TTFT) / 1000)})
		}
	}
	// window estimate of a lane around a moment: median ttft and rate over ±15 min.
	const win = 15 * time.Minute
	estimate := func(id ID, at time.Time) (ttft, rate float64, n int) {
		var tt, rr []float64
		for _, s := range sights[id] {
			if s.at.After(at.Add(-win)) && s.at.Before(at.Add(win)) {
				tt = append(tt, s.ttft)
				rr = append(rr, s.rate)
			}
		}
		return median(tt), median(rr), len(tt)
	}
	lanesOf := map[string]map[string]bool{}
	for id := range sights {
		if lanesOf[id.Model] == nil {
			lanesOf[id.Model] = map[string]bool{}
		}
		lanesOf[id.Model][id.Lane] = true
	}

	type variant struct {
		name       string
		ledger     *ledger
		chooser    *chooser
		outcome429 bool
	}
	var kept struct {
		Beliefs []Belief `json:"beliefs"`
	}
	// REPLAY_FACTS=1 seeds each lane's sheet facts (prices, quantization,
	// limits) from the persisted ledger, so that a price-only policy (λ = 0) can
	// be reproduced. Off by default: the facts also carry the four-bit gate,
	// which refuses the lane the log measures as best for glm-5.3-flash, and a
	// bench comparing timing policies should not be deciding that question.
	if os.Getenv("REPLAY_FACTS") != "" {
		if raw, err := os.ReadFile(os.ExpandEnv("$HOME/.aforge/v3/lanes.json")); err == nil {
			_ = json.Unmarshal(raw, &kept)
		}
	}
	mk := func(name string, o bool) *variant {
		l := newLedger()
		l.readFrom(newSheet())
		for _, b := range kept.Beliefs {
			l.beliefs[b.ID] = Belief{ID: b.ID, Facts: b.Facts}
		}
		return &variant{name: name, ledger: l, chooser: &chooser{ledger: l, pages: newSheet()}, outcome429: o}
	}
	variants := []*variant{mk("as-shipped (429 teaches nothing)", false), mk("429 → refused outcome (availability axis)", true)}

	type tally struct {
		n, agree, noOpinion, pickRecent429, askedRecent429, oracleRecent429 int
		regretPick, regretAsked, regretRealized                             []float64
		picks, asked, oracle                                                map[string]int
	}
	stats := map[string]map[string]*tally{}
	for _, v := range variants {
		stats[v.name] = map[string]*tally{}
	}
	recent429 := map[string]map[string]time.Time{} // model → lane → last 429
	for _, r := range rows {
		model := BareModel(r.Model)
		if r.Phase == "start" {
			end := ends[r.ID]
			if r.Lane == "" || end == nil || len(lanesOf[model]) < 2 {
				continue
			}
			visible, hidden := 0, end.Completion
			if r.Tag == "turn" && end.Finish != "tool_calls" {
				visible = end.Completion - end.Reasoning
				hidden = end.Reasoning
			}
			if visible < 0 {
				visible = 0
			}
			req := Request{Model: model, PromptTokens: end.Prompt, Visible: visible, Hidden: hidden, Tools: r.Tools > 0, ValueOfTime: lambdaFor(r.Tag), QualityNeed: 0.9, Horizon: 50, Now: r.at}
			// oracle over every lane with a window estimate
			bestLane, best := "", math.Inf(1)
			perceivedOf := map[string]float64{}
			for lane := range lanesOf[model] {
				tt, rt, n := estimate(ID{Model: model, Lane: lane}, r.at)
				if n < 2 {
					continue
				}
				p := PerceivedSeconds(tt/1000, rt, visible, hidden)
				perceivedOf[lane] = p
				if p < best {
					best, bestLane = p, lane
				}
			}
			if bestLane == "" {
				continue
			}
			isRecent := func(lane string) bool {
				t, ok := recent429[model][lane]
				return ok && r.at.Sub(t) < 5*time.Minute
			}
			realized := math.NaN()
			if end.Status == 200 && end.TTFT > 0 && end.MS-end.TTFT > 300 && end.Completion > 0 {
				realized = PerceivedSeconds(float64(end.TTFT)/1000, float64(end.Completion)/(float64(end.MS-end.TTFT)/1000), visible, hidden)
			}
			for _, v := range variants {
				ta := stats[v.name][model]
				if ta == nil {
					ta = &tally{picks: map[string]int{}, asked: map[string]int{}, oracle: map[string]int{}}
					stats[v.name][model] = ta
				}
				ta.n++
				ta.oracle[bestLane]++
				ta.asked[r.Lane]++
				if isRecent(r.Lane) {
					ta.askedRecent429++
				}
				if isRecent(bestLane) {
					ta.oracleRecent429++
				}
				if !math.IsNaN(realized) {
					ta.regretRealized = append(ta.regretRealized, realized-best)
				}
				if p, ok := perceivedOf[r.Lane]; ok {
					ta.regretAsked = append(ta.regretAsked, p-best)
				}
				ch := v.chooser.Choose(req)
				if len(ch.Order) == 0 {
					ta.noOpinion++
					continue
				}
				pick := ch.Order[0]
				ta.picks[pick]++
				if pick == r.Lane {
					ta.agree++
				}
				if isRecent(pick) {
					ta.pickRecent429++
				}
				if p, ok := perceivedOf[pick]; ok {
					ta.regretPick = append(ta.regretPick, p-best)
				}
			}
			continue
		}
		// end rows teach
		if r.Status == 200 && r.Served != "" && r.TTFT > 0 && r.Completion > 0 {
			for _, v := range variants {
				v.ledger.Note(Sighting{ID: ID{Model: model, Lane: r.Served}, TTFT: time.Duration(r.TTFT) * time.Millisecond, Gen: time.Duration(r.MS-r.TTFT) * time.Millisecond, Tokens: r.Completion, PromptTokens: r.Prompt, CachedTokens: r.Cached, At: r.at})
				v.ledger.NoteOutcome(Outcome{ID: ID{Model: model, Lane: r.Served}, Accepted: true, At: r.at})
			}
		}
		if r.Status == 429 {
			lane := r.Lane
			if m := viaRE.FindStringSubmatch(r.Error); m != nil {
				lane = m[1]
			}
			if lane == "" {
				continue
			}
			if recent429[model] == nil {
				recent429[model] = map[string]time.Time{}
			}
			recent429[model][lane] = r.at
			for _, v := range variants {
				if v.outcome429 {
					v.ledger.NoteOutcome(Outcome{ID: ID{Model: model, Lane: lane}, Refused: true, Reason: "rate", At: r.at})
				}
			}
		}
	}
	top := func(m map[string]int, k int) string {
		type kv struct {
			k string
			v int
		}
		var s []kv
		for a, b := range m {
			s = append(s, kv{a, b})
		}
		sort.Slice(s, func(i, j int) bool { return s[i].v > s[j].v })
		out := ""
		for i := 0; i < len(s) && i < k; i++ {
			out += fmt.Sprintf("%s=%d ", s[i].k, s[i].v)
		}
		return out
	}
	mean := func(v []float64) float64 {
		if len(v) == 0 {
			return math.NaN()
		}
		s := 0.0
		for _, x := range v {
			s += x
		}
		return s / float64(len(v))
	}
	for _, v := range variants {
		fmt.Printf("\n===== %s =====\n", v.name)
		var models []string
		for m := range stats[v.name] {
			models = append(models, m)
		}
		sort.Slice(models, func(i, j int) bool { return stats[v.name][models[i]].n > stats[v.name][models[j]].n })
		for _, m := range models {
			ta := stats[v.name][m]
			if ta.n < 40 {
				continue
			}
			fmt.Printf("\n%s  starts=%d  chooser had no opinion=%d  pick==asked=%d\n", m, ta.n, ta.noOpinion, ta.agree)
			fmt.Printf("  picked a lane that 429'd <5min ago: replay=%d  real router=%d  (oracle would have: %d)\n", ta.pickRecent429, ta.askedRecent429, ta.oracleRecent429)
			fmt.Printf("  regret vs best lane in ±15min window (seconds of perceived wait): replay pick mean=%.1f med=%.1f | real asked mean=%.1f med=%.1f | realized mean=%.1f med=%.1f\n",
				mean(ta.regretPick), median(ta.regretPick), mean(ta.regretAsked), median(ta.regretAsked), mean(ta.regretRealized), median(ta.regretRealized))
			fmt.Printf("  oracle: %s\n  real asked: %s\n  replay picks: %s\n", top(ta.oracle, 5), top(ta.asked, 5), top(ta.picks, 5))
		}
	}
}

func lambdaFor(tag string) float64 {
	if os.Getenv("REPLAY_TASK_LAMBDA0") != "" && tag != "turn" && tag != "reflex" {
		return 0
	}
	return 90
}
