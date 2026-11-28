package clientrun

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

var (
	// errNeedsFaceID marks a deterministic Face ID refusal so execute
	// distinguishes it from a transient engine read failure (contract S6, S10).
	errNeedsFaceID = errors.New("irreversible ask requires Face ID")
	// errBadCallAsk marks an ask ID that is not open in asks.list(agent) for the
	// named chat so execute refuses it with bad_call before dispatch (contract S6).
	errBadCallAsk = errors.New("queued ask is not open for the named chat")
	// errNotHolderAsk marks an appwire CodeNotFound error from asks.list or
	// ListTasks so execute refuses it deterministically as not_holder rather
	// than treating it as a transient failure (contract S6, S10).
	errNotHolderAsk = errors.New("queued chat is not reachable on this computer")
	// errClientStopped aborts draining a client that became paused, revoked, or
	// tombstoned mid-round without reporting a transient failure (contract V1).
	errClientStopped = errors.New("queued client is no longer active")
)

func (r *Runner) drain(ctx context.Context, c Client, listing *directory.Listing) error {
	var entries []Queued
	err := r.request(ctx, "GET", clientPath(c.ID)+"/queue", nil, func(reader io.Reader) error {
		dec := json.NewDecoder(reader)
		if err := queueStart(dec); err != nil {
			return err
		}
		var last int64
		for dec.More() {
			var entry Queued
			if err := dec.Decode(&entry); err != nil {
				return err
			}
			if len(entry.Sealed) > clientproto.MaxQueueBytes || len(entry.Body) > clientproto.MaxFabricBody {
				return errors.New("relay queue entry exceeds size limit")
			}
			if entry.Number <= last {
				return errors.New("relay queue is not in sequence order")
			}
			last = entry.Number
			// Run at most MaxClientQueue entries per round and leave any excess
			// entries for the next round so a long queue is never wedged.
			if len(entries) < clientproto.MaxClientQueue {
				entries = append(entries, entry)
			}
		}
		return queueEnd(dec)
	})
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !r.clientStillServed(*listing, c.ID) {
			return nil
		}
		if err := r.execute(ctx, c, entry, listing); err != nil {
			if errors.Is(err, errClientStopped) {
				return nil
			}
			return err
		}
	}
	return nil
}

func queueStart(dec *json.Decoder) error {
	for _, want := range []any{json.Delim('{'), "entries", json.Delim('[')} {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		if token != want {
			return errors.New("malformed relay queue")
		}
	}
	return nil
}

func queueEnd(dec *json.Decoder) error {
	for _, want := range []json.Delim{']', '}'} {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		if token != want {
			return errors.New("malformed relay queue")
		}
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing relay queue data")
	}
	return nil
}

