package serve

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

const workerPollLimit = 30 * time.Second
const workerPollEvery = 100 * time.Millisecond

type workerService struct {
	st          *store.Store
	desk        *lease.Desk
	err         error
	now         func() time.Time
	wait        time.Duration
	mu          sync.RWMutex
	credentials map[[32]byte]registeredWorker
}

// registeredWorker retains the machine name beside its identity without
// putting bearer secrets into the floor's public worker facts.
type registeredWorker struct {
	Name   string
	Worker lease.Worker
}

type workerKey struct{}

func (h *Handler) workerRoutes() {
	now := h.config.LeaseOptions.Now
	if now == nil {
		now = time.Now
	}
	wait := h.config.PullWait
	if wait <= 0 || wait > workerPollLimit {
		wait = workerPollLimit
	}
	svc := &workerService{st: h.config.Store, now: now, wait: wait, credentials: map[[32]byte]registeredWorker{}}
	if svc.st != nil {
		svc.desk, svc.err = lease.Open(svc.st, h.config.LeaseOptions)
	}
	h.workers = svc
	h.route("/v1/workers/attach", map[string]http.HandlerFunc{http.MethodPost: h.attachWorker})
	h.read("/v1/workers/pull", h.pullWorker)
	h.route("/v1/workers/leases/{lease}/heartbeat", map[string]http.HandlerFunc{http.MethodPost: h.heartbeatWorker})
	h.route("/v1/workers/leases/{lease}/report", map[string]http.HandlerFunc{http.MethodPost: h.reportWorker})
	h.route("/v1/workers/leases/{lease}/done", map[string]http.HandlerFunc{http.MethodPost: h.doneWorker})
}

func workerPath(path string) bool {
	return strings.HasPrefix(path, "/v1/workers/") && path != "/v1/workers/attach"
}

