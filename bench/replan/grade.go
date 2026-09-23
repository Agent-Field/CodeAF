package main

// grade.go: the verdicts, taken by code. The fixture's own suite is the
// judge, every test file must still hold its seed bytes, and each cell adds
// one mechanical check of its own. No model judges anything.

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type grade struct {
	Pass   bool
	Detail string
}

func gradeCell(c cell, dir string, pristine fixtureFiles) grade {
	if changed := testsChanged(dir, pristine); len(changed) > 0 {
		return grade{Detail: "a test file was changed or removed: " + strings.Join(changed, " ")}
	}
	if out, err := goTest(dir); err != nil {
		return grade{Detail: "suite not green: " + tail(out, 3)}
	}
	switch c.id {
	case "r2":
		if _, err := os.Stat(filepath.Join(dir, "legacy")); err == nil {
			return grade{Detail: "the suite is green but the legacy package is still there"}
		}
		if importers := importersOf(dir, `"bloop/shop/legacy"`); len(importers) > 0 {
			return grade{Detail: "still imports legacy: " + strings.Join(importers, " ")}
		}
		return grade{Pass: true, Detail: "suite green, tests untouched, legacy gone"}
	case "r3":
		return grade{Pass: true, Detail: "suite green (money, report and export), tests untouched"}
	}
	return grade{Pass: true, Detail: "suite green, tests untouched"}
}

// testsChanged names every seed test file that no longer holds its bytes.
func testsChanged(dir string, pristine fixtureFiles) []string {
	var changed []string
	for name, want := range pristine {
		if !strings.HasSuffix(name, "_test.go") {
			continue
		}
		have, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil || string(have) != string(want) {
			changed = append(changed, name)
		}
	}
	sort.Strings(changed)
	return changed
}

// importersOf names the Go files under dir whose source carries needle.
func importersOf(dir, needle string) []string {
	var found []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != dir {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(body), needle) {
			rel, _ := filepath.Rel(dir, path)
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(found)
	return found
}

// goTest runs the fixture's own suite, fresh.
func goTest(dir string) (string, error) {
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off", "GOFLAGS=")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func tail(out string, n int) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// ── the summary ────────────────────────────────────────────────────────────

// summarize prints one line per cell and arm: passes, and the medians of the
// numbers the design doc quotes, over every row that ran.
func summarize(p plan, rows []row) string {
	var b strings.Builder
	fmt.Fprintf(&b, "| cell | arm | n | pass | median $ | median wall s | median steps | median tasks | checks (not holding) | fixes | peak workers | root wakes | root wake $ | overlap files | cell measure |\n")
	fmt.Fprintf(&b, "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, c := range p.Cells {
		for _, a := range p.Arms {
			var mine []row
			for _, r := range rows {
				if r.Cell == c.id && r.Arm == a.Name {
					mine = append(mine, r)
				}
			}
			if len(mine) == 0 {
				continue
			}
			passes, checks, notHolds, fixes, wakes, overlap := 0, 0, 0, 0, 0, 0
			var usd, wall, steps, tasks, peak, wakeUSD []float64
			var measures []string
			for _, r := range mine {
				if r.Pass {
					passes++
				}
				checks += r.Checks
				notHolds += r.CheckNotHolds
				fixes += r.Fixes
				wakes += max(r.RootLaunches-1, 0)
				overlap += r.OverlapFiles
				usd = append(usd, r.USDCalls)
				wall = append(wall, r.WallSeconds)
				steps = append(steps, float64(r.Steps))
				tasks = append(tasks, float64(r.Tasks))
				peak = append(peak, r.PeakParallel)
				wakeUSD = append(wakeUSD, r.RootWakeUSD)
				measures = append(measures, r.CellMetric)
			}
			fmt.Fprintf(&b, "| %s | %s | %d | %d | %.4f | %.0f | %.0f | %.0f | %d (%d) | %d | %.0f | %d | %.4f | %d | %s |\n",
				c.id, a.Name, len(mine), passes, median(usd), median(wall), median(steps), median(tasks),
				checks, notHolds, fixes, median(peak), wakes, median(wakeUSD), overlap, strings.Join(measures, " / "))
		}
	}
	return b.String()
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}
