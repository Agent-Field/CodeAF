package main

// door.go — the do door: the same six cells through `codeaf do`.
//
// The task door (run.go) starts one task inside this driver's own process;
// the do door launches the product binary as a subprocess, the way
// bench/deepswe/run.sh drives it (bench/deepswe/AUTOPSY.md carries the
// invocation's provenance). One door, one flag, the same fixture seeding, the
// same throwaway home, the same readings and the same graders — so the two
// doors' rows are comparable row for row.
//
// THE DOOR'S OWN VOCABULARY, read off its exit ladder
// (cmd/codeaf/envelope.go): 0 done, 1 could not run, 2 ran and did not
// finish, 3 a limit stopped it, 4 needed an answer, 124 the driver's own
// wall fired first (the rig's spelling, not the envelope's).
//
// WHERE THE DOOR WRITES: everything it keeps about a run lands in the
// home's own store, graph.db — the nodes table is the family (parent_id
// 'root' is the fan-out, status the ending of each leaf), usage is the
// per-call ledger (the same per-response accounting the task door's
// v3/usage.jsonl keeps, one row per provider call), usage_turns is one row
// per model turn, and transcript is the workers' own trace — tool_call,
// tool_result and assistant rows, which is where the diagnostics are
// counted and where the belt's fingerprints would have to show. The readers
// below take the store read-only; the task door's ledger-and-journal walk
// (readings.go) is untouched.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // the store driver: graph.db is read read-only
)

// door is one way an invocation reaches the engine.
type door string

const (
	// doorTask is the engine's task door: session.New + StartTask in this
	// process (run.go).
	doorTask door = "task"
	// doorDo is the product's adaptive-run door: the binary, as a subprocess.
	doorDo door = "do"
)

// doExitWords is the do door's exit ladder, in the words the report quotes.
var doExitWords = map[int]string{
	0:   "done",
	1:   "could-not-run",
	2:   "ran-not-finished",
	3:   "limit-stopped",
	4:   "needed-answer",
	124: "wall",
}

// doEnvelope is the one JSON object `codeaf do --json` prints on stdout
// (cmd/codeaf/envelope.go). Only the fields the bench reads are named; the
// rest of the contract is skipped untouched.
type doEnvelope struct {
	OK       bool    `json:"ok"`
	Stop     string  `json:"stop"`
	Answer   string  `json:"answer"`
	Error    string  `json:"error"`
	SpendUSD float64 `json:"spend_usd"`
	Seconds  float64 `json:"seconds"`
	Steps    int     `json:"steps"`
	Run      string  `json:"run"`
	Calls    int     `json:"calls"`
}

// binaryPath is the product binary this bench launches on the do door. The
// driver builds it before the first invocation (ensureBinary); a run against
// a tree with no bin/codeaf fails at the door, not mid-grid.
func binaryPath() string {
	if fromEnv := strings.TrimSpace(os.Getenv("BASHLOOP_BINARY")); fromEnv != "" {
		return fromEnv
	}
	return "bin/codeaf"
}

// ensureBinary builds the product binary once per run on the do door. The
// Makefile's own recipe, run from the checkout the driver was launched in —
// the bench never builds to another path (AGENTS.md, "make build").
func ensureBinary(errOut io.Writer) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	bin := filepath.Join(root, "bin", "codeaf")
	if info, err := os.Stat(bin); err == nil && !info.IsDir() && time.Since(info.ModTime()) < 24*time.Hour {
		return nil
	}
	build := exec.Command("make", "build")
	build.Dir = root
	build.Stdout, build.Stderr = errOut, errOut
	return build.Run()
}

// repoRoot is the checkout the driver's package lives in — resolved from
// this file's own location at runtime through go list, so the binary is
// built from the tree the bench is testing, not from wherever the caller
// happens to stand.
func repoRoot() (string, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").CombinedOutput()
	if err == nil {
		if dir := strings.TrimSpace(string(out)); dir != "" && dir != "(unknown)" {
			return dir, nil
		}
	}
	// A run from inside the checkout: walk up to the Makefile.
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "Makefile")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "bench", "bashloop")); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no checkout root found: run the driver from the repository")
		}
		dir = parent
	}
}

