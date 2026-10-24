package clientrun

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/session"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

// Call is sealed to the executor's public box key from the pairing grant.
// FaceID is a client assertion covered by the original signed ciphertext.
type Call struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	FaceID bool            `json:"face_id,omitempty"`
	// SkipThrough explicitly authorizes missing earlier numbers for this executor.
	SkipThrough *int64 `json:"skip_through,omitempty"`
}

func (r *Runner) drain(ctx context.Context, c Client) error {
	return r.request(ctx, "GET", clientPath(c.ID)+"/queue", nil, func(reader io.Reader) error {
		dec := json.NewDecoder(reader)
		if err := queueStart(dec); err != nil {
			return err
		}
		var last int64
		for count := 0; dec.More(); count++ {
			if count >= clientproto.MaxClientQueue {
				return errors.New("relay queue exceeds its entry limit")
			}
			var entry Queued
			if err := dec.Decode(&entry); err != nil {
				return err
			}
			if entry.Number <= last {
				return errors.New("relay queue is not in sequence order")
			}
			last = entry.Number
			if err := r.execute(ctx, c, entry); err != nil {
				return err
			}
		}
		return queueEnd(dec)
	})
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

func (r *Runner) execute(ctx context.Context, c Client, e Queued) error {
	call, agent, err := r.prepare(ctx, c, e)
	if err != nil {
		return err
	}
	receipt, known, err := r.previous(c.ID, e, call.SkipThrough)
	if err != nil {
		return err
	}
	if !known || receipt.Status == "claiming" {
		if err := r.checkAsk(call, agent); err != nil {
			refusal := Receipt{Client: c.ID, Number: e.Number, Agent: agent, Method: call.Method, At: r.cfg.Now().UTC().Format(time.RFC3339Nano), Status: "refused", Reason: err.Error()}
			return errors.Join(err, r.log(refusal))
		}
	}
	if !known || receipt.Status == "claiming" {
		if !known {
			token := make([]byte, 32)
			if _, err := rand.Read(token); err != nil {
				return err
			}
			receipt = Receipt{Client: c.ID, Number: e.Number, Agent: agent, Method: call.Method, At: r.cfg.Now().UTC().Format(time.RFC3339Nano), Status: "claiming", Digest: proofDigest(e), ClaimToken: hex.EncodeToString(token), SkippedFrom: receipt.SkippedFrom, SkippedThrough: receipt.SkippedThrough}
			if err := r.keep(receipt); err != nil {
				return err
			}
		}
		if err := r.request(ctx, "POST", queuePath(c.ID, e.Number)+"/claim", map[string]string{"token": receipt.ClaimToken}, nil); err != nil {
			return err
		}
		// Claims do not authorize a displaced or revoked executor. Check again
		// after the round trip before recording and dispatching the mutation.
		if err := r.admit(ctx, c, e, agent); err != nil {
			return err
		}
		receipt, err = r.dispatch(c, e, call, agent, receipt)
		if err != nil {
			return err
		}
	}
	// A receipt left at started is uncertain, never a reason to run again.
	if err := r.log(receipt); err != nil {
		return err
	}
	ackErr := r.request(ctx, "DELETE", queuePath(c.ID, e.Number), nil, nil)
	var callErr error
	if receipt.Status == "failed" {
		callErr = errors.New(receipt.Reason)
	}
	return errors.Join(callErr, ackErr)
}

func (r *Runner) prepare(ctx context.Context, c Client, e Queued) (Call, string, error) {
	if err := r.verify(c, e); err != nil {
		return Call{}, "", err
	}
	if e.Target != "any" && e.Target != r.cfg.Signer.Cert().DeviceID() {
		return Call{}, "", errors.New("queued call targets another executor")
	}
	call, err := r.openCall(e)
	if err != nil {
		return Call{}, "", err
	}
	if call.Method != e.Method || !slices.Contains(c.Methods, call.Method) {
		return Call{}, "", errors.New("queued method is outside its pairing")
	}
	agent, err := callAgent(call)
	if err != nil {
		return Call{}, "", err
	}
	return call, agent, r.admit(ctx, c, e, agent)
}

func (r *Runner) dispatch(c Client, e Queued, call Call, agent string, skipped Receipt) (Receipt, error) {
	receipt := Receipt{ClaimToken: skipped.ClaimToken, SkippedFrom: skipped.SkippedFrom, SkippedThrough: skipped.SkippedThrough, Client: c.ID, Number: e.Number, Agent: agent, Method: call.Method, At: r.cfg.Now().UTC().Format(time.RFC3339Nano), Status: "started", Digest: proofDigest(e)}
	if err := r.keep(receipt); err != nil {
		return receipt, err
	}
	if err := r.log(receipt); err != nil {
		return receipt, err
	}
	engine := r.cfg.Engine
	if talker, ok := engine.(appwire.Talker); ok && call.Method == "asks.answer" {
		engine = answerGuard{Engine: engine, Talker: talker, faceID: call.FaceID}
	}
	if call.Method == "agents.create" {
		engine = creationRecorder{Engine: engine, reserve: func(transcript string) error {
			folder, ok := session.FolderOf(transcript)
			if !ok {
				return errors.New("created chat has no transcript folder")
			}
			receipt.Agent = filepath.Base(folder)
			receipt.Transcript = transcript
			return r.keep(receipt)
		}, record: func(agent appwire.Agent) error {
			receipt.Agent = agent.ID
			if err := r.keep(receipt); err != nil {
				return err
			}
			return r.log(receipt)
		}}
	}
	result, callErr := appwire.Call(engine, call.Method, call.Params)
	receipt.Status = "done"
	if callErr != nil {
		receipt.Status = "failed"
		receipt.Reason = callErr.Error()
	}
	if call.Method == "agents.create" && callErr == nil {
		// Creation has no prior chat. Its returned chat receives the final log;
		// the started receipt already protects the any-box claim against replay.
		var created struct {
			Agent appwire.Agent `json:"agent"`
		}
		if err := json.Unmarshal(mustJSON(result), &created); err != nil {
			return receipt, err
		}
		receipt.Agent = created.Agent.ID
	}
	return receipt, r.keep(receipt)
}

// creationRecorder installs a durable handoff through the ordinary create
// request. The app records the chat before releasing its first turn.
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

func (r *Runner) openCall(e Queued) (Call, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.state.Secret) != 32 {
		return Call{}, errors.New("executor encryption key is missing")
	}
	pub, err := curve25519.X25519(r.state.Secret, curve25519.Basepoint)
	if err != nil {
		return Call{}, err
	}
	sealed, err := executorBox(e.Sealed, r.cfg.Signer.Cert().DeviceID())
	if err != nil {
		return Call{}, err
	}
	plain, ok := box.OpenAnonymous(nil, sealed, (*[32]byte)(pub), (*[32]byte)(r.state.Secret))
	if !ok {
		return Call{}, errors.New("queued call could not be opened")
	}
	var call Call
	if err := strictJSON(plain, &call); err != nil {
		return Call{}, err
	}
	return call, nil
}

