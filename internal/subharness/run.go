package subharness

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A run is saved as one file, and the file is the trace. There is no separate
// summary row anywhere: what the harness did is a DAG, and a DAG is what gets
// written down — the nodes that actually ran, in the order they finished, with
// what each of them was. A run of a dynamic harness is therefore also the record
// of the shape it chose, which is the only way to learn whether the dynamism
// was worth granting.
//
// Every run names the revision it ran, because a name alone dates badly: six
// weeks of runs under one name are useless evidence if nobody can tell which of
// them ran which program.

// Verdict is how a run ended.
type Verdict string

const (
	VerdictPass    Verdict = "pass"
	VerdictFail    Verdict = "fail"
	VerdictStopped Verdict = "stopped"
)

// Run is one execution of one revision of one harness.
type Run struct {
	Harness  string `json:"harness"`
	Revision int    `json:"revision"`

	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended,omitempty"`

	// Trigger is the node the run entered at, and Command is the line a person
	// typed when a hosted trigger started it. Together they answer "why did this
	// happen" without anyone having to correlate timestamps.
	Trigger string   `json:"trigger,omitempty"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`

	// Steps are the nodes that ran, in the order they were recorded.
	Steps []Step `json:"steps,omitempty"`

	Verdict Verdict `json:"verdict,omitempty"`
	Note    string  `json:"note,omitempty"`
}

// Step is one node's turn in a run.
type Step struct {
	ID      string    `json:"id"`
	Kind    Kind      `json:"kind"`
	Started time.Time `json:"started,omitempty"`
	Ended   time.Time `json:"ended,omitempty"`
	// Status is the node's own word for how it went, in the executing surface's
	// vocabulary. This package does not constrain it: a trace that had to be
	// translated into an enum before it could be written is a trace that loses
	// the detail someone will eventually need.
	Status string `json:"status,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Stamp is a run file's name: UTC, sortable, and safe on every filesystem.
func Stamp(at time.Time) string { return at.UTC().Format("20060102T150405Z") }

// SaveRun writes a run under harnesses/<name>/run/<ts>.json and returns the
// path. Two runs that start in the same second get -2, -3, … rather than
// overwriting each other: parallel starts are normal, and a lost trace is worse
// than an ugly name.
func (r *Registry) SaveRun(run Run) (string, error) {
	if err := ValidName(run.Harness); err != nil {
		return "", err
	}
	if run.Revision < 1 {
		return "", fmt.Errorf("subharness %q: a run must name the revision it ran", run.Harness)
	}
	if run.Started.IsZero() {
		return "", fmt.Errorf("subharness %q: a run must say when it started", run.Harness)
	}
	raw, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')
	dir := r.RunDir(run.Harness)
	stamp := Stamp(run.Started)
	for n := 1; ; n++ {
		name := stamp
		if n > 1 {
			name = stamp + "-" + strconv.Itoa(n)
		}
		path := filepath.Join(dir, name+ExtRun)
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			return path, writeFile(path, raw)
		} else if err != nil {
			return "", err
		}
	}
}

// Runs lists a harness's run files, oldest first. The stamp format sorts
// lexically in time order, which is the reason it is that format.
func (r *Registry) Runs(name string) ([]string, error) {
	if err := ValidName(name); err != nil {
		return nil, err
	}
	dir := r.RunDir(name)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stems := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ExtRun {
			continue
		}
		stems = append(stems, strings.TrimSuffix(entry.Name(), ExtRun))
	}
	// Sorted on the stem rather than the file name: the collision suffix is a
	// dash, which sorts before the extension's dot, so "…Z-2.json" would come out
	// ahead of "…Z.json" and the second run of a second would read as the first.
	sort.Strings(stems)
	paths := make([]string, 0, len(stems))
	for _, stem := range stems {
		paths = append(paths, filepath.Join(dir, stem+ExtRun))
	}
	return paths, nil
}

// LoadRun reads one saved run.
func (r *Registry) LoadRun(path string) (Run, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Run{}, fmt.Errorf("%w: run %s", ErrNotFound, filepath.Base(path))
	}
	if err != nil {
		return Run{}, err
	}
	var run Run
	if err := json.Unmarshal(raw, &run); err != nil {
		return Run{}, fmt.Errorf("subharness: run %s: %w", filepath.Base(path), err)
	}
	return run, nil
}