func (r *Runner) execute(ctx context.Context, c Client, e Queued, listing *directory.Listing) error {
	// 1. The proof: check the phone's signature over the exact stored body at
	// the entry's own signed RequestTime. A bad proof was never authored by the
	// phone: acknowledge it, keep no receipt, publish no outcome, log it, and move on.
	if err := r.verify(c, e); err != nil {
		r.logf("dropping queued call %d for %s whose proof did not check out: %v", e.Number, c.ID, err)
		return r.ack(ctx, c.ID, e)
	}

	// 2. The target: the relay only lists entries targeted at this computer or
	// "any". A target naming another computer comes from a lying or buggy relay
	// and is skipped without touching its receipt, outcome, watermark, or queue
	// entry (contract S1).
	self := r.cfg.Signer.Cert().DeviceID()
	if e.Target != self && e.Target != "any" {
		r.logf("skipping queued call %d for %s targeted at another computer %s", e.Number, c.ID, e.Target)
		return nil
	}

	receipt, known, watermark := r.previous(c.ID, e.Number)
	// 8. The watermark (S3): a number at or below the finished watermark is a
	// replay (or a retry after a crash between save and DELETE): acknowledge it,
	// run nothing, and publish nothing new.
	if e.Number <= watermark {
		return r.ack(ctx, c.ID, e)
	}

	// 10. A receipt found at started after a crash is never dispatched again.
	if known && receipt.Status == "started" {
		return r.recoverStarted(ctx, c, e, receipt, listing)
	}

	// Every phone call targets one specific computer (never "any").
	if e.Target != self {
		return r.refuseCall(ctx, c, e, *listing, "", clientproto.OutcomeRefused, clientproto.ReasonBadTarget)
	}

	// 3. The seal: open with this computer's executor key and check inner fields
	// and lifetime against the outer signed envelope and Config.Now (S4, S7, S8).
	ourPub, err := r.executorPublicKey()
	if err != nil {
		return err
	}
	ourPriv := r.secretKey()
	call, err := clientproto.OpenCall(e.Sealed, &ourPub, &ourPriv)
	if err != nil {
		return r.refuseCall(ctx, c, e, *listing, "", clientproto.OutcomeRefused, clientproto.ReasonBadCall)
	}
	if err := call.Check(clientproto.OuterCall{
		Number: e.Number,
		At:     e.At,
		Method: e.Method,
		Target: e.Target,
	}, c.ID, self, r.cfg.Now().UnixMilli()); err != nil {
		if errors.Is(err, clientproto.ErrCallExpired) {
			return r.refuseCall(ctx, c, e, *listing, "", clientproto.OutcomeExpired, clientproto.ReasonExpired)
		}
		return r.refuseCall(ctx, c, e, *listing, "", clientproto.OutcomeRefused, clientproto.ReasonBadCall)
	}

	// 4. The method: must be in clientproto.PhoneMethods() and in the client's
	// enrolled Methods (S9).
	if !slices.Contains(clientproto.PhoneMethods(), call.Method) || !slices.Contains(c.Methods, call.Method) {
		return r.refuseCall(ctx, c, e, *listing, "", clientproto.OutcomeRefused, clientproto.ReasonMethodDenied)
	}

	// 5. The params: decode strictly per method (unknown keys refused).
	agent, createClientID, err := validateCallParams(call)
	if err != nil {
		return r.refuseCall(ctx, c, e, *listing, "", clientproto.OutcomeRefused, clientproto.ReasonBadCall)
	}

	// 6. The holder: for every method except agents.create, the chat must be
	// leased by this computer in the round's directory listing (V2, S6).
	if call.Method != clientproto.MethodCreate {
		if !r.holdsChat(*listing, agent) {
			return r.refuseCall(ctx, c, e, *listing, agent, clientproto.OutcomeRefused, clientproto.ReasonNotHolder)
		}
	}

	// 7. Face ID and ask ownership: an ask not in asks.list(agent) for the named
	// chat is refused / bad_call; an appwire CodeNotFound from asks.list or
	// ListTasks is refused / not_holder; an irreversible ask or task without
	// call.FaceID is refused / needs_face_id; any other engine error reading
	// asks or tasks is transient (S6, S10).
	if err := r.checkAsk(call, agent); err != nil {
		if errors.Is(err, errNotHolderAsk) {
			return r.refuseCall(ctx, c, e, *listing, agent, clientproto.OutcomeRefused, clientproto.ReasonNotHolder)
		}
		if errors.Is(err, errBadCallAsk) {
			return r.refuseCall(ctx, c, e, *listing, agent, clientproto.OutcomeRefused, clientproto.ReasonBadCall)
		}
		if errors.Is(err, errNeedsFaceID) {
			return r.refuseCall(ctx, c, e, *listing, agent, clientproto.OutcomeRefused, clientproto.ReasonNeedsFaceID)
		}
		return err
	}

	// 9. Create is idempotent by (phone, client_id) (S2): if the pair is already
	// known, finish done with that chat and create nothing.
	if call.Method == clientproto.MethodCreate {
		if existingAgent, ok := r.createdChat(c.ID, createClientID); ok {
			outAgent := r.outcomeAgent(*listing, existingAgent, clientproto.OutcomeDone, true)
			doneReceipt := Receipt{
				Client: c.ID,
				Number: e.Number,
				Agent:  existingAgent,
				Method: call.Method,
				At:     r.cfg.Now().UTC().Format(time.RFC3339Nano),
				Status: clientproto.OutcomeDone,
				Digest: proofDigest(e),
			}
			outcome := clientproto.Outcome{
				Number: e.Number,
				Status: clientproto.OutcomeDone,
				Agent:  outAgent,
			}
			if err := r.finishCall(doneReceipt, outcome, createClientID); err != nil {
				return err
			}
			if !r.isClientActive(c.ID) {
				return errClientStopped
			}
			if err := r.log(doneReceipt); err != nil {
				return err
			}
			if err := r.flushOutcomes(ctx, c); err != nil {
				return err
			}
			return r.ack(ctx, c.ID, e)
		}
	}

	// 10. Execute with a claiming receipt first.
	if !known || receipt.ClaimToken == "" {
		token := make([]byte, clientproto.ClaimTokenBytes)
		if _, err := rand.Read(token); err != nil {
			return err
		}
		receipt = Receipt{
			Client:     c.ID,
			Number:     e.Number,
			Agent:      agent,
			Method:     call.Method,
			At:         r.cfg.Now().UTC().Format(time.RFC3339Nano),
			Status:     "claiming",
			Digest:     proofDigest(e),
			ClaimToken: hex.EncodeToString(token),
		}
		if err := r.keep(receipt); err != nil {
			return err
		}
	}
	if err := r.request(ctx, "POST", queuePath(c.ID, e.Number)+"/claim", map[string]string{"token": receipt.ClaimToken}, nil); err != nil {
		return err
	}

	// After the claim round trip, re-check the directory, lease, and ask so a
	// displaced or revoked executor never runs (Step 2 item 10, V1, V2).
	freshListing, err := r.cfg.Directory.List(ctx)
	if err != nil {
		return err
	}
	if err := r.checkDirectoryListing(freshListing); err != nil {
		return err
	}
	*listing = freshListing
	if !r.clientStillServed(freshListing, c.ID) {
		return errClientStopped
	}
	if call.Method != clientproto.MethodCreate && !r.holdsChat(freshListing, agent) {
		return r.refuseCall(ctx, c, e, freshListing, agent, clientproto.OutcomeRefused, clientproto.ReasonNotHolder)
	}
	if err := r.checkAsk(call, agent); err != nil {
		if errors.Is(err, errNotHolderAsk) {
			return r.refuseCall(ctx, c, e, freshListing, agent, clientproto.OutcomeRefused, clientproto.ReasonNotHolder)
		}
		if errors.Is(err, errBadCallAsk) {
			return r.refuseCall(ctx, c, e, freshListing, agent, clientproto.OutcomeRefused, clientproto.ReasonBadCall)
		}
		if errors.Is(err, errNeedsFaceID) {
			return r.refuseCall(ctx, c, e, freshListing, agent, clientproto.OutcomeRefused, clientproto.ReasonNeedsFaceID)
		}
		return err
	}

	// Re-check idempotent create map in case a previous attempt finished.
	if call.Method == clientproto.MethodCreate {
		if existingAgent, ok := r.createdChat(c.ID, createClientID); ok {
			outAgent := r.outcomeAgent(*listing, existingAgent, clientproto.OutcomeDone, true)
			doneReceipt := Receipt{
				ClaimToken: receipt.ClaimToken,
				Client:     c.ID,
				Number:     e.Number,
				Agent:      existingAgent,
				Method:     call.Method,
				At:         r.cfg.Now().UTC().Format(time.RFC3339Nano),
				Status:     clientproto.OutcomeDone,
				Digest:     proofDigest(e),
			}
			outcome := clientproto.Outcome{
				Number: e.Number,
				Status: clientproto.OutcomeDone,
				Agent:  outAgent,
			}
			if err := r.finishCall(doneReceipt, outcome, createClientID); err != nil {
				return err
			}
			if !r.isClientActive(c.ID) {
				return errClientStopped
			}
			if err := r.log(doneReceipt); err != nil {
				return err
			}
			if err := r.flushOutcomes(ctx, c); err != nil {
				return err
			}
			return r.ack(ctx, c.ID, e)
		}
	}

	receipt, outcome, err := r.dispatch(ctx, c, e, call, agent, createClientID, receipt, listing)
	if err != nil {
		return err
	}
	if !r.isClientActive(c.ID) {
		return errClientStopped
	}
	if err := r.finishCall(receipt, outcome, createClientID); err != nil {
		return err
	}
	if !r.isClientActive(c.ID) {
		return errClientStopped
	}
	if err := r.log(receipt); err != nil {
		return err
	}
	if err := r.flushOutcomes(ctx, c); err != nil {
		return err
	}
	return r.ack(ctx, c.ID, e)
}

