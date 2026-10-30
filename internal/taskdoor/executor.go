package taskdoor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/run"
)

const (
	DefaultTaskTimeout = 6 * time.Hour
	ChildStopGrace     = 30 * time.Second
	// TaskStoreFlag is private plumbing between serve and do, hidden from help
	// and deliberately absent from the manual because it is not a user option.
	TaskStoreFlag           = "task-store"
	UnsupportedComputerLine = "this computer cannot take tasks from other computers yet"
	childPoll               = 100 * time.Millisecond
)

type ExecutorConfig struct {
	Copies CopyDeps
	Binary string
	// Command substitutes a child program at the operating-system boundary.
	// It must retain args, including the attempt's work and run paths, so
	// adoption can check that a recorded pid still belongs to this attempt.
	Command func(args []string) *exec.Cmd
	Ledger  string
	Clock   Clock
}

// ChildExecutor follows durable files rather than pipes. A serving process
// can therefore stop without killing the task or losing its eventual receipt.
type ChildExecutor struct {
	config ExecutorConfig
	clock  Clock
	mu     sync.Mutex
	active map[string]*Attempt
	stops  map[string]bool
}

func NewExecutor(config ExecutorConfig) *ChildExecutor {
	if config.Ledger == "" {
		config.Ledger = home.Join("v3", "usage.jsonl")
	}
	return &ChildExecutor{config: config, clock: clockOrWall(config.Clock), active: map[string]*Attempt{}, stops: map[string]bool{}}
}

func (e *ChildExecutor) attach(a *Attempt) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.active[a.ID] = a
	if e.stops[a.ID] && a.State().StopRequestedAt.IsZero() {
		return a.UpdateState(func(s *State) { s.StopRequestedAt = e.clock.Now() })
	}
	return nil
}
func (e *ChildExecutor) detach(a *Attempt) { e.mu.Lock(); delete(e.active, a.ID); e.mu.Unlock() }