// runDoDoor runs one invocation through the do door and answers its row. The
// shape is the rig's (bench/deepswe/run.sh's drive.sh): codeaf do -w <fixture>
// -model <pinned> -plan-model <pinned> -db <home>/graph.db -keep -timeout
// <seconds> -yes-spend -json < <brief>, with the env the door needs —
// CODEAF_HOME, HOME, the provider key, and the arm's belt switch.
func (r *runner) runDoDoor(iv invocation, cellDef cell, fixtureDir, homeDir string, startedAt time.Time) row {
	bin, err := filepath.Abs(binaryPath())
	if err != nil {
		return r.failedRow(iv, fmt.Sprintf("resolve the binary: %v", err))
	}
	if _, err := os.Stat(bin); err != nil {
		if err := ensureBinary(r.out); err != nil {
			return r.failedRow(iv, fmt.Sprintf("build the binary: %v", err))
		}
	}
	if _, err := os.Stat(bin); err != nil {
		return r.failedRow(iv, fmt.Sprintf("no binary at %s: %v", bin, err))
	}

	briefFile := filepath.Join(iv.RunDir, "brief.md")
	if err := os.WriteFile(briefFile, []byte(iv.Brief), 0o600); err != nil {
		return r.failedRow(iv, fmt.Sprintf("write the brief: %v", err))
	}

	wallSeconds := int(iv.Wall.Seconds())
	argv := []string{
		"do",
		"-w", fixtureDir,
		"-model", iv.Model,
		"-plan-model", iv.Model,
		"-db", filepath.Join(homeDir, "graph.db"),
		"-keep",
		"-timeout", fmt.Sprintf("%ds", wallSeconds),
		"-yes-spend",
		"-json",
	}
	cmd := exec.Command(bin, argv...)
	cmd.Stdin, err = os.Open(briefFile)
	if err != nil {
		return r.failedRow(iv, fmt.Sprintf("open the brief: %v", err))
	}
	envelopeFile, err := os.Create(filepath.Join(iv.RunDir, "do.json"))
	if err != nil {
		return r.failedRow(iv, fmt.Sprintf("open the envelope: %v", err))
	}
	defer envelopeFile.Close()
	streamFile, err := os.Create(filepath.Join(iv.RunDir, "run.log"))
	if err != nil {
		return r.failedRow(iv, fmt.Sprintf("open the stream: %v", err))
	}
	defer streamFile.Close()
	cmd.Stdout = envelopeFile
	cmd.Stderr = streamFile
	// The env the door needs. The arm's belt switch and the throwaway home's
	// CODEAF_HOME are already in this process's environment (runOne set them
	// before the door dispatch), and the profile inside the home carries the
	// key; HOME moves to the throwaway home so nothing the door reads lands
	// on the machine's own furniture — the rig's shape, with the home doing
	// /root's work.
	cmd.Env = append(os.Environ(),
		"HOME="+homeDir,
	)

	// THE DOOR'S OWN WALL. The -timeout flag is the door's limit, and this
	// backstop is the driver's: if the door has not come home one minute
	// after its own wall, the driver stops waiting and the row says so.
	if err := cmd.Start(); err != nil {
		return r.failedRow(iv, fmt.Sprintf("start the door: %v", err))
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	timedOut := false
	select {
	case waitErr = <-done:
	case <-time.After(iv.Wall + time.Minute):
		timedOut = true
		_ = cmd.Process.Kill()
		<-done
	}
	wall := time.Since(startedAt)

	exitCode := 0
	if timedOut {
		exitCode = 124
	} else if waitErr != nil {
		var exitErr *exec.ExitError
		if ok := errors.As(waitErr, &exitErr); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return r.failedRow(iv, fmt.Sprintf("the door ended unreadably: %v", waitErr))
		}
	}

	// The envelope: the door's own account of itself, read once.
	env := readDoEnvelope(filepath.Join(iv.RunDir, "do.json"))

	// THE READINGS: the do door keeps its numbers in its own store, not in
	// the v3 ledger-and-journals layout the task door writes — the pair run
	// proved that (no v3/usage.jsonl, no v3/runs) — so the same figures are
	// read from the store's own tables, through the same helpers where the
	// shapes match: cost and models from the usage table (the door's
	// per-call ledger), steps from the transcript's finished tool calls (the
	// task door's `took` unit), the diagnostics from the tool results, and
	// the family shape from the nodes table.
	pristine, err := pristineFixture(cellDef)
	if err != nil {
		return r.failedRow(iv, fmt.Sprintf("read the pristine fixture: %v", err))
	}
	reading := collectReadings(homeDir, "", "", fixtureDir, pristine)
	if err := applyStoreReadings(&reading, filepath.Join(homeDir, "graph.db")); err != nil {
		return r.failedRow(iv, fmt.Sprintf("read the run store: %v", err))
	}
	// The do door's own account of itself, folded onto the readings the same
	// way the task door folds its landing notice: the ending word, the
	// deliverable, and the wall — the envelope's seconds, the door's own
	// clock over the run, with the driver's wall kept beside it.
	reading.applyNotice(doExitWord(exitCode, timedOut), env.Answer, startedAt, startedAt.Add(wall))
	// The wall: the envelope's seconds is the door's own clock over the run
	// (wallSeconds reads it back as the node's own record); a run that never
	// printed one falls back to the driver's clock through applyNotice's
	// dates.
	if env.Seconds > 0 {
		reading.StartedAt, reading.EndedAt = startedAt, startedAt.Add(time.Duration(env.Seconds*float64(time.Second)))
	}

	g := gradeCell(cellDef, fixtureDir, reading, pristine)
	wallRead, wallFrom := reading.wallSeconds()
	return row{
		Date: r.date, Arm: iv.Arm, Cell: iv.Cell, Replicate: iv.Replicate,
		Model: iv.Model, ModelsUsed: reading.modelsUsedLine(),
		Graded: g.Pass, GradeDetail: g.Detail,
		Ending: reading.Ending, Report: firstLine(env.Answer),
		Steps: reading.Steps, CostUSD: reading.CostUSD, Unbilled: reading.Unbilled,
		WallSeconds: wallRead, WallSource: wallFrom,
		ChangedFiles: reading.ChangedFiles,
		ChildrenDone: reading.ChildrenDone, ChildrenTotal: reading.ChildrenTotal,
		NodesFailed:  reading.NodesFailed,
		RunDir:       iv.RunDir,
	}
}