func (r *Runner) recoverStarted(ctx context.Context, c Client, e Queued, receipt Receipt, listing *directory.Listing) error {
	createClientID := ""
	chatExists := false
	if receipt.Method == clientproto.MethodCreate {
		if ourPub, err := r.executorPublicKey(); err == nil {
			ourPriv := r.secretKey()
			if call, err := clientproto.OpenCall(e.Sealed, &ourPub, &ourPriv); err == nil {
				if _, cid, err := validateCallParams(call); err == nil {
					createClientID = cid
					if receipt.Agent == "" {
						if existing, ok := r.createdChat(c.ID, cid); ok {
							receipt.Agent = existing
						}
					}
				}
			}
		}
		chatExists = receipt.Agent != "" && chatFolderExists(receipt.Transcript)
		if _, local := r.localChats(); !local && chatExists && r.cfg.LeaseCreated != nil {
			_ = r.cfg.LeaseCreated(ctx, appwire.Agent{ID: receipt.Agent})
			if fresh, err := r.cfg.Directory.List(ctx); err == nil {
				*listing = fresh
			}
		}
	}
	if err := r.log(receipt); err != nil {
		return err
	}
	var outcome clientproto.Outcome
	if receipt.Method == clientproto.MethodCreate && chatExists {
		outcome = clientproto.Outcome{
			Number: e.Number,
			Status: clientproto.OutcomeDone,
			Agent:  r.outcomeAgent(*listing, receipt.Agent, clientproto.OutcomeDone, true),
		}
	} else {
		outcome = clientproto.Outcome{
			Number: e.Number,
			Status: clientproto.OutcomeRefused,
			Agent:  r.outcomeAgent(*listing, receipt.Agent, clientproto.OutcomeRefused, chatExists),
			Reason: clientproto.ReasonFailed,
		}
	}
	if err := r.finishCall(receipt, outcome, createClientID); err != nil {
		return err
	}
	if !r.isClientActive(c.ID) {
		return errClientStopped
	}
	if err := r.flushOutcomes(ctx, c); err != nil {
		return err
	}
	return r.ack(ctx, c.ID, e)
}

