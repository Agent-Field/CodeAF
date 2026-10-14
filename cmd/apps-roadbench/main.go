// Command apps-roadbench drives the real Builder service in process on both
// roads (calls and loop) over the same paragraphs, with the real model door,
// and writes a CSV, a JSONL and REPORT.md. It is a measurement tool, not part
// of the shipped binary. The OpenRouter key is resolved by the product's own
// config and is never printed.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// defaultModel is the one model for every role.
const defaultModel = "deepseek/deepseek-v4.1-flash"

func main() {
	file := flag.String("paragraphs", "", "paragraphs file: zoo pick output, JSON lines {id,paragraph,family}, or blank-line-separated text")
	roads := flag.String("roads", "calls,loop", "comma-separated roads")
	out := flag.String("out", "", "output directory (default ./roadbench-<time>)")
	parallel := flag.Int("parallel", 4, "builds running at once per road")
	model := flag.String("model", defaultModel, "model for every role")
	bin := flag.String("bin", "bin/codeaf", "codeaf binary built from the branch")
	build := flag.String("build", "", "commit sha of the binary (default: git rev-parse --short HEAD)")
	zoo := flag.String("zoo", "", "path of the zoo tool: adds families to the matrix and records stages")
	judge := flag.String("judge", "", "blind usefulness judge command, run as `<cmd> <app-id> <dir>`; unset leaves usefulness pending")
	run := flag.String("run", "", "zoo run id prefix, road is appended (e.g. bl-2115)")
	combineDirs := flag.String("combine", "", "comma-separated result dirs to merge into one report at -out (no build is run)")
	verdicts := flag.String("verdicts", "", "with -combine: JSON file of blind judge verdicts, {\"<road>/<id>\": {\"verdict\":..,\"scores\":..}}")
	b7 := flag.String("b7", "", "comma-separated app ids to mark b7-fail with -combine")
	flag.Parse()
	if *combineDirs != "" {
		fail(combineReport(strings.Split(*combineDirs, ","), *b7, *verdicts, *out, *build, *zoo, *run, *model))
		return
	}
	o := options{*file, strings.Split(*roads, ","), *out, *model, *bin, *build, *zoo, *run, *judge, *parallel}
	fail(execute(o))
}

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "apps-roadbench:", err)
		os.Exit(1)
	}
}

// combineReport merges runs into one REPORT.md. With zoo and run set it also
// records the finished rows (never the running ones) to the zoo.
func combineReport(dirs []string, b7, verdicts, out, build, zoo, run, model string) error {
	rows, err := combine(dirs, b7, verdicts)
	if err != nil {
		return err
	}
	if zoo != "" {
		for i := range rows {
			if rows[i].Family == "" {
				rows[i].Family = zooTags(zoo, rows[i].ID)
			}
		}
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	if err := writeAll(out, build, rows); err != nil {
		return err
	}
	if zoo == "" || run == "" {
		return nil
	}
	return recordZoo(zoo, run, build, model, finished(rows))
}

func finished(rows []row) []row {
	var out []row
	for _, r := range rows {
		if r.State != "running" {
			out = append(out, r)
		}
	}
	return out
}

type options struct {
	file       string
	roads      []string
	out, model string
	bin, build string
	zoo, run   string
	judge      string
	parallel   int
}

func execute(o options) error {
	ps, err := readParagraphs(o.file)
	if err != nil {
		return err
	}
	if o.out == "" {
		o.out = "roadbench-" + time.Now().Format("20060102-150405")
	}
	if o.build == "" {
		o.build = gitSha()
	}
	if o.zoo != "" {
		for i := range ps {
			if ps[i].Family == "" {
				ps[i].Family = zooTags(o.zoo, ps[i].ID)
			}
		}
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	labs, err := openLabs(ctx, o)
	if err != nil {
		return err
	}
	defer func() {
		for _, l := range labs {
			l.close()
		}
	}()
	var mu sync.Mutex
	rows := runAll(ctx, labs, ps, o.parallel, limits{plan: 8 * time.Minute, build: 25 * time.Minute}, func(r row) {
		mu.Lock()
		progress(r)
		mu.Unlock()
	})
	if err := writeAll(o.out, o.build, rows); err != nil {
		return err
	}
	if o.zoo != "" && o.run != "" {
		return recordZoo(o.zoo, o.run, o.build, o.model, rows)
	}
	return nil
}

func gitSha() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func openLabs(ctx context.Context, o options) ([]*lab, error) {
	var labs []*lab
	for _, road := range o.roads {
		road = strings.TrimSpace(road)
		l, err := newLab(ctx, o.bin, road, o.model, filepath.Join(o.out, "home-"+road), o.out, o.judge)
		if err != nil {
			for _, started := range labs {
				started.close()
			}
			return nil, err
		}
		labs = append(labs, l)
	}
	return labs, nil
}

func progress(r row) {
	fmt.Fprintf(os.Stderr, "%-24s %-6s %-11s used=%-8s check=%v opens=%v dry=%v keep=%v use=%v raw=%d first=%.1fs total=%.0fs $%.4f %s\n",
		r.ID, r.Road, r.State, r.RoadUsed, r.CheckPass, r.OpensInUse, r.RunsDryOK, r.Stages.Keep, r.Stages.Use, r.RawErrors, r.FirstLineS, r.TotalS, r.CostUSD, r.Cause)
}