// readDoEnvelope reads the door's one JSON envelope. A missing or unreadable
// envelope answers a zero envelope — an honest no-reading, not a guess — and
// the run.log beside it is the record a person reads.
func readDoEnvelope(path string) doEnvelope {
	body, err := os.ReadFile(path)
	if err != nil {
		return doEnvelope{}
	}
	var env doEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return doEnvelope{}
	}
	return env
}

// doExitWord turns the exit ladder into the ending word the row quotes.
func doExitWord(code int, timedOut bool) string {
	if timedOut {
		return "wall"
	}
	if word, ok := doExitWords[code]; ok {
		return word
	}
	return fmt.Sprintf("exit-%d", code)
}

// doDoorLine is the invocation the dry run prints for the do door — the
// exact command a person could run by hand, the rig's own shape.
func doDoorLine(iv invocation, fixtureDir, homeDir string) string {
	return fmt.Sprintf("%s do -w %s -model %s -plan-model %s -db %s -keep -timeout %ds -yes-spend -json < %s",
		binaryPath(), fixtureDir, iv.Model, iv.Model,
		filepath.Join(homeDir, "graph.db"), int(iv.Wall.Seconds()),
		filepath.Join(iv.RunDir, "brief.md"))
}

// ── the do door's store reader ─────────────────────────────────────────────

