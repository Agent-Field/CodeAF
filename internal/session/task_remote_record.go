package session

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

var ErrTaskAttemptAbandoned = errors.New("this task attempt was abandoned")

// Each plan's sidecar is authoritative for its remote cursor and fencing. The
// checkpoint projects it onto run rows for readers that do not open the plan.
func taskRemotePath(store *plandb.Store) string {
	return filepath.Join(plandb.TaskDir(filepath.Dir(store.Path()), store.RootID()), "remote.json")
}
func writeRemoteRecord(store *plandb.Store, record TaskRemoteRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return atomicRemoteFile(taskRemotePath(store), append(raw, '\n'))
}
func atomicRemoteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".remote-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
func readRemoteRecord(store *plandb.Store, fallback *TaskRemoteRecord) *TaskRemoteRecord {
	// A local row is authoritative about being local. A stray task-door file
	// must not change recovery or cause any new I/O for an ordinary run.
	if fallback == nil || fallback.Executor.Device == "" {
		return nil
	}
	raw, err := os.ReadFile(taskRemotePath(store))
	if err != nil {
		return cloneTaskRemote(fallback)
	}
	var r TaskRemoteRecord
	if json.Unmarshal(raw, &r) != nil || r.Executor.Device == "" {
		return cloneTaskRemote(fallback)
	}
	return &r
}

func (a *Agent) placeBeltRun(ctx context.Context, engine RunEngine, run *beltRun) {
	run.placement = taskPlacementOf(ctx)
	wish := crewWishOf(ctx)
	if run.placement.effort == "" {
		run.placement.effort = string(wish.effort)
	}
	if run.placement.model == "" {
		run.placement.model = wish.worker
	}
	if run.placement.computer == nil {
		return
	}
	run.remoteEngine, _ = engine.(TaskRunEngine)
	c := run.placement.computer
	run.remote = &TaskRemoteRecord{Executor: TaskExecutor{Device: c.Device, Name: c.Name}, NamedModel: run.placement.model, NamedEffort: run.placement.effort, MaxSteps: run.placement.maxSteps, NoProgress: run.placement.noProgress}
}

// remoteBeltSpec hands the adapter durable writes and local book ownership.
// It refuses every late callback after abandonment under the same copy lock.
func (a *Agent) remoteBeltSpec(run *beltRun, spec RunSpec) RunSpec {
	if run.remoteEngine == nil {
		return spec
	}
	run.remoteMu.Lock()
	spec.Remote = cloneTaskRemote(run.remote)
	run.remoteMu.Unlock()
	spec.TaskNumber = run.row
	spec.NamedModel, spec.NamedEffort = spec.Remote.NamedModel, spec.Remote.NamedEffort
	if spec.Remote.MaxSteps > 0 {
		spec.StepsPerTask = spec.Remote.MaxSteps
	}
	spec.NoProgress = spec.Remote.NoProgress
	spec.SaveRemote = func(record TaskRemoteRecord, force bool) error {
		run.remoteMu.Lock()
		defer run.remoteMu.Unlock()
		if run.remote != nil && run.remote.Abandoned {
			return ErrTaskAttemptAbandoned
		}
		previous := run.remote
		run.remote = cloneTaskRemote(&record)
		if !force && time.Since(run.remoteSaved) < time.Second {
			return nil
		}
		if err := writeRemoteRecord(run.store, record); err != nil {
			run.remote = previous
			return err
		}
		run.remoteSaved = time.Now()
		a.publishRemoteRow(run)
		return nil
	}
	spec.WithRemoteCopy = func(apply func() error) error {
		run.remoteMu.Lock()
		defer run.remoteMu.Unlock()
		if run.remote.Abandoned {
			return ErrTaskAttemptAbandoned
		}
		return apply()
	}
	steps := &remoteStepMirror{}
	spec.OnRemoteStep = func(seq uint64, step json.RawMessage) error {
		run.remoteMu.Lock()
		defer run.remoteMu.Unlock()
		if run.remote.Abandoned {
			return ErrTaskAttemptAbandoned
		}
		return steps.append(run.store, run.remote.Attempt, seq, step)
	}
	spec.OnRemoteCharge = func(seq uint64, charge RunCharge, seat string) error {
		run.remoteMu.Lock()
		defer run.remoteMu.Unlock()
		if run.remote.Abandoned {
			return ErrTaskAttemptAbandoned
		}
		if err := a.recordUsageLine(bankedCall{model: charge.Model, used: Usage{Input: charge.TokensIn, Output: charge.TokensOut, CacheRead: charge.Cached, Calls: 1, CostUSD: charge.USD}, remote: &remoteUsage{Task: strconv.FormatUint(run.row, 10), Computer: run.remote.Executor.Name, Event: fmt.Sprintf("%s:%d", run.remote.Attempt, seq), Seat: Seat(seat)}}); err != nil {
			return err
		}
		return a.foldRemoteCharge(fmt.Sprintf("%s:%d", run.remote.Attempt, seq), charge)

	}
	spec.RemoteStopped = func() bool { stopped, _ := a.beltRunStopped(run); return stopped }
	spec.OnRemotePhase = func(words string) {
		a.emitTaskPhase(TaskPhaseNotice{ID: run.row, Phase: TaskPhaseWorking, Text: words})
	}
	return spec
}

