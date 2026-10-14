package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// combine reads the results.jsonl of several runs into one row list, marks the
// apps in b7 as earlier calls-road failures, and puts those first.
func combine(dirs []string, b7, verdicts string) ([]row, error) {
	marked := map[string]bool{}
	for _, id := range strings.Split(b7, ",") {
		marked[strings.TrimSpace(id)] = true
	}
	var rows []row
	for _, dir := range dirs {
		more, err := rowsOf(dir)
		if err != nil {
			return nil, err
		}
		rows = append(rows, more...)
	}
	rows = withoutJunk(rows)
	if err := applyVerdicts(rows, verdicts); err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].B7 = marked[rows[i].ID]
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].B7 && !rows[j].B7 })
	return rows, nil
}

func readRows(path string) ([]row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows []row
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var r row
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}
	return rows, sc.Err()
}

// junkID matches the ids the old reader gave to zoo "jobs:" lines (p02, p04...).
var junkID = regexp.MustCompile(`^p\d+$`)

func withoutJunk(rows []row) []row {
	var out []row
	for _, r := range rows {
		if !junkID.MatchString(r.ID) {
			out = append(out, r)
		}
	}
	return out
}

// rowsOf reads a finished run's results.jsonl, or, while the run is still
// going, rebuilds the finished rows from bench.log and lists the rest as running.
func rowsOf(dir string) ([]row, error) {
	if rows, err := readRows(filepath.Join(dir, "results.jsonl")); err == nil {
		return rows, nil
	}
	log, err := os.ReadFile(filepath.Join(dir, "bench.log"))
	if err != nil {
		return nil, err
	}
	rows := logRows(string(log))
	have := map[string]bool{}
	for _, r := range rows {
		have[r.ID+"/"+r.Road] = true
	}
	ps, err := readParagraphs(filepath.Join(dir, "apps.txt"))
	if err != nil {
		return rows, nil
	}
	for _, p := range ps {
		for _, road := range []string{"calls", "loop"} {
			if !have[p.ID+"/"+road] {
				rows = append(rows, row{ID: p.ID, Road: road, State: "running"})
			}
		}
	}
	return rows, nil
}

var (
	logKV   = regexp.MustCompile(`(used|check|opens|dry|keep|use|raw|first|total)=(\S*)`)
	logCost = regexp.MustCompile(`\$([0-9.]+)\s*(\S*)`)
)

// logRows parses the progress lines a run prints as each app ends.
func logRows(log string) []row {
	var rows []row
	for _, line := range strings.Split(log, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || (f[1] != "calls" && f[1] != "loop") {
			continue
		}
		kv := map[string]string{}
		for _, m := range logKV.FindAllStringSubmatch(line, -1) {
			kv[m[1]] = m[2]
		}
		r := row{ID: f[0], Road: f[1], State: f[2], RoadUsed: kv["used"], CheckPass: kv["check"] == "true",
			OpensInUse: kv["opens"] == "true", RunsDryOK: kv["dry"] == "true"}
		r.Stages = stageSet{Plan: f[2] != "plan-failed", Write: f[2] == "preview", Check: r.CheckPass, Preview: f[2] == "preview",
			Keep: kv["keep"] == "true", Use: kv["use"] == "true"}
		r.RawErrors, _ = strconv.Atoi(kv["raw"])
		r.FirstLineS, _ = strconv.ParseFloat(strings.TrimSuffix(kv["first"], "s"), 64)
		r.TotalS, _ = strconv.ParseFloat(strings.TrimSuffix(kv["total"], "s"), 64)
		if m := logCost.FindStringSubmatch(line); m != nil {
			r.CostUSD, _ = strconv.ParseFloat(m[1], 64)
			r.Cause = m[2]
		}
		rows = append(rows, r)
	}
	return rows
}

// applyVerdicts fills usefulness from a JSON file {"<road>/<id>": {verdict, scores}}
// written after the blind judge ran; no file leaves the column pending.
func applyVerdicts(rows []row, path string) error {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var v map[string]struct{ Verdict, Scores string }
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	for i := range rows {
		if j, ok := v[rows[i].Road+"/"+rows[i].ID]; ok {
			rows[i].Useful, rows[i].UsefulWhy = j.Verdict, j.Scores
		}
	}
	return nil
}