// applyStoreReadings fills a reading from the do door's own store, the home's
// graph.db, read read-only: cost, unbilled and models from the usage table
// (the door's per-call ledger — the same per-response accounting the task
// door's v3/usage.jsonl keeps, so the two doors' cost columns stay the same
// figure), steps from the transcript's finished tool calls (the task door's
// `took` unit), the diagnostics from the tool results and shell calls, and
// the family shape from the nodes table. A table that is not there is a zero
// and no error — a run that ended before the store carried that table is an
// honest no-reading, the same law collectReadings keeps.
func applyStoreReadings(reading *readings, dbPath string) error {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()

	// The ledger: every priced row of the run's own home.
	var cost float64
	var unbilled, calls int
	err = db.QueryRow(`select coalesce(sum(cost), 0),` +
		` sum(case when cost = 0 and prompt_tokens + completion_tokens > 0 then 1 else 0 end),` +
		` count(*) from usage`).Scan(&cost, &unbilled, &calls)
	if err == nil {
		reading.CostUSD = cost
		reading.Unbilled = unbilled
		models, mErr := readStoreModels(db)
		if mErr == nil {
			reading.ModelsUsed = models
		}
	}

	// The workers' trace: finished tool calls are the steps; tool results
	// carry the truncation and rejection diagnostics; shell calls carry the
	// edit-idiom discipline. The shapes are read through the same helpers the
	// task door's journal reader uses, so the two doors' diagnostics cannot
	// drift apart.
	rows, err := db.Query(`select kind, tool, body from transcript order by seq`)
	if err == nil {
		defer rows.Close()
		countedSearch := false
		for rows.Next() {
			var kind, tool, body string
			if err := rows.Scan(&kind, &tool, &body); err != nil {
				continue
			}
			switch kind {
			case "tool_call":
				reading.Steps++
				command := storeCommand(tool, body)
				if command == "" {
					continue
				}
				if isCountingSearch(command) {
					countedSearch = true
					continue
				}
				if isInPlaceEdit(command) && !countedSearch {
					reading.EditIdiomFlags++
				}
			case "tool_result":
				if strings.Contains(body, truncationMarker) {
					reading.Truncations++
				}
				if isInvalidAction(body) {
					reading.InvalidActions++
				}
			}
		}
	}

	// The family shape: the root's own fan-out, and every node the engine
	// marked failed.
	var done, total, failed int
	if err := db.QueryRow(`select count(*),` +
		` sum(case when status = 'done' then 1 else 0 end),` +
		` (select count(*) from nodes where status = 'failed')` +
		` from nodes where parent_id = 'root'`).Scan(&total, &done, &failed); err == nil {
		reading.ChildrenDone, reading.ChildrenTotal, reading.NodesFailed = done, total, failed
	}
	return nil
}

// readStoreModels answers the models the store's ledger billed, sorted.
func readStoreModels(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`select distinct model from usage where model <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var models []string
	for rows.Next() {
		var model string
		if err := rows.Scan(&model); err == nil && model != "" {
			models = append(models, model)
		}
	}
	sort.Strings(models)
	return models, nil
}

// storeCommand pulls a shell command out of a transcript tool call. The do
// door's workers name their shell by the subharness that built it (`sh` on
// the linear program, `bash` on the belt), and the argument is `cmd` or
// `command` — anything else is not a shell call and answers empty.
func storeCommand(tool, body string) string {
	if tool != "bash" && tool != "sh" {
		return ""
	}
	var args struct {
		Command string `json:"command"`
		Cmd     string `json:"cmd"`
	}
	if err := json.Unmarshal([]byte(body), &args); err != nil {
		return ""
	}
	if args.Command != "" {
		return args.Command
	}
	return args.Cmd
}

// storeBeltFingerprints reads one do-door store for the belt's fingerprints:
// bash-family tool calls (a belted worker's own shell hand is named `bash`;
// the linear subharness's own executor names it `sh`), one-action rejections
// (the belt's envelope diagnostic), and the plan CLI's name (the bashworker
// page teaches it). Zero bash calls with zero rejections and zero plan-CLI
// words is the shape an unarmed arm leaves.
func storeBeltFingerprints(dbPath string) (bashCalls, rejections, planWords int) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return 0, 0, 0
	}
	defer db.Close()
	_ = db.QueryRow(`select count(*) from transcript where kind = 'tool_call' and tool = 'bash'`).Scan(&bashCalls)
	_ = db.QueryRow(`select count(*) from transcript where kind = 'tool_result' and lower(body) like '%no action executed%'`).Scan(&rejections)
	_ = db.QueryRow(`select count(*) from transcript where body like '%plandb%'`).Scan(&planWords)
	return bashCalls, rejections, planWords
}