func (r *Runner) refuseCall(ctx context.Context, c Client, e Queued, listing directory.Listing, agent, status, reason string) error {
	outAgent := r.outcomeAgent(listing, agent, status, false)
	receipt := Receipt{
		Client: c.ID,
		Number: e.Number,
		Agent:  outAgent,
		Method: e.Method,
		At:     r.cfg.Now().UTC().Format(time.RFC3339Nano),
		Status: status,
		Reason: reason,
		Digest: proofDigest(e),
	}
	outcome := clientproto.Outcome{
		Number: e.Number,
		Status: status,
		Agent:  outAgent,
		Reason: reason,
	}
	if err := r.finishCall(receipt, outcome, ""); err != nil {
		return err
	}
	if !r.isClientActive(c.ID) {
		return errClientStopped
	}
	if err := r.log(receipt); err != nil {
		return err
	}
	if err := r.flushOutcomes(ctx, c); err != nil {
		return err
	}
	return r.ack(ctx, c.ID, e)
}

// outcomeAgent bounds the chat ID placed on an Outcome to MaxOutcomeAgent and,
// for a refused or failed call, includes it only when it names a chat this
// computer holds or just created so a hostile call can never overflow the
// outcome slot (contract S11).
func (r *Runner) outcomeAgent(listing directory.Listing, agent, status string, justCreated bool) string {
	if agent == "" || len(agent) > clientproto.MaxOutcomeAgent {
		return ""
	}
	if status == clientproto.OutcomeDone {
		return agent
	}
	if justCreated || r.holdsChat(listing, agent) {
		return agent
	}
	return ""
}

func chatFolderExists(transcript string) bool {
	if transcript == "" {
		return false
	}
	folder, ok := session.FolderOf(transcript)
	if !ok {
		return false
	}
	info, err := os.Stat(folder)
	return err == nil && info.IsDir()
}

func (r *Runner) dispatch(ctx context.Context, c Client, e Queued, call clientproto.CallPlain, agent, createClientID string, claimed Receipt, listing *directory.Listing) (Receipt, clientproto.Outcome, error) {
	receipt := Receipt{
		ClaimToken: claimed.ClaimToken,
		Client:     c.ID,
		Number:     e.Number,
		Agent:      agent,
		Method:     call.Method,
		At:         r.cfg.Now().UTC().Format(time.RFC3339Nano),
		Status:     "started",
		Digest:     proofDigest(e),
	}
	if err := r.keep(receipt); err != nil {
		return receipt, clientproto.Outcome{}, err
	}
	if err := r.log(receipt); err != nil {
		return receipt, clientproto.Outcome{}, err
	}
	engine := r.cfg.Engine
	if talker, ok := engine.(appwire.Talker); ok && call.Method == clientproto.MethodAnswer {
		engine = answerGuard{Engine: engine, Talker: talker, faceID: call.FaceID}
	}
	leased := false
	lease := func(a appwire.Agent) error {
		if leased || r.cfg.LeaseCreated == nil {
			return nil
		}
		if _, local := r.localChats(); local {
			leased = true
			return nil
		}
		if err := r.cfg.LeaseCreated(ctx, a); err != nil {
			return err
		}
		leased = true
		if fresh, err := r.cfg.Directory.List(ctx); err == nil {
			*listing = fresh
		}
		return nil
	}
	callParams := call.Params
	if call.Method == clientproto.MethodCreate {
		// Do not forward client_id to the engine's create: the engine only uses
		// it to name the first message, whereas phone create de-duplicates in
		// the runner (Step 2 item 9).
		callParams = json.RawMessage("{}")
		engine = creationRecorder{
			Engine: engine,
			reserve: func(transcript string) error {
				folder, ok := session.FolderOf(transcript)
				if !ok {
					return errors.New("created chat has no transcript folder")
				}
				receipt.Agent = filepath.Base(folder)
				receipt.Transcript = transcript
				return r.keepCreate(receipt, createClientID)
			},
			record: func(created appwire.Agent) error {
				receipt.Agent = created.ID
				if err := r.keepCreate(receipt, createClientID); err != nil {
					return err
				}
				if err := r.log(receipt); err != nil {
					return err
				}
				return lease(created)
			},
		}
	}
	result, callErr := appwire.Call(engine, call.Method, callParams)
	if callErr != nil {
		justCreated := call.Method == clientproto.MethodCreate && chatFolderExists(receipt.Transcript)
		outAgent := r.outcomeAgent(*listing, receipt.Agent, clientproto.OutcomeRefused, justCreated)
		if errors.Is(callErr, errNeedsFaceID) {
			receipt.Status = clientproto.OutcomeRefused
			receipt.Reason = clientproto.ReasonNeedsFaceID
			return receipt, clientproto.Outcome{
				Number: e.Number,
				Status: clientproto.OutcomeRefused,
				Agent:  outAgent,
				Reason: clientproto.ReasonNeedsFaceID,
			}, nil
		}
		receipt.Status = "failed"
		receipt.Reason = callErr.Error()
		return receipt, clientproto.Outcome{
			Number: e.Number,
			Status: clientproto.OutcomeRefused,
			Agent:  outAgent,
			Reason: clientproto.ReasonFailed,
		}, nil
	}
	receipt.Status = clientproto.OutcomeDone
	if call.Method == clientproto.MethodCreate {
		var created struct {
			Agent appwire.Agent `json:"agent"`
		}
		if err := json.Unmarshal(mustJSON(result), &created); err != nil {
			return receipt, clientproto.Outcome{}, err
		}
		receipt.Agent = created.Agent.ID
		if err := lease(created.Agent); err != nil {
			receipt.Status = "failed"
			receipt.Reason = err.Error()
			return receipt, clientproto.Outcome{
				Number: e.Number,
				Status: clientproto.OutcomeRefused,
				Agent:  r.outcomeAgent(*listing, receipt.Agent, clientproto.OutcomeRefused, true),
				Reason: clientproto.ReasonFailed,
			}, nil
		}
	}
	return receipt, clientproto.Outcome{
		Number: e.Number,
		Status: clientproto.OutcomeDone,
		Agent:  r.outcomeAgent(*listing, receipt.Agent, clientproto.OutcomeDone, call.Method == clientproto.MethodCreate),
	}, nil
}

