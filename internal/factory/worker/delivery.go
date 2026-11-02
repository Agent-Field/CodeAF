package worker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
)

// This is a bounded delivery bridge, not a restart-replay or accounting protocol.
// The private artifact survives a discarded mirror, but an old lease is never
// replayed automatically. Server response replay still has a commit/cache gap.
type pendingDelivery struct {
	Lease, Factory string
	Request        client.PreparedWorkerReport
	Digest         string
	Room           *client.PreparedWorkerReport `json:",omitempty"`
	RoomSettled    bool
	Empty          bool
	Snapshot       factory.Item
	// Reconciliation is the later mirror state, never a replacement request or
	// an acknowledged baseline. It preserves work recorded while delivery waited.
	Reconciliation   *factory.Item `json:",omitempty"`
	ReconciliationAt time.Time     `json:",omitzero"`
	Thread           []factory.Message
	Delivered        []string
	Reason           string `json:",omitempty"`
	Deadline         time.Time
	NextAt           time.Time
	Attempts         int
	RecoveryAttempts int
	RecoveryNextAt   time.Time
	RetrySeen        time.Time
}

const deliveryRequestTimeout = 5 * time.Second
const deliveryControlAttempts = 3

func deliveryKey() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return "worker-report-" + hex.EncodeToString(id[:]), nil
}

func (j *job) deliveryPath() string {
	root := j.o.DeliveryRoot
	if root == "" {
		root = filepath.Join(j.o.Mirror.Root(), "delivery")
	}
	id := sha256.Sum256([]byte(j.w.Base() + "\x00" + j.lease))
	return filepath.Join(root, fmt.Sprintf("%d-%x.json", j.id, id[:12]))
}

func (j *job) saveDelivery(p *pendingDelivery) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	path := j.deliveryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".report-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	if _, err := f.Write(raw); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (j *job) prepareDelivery(cur factory.Item, report factory.RunReport) (*pendingDelivery, error) {
	p := &pendingDelivery{Lease: j.lease, Factory: j.w.Base(), Snapshot: cur, Thread: report.Thread, Delivered: report.Delivered, Deadline: j.now().Add(factory.RecoveryWindow)}
	// Room metadata remains isolated for older strict factories. Refusing only
	// this optional metadata must never discard the cost/results beside it.
	if report.Room != "" {
		key, err := deliveryKey()
		if err != nil {
			return nil, err
		}
		room, err := client.PrepareWorkerReport(api.WorkerReport{Room: report.Room}, key)
		if err != nil {
			return nil, err
		}
		p.Room = &room
		report.Room = ""
	}
	p.Empty = reflect.DeepEqual(report, factory.RunReport{})
	key, err := deliveryKey()
	if err != nil {
		return nil, err
	}
	p.Request, err = client.PrepareWorkerReport(api.FromRunReport(report), key)
	if err != nil {
		return nil, err
	}
	frozen, err := json.Marshal(p.Request)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(frozen)
	p.Digest = hex.EncodeToString(digest[:])
	if err := j.saveDelivery(p); err != nil {
		return nil, err
	}
	return p, nil
}

func reportRetryable(err error) bool {
	var refused client.Refusal
	if !errors.As(err, &refused) {
		return true
	}
	return refused.Status >= 500 || refused.Status == http.StatusRequestTimeout || refused.Status == http.StatusTooManyRequests
}

func (j *job) sendRequest(ctx context.Context, request client.PreparedWorkerReport) error {
	ctx, cancel := context.WithTimeout(ctx, deliveryRequestTimeout)
	defer cancel()
	return j.w.ReportPrepared(ctx, j.lease, request)
}

