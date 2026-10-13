// Command apps-buildbench runs the Apps Builder prompt pack against OpenRouter
// for a paragraph or a named preset, validates what comes back, and prints
// seconds, cost and issues per run. It exists to tune the prompts and to pick
// the default model; it is not part of the shipped binary.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
)

const defaultModel = "deepseek/deepseek-v4.1-flash"

type options struct {
	preset, paragraph, models, out string
	rounds                         int
	think, verbose                 bool
}

func main() {
	var o options
	flag.StringVar(&o.preset, "preset", "all", "preset name ("+strings.Join(presetNames(), ", ")+") or all")
	flag.StringVar(&o.paragraph, "paragraph", "", "your own paragraph (overrides -preset)")
	flag.StringVar(&o.models, "model", defaultModel, "comma-separated OpenRouter model ids")
	flag.StringVar(&o.out, "out", ".tmp/buildbench", "directory the app files are written under")
	flag.IntVar(&o.rounds, "rounds", 3, "maximum FIX rounds")
	flag.BoolVar(&o.think, "think", false, "let the model reason before answering")
	flag.BoolVar(&o.verbose, "v", false, "print issues and quality notes")
	flag.Parse()
	if err := execute(o); err != nil {
		fmt.Fprintln(os.Stderr, "apps-buildbench:", err)
		os.Exit(1)
	}
}

func execute(o options) error {
	jobs, err := o.jobs()
	if err != nil {
		return err
	}
	client, err := newOpenRouter(o.think)
	if err != nil {
		return err
	}
	p, err := newPipeline(client, o.rounds)
	if err != nil {
		return err
	}
	results := runAll(p, jobs)
	failed := 0
	for _, r := range results {
		printResult(r, o.verbose)
		if err := save(o.out, r); err != nil {
			return err
		}
		if !r.Pass {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d runs failed", failed, len(results))
	}
	return nil
}

type job struct{ name, paragraph, model string }

func (o options) jobs() ([]job, error) {
	paragraphs, err := o.paragraphs()
	if err != nil {
		return nil, err
	}
	var jobs []job
	for _, model := range strings.Split(o.models, ",") {
		for _, name := range sortedKeys(paragraphs) {
			jobs = append(jobs, job{name, paragraphs[name], strings.TrimSpace(model)})
		}
	}
	return jobs, nil
}

func (o options) paragraphs() (map[string]string, error) {
	switch {
	case o.paragraph != "":
		return map[string]string{"custom": o.paragraph}, nil
	case o.preset == "all":
		return presets, nil
	}
	text, ok := presets[o.preset]
	if !ok {
		return nil, fmt.Errorf("unknown preset %q (have %s)", o.preset, strings.Join(presetNames(), ", "))
	}
	return map[string]string{o.preset: text}, nil
}

// runAll runs every job at once; the order of results follows the jobs.
func runAll(p *pipeline, jobs []job) []*result {
	results := make([]*result, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = p.run(context.Background(), j.name, j.paragraph, j.model)
		}()
	}
	wg.Wait()
	return results
}