// creationRecorder installs a durable handoff through the ordinary create
// request. The runner records the chat and its client_id before releasing its
// first turn.
type creationRecorder struct {
	appwire.Engine
	record  func(appwire.Agent) error
	reserve func(string) error
}

func (e creationRecorder) CreateAgent(ask appwire.CreateAsk) (appwire.Agent, error) {
	ask.BeforeMint = e.reserve
	ask.BeforeStart = e.record
	return e.Engine.CreateAgent(ask)
}

// validateCallParams decodes call.Params strictly per method, refusing unknown
// keys or malformed routing targets with ErrCall (Step 2 item 5).
func validateCallParams(call clientproto.CallPlain) (agent string, createClientID string, err error) {
	switch call.Method {
	case clientproto.MethodCreate:
		var p struct {
			ClientID string `json:"client_id"`
		}
		if err := strictJSON(call.Params, &p); err != nil || strings.TrimSpace(p.ClientID) == "" {
			return "", "", clientproto.ErrCall
		}
		return "", p.ClientID, nil
	case clientproto.MethodSend:
		var p struct {
			ID       string `json:"id"`
			Text     string `json:"text"`
			ClientID string `json:"client_id"`
		}
		if err := strictJSON(call.Params, &p); err != nil || strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Text) == "" {
			return "", "", clientproto.ErrCall
		}
		return p.ID, "", nil
	case clientproto.MethodStop:
		var p struct {
			ID string `json:"id"`
		}
		if err := strictJSON(call.Params, &p); err != nil || strings.TrimSpace(p.ID) == "" {
			return "", "", clientproto.ErrCall
		}
		return p.ID, "", nil
	case clientproto.MethodAnswer:
		var p struct {
			Scope     string `json:"scope"`
			Agent     string `json:"agent"`
			ID        string `json:"id"`
			Option    string `json:"option"`
			Text      string `json:"text"`
			Why       string `json:"why"`
			BatchItem string `json:"batch_item"`
			Later     bool   `json:"later"`
			YouDecide bool   `json:"you_decide"`
		}
		if err := strictJSON(call.Params, &p); err != nil {
			return "", "", clientproto.ErrCall
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.ID) == "" ||
			strings.HasPrefix(p.ID, "crew:") || strings.HasPrefix(p.BatchItem, "crew:") ||
			(strings.TrimSpace(p.Option) == "" && strings.TrimSpace(p.Text) == "" && !p.Later && !p.YouDecide) {
			return "", "", clientproto.ErrCall
		}
		return p.Agent, "", nil
	case clientproto.MethodInboxAnswer:
		var p struct {
			ID     string `json:"id"`
			Option string `json:"option"`
			Text   string `json:"text"`
			Why    string `json:"why"`
			Scope  string `json:"scope"`
		}
		if err := strictJSON(call.Params, &p); err != nil {
			return "", "", clientproto.ErrCall
		}
		owner, ask, ok := strings.Cut(p.ID, "/")
		if !ok || strings.TrimSpace(owner) == "" || strings.TrimSpace(ask) == "" ||
			strings.HasPrefix(ask, "crew:") ||
			(strings.TrimSpace(p.Option) == "" && strings.TrimSpace(p.Text) == "") {
			return "", "", clientproto.ErrCall
		}
		return owner, "", nil
	case clientproto.MethodTaskStart:
		var p struct {
			ID string `json:"id"`
		}
		if err := strictJSON(call.Params, &p); err != nil {
			return "", "", clientproto.ErrCall
		}
		i := strings.LastIndex(p.ID, ":")
		if i <= 0 || i >= len(p.ID)-1 || strings.TrimSpace(p.ID[:i]) == "" || strings.TrimSpace(p.ID[i+1:]) == "" {
			return "", "", clientproto.ErrCall
		}
		return p.ID[:i], "", nil
	default:
		return "", "", clientproto.ErrCall
	}
}

