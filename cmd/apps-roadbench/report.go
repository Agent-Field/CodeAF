package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var csvHead = []string{"id", "family", "road", "road_used", "state", "check_pass", "opens_in_use", "runs_dry_ok",
	"raw_errors", "time_to_first_line_s", "total_s", "cost_usd", "steps", "plan", "write", "check", "preview", "keep", "use", "cause", "error"}

func (r row) csv() []string {
	b := strconv.FormatBool
	f := func(x float64) string { return strconv.FormatFloat(x, 'f', 4, 64) }
	return []string{r.ID, r.Family, r.Road, r.RoadUsed, r.State, b(r.CheckPass), b(r.OpensInUse), b(r.RunsDryOK),
		strconv.Itoa(r.RawErrors), f(r.FirstLineS), f(r.TotalS), f(r.CostUSD), strconv.Itoa(r.Steps),
		b(r.Stages.Plan), b(r.Stages.Write), b(r.Stages.Check), b(r.Stages.Preview), b(r.Stages.Keep), b(r.Stages.Use), r.Cause, r.Error}
}

func writeCSV(path string, rows []row) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write(csvHead)
	for _, r := range rows {
		_ = w.Write(r.csv())
	}
	w.Flush()
	return w.Error()
}

func writeJSONL(path string, rows []row) error {
	var sb strings.Builder
	for _, r := range rows {
		raw, err := json.Marshal(r)
		if err != nil {
			return err
		}
		sb.Write(raw)
		sb.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// writeAll writes the CSV, the JSONL and REPORT.md under dir.
func writeAll(dir, build string, rows []row) error {
	if err := writeCSV(filepath.Join(dir, "results.csv"), rows); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(dir, "results.jsonl"), rows); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "REPORT.md"), []byte(report(build, rows)), 0o644)
}

func report(build string, rows []row) string {
	var sb strings.Builder
	roads := roadsOf(rows)
	fmt.Fprintf(&sb, "# Builder road bench\n\nPath: served-bridge (real `codeaf app serve`, bridge WebSocket). Binary commit: %s\n\n## Totals per road\n\n", build)
	sb.WriteString("| road | apps | reach Use | useful/thin/useless | check pass | opens in Use | runs dry ok | raw errors | first line med/p90 s | total med/p90 s | mean cost $ | rungs |\n|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	done := finished(rows)
	for _, road := range roads {
		sb.WriteString(totals(road, byRoad(done, road)) + "\n")
	}
	if n := len(rows) - len(done); n > 0 {
		fmt.Fprintf(&sb, "\nTotals count finished apps only. %d app-runs are still running and are listed as running below.\n", n)
	}
	sb.WriteString("\n## Per app\n\n| id | road | reach_use | check_pass | usefulness | road_used | raw_errors | first_line_s | total_s | cost | state | cause |\n|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&sb, "| %s | %s | %v | %v | %s | %s | %d | %.1f | %.0f | %.4f | %s | %s |\n", idCell(r), r.Road, r.Stages.Use, r.CheckPass,
			orPending(r.Useful), r.RoadUsed, r.RawErrors, r.FirstLineS, r.TotalS, r.CostUSD, r.State, r.Cause)
	}
	sb.WriteString(rawSection(rows))
	sb.WriteString("\n## MATRIX\n\n" + matrix(done, roads))
	return sb.String()
}

func roadsOf(rows []row) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if !seen[r.Road] {
			seen[r.Road] = true
			out = append(out, r.Road)
		}
	}
	sort.Strings(out)
	return out
}

func byRoad(rows []row, road string) []row {
	var out []row
	for _, r := range rows {
		if r.Road == road {
			out = append(out, r)
		}
	}
	return out
}

func count(rows []row, pred func(row) bool) int {
	n := 0
	for _, r := range rows {
		if pred(r) {
			n++
		}
	}
	return n
}