// publishRemoteRow is called under remoteMu; the published value is a copy so
// a watcher cannot mutate a checkpoint while another writer serializes it.
func (a *Agent) publishRemoteRow(run *beltRun) {
	g := a.graph()
	if g == nil {
		return
	}
	kept, ok := runRowOf(g, run.row)
	if !ok {
		return
	}
	kept.Remote = cloneTaskRemote(run.remote)
	kept.Computer = run.remote.Executor.Name
	kept.Unreachable = run.remote.Unreachable
	kept.CostUSD = max(kept.CostUSD, run.remote.USD)
	kept.RanElsewhere = run.remote.Finished && !run.remote.StartFailed
	if run.remote.Unreachable {
		kept.Doing = TaskComputerStoppedAnswering(kept.Computer)
	} else if run.remote.StartFailed {
		kept.Doing = kept.Report
	} else if run.remote.Finished {
		kept.Doing = TaskRanOn(kept.Computer)
	} else {
		kept.Doing = TaskRunningOn(kept.Computer)
	}
	a.publishRunRow(g, kept)
}

// mirrorRemoteStep keeps remote output paths off local file actions. The
// remote sequence on each line makes replay after a checkpoint crash harmless.
type remoteStepMirror struct {
	seen                  map[uint64]bool
	readBytes, writeBytes int64
}

func (mirror *remoteStepMirror) append(store *plandb.Store, attempt string, seq uint64, raw json.RawMessage) error {
	path := filepath.Join(plandb.TaskDir(filepath.Dir(store.Path()), store.RootID()), "trajectory.jsonl")
	if mirror.seen == nil {
		if err := mirror.readPrefix(path, attempt); err != nil {
			return err
		}
	}
	if mirror.seen[seq] {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	delete(fields, "full_output")
	delete(fields, "children")
	fields["remote_attempt"], _ = json.Marshal(attempt)
	fields["remote_seq"], _ = json.Marshal(seq)
	line, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	n, err := file.Write(append(line, '\n'))
	mirror.writeBytes += int64(n)
	if err == nil {
		err = file.Sync()
	}
	closed := file.Close()
	if err == nil {
		err = closed
	}
	if err == nil {
		mirror.seen[seq] = true
	}
	return err
}

// readPrefix loads replay identities once; ordinary appends then do only new work.
func (mirror *remoteStepMirror) readPrefix(path, attempt string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	mirror.readBytes += int64(len(existing))
	// An interrupted append cannot swallow the next complete step.
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		end := bytes.LastIndexByte(existing, '\n') + 1
		if err := os.Truncate(path, int64(end)); err != nil {
			return err
		}
		existing = existing[:end]
	}
	mirror.seen = map[uint64]bool{}
	scan := bufio.NewScanner(bytes.NewReader(existing))
	scan.Buffer(make([]byte, 4096), 1<<20)
	for scan.Scan() {
		var line struct {
			Attempt string `json:"remote_attempt"`
			Seq     uint64 `json:"remote_seq"`
		}
		if json.Unmarshal(scan.Bytes(), &line) == nil && line.Attempt == attempt {
			mirror.seen[line.Seq] = true
		}
	}
	if err := scan.Err(); err != nil {
		return err
	}

	return nil
}

