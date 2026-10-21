package serve

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	Kind  string
	Slots int
	// LiveCapacity is what the worker said about itself at attach; false for
	// any worker that predates the field.
	LiveCapacity bool
	// Attached is when this credential was issued. Unlike Last it is never
	// moved by presence or heartbeat, so it alone says which attach is the
	// newest for a machine (see newestAttestationLocked). Zero on a row saved
	// before the field existed.
	Attached  time.Time
	Checkouts []api.Checkout
	Asleep    bool
	Name      string
	OS        string
	Version   string
	Last      time.Time
	Worker    lease.Worker
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
		opts := h.config.LeaseOptions
		if opts.Record == nil {
			opts.Record = h.config.Record
		}
		svc.desk, svc.err = lease.Open(svc.st, opts)
	}
	if svc.err == nil {
		svc.err = svc.loadRegistry()
	}
	h.workers = svc
	h.route("/v1/workers/attach", map[string]http.HandlerFunc{http.MethodPost: h.attachWorker})
	h.read("/v1/workers/pull", h.pullWorker)
	h.route("/v1/workers/leases/{lease}/heartbeat", map[string]http.HandlerFunc{http.MethodPost: h.heartbeatWorker})
	h.route("/v1/workers/leases/{lease}/report", map[string]http.HandlerFunc{http.MethodPost: h.reportWorker})
	h.route("/v1/workers/leases/{lease}/done", map[string]http.HandlerFunc{http.MethodPost: h.doneWorker})
}

func workerPath(path string) bool {
	return (strings.HasPrefix(path, "/v1/workers/") && path != "/v1/workers/attach") ||
		(strings.HasPrefix(path, "/v1/runners/leases/")) || path == "/v1/runners/pull" || path == "/v1/runners/presence" || path == "/v1/runner/self"
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
		if err := s.saveRegistry(); err != nil {
			s.err = err
		}
		return
	}
}

func requestWorker(r *http.Request) lease.Worker {
	return r.Context().Value(workerKey{}).(lease.Worker)
}