func (j *job) sendPending(ctx context.Context) bool {
	p := j.pending
	if !j.now().Before(p.Deadline) {
		j.blockDelivery("The factory did not confirm this step's results within the recovery window. Work stopped and unsent results are retained for reconciliation before retrying.")
		return j.reportBlocked(ctx)
	}
	if j.now().Before(p.NextAt) {
		return true
	}
	if p.Room != nil && !p.RoomSettled {
		err := j.sendRequest(ctx, *p.Room)
		if gone(err) {
			return false
		}
		if err != nil && reportRetryable(err) {
			return j.retryDelivery(ctx)
		}
		p.RoomSettled = true
		if err := j.saveDelivery(p); err != nil {
			j.blockDelivery("The factory received metadata, but its delivery receipt could not be retained; work stopped for reconciliation.")
			return j.reportBlocked(ctx)
		}
	}
	if !p.Empty {
		err := j.sendRequest(ctx, p.Request)
		if gone(err) {
			return false
		}
		if err != nil {
			if reportRetryable(err) {
				return j.retryDelivery(ctx)
			}
			var refused client.Refusal
			_ = errors.As(err, &refused)
			j.blockDelivery(fmt.Sprintf("The factory refused this step's results (HTTP %d): %s. Saved results are retained for recovery; completed work does not need to be repeated.", refused.Status, err.Error()))
			return j.reportBlocked(ctx)
		}
	}
	// This snapshot, rather than a later mirror read, is the acknowledged
	// baseline. New work becomes a new delta only after this exact request lands.
	j.sent = p.Snapshot
	j.mu.Lock()
	for _, id := range p.Delivered {
		j.acknowledged[id] = true
	}
	j.mu.Unlock()
	j.thread = append(j.thread, p.Thread...)
	if len(j.thread) > factory.ThreadMost {
		j.thread = j.thread[len(j.thread)-factory.ThreadMost:]
	}
	j.pending = nil
	_ = os.Remove(j.deliveryPath())
	return true
}

func (j *job) now() time.Time {
	if j.o.Now != nil {
		return j.o.Now()
	}
	return time.Now()
}

func (j *job) retryDelivery(ctx context.Context) bool {
	p := j.pending
	p.Attempts++
	delays := []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second}
	p.NextAt = j.now().Add(delays[min(p.Attempts-1, len(delays)-1)])
	if err := j.saveDelivery(p); err != nil {
		j.blockDelivery("Result delivery was interrupted and its retry state could not be retained; work stopped for reconciliation.")
		return j.reportBlocked(ctx)
	}
	return true
}

func (j *job) blockDelivery(reason string) {
	j.blocked = reason
	if j.pending != nil {
		j.pending.Reason = reason
		_ = j.retainDeliverySnapshot()
	}
	j.mu.Lock()
	cancel := j.replyCancel
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	_ = j.o.Runner.Stop(j.id)
}

// Retain the latest recorded mirror separately from the frozen request. This is
// private reconciliation evidence only; it never changes retry bytes or spend
// acknowledgment. Capture again at cleanup for usage recorded after Stop began.
func (j *job) retainDeliverySnapshot() error {
	if j.pending == nil {
		return nil
	}
	cur, err := j.o.Mirror.Get(j.id)
	if err != nil {
		return err
	}
	j.pending.Reconciliation = &cur
	j.pending.ReconciliationAt = j.now()
	return j.saveDelivery(j.pending)
}

func (j *job) clearMirror() {
	// An initial delivery save can fail before a pending artifact exists.
	// Keep the mirror itself when it is the only copy of unconfirmed work.
	if j.blocked != "" && j.pending == nil {
		return
	}
	if err := j.retainDeliverySnapshot(); err != nil {
		// A failed private save is not permission to erase the last local copy.
		return
	}
	_ = os.Remove(j.o.Mirror.ItemPath(j.id))
}

// A separately keyed, zero-cost question makes a deterministic reporting block
// visible without asserting that rejected results were saved. No model runs and
// no Done releases the lease. If this control message also fails, retain the
// artifact and stop after a bounded number of attempts while heartbeats continue.
func (j *job) reportBlocked(ctx context.Context) bool {
	if j.controlSent || j.controlTries >= deliveryControlAttempts {
		return true
	}
	if j.control == nil {
		key, err := deliveryKey()
		if err != nil {
			return true
		}
		request, err := client.PrepareWorkerReport(api.WorkerReport{Question: &api.Question{Text: j.blocked, Kind: "results_not_saved"}}, key)
		if err != nil {
			return true
		}
		j.control = &request
	}
	j.controlTries++
	err := j.sendRequest(ctx, *j.control)
	if gone(err) {
		return false
	}
	j.controlSent = err == nil
	return true
}