func (e *ChildExecutor) Run(ctx context.Context, a *Attempt, j Journal) error {
	if err := e.attach(a); err != nil {
		return err
	}
	defer e.detach(a)
	if err := a.UpdateState(func(s *State) { s.LaunchStartedAt = e.clock.Now() }); err != nil {
		return err
	}
	work, record := filepath.Join(a.Dir, "work"), filepath.Join(a.Dir, "run")
	if err := ReceiveCopy(ctx, e.config.Copies, a.Request.Input, work); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !a.State().StopRequestedAt.IsZero() {
		return e.finish(ctx, a, j, nil, false)
	}
	if err := os.MkdirAll(record, 0700); err != nil {
		return err
	}
	wall := DefaultTaskTimeout
	if a.Request.Limits.Hours > 0 {
		wall = time.Duration(a.Request.Limits.Hours * float64(time.Hour))
	}
	args := []string{"do", "--dir", work, "--json", "--" + TaskStoreFlag, record, "--timeout", wall.String()}
	if a.Request.Model != "" {
		args = append(args, "--model", a.Request.Model)
	}
	// A named quality preference belongs to B's crew and provider catalog.
	if choice, ok := crewroute.ParseEffort(a.Request.Effort); ok && choice != crewroute.EffortKnee {
		args = append(args, "--"+string(choice))
	}
	// A brief can itself be a flag-shaped word; it remains positional text.
	args = append(args, "--", a.Request.Instruction)
	var cmd *exec.Cmd
	if e.config.Command != nil {
		cmd = e.config.Command(args)
	} else {
		binary := e.config.Binary
		if binary == "" {
			var err error
			binary, err = os.Executable()
			if err != nil {
				return err
			}
		}
		cmd = exec.Command(binary, args...)
	}
	stdout, err := os.OpenFile(filepath.Join(a.Dir, "do.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer stdout.Close()
	stderr, err := os.OpenFile(filepath.Join(a.Dir, "do.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer stderr.Close()
	cmd.Stdout, cmd.Stderr = stdout, stderr
	separateProcessGroup(cmd)
	// Record the ledger boundary before starting, so other runs' historical
	// charges never become work to scan on every beat of this attempt.
	var ledgerOffset int64
	if info, err := os.Stat(e.config.Ledger); err == nil {
		ledgerOffset = info.Size()
	}
	if err := a.UpdateState(func(s *State) { s.LedgerOffset = ledgerOffset; s.LaunchStartedAt = e.clock.Now() }); err != nil {
		return err
	}
	e.mu.Lock()
	err = cmd.Start()
	if err != nil {
		e.mu.Unlock()
		return err
	}
	pid := cmd.Process.Pid
	birth, hash, identityErr := childIdentity(pid, work, record)
	// A launcher can still be replacing its argv immediately after Start.
	// Give that handoff a bounded moment before recording the pinned identity.
	for tries := 0; identityErr != nil && tries < 20; tries++ {
		time.Sleep(10 * time.Millisecond)
		birth, hash, identityErr = childIdentity(pid, work, record)
	}
	if identityErr != nil {
		// A refusal can print its complete receipt and exit before inspection.
		// Preserve that reason instead of replacing it with a pid-read error.
		var receipt childReceipt
		if err := readJSON(filepath.Join(a.Dir, "do.json"), &receipt); err != nil || receipt.OK == nil && receipt.Stop == "" && receipt.Deliverable == "" {
			_ = signalChildGroup(pid, true)
			_ = cmd.Wait()
			e.mu.Unlock()
			return fmt.Errorf("could not record the task's child: %w", identityErr)
		}
	}
	err = a.UpdateState(func(s *State) {
		s.PID = pid
		s.ChildStartedAt = e.clock.Now()
		s.ProcessStart = birth
		s.CommandHash = hash
	})
	if err == nil && !a.State().StopRequestedAt.IsZero() {
		err = signalChildGroup(pid, false)
	}
	e.mu.Unlock()
	if err != nil {
		_ = signalChildGroup(pid, true)
		_ = cmd.Wait()
		return err
	}
	// Only Wait reaps our own child. An adopter polls the same pid without
	// trying to wait on a process that belongs to a previous serving process.
	done := make(chan int, 1)
	go func() {
		err := cmd.Wait()
		code := 0
		if err != nil {
			code = 1
			var exited *exec.ExitError
			if errors.As(err, &exited) {
				code = exited.ExitCode()
			}
		}
		done <- code
	}()
	return e.follow(ctx, a, j, done)
}

func (e *ChildExecutor) Adopt(ctx context.Context, a *Attempt, j Journal) error {
	if err := e.attach(a); err != nil {
		return err
	}
	defer e.detach(a)
	if a.State().PID == 0 && !a.State().LaunchStartedAt.IsZero() {
		// A child waits at do's hidden launch gate until its pid is durable.
		// Discovery closes the crash window between Start and that state write.
		pid, birth, hash, err := findAttemptChild(filepath.Join(a.Dir, "work"), filepath.Join(a.Dir, "run"))
		if err == nil && pid > 0 {
			if err := a.UpdateState(func(s *State) {
				s.PID = pid
				s.ProcessStart = birth
				s.CommandHash = hash
				s.ChildStartedAt = e.clock.Now()
			}); err != nil {
				return err
			}
		}
	}
	if !a.State().StopRequestedAt.IsZero() && e.alive(a) {
		_ = signalChildGroup(a.State().PID, false)
	}
	return e.follow(ctx, a, j, nil)
}

// Stop's timestamp survives a serving restart; the adopter owes the same
// thirty-second grace rather than granting an unresponsive child a new one.
func (e *ChildExecutor) Stop(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stops[id] = true
	a := e.active[id]
	if a == nil {
		return nil
	}
	if err := a.UpdateState(func(s *State) {
		if s.StopRequestedAt.IsZero() {
			s.StopRequestedAt = e.clock.Now()
		}
	}); err != nil {
		return err
	}
	if e.alive(a) {
		return signalChildGroup(a.State().PID, false)
	}
	return nil
}

func (e *ChildExecutor) alive(a *Attempt) bool {
	s := a.State()
	if s.PID <= 0 || s.ProcessStart == "" || s.CommandHash == "" {
		return false
	}
	birth, hash, err := childIdentity(s.PID, filepath.Join(a.Dir, "work"), filepath.Join(a.Dir, "run"))
	return err == nil && birth == s.ProcessStart && hash == s.CommandHash
}

func (e *ChildExecutor) follow(ctx context.Context, a *Attempt, j Journal, done <-chan int) (followErr error) {
	defer func() {
		if followErr != nil && ctx.Err() == nil && e.alive(a) {
			e.endUnfollowedChild(ctx, a, done)
		}
	}()
	follower, err := newChildFollower(a, e.config.Ledger)
	if err != nil {
		return err
	}
	beat := e.clock.NewTicker(childPoll)
	defer beat.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := follower.poll(j); err != nil {
			return err
		}
		var code *int
		ended := false
		if done != nil {
			select {
			case exit := <-done:
				code = &exit
				ended = true
			default:
			}
		} else {
			ended = !e.alive(a)
			code = a.State().ExitCode
		}
		if ended {
			if done != nil {
				if err := a.UpdateState(func(s *State) { s.ExitCode = code }); err != nil {
					return err
				}
			}
			if err := follower.poll(j); err != nil {
				return err
			}
			return e.finish(ctx, a, j, code, done == nil)
		}
		state := a.State()
		if !state.StopRequestedAt.IsZero() && e.clock.Now().Sub(state.StopRequestedAt) >= ChildStopGrace && e.alive(a) {
			_ = signalChildGroup(state.PID, true)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-beat.C():
		}
	}
}

// A broken trajectory or journal cannot free admission while its child keeps
// spending. Reporting failures stop and join that child before Serve records
// an incomplete ending; ordinary serving shutdown still leaves it adoptable.
func (e *ChildExecutor) endUnfollowedChild(ctx context.Context, a *Attempt, done <-chan int) {
	started := e.clock.Now()
	if e.alive(a) {
		_ = signalChildGroup(a.State().PID, false)
	}
	beat := e.clock.NewTicker(childPoll)
	defer beat.Stop()
	for e.alive(a) {
		if e.clock.Now().Sub(started) >= ChildStopGrace {
			_ = signalChildGroup(a.State().PID, true)
		}
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-beat.C():
		}
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
		}
	}
}

type childReceipt struct {
	OK          *bool    `json:"ok"`
	Stop        string   `json:"stop"`
	Answer      string   `json:"answer"`
	Deliverable string   `json:"deliverable"`
	Error       string   `json:"error"`
	BlockedOn   string   `json:"blocked_on"`
	Incomplete  string   `json:"incomplete"`
	Model       string   `json:"model"`
	Files       []string `json:"files"`
}

func (e *ChildExecutor) finish(ctx context.Context, a *Attempt, j Journal, code *int, adopted bool) error {
	var receipt childReceipt
	err := readJSON(filepath.Join(a.Dir, "do.json"), &receipt)
	complete := err == nil && (receipt.OK != nil || receipt.Stop != "" || receipt.Deliverable != "")
	end := Finished{Outcome: "incomplete", Model: receipt.Model}
	for _, path := range receipt.Files {
		if filepath.IsAbs(path) {
			relative, err := filepath.Rel(filepath.Join(a.Dir, "work"), path)
			if err != nil {
				continue
			}
			path = relative
		}
		if path != "." && filepath.IsLocal(path) {
			end.Changed = append(end.Changed, filepath.ToSlash(path))
		}
	}
	sort.Strings(end.Changed)
	if receipt.Answer != "" {
		end.Report = receipt.Answer
	} else {
		end.Report = receipt.Deliverable
	}
	if end.Report == "" {
		end.Report = firstWords(receipt.Error, receipt.BlockedOn, receipt.Incomplete, "the task ended without an answer")
	}
	if complete {
		status := 1
		if code != nil {
			status = *code
		} else {
			switch receipt.Stop {
			case "done":
				status = 0
			case "budget", "turn-cap", "deadline", "price":
				status = 3
			default:
				if receipt.OK != nil && *receipt.OK {
					status = 0
				}
			}
		}
		switch status {
		case 0:
			end.Outcome = "done"
		case 3, 124:
			end.Outcome = "stopped"
		}
	} else if adopted {
		end.Outcome = "interrupted"
		end.Report = "the task was interrupted on this computer before it left an answer"
	}
	if !a.State().StopRequestedAt.IsZero() {
		end.Outcome = "stopped"
	}
	if _, err := os.Stat(filepath.Join(a.Dir, "work")); err == nil {
		result, err := SendResult(ctx, e.config.Copies, filepath.Join(a.Dir, "work"), a.Request.Input)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			end.Report += "\nThe work could not be sent back: " + err.Error()
		} else {
			end.Result = &result
		}
	}
	return j.Append(end)
}
func firstWords(words ...string) string {
	for _, s := range words {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

type childFollower struct {
	attempt   *Attempt
	ledger    string
	offsets   map[string]int64
	nextOrder uint64
	pending   map[uint64]Step
	sessions  map[string]bool
	headers   map[string]bool
}

func newChildFollower(a *Attempt, ledger string) (*childFollower, error) {
	f := &childFollower{attempt: a, ledger: ledger, offsets: map[string]int64{"usage": a.State().LedgerOffset}, nextOrder: 1, pending: map[uint64]Step{}, sessions: map[string]bool{}, headers: map[string]bool{}}
	events, err := readEvents(a.Dir)
	if err != nil {
		return nil, err
	}
	for _, event := range events {
		var mark struct {
			Source string `json:"source"`
			Offset int64  `json:"offset"`
		}
		if err := event.Decode(&mark); err != nil {
			return nil, err
		}
		if mark.Source != "" && mark.Offset > f.offsets[mark.Source] {
			f.offsets[mark.Source] = mark.Offset
		}
		if event.Type == "step" {
			var line Step
			if err := event.Decode(&line); err != nil {
				return nil, err
			}
			if line.Step.RecordedOrder >= f.nextOrder {
				f.nextOrder = line.Step.RecordedOrder + 1
			}
		}
	}
	return f, nil
}

type pendingStep struct {
	step Step
	at   time.Time
}

func (f *childFollower) poll(j Journal) error {
	paths, err := filepath.Glob(filepath.Join(f.attempt.Dir, "run", "tasks", "*", "trajectory.jsonl"))
	if err != nil {
		return err
	}
	var pending []pendingStep
	for _, path := range paths {
		rel, _ := filepath.Rel(filepath.Join(f.attempt.Dir, "run"), path)
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		err = f.lines(path, rel, func(raw []byte, offset int64) error {
			var step run.Step
			if err := json.Unmarshal(raw, &step); err != nil {
				return err
			}
			at := step.RecordedAt
			if at.IsZero() {
				at = info.ModTime()
			}
			line := Step{Step: step, Source: rel, Offset: offset}
			if step.RecordedOrder > 0 {
				if step.RecordedOrder >= f.nextOrder {
					f.pending[step.RecordedOrder] = line
				}
			} else {
				pending = append(pending, pendingStep{line, at})
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	sort.SliceStable(pending, func(i, k int) bool { return pending[i].at.Before(pending[k].at) })
	for _, p := range pending {
		if err := j.Append(p.step); err != nil {
			return err
		}
		f.offsets[p.step.Source] = p.step.Offset
	}
	for {
		line, ok := f.pending[f.nextOrder]
		if !ok {
			break
		}
		if err := j.Append(line); err != nil {
			return err
		}
		f.offsets[line.Source] = line.Offset
		delete(f.pending, f.nextOrder)
		f.nextOrder++
	}
	return f.lines(f.ledger, "usage", func(raw []byte, offset int64) error {
		var line struct {
			At      time.Time `json:"at"`
			Model   string    `json:"model"`
			Seat    string    `json:"seat"`
			Session string    `json:"session"`
			Input   int       `json:"in"`
			Output  int       `json:"out"`
			USD     float64   `json:"usd"`
		}
		if err := json.Unmarshal(raw, &line); err != nil {
			return err
		}
		// The child's own worker headers identify its provider sessions. A
		// different conversation opened in the same folder is not its bill.
		if line.Session != "" {
			if _, known := f.sessions[line.Session]; !known {
				if err := f.learnSessions(); err != nil {
					return err
				}
				if _, known := f.sessions[line.Session]; !known {
					f.sessions[line.Session] = false
				}
			}
		}
		if f.sessions[line.Session] {
			if err := j.Append(Spend{Line: SpendLine{Model: line.Model, Seat: line.Seat, InputTokens: line.Input, OutputTokens: line.Output, USD: line.USD}, Source: "usage", Offset: offset}); err != nil {
				return err
			}
		}
		f.offsets["usage"] = offset
		return nil
	})
}

func (f *childFollower) learnSessions() error {
	paths, err := filepath.Glob(filepath.Join(f.attempt.Dir, "run", "tasks", "*", "*_worker.jsonl"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		if f.headers[path] {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		raw, readErr := bufio.NewReader(file).ReadBytes('\n')
		closeErr := file.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		var header struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		// Another worker can still be creating its first line. Its provider
		// call cannot precede that header, so leave partial headers to retry.
		if json.Unmarshal(raw, &header) != nil || header.Type != "session" || header.ID == "" {
			continue
		}
		f.sessions[header.ID] = true
		f.headers[path] = true
	}
	return nil
}

// lines ignores an unfinished final write and advances only after its event
// is durable. The journal itself is the cursor, closing the crash window
// between copying an event and saving a separate offset file.
func (f *childFollower) lines(path, source string, visit func([]byte, int64) error) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	offset := f.offsets[source]
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReader(file)
	for {
		raw, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		offset += int64(len(raw))
		if len(raw) > MaxFrameBytes {
			return fmt.Errorf("the task's saved event is too large")
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		if err := visit(raw, offset); err != nil {
			return err
		}
	}
}