// callAgent restricts queued mutations to methods with an explicit, understood
// routing rule. Unknown scope is refused before dispatch, even if allowlisted.
func callAgent(call Call) (string, error) {
	var p struct {
		Agent string `json:"agent"`
		ID    string `json:"id"`
	}
	if err := json.Unmarshal(call.Params, &p); err != nil {
		return "", err
	}
	switch call.Method {
	case "asks.answer":
		if p.Agent != "" {
			return p.Agent, nil
		}
	case "agents.send", "agents.stop":
		if p.ID != "" {
			return p.ID, nil
		}
	case "agents.create":
		return "", nil
	}
	return "", errors.New("queued method has no supported routing rule")
}

func (r *Runner) admit(ctx context.Context, c Client, e Queued, agent string) error {
	l, err := r.cfg.Directory.List(ctx)
	if err != nil {
		return err
	}
	d, ok := l.Devices[c.ID]
	if !ok || d.Kind != "client" || d.Revoked || !slices.Contains(d.Methods, e.Method) {
		return errors.New("queued client is revoked or method no longer allowed")
	}
	if !r.enabled(c.ID) {
		return errors.New("queued client is paused or unpaired")
	}
	if agent == "" {
		if e.Target != "any" {
			return errors.New("new work must target any executor")
		}
		return nil
	}
	cell, ok := l.Cells[agent]
	if !ok || cell.Archived || cell.Lease.Device != r.cfg.Signer.Cert().DeviceID() || cell.Lease.Expires <= l.Now {
		return errors.New("queued chat is not leased by this executor")
	}
	return nil
}

