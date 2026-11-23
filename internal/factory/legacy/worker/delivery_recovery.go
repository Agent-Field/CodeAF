package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
)

const deliveryRecoveryLimit = 3

func (o Options) deliveryRoot() string {
	if o.DeliveryRoot != "" {
		return o.DeliveryRoot
	}
	return filepath.Join(o.Mirror.Root(), "delivery")
}
func (p *pendingDelivery) recoverable() bool {
	// Unknown outcome belongs to the ordinary immutable retry protocol. Only a
	// confirmed rejection may enter reconciliation; never guess that cost was lost.
	return strings.Contains(p.Reason, "HTTP 422") && !p.Empty
}
func (p *pendingDelivery) validRequest() bool {
	raw, err := json.Marshal(p.Request)
	if err != nil {
		return false
	}
	hash := sha256.Sum256(raw)
	return p.Digest != "" && p.Digest == hex.EncodeToString(hash[:])
}
func writeRecovery(path string, p *pendingDelivery) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".recovery-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(raw); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func (o Options) recoverOne(ctx context.Context, w *client.WorkerClient, id int, path string, p *pendingDelivery, now time.Time) (api.WorkerRecovered, bool) {
	if !o.sameRecoveryFactory(p.Factory, w.Base()) || !p.recoverable() || !p.validRequest() || now.Before(p.RecoveryNextAt) {
		return api.WorkerRecovered{}, false
	}
	if p.RecoveryAttempts >= deliveryRecoveryLimit {
		readCtx, cancel := context.WithTimeout(ctx, deliveryRequestTimeout)
		retryAt, err := w.RecoveryRetry(readCtx, id)
		cancel()
		if err != nil || !retryAt.After(p.RetrySeen) {
			return api.WorkerRecovered{}, false
		}
		p.RetrySeen = retryAt
		p.RecoveryAttempts = 0
	}
	p.RecoveryAttempts++
	p.RecoveryNextAt = now.Add(time.Duration(p.RecoveryAttempts) * 5 * time.Second)
	// Persist the attempt before sending; a lost response can retry the identical
	// digest and its server-owned receipt without charging again.
	if writeRecovery(path, p) != nil {
		return api.WorkerRecovered{}, false
	}
	requestCtx, cancel := context.WithTimeout(ctx, deliveryRequestTimeout)
	defer cancel()
	additionalCost := 0.0
	if p.Reconciliation != nil && p.Reconciliation.Stream != nil && p.Snapshot.Stream != nil {
		additionalCost = max(0, p.Reconciliation.Stream.Spent-p.Snapshot.Stream.Spent)
	}
	out, err := w.Recover(requestCtx, id, p.Lease, p.Request, additionalCost)
	if err != nil {
		return api.WorkerRecovered{}, false
	}
	// Keep the full private evidence, including later non-cost state, after
	// acknowledgment. The active scanner never replays this archived artifact.
	_ = os.Rename(path, path+".recovered")
	return out, true
}

// Scan before pulling any new work. This also recovers pre-upgrade artifacts
// whose original lease has expired, without starting another model or test run.
func (o Options) recoverRetained(ctx context.Context, w *client.WorkerClient) {
	files, _ := filepath.Glob(filepath.Join(o.deliveryRoot(), "*.json"))
	for _, path := range files {
		if ctx.Err() != nil {
			return
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() > 32<<20 {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var p pendingDelivery
		if json.Unmarshal(raw, &p) != nil || p.Snapshot.ID <= 0 || !strings.HasPrefix(filepath.Base(path), fmt.Sprintf("%d-", p.Snapshot.ID)) {
			continue
		}
		now := time.Now()
		if o.Now != nil {
			now = o.Now()
		}
		_, _ = o.recoverOne(ctx, w, p.Snapshot.ID, path, &p, now)
	}
}

// A blocked live worker never resumes its runner. It first reconciles saved
// results and releases the old lease; subsequent work needs a fresh pull.
func (j *job) recoverBlocked(ctx context.Context, manual bool) bool {
	if j.pending == nil {
		return false
	}
	// Stop returns before executor and manager deferred usage cleanup. Keep
	// heartbeats alive, but wait for both to quiesce before taking the final
	// reconciliation snapshot or releasing the old lease.
	if j.o.Runner.Running(j.id) || j.replyingNow() {
		return false
	}
	if err := j.retainDeliverySnapshot(); err != nil {
		return false
	}
	if manual {
		j.pending.RecoveryAttempts = 0
		j.pending.RecoveryNextAt = time.Time{}
	}
	_, ok := j.o.recoverOne(ctx, j.w, j.id, j.deliveryPath(), j.pending, j.now())
	if ok {
		j.pending = nil
		j.blocked = ""
		return true
	}
	return false
}

// Legacy local artifacts recorded the listener's ephemeral URL. This opt-in
// comes only from the app host serving the same private factory store; remote
// origins always require exact equality. The server still checks owner, machine,
// original lease, current head, event and phase before accepting any result.
func (o Options) sameRecoveryFactory(saved, current string) bool {
	if saved == current {
		return true
	}
	if !o.LocalFactory {
		return false
	}
	local := func(value string) bool {
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return false
		}
		ip := net.ParseIP(u.Hostname())
		return ip != nil && ip.IsLoopback()
	}
	return local(saved) && local(current)
}