func (r *Runner) holdsChat(l directory.Listing, agent string) bool {
	if agent == "" {
		return false
	}
	if local, ok := r.localChats(); ok {
		return slices.Contains(local, agent)
	}
	if r.cfg.Signer == nil {
		return false
	}
	self := r.cfg.Signer.Cert().DeviceID()
	c, ok := l.Cells[agent]
	if !ok || c.Archived {
		return false
	}
	if c.Class != "" && !slices.Contains([]string{"chat", string(cell.HostBound), string(cell.Sandboxed), string(cell.FilesOnly)}, c.Class) {
		return false
	}
	return c.Lease.Device == self && c.Lease.Expires > l.Now
}

func (r *Runner) clientActiveLocked(clientID string) bool {
	if clientID == "" || r.isTombstonedLocked(clientID) {
		return false
	}
	_, enrolled := r.enrolled[clientID]
	return enrolled
}

func (r *Runner) isClientActive(clientID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.clientActiveLocked(clientID)
}

func (r *Runner) clientStillServed(l directory.Listing, clientID string) bool {
	d, ok := l.Devices[clientID]
	if !ok || d.Kind != "client" || d.Revoked {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.clientActiveLocked(clientID) || r.state.Paused[clientID] {
		return false
	}
	return true
}

func isAppwireNotFound(err error) bool {
	var wireErr *appwire.Error
	return errors.As(err, &wireErr) && wireErr.Code == appwire.CodeNotFound
}

func (r *Runner) listChatAsks(agent string) ([]appwire.Ask, error) {
	result, err := appwire.Call(r.cfg.Engine, "asks.list", mustJSON(appwire.AgentParams{Agent: agent}))
	if err != nil {
		if isAppwireNotFound(err) {
			return nil, errNotHolderAsk
		}
		return nil, err
	}
	all := result.(*appwire.AsksResult).Asks
	out := make([]appwire.Ask, 0, len(all))
	for _, ask := range all {
		if ask.Agent == "" || ask.Agent == agent {
			out = append(out, ask)
		}
	}
	return out, nil
}

func (r *Runner) checkAsk(call clientproto.CallPlain, agent string) error {
	if r.cfg.Engine == nil {
		return nil
	}
	if call.Method == clientproto.MethodTaskStart {
		var p appwire.IDParams
		if err := json.Unmarshal(call.Params, &p); err != nil {
			return err
		}
		rows, err := r.cfg.Engine.ListTasks(agent)
		if err != nil {
			if isAppwireNotFound(err) {
				return errNotHolderAsk
			}
			return err
		}
		for _, row := range rows {
			if row.ID == p.ID && row.Agent == agent {
				return requireFaceID([]appwire.Ask{{Irreversible: row.Irreversible}}, call.FaceID)
			}
		}
		return nil
	}
	switch call.Method {
	case clientproto.MethodInboxAnswer:
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(call.Params, &p); err != nil {
			return err
		}
		_, askID, _ := strings.Cut(p.ID, "/")
		asks, err := r.listChatAsks(agent)
		if err != nil {
			return err
		}
		idx := slices.IndexFunc(asks, func(a appwire.Ask) bool { return a.ID == askID })
		if idx < 0 {
			return errBadCallAsk
		}
		// inbox.answer dispatches through Inboxer.AnswerInbox rather than
		// Talker.AnswerAskWith and cannot reach AskAnswer.BeforeResolve, so it
		// refuses conservatively when any open ask of that chat is irreversible
		// and face_id is false.
		return requireFaceID(asks, call.FaceID)
	case clientproto.MethodAnswer:
		var p struct {
			ID        string `json:"id"`
			BatchItem string `json:"batch_item"`
			Later     bool   `json:"later"`
			YouDecide bool   `json:"you_decide"`
		}
		if err := json.Unmarshal(call.Params, &p); err != nil {
			return err
		}
		asks, err := r.listChatAsks(agent)
		if err != nil {
			return err
		}
		idx := slices.IndexFunc(asks, func(a appwire.Ask) bool { return a.ID == p.ID })
		if idx < 0 {
			return errBadCallAsk
		}
		targetAsk := asks[idx]
		if p.BatchItem != "" && !p.Later && !p.YouDecide {
			bIdx := slices.IndexFunc(asks, func(a appwire.Ask) bool { return a.ID == p.BatchItem })
			if bIdx < 0 {
				return errBadCallAsk
			}
			targetAsk = asks[bIdx]
		}
		return requireFaceID([]appwire.Ask{targetAsk}, call.FaceID)
	default:
		return nil
	}
}

type answerGuard struct {
	appwire.Engine
	appwire.Talker
	faceID bool
}

func (e answerGuard) AnswerAskWith(agent, id string, answer appwire.AskAnswer) error {
	answer.BeforeResolve = func(asks []appwire.Ask) error { return requireFaceID(asks, e.faceID) }
	return e.Talker.AnswerAskWith(agent, id, answer)
}

func requireFaceID(asks []appwire.Ask, faceID bool) error {
	for _, ask := range asks {
		if ask.Irreversible && !faceID {
			return errNeedsFaceID
		}
	}
	return nil
}

func mustJSON(value any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

func proofDigest(e Queued) string {
	sum := sha256.Sum256(e.Body)
	return hex.EncodeToString(sum[:])
}

// previous returns the stored receipt for (client, number), if any, and the
// client's highest finished queue number watermark (Step 2 item 8, contract S3).
func (r *Runner) previous(client string, number int64) (Receipt, bool, int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	watermark := r.state.Watermarks[client]
	for _, rec := range r.state.Receipts {
		if rec.Client == client && rec.Status != "claiming" && rec.Status != "started" && rec.Number > watermark {
			watermark = rec.Number
		}
	}
	rec, ok := r.state.Receipts[receiptKey(client, number)]
	return rec, ok, watermark
}

func (r *Runner) createdChat(client, clientID string) (string, bool) {
	if client == "" || clientID == "" {
		return "", false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range r.state.Creates[client] {
		if row.ClientID == clientID && row.Agent != "" {
			if row.Transcript != "" && !chatFolderExists(row.Transcript) {
				return "", false
			}
			return row.Agent, true
		}
	}
	return "", false
}

func (r *Runner) recordCreatedChatLocked(client, clientID, agent string, transcript ...string) {
	if client == "" || clientID == "" || agent == "" {
		return
	}
	tr := ""
	if len(transcript) > 0 {
		tr = transcript[0]
	}
	if r.state.Creates == nil {
		r.state.Creates = map[string][]createdChat{}
	}
	list := slices.Clone(r.state.Creates[client])
	for _, prev := range list {
		if prev.ClientID == clientID && tr == "" && prev.Transcript != "" {
			tr = prev.Transcript
		}
	}
	list = slices.DeleteFunc(list, func(c createdChat) bool { return c.ClientID == clientID })
	list = append(list, createdChat{ClientID: clientID, Agent: agent, Transcript: tr})
	if len(list) > clientproto.MaxClientCreates {
		list = slices.Clone(list[len(list)-clientproto.MaxClientCreates:])
	}
	r.state.Creates[client] = list
}

func (r *Runner) deleteCreatedChatLocked(client, clientID string) {
	if client == "" || clientID == "" || len(r.state.Creates[client]) == 0 {
		return
	}
	list := slices.DeleteFunc(slices.Clone(r.state.Creates[client]), func(c createdChat) bool {
		return c.ClientID == clientID
	})
	if len(list) == 0 {
		delete(r.state.Creates, client)
	} else {
		r.state.Creates[client] = list
	}
}

func (r *Runner) keep(receipt Receipt) error {
	return r.keepCreate(receipt, "")
}

func (r *Runner) keepCreate(receipt Receipt, createClientID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.clientActiveLocked(receipt.Client) {
		return nil
	}
	key := receiptKey(receipt.Client, receipt.Number)
	previous, had := r.state.Receipts[key]
	prevCreates := slices.Clone(r.state.Creates[receipt.Client])
	r.state.Receipts[key] = receipt
	if receipt.Method == clientproto.MethodCreate && receipt.Agent != "" && createClientID != "" {
		r.recordCreatedChatLocked(receipt.Client, createClientID, receipt.Agent, receipt.Transcript)
	}
	if err := r.save(); err != nil {
		if had {
			r.state.Receipts[key] = previous
		} else {
			delete(r.state.Receipts, key)
		}
		if prevCreates == nil {
			delete(r.state.Creates, receipt.Client)
		} else {
			r.state.Creates[receipt.Client] = prevCreates
		}
		return err
	}
	return nil
}

// finishCall records the finished receipt, advances the client's watermark,
// records or cleans up any created chat mapping, and appends the entry's
// outcome in one atomic save of runner.json (Step 2 items 8, 9, and 11).
func (r *Runner) finishCall(receipt Receipt, outcome clientproto.Outcome, createClientID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.clientActiveLocked(receipt.Client) {
		return nil
	}
	if !outcome.Valid() {
		return clientproto.ErrOutcome
	}
	oldReceipts := maps.Clone(r.state.Receipts)
	oldWatermark, hadWatermark := r.state.Watermarks[receipt.Client]
	oldOutcomes := slices.Clone(r.state.Outcomes[receipt.Client])
	oldCreates := slices.Clone(r.state.Creates[receipt.Client])

	for key, prev := range r.state.Receipts {
		if prev.Client == receipt.Client && prev.Number < receipt.Number {
			delete(r.state.Receipts, key)
		}
	}
	r.state.Receipts[receiptKey(receipt.Client, receipt.Number)] = receipt
	if r.state.Watermarks == nil {
		r.state.Watermarks = map[string]int64{}
	}
	if receipt.Number > r.state.Watermarks[receipt.Client] {
		r.state.Watermarks[receipt.Client] = receipt.Number
	}
	if receipt.Method == clientproto.MethodCreate && createClientID != "" {
		if outcome.Status == clientproto.OutcomeDone || (receipt.Agent != "" && chatFolderExists(receipt.Transcript)) {
			r.recordCreatedChatLocked(receipt.Client, createClientID, receipt.Agent, receipt.Transcript)
		} else {
			r.deleteCreatedChatLocked(receipt.Client, createClientID)
		}
	}
	if r.state.Outcomes == nil {
		r.state.Outcomes = map[string][]clientproto.Outcome{}
	}
	list := slices.Clone(r.state.Outcomes[receipt.Client])
	alreadyRecorded := slices.ContainsFunc(list, func(o clientproto.Outcome) bool {
		return o.Number == outcome.Number
	})
	if !alreadyRecorded {
		list = append(list, outcome)
		if len(list) > clientproto.MaxOutcomes {
			list = slices.Clone(list[len(list)-clientproto.MaxOutcomes:])
		}
		r.state.Outcomes[receipt.Client] = list
	}
	if err := r.save(); err != nil {
		r.state.Receipts = oldReceipts
		if hadWatermark {
			r.state.Watermarks[receipt.Client] = oldWatermark
		} else {
			delete(r.state.Watermarks, receipt.Client)
		}
		if oldOutcomes == nil {
			delete(r.state.Outcomes, receipt.Client)
		} else {
			r.state.Outcomes[receipt.Client] = oldOutcomes
		}
		if oldCreates == nil {
			delete(r.state.Creates, receipt.Client)
		} else {
			r.state.Creates[receipt.Client] = oldCreates
		}
		return err
	}
	return nil
}

// ack removes a finished or dropped entry from the relay queue. When an entry
// was refused, expired, replayed, or dropped before Step 10 claimed it, the
// relay requires the caller to claim ownership before DELETE; if the entry was
// addressed to another specific computer and a lying relay listed it anyway,
// the relay's 401 refusal on DELETE is treated as done so the local queue does
// not wedge.
func (r *Runner) ack(ctx context.Context, clientID string, e Queued) error {
	path := queuePath(clientID, e.Number)
	err := r.request(ctx, "DELETE", path, nil, nil)
	if err == nil || strings.HasSuffix(err.Error(), "HTTP 404") {
		return nil
	}
	self := ""
	if r.cfg.Signer != nil {
		self = r.cfg.Signer.Cert().DeviceID()
	}
	if e.Target != "any" && e.Target != self && strings.HasSuffix(err.Error(), "HTTP 401") {
		return nil
	}
	if strings.HasSuffix(err.Error(), "HTTP 409") {
		if claimErr := r.request(ctx, "POST", path+"/claim", nil, nil); claimErr != nil {
			if strings.HasSuffix(claimErr.Error(), "HTTP 404") {
				return nil
			}
			if e.Target != "any" && e.Target != self && strings.HasSuffix(claimErr.Error(), "HTTP 401") {
				return nil
			}
			return err
		}
		delErr := r.request(ctx, "DELETE", path, nil, nil)
		if delErr == nil || strings.HasSuffix(delErr.Error(), "HTTP 404") {
			return nil
		}
		return delErr
	}
	return err
}