func bearer(r *http.Request) string {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return ""
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

// Worker credentials grant only execution endpoints. Loopback and the floor
// access token never replace proof of which machine holds the lease.
func (h *Handler) authorizeWorker(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	h.workers.mu.RLock()
	worker, ok := h.workers.credentials[sha256.Sum256([]byte(bearer(r)))]
	h.workers.mu.RUnlock()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="factory worker"`)
		h.fail(w, http.StatusUnauthorized, "a bearer worker credential is required")
		return r, false
	}
	return r.WithContext(context.WithValue(r.Context(), workerKey{}, worker.Worker)), true
}

func requestWorker(r *http.Request) lease.Worker {
	return r.Context().Value(workerKey{}).(lease.Worker)
}

func (h *Handler) workerReady(w http.ResponseWriter) bool {
	if h.workers.err != nil {
		h.failed(w, h.workers.err)
		return false
	}
	return h.requireVerb(w, h.workers.desk != nil)
}

func secret() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func (h *Handler) attachWorker(w http.ResponseWriter, r *http.Request) {
	if !h.workerReady(w) {
		return
	}
	var body api.WorkerAttach
	if !h.decode(w, r, &body, false) {
		return
	}
	body.Name, body.Handle = strings.TrimSpace(body.Name), factory.NormalizeHandle(body.Handle)
	if body.Name == "" || body.Handle == "" {
		h.fail(w, http.StatusBadRequest, "worker attach requires name and handle")
		return
	}
	id, err := secret()
	if h.failed(w, err) {
		return
	}
	credential, err := secret()
	if h.failed(w, err) {
		return
	}
	body.Machine = strings.TrimSpace(body.Machine)
	if body.Machine == "" {
		// Older workers introduced their machine by name before a separate id existed.
		body.Machine = body.Name
	}
	worker := lease.Worker{Machine: body.Machine, ID: "worker_" + id, Handle: body.Handle, TakesUnassigned: body.TakesUnassigned}
	h.workers.mu.Lock()
	h.workers.credentials[sha256.Sum256([]byte(credential))] = registeredWorker{Name: body.Name, Worker: worker}
	h.workers.mu.Unlock()
	h.write(w, http.StatusCreated, api.WorkerAttached{Factory: h.factory, WorkerID: worker.ID, Credential: credential})
}

func (h *Handler) pullWorker(w http.ResponseWriter, r *http.Request) {
	if !h.workerReady(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.workers.wait)
	defer cancel()
	tick := time.NewTicker(workerPollEvery)
	defer tick.Stop()
	for {
		if ctx.Err() != nil {
			if r.Context().Err() == nil {
				w.WriteHeader(http.StatusNoContent)
			}
			return
		}
		held, err := h.workers.desk.Pull(requestWorker(r))
		if err == nil {
			h.pulled(w, held)
			return
		}
		if errors.Is(err, lease.ErrWorkerBusy) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !errors.Is(err, lease.ErrNoJob) {
			h.workerError(w, err)
			return
		}
		select {
		case <-ctx.Done():
			if r.Context().Err() == nil {
				w.WriteHeader(http.StatusNoContent)
			}
			return
		case <-tick.C:
		}
	}
}

func workerLease(held lease.Lease) api.WorkerLease {
	return api.WorkerLease{ID: held.ID, Job: held.Job, Worker: held.Worker, Machine: held.Machine, Handle: held.Handle, ExpiresAt: held.ExpiresAt}
}

func (h *Handler) pulled(w http.ResponseWriter, held lease.Lease) {
	it, err := h.workers.st.Get(held.Job)
	if h.failed(w, err) {
		return
	}
	signals, err := h.workers.desk.Drain(held.ID, held.Worker)
	if h.workerError(w, err) {
		return
	}
	h.write(w, http.StatusOK, api.WorkerPull{Factory: h.factory, Lease: workerLease(held), Job: api.FromItem(it), Signals: wireSignals(signals)})
}

func wireSignals(in []lease.Signal) []api.WorkerSignal {
	out := make([]api.WorkerSignal, len(in))
	for i, s := range in {
		out[i] = api.WorkerSignal{Verb: s.Verb, Yes: s.Yes, Words: s.Words}
	}
	return out
}

func (h *Handler) heartbeatWorker(w http.ResponseWriter, r *http.Request) {
	if !h.workerReady(w) {
		return
	}
	held, err := h.workers.desk.Renew(r.PathValue("lease"), requestWorker(r).ID)
	if h.workerError(w, err) {
		return
	}
	// THE HEARTBEAT IS THE SIGNAL CHANNEL: what a person asked of the job
	// since the last beat rides back on the answer (lease/signals.go).
	signals, err := h.workers.desk.DrainLive(held.ID, held.Worker)
	if h.workerError(w, err) {
		return
	}
	h.write(w, http.StatusOK, api.WorkerHeartbeat{Factory: h.factory, Lease: workerLease(held), Signals: wireSignals(signals)})
}

func (h *Handler) reportWorker(w http.ResponseWriter, r *http.Request) {
	if !h.workerReady(w) {
		return
	}
	var body api.WorkerReport
	if !h.decode(w, r, &body, false) {
		return
	}
	report := api.ToWorkerReport(body)
	h.applyWorker(w, r, false, func(it *factory.Item) error { return run.ApplyReport(it, report, h.workers.now()) })
}

func (h *Handler) doneWorker(w http.ResponseWriter, r *http.Request) {
	if !h.workerReady(w) {
		return
	}
	var body api.WorkerDone
	if !h.decode(w, r, &body, true) {
		return
	}
	state, ok := api.ToWorkerState(body)
	if !ok {
		h.fail(w, http.StatusBadRequest, "unknown final job state")
		return
	}
	h.applyWorker(w, r, true, func(it *factory.Item) error {
		now := h.workers.now()
		if state != factory.StateShipped {
			return run.FinishReport(it, state, now)
		}
		// A WORKER NEVER APPROVES: done as shipped is a publish only beside
		// the job's recorded approval and a reported pull request; anything
		// else is the proof gate's to decide, as it always has been.
		trailer := h.approval(r.Context(), it.ID)
		if trailer == "" || it.PullRequest == nil {
			return run.FinishReport(it, state, now)
		}
		if err := run.ShipPublished(it, now); err != nil {
			return err
		}
		// The pull request is already out, so a record that cannot take the
		// line does not strand the job's lease.
		_, _ = h.config.Record.Append(r.Context(), record.Event{Kind: record.Posted, Job: it.ID, Handle: run.TrailerHandle(trailer), Cost: jobCost(*it), Detail: "pr · " + it.PullRequest.URL})
		return nil
	})
}

// approval is the job's recorded approval as its trailer, "" for none.
func (h *Handler) approval(ctx context.Context, id int) string {
	if h.config.Record == nil {
		return ""
	}
	trailer, err := h.config.Record.Trailer(ctx, id)
	if err != nil || !run.ValidTrailer(trailer) {
		return ""
	}
	return trailer
}

func (h *Handler) applyWorker(w http.ResponseWriter, r *http.Request, complete bool, apply func(*factory.Item) error) {
	// Validation failures are distinct from storage failures, so a caller can
	// correct its report without mistaking disk failure for bad input.
	var invalid error
	err := h.workers.desk.Apply(r.PathValue("lease"), requestWorker(r).ID, complete, func(it *factory.Item) error {
		invalid = apply(it)
		return invalid
	})
	if invalid != nil {
		h.fail(w, http.StatusBadRequest, invalid.Error())
		return
	}
	if !h.workerError(w, err) {
		h.finish(w, nil)
	}
}

func (h *Handler) workerError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, lease.ErrWorker):
		h.fail(w, http.StatusForbidden, "lease belongs to another worker")
		return true
	case errors.Is(err, lease.ErrNoLease):
		h.fail(w, http.StatusConflict, "lease is no longer live")
		return true
	case errors.Is(err, store.ErrNotFound):
		h.fail(w, http.StatusNotFound, "job not found")
		return true
	default:
		return h.failed(w, err)
	}
}