func (r *Runner) checkAsk(call Call, agent string) error {
	if call.Method != "asks.answer" {
		return nil
	}
	// The selected ask gets a preflight check; Talker additionally checks its
	// fixed batch through answerGuard during dispatch, after the claim trip.
	var params struct {
		ID        string `json:"id"`
		BatchItem string `json:"batch_item"`
		Later     bool   `json:"later"`
		YouDecide bool   `json:"you_decide"`
	}
	if err := json.Unmarshal(call.Params, &params); err != nil {
		return err
	}
	if params.BatchItem != "" && !params.Later && !params.YouDecide {
		params.ID = params.BatchItem
	}
	result, err := appwire.Call(r.cfg.Engine, "asks.list", mustJSON(appwire.AgentParams{Agent: agent}))
	if err != nil {
		return err
	}
	for _, ask := range result.(*appwire.AsksResult).Asks {
		if ask.ID == params.ID {
			return requireFaceID([]appwire.Ask{ask}, call.FaceID)
		}
	}
	return errors.New("queued ask is no longer open")
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
			return errors.New("irreversible ask requires Face ID")
		}
	}
	return nil
}

func mustJSON(value any) json.RawMessage { raw, _ := json.Marshal(value); return raw }
func receiptKey(client string, number int64) string {
	return client + "/" + strconv.FormatInt(number, 10)
}
func proofDigest(e Queued) string { sum := sha256.Sum256(e.Body); return hex.EncodeToString(sum[:]) }

func (r *Runner) previous(client string, e Queued, skip *int64) (Receipt, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if previous, ok := r.state.Receipts[receiptKey(client, e.Number)]; ok {
		if previous.Digest != proofDigest(e) {
			return Receipt{}, false, errors.New("queued number reuses another call")
		}
		return previous, true, nil
	}
	var last int64
	for _, receipt := range r.state.Receipts {
		if receipt.Client == client {
			last = max(last, receipt.Number)
		}
	}
	if e.Number != last+1 {
		if e.Number > last+1 && skip != nil && *skip == e.Number-1 {
			return Receipt{SkippedFrom: last + 1, SkippedThrough: e.Number - 1}, false, nil
		}
		return Receipt{}, false, fmt.Errorf("queued sequence gap: got %d, expected %d", e.Number, last+1)
	}
	return Receipt{}, false, nil
}
func (r *Runner) keep(receipt Receipt) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := receiptKey(receipt.Client, receipt.Number)
	previous, had := r.state.Receipts[key]
	r.state.Receipts[key] = receipt
	if err := r.save(); err != nil {
		if had {
			r.state.Receipts[key] = previous
		} else {
			delete(r.state.Receipts, key)
		}
		return err
	}
	return nil
}
func (r *Runner) log(receipt Receipt) error {
	if r.cfg.Log == nil {
		return errors.New("queued calls need a chat logger")
	}
	return r.cfg.Log(receipt)
}
func (r *Runner) enabled(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.state.Clients[id]
	return ok && !c.Paused
}
