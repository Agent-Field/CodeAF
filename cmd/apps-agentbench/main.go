// Command apps-agentbench measures the agent inside already-built apps, never
// the Builder. Like apps-buildbench, it is a separate bench-only command and
// is not linked into cmd/codeaf or included in make build.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const defaultModel = "deepseek/deepseek-v4.1-flash"

type options struct {
	apps, models, preset, out string
	parallel                  int
	timeout                   time.Duration
}

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fatal(err)
	}
	var o options
	flag.StringVar(&o.apps, "apps", filepath.Join(home, "af-w2/models/builder/files/deepseek_deepseek-v4.1-flash"), "builder output directory containing crm/, tracker/, inventory/, booking/")
	flag.StringVar(&o.models, "model", defaultModel, "comma-separated OpenRouter model ids")
	flag.StringVar(&o.preset, "preset", "all", "crm, tracker, inventory, booking, or all")
	flag.StringVar(&o.out, "out", ".tmp/agentbench", "directory for one JSONL file per task run")
	flag.IntVar(&o.parallel, "parallel", 8, "maximum concurrent task runs")
	flag.DurationVar(&o.timeout, "timeout", 4*time.Minute, "deadline for each agent turn")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := execute(ctx, o); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "apps-agentbench:", err)
	os.Exit(1)
}

type job struct {
	model, preset, dir string
	task               task
}

func (o options) jobs() ([]job, error) {
	if o.parallel < 1 || o.timeout <= 0 {
		return nil, fmt.Errorf("-parallel and -timeout must be positive")
	}
	names := presetNames()
	if o.preset != "all" {
		if _, ok := presets[o.preset]; !ok {
			return nil, fmt.Errorf("unknown preset %q (have %s)", o.preset, strings.Join(names, ", "))
		}
		names = []string{o.preset}
	}
	root, err := expandHome(o.apps)
	if err != nil {
		return nil, err
	}
	var jobs []job
	for _, model := range strings.Split(o.models, ",") {
		model = strings.TrimSpace(model)
		if model == "" {
			return nil, fmt.Errorf("-model contains an empty model id")
		}
		for _, name := range names {
			dir := filepath.Join(root, name)
			for _, task := range presets[name] {
				jobs = append(jobs, job{model, name, dir, task})
			}
		}
	}
	return jobs, nil
}

func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/")), nil
}

func execute(ctx context.Context, o options) error {
	jobs, err := o.jobs()
	if err != nil {
		return err
	}
	if strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")) == "" {
		return fmt.Errorf("OPENROUTER_API_KEY is required")
	}
	// Every invocation gets a fresh directory; earlier runs remain available for
	// blind comparisons instead of being overwritten by a repeated model id.
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(o.out, "run-")
	if err != nil {
		return err
	}
	results := runAll(ctx, o, jobs)
	printTable(results)
	failed := 0
	for i, r := range results {
		if err := saveResult(dir, i, r); err != nil {
			return err
		}
		if !r.OK || !r.DataOK {
			failed++
		}
	}
	fmt.Println("JSONL:", dir)
	if failed > 0 {
		return fmt.Errorf("%d of %d tasks have errors or unmet data checks", failed, len(results))
	}
	return nil
}

// runAll bounds whole task runs, including their private SQLite stores. Results
// retain input order so parallel scheduling does not bias the printed table.
func runAll(ctx context.Context, o options, jobs []job) []result {
	results := make([]result, len(jobs))
	work := make(chan int)
	var wg sync.WaitGroup
	for range min(o.parallel, len(jobs)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				results[i] = runJob(ctx, jobs[i], o.timeout)
			}
		}()
	}
	for i := range jobs {
		work <- i
	}
	close(work)
	wg.Wait()
	return results
}