func (h *Handler) workerReady(w http.ResponseWriter) bool {
	h.workers.mu.RLock()
	err := h.workers.err
	h.workers.mu.RUnlock()
	if err != nil {
		h.failed(w, err)
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
	// Current workers attest in a header so a factory that predates the field
	// never sees an unknown JSON key; the body field stays for direct callers.
	if r.Header.Get(api.LiveCapacityHeader) == api.LiveCapacityHeaderValue {
		body.LiveCapacity = true
	}
	if body.Runner != "" {
		body.Machine = body.Runner
	}
	if body.Owner != "" {
		body.Handle = body.Owner
	}
	if body.Kind == "" {
		body.Kind = api.RunnerCI
		if body.OS == "darwin" {
			body.Kind = api.RunnerMac
		}
	}
	if body.Slots == 0 {
		body.Slots = api.DefaultRunnerSlots
	}
	// A worker pinned above the ceiling by an old environment variable is
	// clamped, not refused: it must still attach and run what it can.
	body.Slots = min(body.Slots, api.MaxRunnerSlots)
	if body.Slots < 1 || (body.Kind != api.RunnerCI && body.Kind != api.RunnerMac && body.Kind != api.RunnerThisMac) {
		h.fail(w, http.StatusUnprocessableEntity, "runner requires kind this_mac, mac or ci and positive slots")
		return
	}
	body.Name, body.Handle = strings.TrimSpace(body.Name), factory.NormalizeHandle(body.Handle)
	if h.mode() == api.FactoryTeam && !loopbackRequest(r) && !h.floorToken(bearer(r)) {
		session, ok := h.credential(r)
		if !ok || session.profile == nil {
			h.fail(w, http.StatusForbidden, "Sign in with GitHub through an access link")
			return
		}
	}
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
	worker := lease.Worker{Machine: body.Machine, ID: "worker_" + id, Handle: body.Handle, TakesUnassigned: body.TakesUnassigned, Slots: body.Slots}
	h.workers.mu.Lock()
	asleep := false
	newRunner := true
	for _, row := range h.workers.credentials {
		if row.Worker.Handle == worker.Handle && row.Worker.Machine == worker.Machine {
			newRunner = false
			asleep = row.Asleep
			// The saved number wins, read through the ceiling: a legacy row
			// above it is granted the ceiling, never more.
			body.Slots = grantedRunnerSlots(row.Slots)
			worker.Slots = body.Slots
			if body.Checkouts == nil {
				body.Checkouts = row.Checkouts
			}
			break
		}
	}
	h.workers.credentials[sha256.Sum256([]byte(credential))] = registeredWorker{
		Name: body.Name, OS: strings.TrimSpace(body.OS), Version: strings.TrimSpace(body.CodeafVersion),
		Last: h.workers.now(), Attached: h.workers.now(), Worker: worker, Kind: body.Kind, Slots: body.Slots, LiveCapacity: body.LiveCapacity, Checkouts: body.Checkouts, Asleep: asleep,
	}
	err = h.workers.saveRegistry()
	h.workers.mu.Unlock()
	if h.failed(w, err) {
		return
	}
	if newRunner && h.config.Record != nil {
		_, err = h.config.Record.Append(r.Context(), record.Event{Kind: record.RunnerAdded, Handle: body.Handle, Object: "runner:" + body.Machine, Detail: body.Name})
		if h.failed(w, err) {
			return
		}
	}
	h.write(w, http.StatusCreated, api.WorkerAttached{Factory: h.factory, WorkerID: worker.ID, RunnerID: body.Machine, Credential: credential, Slots: body.Slots})
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
		// machine's heartbeat: the same sixty-second deadline as a busy runner.
		h.workers.heard(worker.ID, h.workers.now())
		h.machineEvents()
		h.workers.mu.RLock()
		// Authorization captured the worker before this handler (and possibly
		// before a long-poll wait). A saved lowering governs every new lease,
		// including a request already waiting with the old capacity. Keep the
		// read lock through reservation so an owner write cannot pass between
		// reading the cap and handing another job out. No network work is held.
		current, known := h.workers.credentials[sha256.Sum256([]byte(bearer(r)))]
		if !known || current.Worker.ID != worker.ID {
			h.workers.mu.RUnlock()
			h.fail(w, http.StatusUnauthorized, "a bearer worker credential is required")
			return
		}
		worker.Slots = current.Worker.Slots
		asleep := false
		for _, row := range h.workers.credentials {
			if row.Worker.Machine == worker.Machine && row.Worker.Handle == worker.Handle && row.Asleep {
				asleep = true
			}
		}
		if asleep {
			h.workers.mu.RUnlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		held, err := h.workers.desk.Pull(worker)
		h.workers.mu.RUnlock()
		if err == nil {
			h.pulled(w, r, held)
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
	return api.WorkerLease{Runner: held.Machine, Owner: held.Handle, ThreadOnly: held.ThreadOnly, ID: held.ID, Job: held.Job, Worker: held.Worker, Machine: held.Machine, Handle: held.Handle, ExpiresAt: held.ExpiresAt}
}

func (h *Handler) pulled(w http.ResponseWriter, r *http.Request, held lease.Lease) {
	it, err := h.workers.st.Get(held.Job)
	if h.failed(w, err) {
		return
	}
	signals, err := h.workers.desk.Drain(held.ID, held.Worker)
	if h.workerError(w, err) {
		return
	}
	job := h.withVersion(api.WorkerJob(it))
	job.Can = api.CanFor(it, requestWorker(r).Handle)
	// The lease holder's own copy keeps each step's chat, and carries each
	// step's whole report: they are how a resumed chat stage finds its
	// conversation again, and how the steps after it hear what came before.
	h.presentWorkerJob(r, &job)
	h.write(w, http.StatusOK, api.WorkerPull{Factory: h.factory, Lease: workerLease(held), Job: job, Signals: wireSignals(signals)})
}

func wireSignals(in []lease.Signal) []api.WorkerSignal {
	out := make([]api.WorkerSignal, len(in))
	for i, s := range in {
		out[i] = api.WorkerSignal{Budget: s.Budget, ID: s.ID, Verb: s.Verb, Yes: s.Yes, Words: s.Words}
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
	if raw := r.Header.Get(api.StepModelsHeader); raw != "" {
		var models map[string]string
		if len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &models) != nil {
			h.fail(w, http.StatusBadRequest, "invalid step models")
			return
		}
		for i := range body.Steps {
			if model := models[body.Steps[i].Name]; model != "" {
				body.Steps[i].Model = model
			}
		}
	}
	if raw := r.Header.Get(api.StepChatsHeader); raw != "" {
		var chats map[string]string
		if len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &chats) != nil {
			h.fail(w, http.StatusBadRequest, "invalid step chats")
			return
		}
		for i := range body.Steps {
			body.Steps[i].Chat = chats[body.Steps[i].Name]
		}
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
	h.applyWorker(w, r, false, func(it *factory.Item) error {
		before := failureKey(it.Failure)
		if err := run.ApplyReport(it, report, h.workers.now()); err != nil {
			return err
		}
		after := it.Failure
		// A PERSON'S STOP IS NOT A STEP THAT ERRORED: a worker whose run
		// stopped on a person's no reports the cut step with the stop on it,
		// and the person's answer is already on the record.
		if after == nil || after.Code == factory.CodeStopped || failureKey(after) == before || h.config.Record == nil {
			return nil
		}
		_, _ = h.config.Record.Append(r.Context(), record.Event{
			Kind: record.StepFailed, Job: it.ID,
			Detail: after.Code + " · " + after.Step + " · " + after.Next,
		})
		return nil
	})
}

func failureKey(f *factory.Failure) string {
	if f == nil {
		return ""
	}
	return f.Step + "\n" + f.Code + "\n" + f.Next + "\n" + f.At.UTC().Format(time.RFC3339Nano)
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
	merged, err := h.workers.desk.CarriedMerge(r.PathValue("lease"), requestWorker(r).ID)
	if h.workerError(w, err) {
		return
	}
	if merged {
		// A MERGE'S DONE IS NOT A PUBLISH. The job's pull request is already
		// out; its report said what the merge did, and the job stays done or
		// waits on the person whose merge failed.
		h.applyWorker(w, r, true, func(it *factory.Item) error {
			it.State = factory.StateShipped
			if m := it.Merging; m != nil && (m.State == factory.MergeQueued || m.State == factory.MergeMerging) {
				cp := *m
				cp.State, cp.Why = factory.MergeFailed, "the runner finished without saying what the merge did"
				it.Merging = &cp
			}
			return nil
		})
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
		if trailer == "" && it.FollowUp != nil && it.FollowUp.AutoPush && h.config.Record != nil {
			original := it.FollowUp.PublishConsent
			recorded, _ := h.config.Record.TrailerFor(r.Context(), it.ID, run.TrailerCommit(original))
			if run.ValidTrailer(original) && recorded == original {
				trailer = original
			}
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