// ackRemoteRun follows local settlement, so B cannot discard an unreceived
// change. A failed acknowledgement remains durable for the next reopen.
func (a *Agent) ackRemoteRun(run *beltRun) {
	run.remoteMu.Lock()
	if run.remote == nil || run.remote.Abandoned || run.remote.StartFailed || !run.remote.Finished {
		run.remoteMu.Unlock()
		return
	}
	record := *cloneTaskRemote(run.remote)
	record.Settled, record.AckPending = true, true
	if err := writeRemoteRecord(run.store, record); err != nil {
		run.remoteMu.Unlock()
		return
	}
	run.remote = &record
	a.publishRemoteRow(run)
	run.remoteMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), TaskComputerControlWithin)
	defer cancel()
	if run.remoteEngine.Acknowledge(ctx, record) != nil {
		return
	}
	run.remoteMu.Lock()
	defer run.remoteMu.Unlock()
	run.remote.AckPending = false
	_ = writeRemoteRecord(run.store, *run.remote)
	a.publishRemoteRow(run)
}

// retryRemoteAck updates only the attempt metadata of the current row. Network
// latency must not let an old receipt overwrite a newer local task ending.
func (a *Agent) retryRemoteAck(g *TaskGraph, store *plandb.Store, kept TaskNotice) {
	record := *cloneTaskRemote(kept.Remote)
	a.remoteAckJobs.Add(1)
	go func() {
		defer a.remoteAckJobs.Done()
		ctx, cancel := context.WithTimeout(a.rootContext(), TaskComputerControlWithin)
		defer cancel()
		engine, err := a.taskExecutorEngine(ctx, record.Executor)
		if err != nil {
			return
		}
		if record.Abandoned {
			err = engine.Abandon(ctx, record)
		} else {
			err = engine.Acknowledge(ctx, record)
		}
		if err != nil {
			return
		}
		record.AckPending = false
		_ = writeRemoteRecord(store, record)
		// Only metadata changes here. Publishing an earlier full row after a
		// concurrent local completion could put its state back to running.
		g.mu.Lock()
		rows := g.runs[kept.ID]
		for i := range rows {
			if rows[i].ID == kept.ID && rows[i].Remote != nil && rows[i].Remote.Attempt == record.Attempt {
				rows[i].Remote = cloneTaskRemote(&record)
			}
		}
		g.runs[kept.ID] = rows
		g.mu.Unlock()
		g.checkpoint()
	}()
}

// Settled attempts can outlive the current plan after a lost acknowledgement.
// Reopening retries those saved obligations as well as the newest run's watch.
func (a *Agent) retrySavedRemoteAcks(g *TaskGraph) {
	if a.config.TaskComputers == nil {
		return
	}
	g.mu.Lock()
	rows := g.runRowsLocked()
	g.mu.Unlock()
	for _, row := range rows {
		if row.Remote == nil || row.Remote.Executor.Device == "" {
			continue
		}
		store, err := openRemoteTaskPlan(g, row.ID)
		if err != nil {
			continue
		}
		record := readRemoteRecord(store, row.Remote)
		if record != nil && record.AckPending {
			row.Remote = record
			a.retryRemoteAck(g, store, row)
		}
		_ = store.Close()
	}
}

func (run *beltRun) taskComputerName() string {
	run.remoteMu.Lock()
	defer run.remoteMu.Unlock()
	if run.remote != nil && !run.remote.Abandoned {
		return run.remote.Executor.Name
	}
	return ""
}

// Abandonment fences the result before the ordinary ending road is entered.
func (run *beltRun) remoteAbandoned() bool {
	run.remoteMu.Lock()
	defer run.remoteMu.Unlock()
	return run.remote != nil && run.remote.Abandoned
}

// A closing chat retains a remote watch that has not received its final receipt.
func (run *beltRun) remoteWaitingOnClose(closing bool) bool {
	if !closing || run.remoteEngine == nil {
		return false
	}
	run.remoteMu.Lock()
	defer run.remoteMu.Unlock()
	return run.remote == nil || !run.remote.Finished
}

// Preparation receipts survive cancellation before dispatch, including retained partial roots.
func (a *Agent) rememberPendingRemoteCopy(run *beltRun, tree taskTree) {
	if run.remoteEngine == nil || tree.dir == "" && tree.workspaceSet == nil {
		return
	}
	a.beltMu.Lock()
	run.tree = tree
	a.beltMu.Unlock()
	if g := a.graph(); g != nil {
		a.publishRunRow(g, TaskNotice{ID: run.row, Title: run.title, State: TaskQueued,
			Copy: runCopyOf(tree), WorkspaceSet: cloneWorkspaceSet(tree.workspaceSet)})
	}
}
