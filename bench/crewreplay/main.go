// Command crewreplay feeds recorded tasks through the crew router offline —
// no model is called and nothing is spent — and reports how each one was
// classified and which crew it would have run on.
//
// It is the router's acceptance check against the trial its defaults were
// read from. Each task is a directory holding the issue as the harness handed
// it over (issue.md, whose "Task as given to the agent" section is the text
// codeaf received); the truth file maps each task's directory name to the
// class the trial filed it under, and a trial batch name is accepted in place
// of a class (OE* is open-ended work, fresh* a narrow fix):
//
//	go run ./bench/crewreplay -review results/codeaf_trial/review -truth truth.json
//
// The candidates are the trial's own model set on OpenRouter at the prices the
// table snapshotted, so the replay asks the question the trial answered and
// not a question about today's catalog.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/crewroute"
)

func main() {
	review := flag.String("review", "", "directory of task directories, each holding issue.md")
	truthPath := flag.String("truth", "", "JSON object mapping a task directory to its class or trial batch")
	flag.Parse()
	if *review == "" || *truthPath == "" {
		fmt.Fprintln(os.Stderr, "usage: crewreplay -review DIR -truth FILE")
		os.Exit(2)
	}
	truth, err := readTruth(*truthPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	names := make([]string, 0, len(truth))
	for name := range truth {
		names = append(names, name)
	}
	sort.Strings(names)
	candidates := trialCandidates()
	var right, total int
	byClass := map[crewroute.Class][2]int{}
	var est float64
	fmt.Println("| task | truth | read as | why | worker | planner | checker | est |")
	fmt.Println("|---|---|---|---|---|---|---|---|")
	for _, name := range names {
		text, err := taskText(filepath.Join(*review, name, "issue.md"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		want := truth[name]
		d, err := crewroute.Decide(crewroute.Request{Task: crewroute.Task{Text: text}, Candidates: candidates})
		if err != nil {
			fmt.Fprintln(os.Stderr, name+": "+err.Error())
			os.Exit(1)
		}
		total++
		counts := byClass[want]
		counts[1]++
		if d.Class == want {
			right++
			counts[0]++
		}
		byClass[want] = counts
		est += d.EstUSD
		fmt.Printf("| %s | %s | %s | %s | %s | %s | %s | %s |\n", name, want, d.Class, d.Why,
			crewroute.ShortModel(d.Seat(crewroute.Worker).Model), crewroute.ShortModel(d.Seat(crewroute.Planner).Model),
			crewroute.ShortModel(d.Seat(crewroute.Checker).Model), crewroute.Money(d.EstUSD))
	}
	fmt.Println()
	fmt.Printf("classified %d of %d as the trial filed them (%.0f%%)\n", right, total, 100*float64(right)/float64(max(total, 1)))
	for _, class := range crewroute.Classes {
		if counts, ok := byClass[class]; ok {
			fmt.Printf("  %s: %d of %d\n", class, counts[0], counts[1])
		}
	}
	fmt.Printf("mean estimated crew cost %s a task\n", crewroute.Money(est/float64(max(total, 1))))
}

// readTruth reads the truth file, folding trial batch names onto classes.
func readTruth(path string) (map[string]crewroute.Class, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var words map[string]string
	if err := json.Unmarshal(raw, &words); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out := make(map[string]crewroute.Class, len(words))
	for name, word := range words {
		lower := strings.ToLower(word)
		switch {
		case strings.HasPrefix(lower, "oe"), lower == string(crewroute.OpenEnded):
			out[name] = crewroute.OpenEnded
		case strings.HasPrefix(lower, "fresh"), lower == string(crewroute.Bugfix):
			out[name] = crewroute.Bugfix
		default:
			out[name] = crewroute.Class(lower)
		}
	}
	return out, nil
}

// taskText is the part of an issue.md the harness handed codeaf: the
// "Task as given to the agent" section, or the whole file when it has none.
func taskText(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := string(raw)
	const heading = "## Task as given to the agent"
	if at := strings.Index(text, heading); at >= 0 {
		text = text[at+len(heading):]
	}
	return strings.TrimSpace(text), nil
}

// trialCandidates is the trial's model set on OpenRouter.
func trialCandidates() []crewroute.Candidate {
	var out []crewroute.Candidate
	for _, id := range crewroute.Measured() {
		m, _ := crewroute.Snapshot(id)
		out = append(out, crewroute.Candidate{Model: m, Routes: []crewroute.Route{{Provider: "openrouter", Send: id, Kind: crewroute.Metered}}})
	}
	return out
}