func totals(road string, rs []row) string {
	n := len(rs)
	first, total := column(rs, func(r row) float64 { return r.FirstLineS }), column(rs, func(r row) float64 { return r.TotalS })
	cost := column(rs, func(r row) float64 { return r.CostUSD })
	raw := 0
	for _, r := range rs {
		raw += r.RawErrors
	}
	rungs := map[string]int{}
	for _, r := range rs {
		rungs[orDash(r.RoadUsed)]++
	}
	return fmt.Sprintf("| %s | %d | %d/%d | %s | %d/%d | %d/%d | %d/%d | %d | %.1f / %.1f | %.0f / %.0f | %.4f | %s |", road, n,
		count(rs, func(r row) bool { return r.Stages.Use }), n, usefulText(rs), count(rs, func(r row) bool { return r.CheckPass }), n, count(rs, func(r row) bool { return r.OpensInUse }), n,
		count(rs, func(r row) bool { return r.RunsDryOK }), n, raw,
		percentile(first, 50), percentile(first, 90), percentile(total, 50), percentile(total, 90), mean(cost), rungText(rungs))
}

func column(rs []row, f func(row) float64) []float64 {
	out := make([]float64, len(rs))
	for i, r := range rs {
		out[i] = f(r)
	}
	return out
}

// percentile is the nearest-rank percentile of xs.
func percentile(xs []float64, p int) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	rank := (p*len(s) + 99) / 100
	return s[max(rank, 1)-1]
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func orDash(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func rungText(m map[string]int) string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, k := range names {
		parts[i] = fmt.Sprintf("%s %d", k, m[k])
	}
	return strings.Join(parts, ", ")
}

// rawSection lists the raw-looking lines, so a count can be checked by eye.
func rawSection(rows []row) string {
	var sb strings.Builder
	for _, r := range rows {
		for _, s := range r.RawSamples {
			fmt.Fprintf(&sb, "- %s/%s: `%s`\n", r.ID, r.Road, strings.ReplaceAll(s, "`", "'"))
		}
	}
	if sb.Len() == 0 {
		return ""
	}
	return "\n## Raw-looking lines (first 3 per app)\n\n" + sb.String()
}

// matrix is stages reached per component family and road: plan, write, check,
// preview, each as reached/apps.
func matrix(rows []row, roads []string) string {
	families := map[string]bool{}
	for _, r := range rows {
		for _, f := range tagsOf(r.Family) {
			families[f] = true
		}
	}
	names := make([]string, 0, len(families))
	for f := range families {
		names = append(names, f)
	}
	sort.Strings(names)
	var sb strings.Builder
	sb.WriteString("| family | road | apps | plan | write | check | preview | keep | use |\n|---|---|---|---|---|---|---|---|---|\n")
	for _, fam := range names {
		for _, road := range roads {
			rs := inFamily(byRoad(rows, road), fam)
			if len(rs) == 0 {
				continue
			}
			n := len(rs)
			fmt.Fprintf(&sb, "| %s | %s | %d | %d/%d | %d/%d | %d/%d | %d/%d | %d/%d | %d/%d |\n", fam, road, n,
				count(rs, func(r row) bool { return r.Stages.Plan }), n, count(rs, func(r row) bool { return r.Stages.Write }), n,
				count(rs, func(r row) bool { return r.Stages.Check }), n, count(rs, func(r row) bool { return r.Stages.Preview }), n,
				count(rs, func(r row) bool { return r.Stages.Keep }), n, count(rs, func(r row) bool { return r.Stages.Use }), n)
		}
	}
	return sb.String()
}

// tagsOf splits the comma list of an app's families and value types.
func tagsOf(family string) []string {
	if family == "" {
		return []string{"none"}
	}
	return strings.Split(family, ",")
}

func inFamily(rs []row, fam string) []row {
	var out []row
	for _, r := range rs {
		for _, f := range tagsOf(r.Family) {
			if f == fam {
				out = append(out, r)
			}
		}
	}
	return out
}

func orPending(s string) string {
	if s == "" {
		return "pending"
	}
	return s
}

// usefulText is "useful/thin/useless" counts, with pending ones noted.
func usefulText(rs []row) string {
	c := func(w string) int { return count(rs, func(r row) bool { return orPending(r.Useful) == w }) }
	text := fmt.Sprintf("%d/%d/%d", c("useful"), c("thin"), c("useless"))
	if p := c("pending"); p > 0 {
		text += fmt.Sprintf(" (+%d pending)", p)
	}
	return text
}

// idCell marks the apps that failed on the earlier calls road.
func idCell(r row) string {
	if r.B7 {
		return r.ID + " (b7-fail)"
	}
	return r.ID
}
