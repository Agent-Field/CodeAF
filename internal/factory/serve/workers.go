package serve

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"reflect"
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

// registeredWorker retains the machine beside its identity without putting
// bearer secrets into the floor's public facts. Last is the last request
// that proved this process is still there: an attach, a pull in progress,
// or a lease heartbeat.
type registeredWorker struct {
	Name    string
	OS      string
	Version string
	Last    time.Time
	Worker  lease.Worker
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
	// Heard after the lock is released: heard takes the write lock, and
	// holding the read lock across it would deadlock.
	h.workers.heard(worker.Worker.ID, h.workers.now())
	return r.WithContext(context.WithValue(r.Context(), workerKey{}, worker.Worker)), true
}

// heard moves a worker's last heartbeat forward. A clock that went backwards
// does not erase a newer beat.
func (s *workerService) heard(id string, at time.Time) {
	if s == nil || id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, row := range s.credentials {
		if row.Worker.ID != id || !at.After(row.Last) {
			continue
		}
		row.Last = at
		s.credentials[key] = row
		return
	}
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
	if session, ok := h.credential(r); ok && session.profile != nil {
		if body.Handle != "" && body.Handle != session.profile.Handle {
			h.fail(w, http.StatusForbidden, "worker handle must match the signed-in caller")
			return
		}
		body.Handle = session.profile.Handle
	}
	if body.Handle == "" {
		if !loopbackRequest(r) {
			h.fail(w, http.StatusUnprocessableEntity, "worker attach requires a handle")
			return
		}
		// Only a worker on this machine may use the factory's person when
		// it has no forge login; an access token never supplies that identity.
		handle, err := h.seam.PersonHandle(r.Context())
		if h.failed(w, err) {
			return
		}
		body.Handle = handle
	}
	if body.Name == "" || body.Handle == "" {
		h.fail(w, http.StatusUnprocessableEntity, "worker attach requires name and handle")
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
	h.workers.credentials[sha256.Sum256([]byte(credential))] = registeredWorker{
		Name: body.Name, OS: strings.TrimSpace(body.OS), Version: strings.TrimSpace(body.CodeafVersion),
		Last: h.workers.now(), Worker: worker,
	}
	h.workers.mu.Unlock()
	h.write(w, http.StatusCreated, api.WorkerAttached{Factory: h.factory, WorkerID: worker.ID, Credential: credential})
}

// machineFor is the attached worker's machine for this handle, or "" when
// nobody with that handle has registered on this server.
func (h *Handler) machineFor(handle string) string {
	if h == nil || h.workers == nil || strings.TrimSpace(handle) == "" {
		return ""
	}
	h.workers.mu.RLock()
	defer h.workers.mu.RUnlock()
	for _, reg := range h.workers.credentials {
		if strings.EqualFold(reg.Worker.Handle, handle) && strings.TrimSpace(reg.Worker.Machine) != "" {
			return reg.Worker.Machine
		}
	}
	return ""
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
		// THE SAVED TOGGLE WINS. Attach records what this process was willing
		// to take. Once this handle has a persisted takes_unassigned map,
		// every pull the worker loop makes uses that map. Re-read each wait
		// so a toggle saved during the long poll is the next offer.
		worker, err := h.consent(requestWorker(r))
		if h.failed(w, err) {
			return
		}
		// A pull sits open for up to thirty seconds. That wait is the idle
		// machine's heartbeat: three missed intervals, not the lease TTL.
		h.workers.heard(worker.ID, h.workers.now())
		h.machineEvents()
		held, err := h.workers.desk.Pull(worker)
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
	return api.WorkerLease{ThreadOnly: held.ThreadOnly, ID: held.ID, Job: held.Job, Worker: held.Worker, Machine: held.Machine, Handle: held.Handle, ExpiresAt: held.ExpiresAt}
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
	h.write(w, http.StatusOK, api.WorkerPull{Factory: h.factory, Lease: workerLease(held), Job: h.withVersion(api.FromItem(it)), Signals: wireSignals(signals)})
}

func wireSignals(in []lease.Signal) []api.WorkerSignal {
	out := make([]api.WorkerSignal, len(in))
	for i, s := range in {
		out[i] = api.WorkerSignal{ID: s.ID, Verb: s.Verb, Yes: s.Yes, Words: s.Words}
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
	only, err := h.workers.desk.ThreadOnly(r.PathValue("lease"), requestWorker(r).ID)
	if h.workerError(w, err) {
		return
	}
	if only {
		rest := report
		rest.Thread, rest.Delivered = nil, nil
		if !reflect.DeepEqual(rest, factory.RunReport{}) {
			h.fail(w, http.StatusBadRequest, "manager reply cannot change job steps")
			return
		}
		h.applyWorker(w, r, false, func(it *factory.Item) error { return factory.MergeThread(it, report.Thread, report.Delivered) })
		return
	}
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
	only, err := h.workers.desk.ThreadOnly(r.PathValue("lease"), requestWorker(r).ID)
	if h.workerError(w, err) {
		return
	}
	if only {
		h.applyWorker(w, r, true, func(*factory.Item) error { return nil })
		return
	}
	state, ok := api.ToWorkerState(body)
	if !ok {
		h.fail(w, http.StatusUnprocessableEntity, "unknown final job state")
		return
	}
	h.applyWorker(w, r, true, func(it *factory.Item) error {
		now := h.workers.now()
		if state != factory.StateShipped {
			if it.PublishStatus != nil && it.PublishStatus.State == "failed" {
				it.State = factory.StateLanded
				return nil
			}
			return run.FinishReport(it, state, now)
		}
		// A WORKER NEVER APPROVES: done as shipped is a publish only beside
		// the job's recorded approval and a reported PR or comment; anything
		// else is the proof gate's to decide, as it always has been.
		trailer := ""
		if h.config.Record != nil {
			trailer, _ = h.config.Record.TrailerFor(r.Context(), it.ID, it.HeadCommit)
		}
		if trailer == "" || (it.PullRequest == nil && it.CommentURL == "") {
			return run.FinishReport(it, state, now)
		}
		if err := run.ShipPublished(it, now); err != nil {
			return err
		}
		// The approved proof is already out, so a record that cannot take the
		// line does not strand the job's lease.
		detail := "comment · " + it.CommentURL
		if it.PullRequest != nil {
			detail = "pr · " + it.PullRequest.URL
		}
		_, _ = h.config.Record.Append(r.Context(), record.Event{Kind: record.Posted, Job: it.ID, Handle: requestWorker(r).Handle, Cost: jobCost(*it), Detail: detail})
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
		h.fail(w, http.StatusUnprocessableEntity, invalid.Error())
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
