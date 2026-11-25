package serve

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
	"github.com/Agent-Field/codeaf/internal/teams"
)

const (
	defaultHostedSessionCap = 5.00
	hostedModelIdle         = 10 * time.Minute
	hostedModelInFlight     = 4

	hostedChatLimit  = "This chat has reached its spending limit of %s, so it is paused. Start a new chat to carry on."
	hostedMonthLimit = "You have reached your monthly limit of %s, so this chat is paused. The organization's operator can raise it in Settings."
	hostedModelBusy  = "This chat is already making as many requests as it may at once. Try again in a moment."
	hostedOwnKey     = "Your own model key could not be opened on this server, so this chat paused instead of using the organization's."
	// A provider that refuses a key (401 or 403) is a fact about the key, not a
	// hiccup: the chat says so once, in a sentence, and is not retried.
	hostedOrgKeyRefused = "The organization's model access was refused by its provider, so this chat paused. Ask the organization's operator to check the model key."
	hostedOwnKeyRefused = "Your own model key was refused by its provider, so this chat paused. Replace it or remove it, then send your message again."
	hostedLimitCheck    = "This chat could not check its limit just now."
)

var hostedCompletionPath = modelapi.ChatURL("")

type modelRegistry struct {
	mu      sync.Mutex
	entries map[string]*hostedModel
	closed  bool
}

type hostedModel struct {
	server   *modelapi.Server
	person   string
	capUSD   float64
	inFlight int
	idle     *time.Timer
	closing  chan struct{}

	// Only the two metering callbacks set this flag, for the duration of
	// their synchronous call into modelapi's bank. Receipts keep the original
	// flag even when a later request has already selected a different key.
	billingMu sync.Mutex
	ownCharge bool
}

type hostedModelRefusal struct {
	status  int
	message string
}

func (h *Handler) serveHostedCompletion(w http.ResponseWriter, r *http.Request, bearer string, row bearerRow) {
	cfg := h.config.Hosted
	if h.config.Store == nil {
		writeHostedModelError(w, http.StatusServiceUnavailable, hostedLimitCheck)
		return
	}
	space, err := h.ensureSpace(row.person)
	if err != nil {
		writeHostedModelError(w, http.StatusServiceUnavailable, hostedLimitCheck)
		return
	}
	capUSD := cfg.SessionCapUSD
	if !(capUSD > 0) {
		capUSD = defaultHostedSessionCap
	}
	policy, err := h.config.Store.OrgPolicy()
	if err != nil {
		writeHostedModelError(w, http.StatusServiceUnavailable, hostedLimitCheck)
		return
	}
	if override := policy.Hosting.MaxSessionCostUSD; override > 0 {
		capUSD = override
	}
	entry, refusal := cfg.state.models.get(r.Context(), h, bearer, row, space, capUSD)
	if entry == nil {
		if refusal.status == http.StatusUnauthorized {
			h.modelAccessEnded(w)
		} else if refusal.status != 0 {
			writeHostedModelError(w, refusal.status, refusal.message)
		}
		return
	}
	defer cfg.state.models.release(row.session, entry)

	key, own, err := h.chooseHostedModelKey(row.person)
	if err != nil {
		writeHostedModelError(w, http.StatusPaymentRequired, hostedOwnKey)
		return
	}
	call := &hostedModelCall{key: key, own: own}
	request := r.Clone(context.WithValue(r.Context(), hostedModelCallKey{}, call))
	request.URL.Path = modelapi.ChatURL("/v1")
	request.URL.RawPath = ""
	request.Header.Set("Authorization", "Bearer "+entry.server.API().Token)
	reply := &hostedModelWriter{ResponseWriter: w, call: call, capUSD: capUSD}
	entry.server.Handler().ServeHTTP(reply, request)
}

// get reserves a place through the last write of the response. A closing
// entry stays visible until its receipts have reached the ledger.
func (m *modelRegistry) get(ctx context.Context, h *Handler, bearer string, row bearerRow, space factory.OrgWorkstream, capUSD float64) (*hostedModel, hostedModelRefusal) {
	for {
		m.mu.Lock()
		entry := m.entries[row.session]
		if entry != nil && entry.closing != nil {
			done := entry.closing
			m.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return nil, hostedModelRefusal{}
			}
		}
		if entry != nil && entry.capUSD != capUSD && !m.closed {
			// The model server's reservations use its starting ceiling. Settle the
			// old server before opening one under the new cap so its charges count.
			if entry.inFlight != 0 {
				m.mu.Unlock()
				return nil, hostedModelRefusal{http.StatusTooManyRequests, hostedModelBusy}
			}
			m.startClosing(entry)
			m.mu.Unlock()
			m.finishClosing(row.session, entry)
			continue
		}
		entry, refusal := m.admit(h, bearer, row, space, capUSD, entry)
		m.mu.Unlock()
		return entry, refusal
	}
}

// admit holds the registry lock while checking spend and installing a new
// server so a replacement always starts with the last server's settled spend.
func (m *modelRegistry) admit(h *Handler, bearer string, row bearerRow, space factory.OrgWorkstream, capUSD float64, entry *hostedModel) (*hostedModel, hostedModelRefusal) {
	if m.closed {
		return nil, hostedModelRefusal{http.StatusServiceUnavailable, modelNotAnswer}
	}
	// Revocation can race a request that passed the first check or was
	// waiting for an idle server to finish its receipts.
	if _, live := h.config.Hosted.state.bearers.lookup(bearer, h.clock()); !live || entry != nil && entry.person != row.person {
		return nil, hostedModelRefusal{http.StatusUnauthorized, modelEnded}
	}
	spent, err := h.config.Store.ChatSpend(space.ID, "hosted:"+row.session)
	if err != nil {
		return nil, hostedModelRefusal{http.StatusServiceUnavailable, hostedLimitCheck}
	}
	if spent >= capUSD {
		return nil, hostedModelRefusal{http.StatusPaymentRequired, fmt.Sprintf(hostedChatLimit, limitMoney(capUSD))}
	}
	if limit, spent := h.spaceMonth(space.ID); spent >= limit {
		return nil, hostedModelRefusal{http.StatusPaymentRequired, fmt.Sprintf(hostedMonthLimit, limitMoney(limit))}
	}
	if entry == nil {
		entry, err = h.openHostedModel(row, space, capUSD-spent)
		if err != nil {
			log.Print("hosted model: could not open the local model server")
			return nil, hostedModelRefusal{http.StatusServiceUnavailable, modelNotAnswer}
		}
		if m.entries == nil {
			m.entries = make(map[string]*hostedModel)
		}
		entry.capUSD = capUSD
		m.entries[row.session] = entry
	}
	if entry.inFlight >= hostedModelInFlight {
		return nil, hostedModelRefusal{http.StatusTooManyRequests, hostedModelBusy}
	}
	if entry.idle != nil {
		entry.idle.Stop()
		entry.idle = nil
	}
	entry.inFlight++
	return entry, hostedModelRefusal{}
}

func (m *modelRegistry) release(chat string, entry *hostedModel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry.inFlight--
	if entry.inFlight != 0 || entry.closing != nil {
		return
	}
	var idle *time.Timer
	idle = time.AfterFunc(hostedModelIdle, func() {
		m.mu.Lock()
		// A new call may have stopped this timer while its callback waited
		// for the lock. Only the current idle period can end the server.
		if entry.idle != idle {
			m.mu.Unlock()
			return
		}
		m.startClosing(entry)
		m.mu.Unlock()
		m.finishClosing(chat, entry)
	})
	entry.idle = idle
}

// forget closes a chat's server. Stop and delete supply the verified person
// because an unsuccessful attempt to stop someone else's chat cannot end it.
func (m *modelRegistry) forget(chat string, person ...string) {
	m.mu.Lock()
	entry := m.entries[chat]
	if entry == nil || len(person) > 0 && entry.person != person[0] {
		m.mu.Unlock()
		return
	}
	if entry.closing != nil {
		done := entry.closing
		m.mu.Unlock()
		<-done
		return
	}
	m.startClosing(entry)
	m.mu.Unlock()
	m.finishClosing(chat, entry)
}

func (m *modelRegistry) startClosing(entry *hostedModel) {
	entry.closing = make(chan struct{})
	if entry.idle != nil {
		entry.idle.Stop()
		entry.idle = nil
	}
}

func (m *modelRegistry) finishClosing(chat string, entry *hostedModel) {
	_ = entry.server.Close()
	m.mu.Lock()
	delete(m.entries, chat)
	close(entry.closing)
	m.mu.Unlock()
}

func (h *Handler) closeHostedModels() {
	cfg := h.config.Hosted
	if cfg == nil || cfg.state == nil {
		return
	}
	m := &cfg.state.models
	m.mu.Lock()
	m.closed = true
	chats := make([]string, 0, len(m.entries))
	for chat := range m.entries {
		chats = append(chats, chat)
	}
	m.mu.Unlock()
	// Closing can wait for receipts. Wait for the chats in parallel so one
	// chat's settlement does not delay the start of another's.
	var closed sync.WaitGroup
	for _, chat := range chats {
		closed.Go(func() { m.forget(chat) })
	}
	closed.Wait()
}

// hostedKeyForHook replaces the sealed store in tests that need a person's own
// key to appear, disappear or fail without sealing one. It is nil in every
// running server.
var hostedKeyForHook func(person, provider string) (string, bool, error)

// hostedKeyLookup is how a person's own key is found: the sealed store, except
// in the tests that replace it.
func (h *Handler) hostedKeyLookup() func(person, provider string) (string, bool, error) {
	if hostedKeyForHook != nil {
		return hostedKeyForHook
	}
	return h.hostedKeyFor
}

func (h *Handler) chooseHostedModelKey(person string) (string, bool, error) {
	if key, own, err := h.hostedKeyLookup()(person, modelsource.DefaultID); err != nil || own {
		return key, own, err
	}
	cfg := h.config.Hosted
	if key := cfg.state.upstreamKey; key != "" {
		return key, false, nil
	}
	if key := h.orgModelKey(); key != "" {
		return key, false, nil
	}
	if cfg.ModelUpstream != "" {
		return "keyless", false, nil
	}
	return "", false, nil
}

type hostedModelCallKey struct{}

type hostedModelCall struct {
	key      string
	own      bool
	called   atomic.Bool
	rejected atomic.Pointer[string]
}

type hostedModelCompleter func(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error)

func (c hostedModelCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	return c(ctx, messages, options...)
}

func (h *Handler) openHostedModel(row bearerRow, space factory.OrgWorkstream, ceiling float64) (*hostedModel, error) {
	entry := &hostedModel{person: row.person}
	cfg := h.config.Hosted
	server, err := modelapi.Open(modelapi.Config{
		Ceiling: ceiling,
		Role:    lanes.RoleLeafUnattended,
		Node:    "hosted",
		CompleterFor: func(model string) modelapi.Completer {
			return hostedModelCompleter(func(ctx context.Context, messages []ai.Message, opts ...ai.Option) (*ai.Response, error) {
				call, ok := ctx.Value(hostedModelCallKey{}).(*hostedModelCall)
				if !ok {
					return nil, errors.New(modelEnded)
				}
				factory := cfg.CompleterFor(cfg.ModelUpstream, call.key)
				if factory == nil {
					return nil, errors.New(modelNotAnswer)
				}
				completer := factory(model)
				if completer == nil {
					return nil, errors.New(modelNotAnswer)
				}

				ctx = entry.withBillingSource(ctx, call.own)
				call.called.Store(true)
				answer, err := completer.CompleteWithMessages(ctx, messages, opts...)
				// THE PROVIDER REFUSING THE KEY IS ANSWERED AS A REFUSAL, NEVER AS A
				// BAD GATEWAY. A 502 tells the engine the model may answer later, so it
				// retried about once a minute while nothing said why. A 401 is its
				// "this account is not served" answer: it stops, and the person reads
				// the sentence. The provider's own words are not passed on (they can
				// echo a key); the operator gets one line naming the cause.
				if status, _ := provider.StatusOf(err); status == http.StatusUnauthorized || status == http.StatusForbidden {
					message := hostedOrgKeyRefused
					if call.own {
						message = hostedOwnKeyRefused
					}
					call.rejected.Store(&message)
					cfg.state.refusals.refused(h.clock(), call.own, row.person, status)
				} else if err == nil {
					cfg.state.refusals.accepted(call.own, row.person)
				}
				return answer, err
			})
		},
		Bank: func(charge modelapi.Charge) {
			h.postHostedSpend(space, row, entry.ownCharge, charge.CostUSD)
		},
		Unbilled: func(string) {
			log.Print("hosted model: one call could not be priced")
		},
	})
	if err != nil {
		return nil, err
	}
	entry.server = server
	return entry, nil
}

func (m *hostedModel) withBillingSource(ctx context.Context, own bool) context.Context {
	bank := provider.BillingSinkFrom(ctx)
	receipt := provider.ReconcileSinkFrom(ctx)
	ctx = provider.WithBilling(ctx, func(bill provider.Billed) {
		m.billingMu.Lock()
		defer m.billingMu.Unlock()
		m.ownCharge = own
		bank(bill)
	})
	return provider.WithReconcile(ctx, func(bill provider.Reconciled) {
		m.billingMu.Lock()
		defer m.billingMu.Unlock()
		m.ownCharge = own
		receipt(bill)
	})
}

func (h *Handler) postHostedSpend(space factory.OrgWorkstream, row bearerRow, own bool, usd float64) {
	if usd == 0 {
		return
	}
	now := h.clock().UTC()
	var suffix [2]byte
	_, _ = rand.Read(suffix[:])
	chat := "hosted:" + row.session
	key := fmt.Sprintf("%s:%d:%x", chat, now.UnixNano(), suffix)
	if own {
		chat += ":own"
	}
	spend := store.Spend{At: now.Format(time.RFC3339Nano), By: row.person, USD: usd, Chat: chat, Key: key}
	// A charge approximates who prompted it by recording whoever last put words into the chat through this server.
	if asker := h.config.Hosted.state.askerOf(row.session); asker != row.person {
		spend.Asker = asker
	}
	if _, err := h.config.Store.AddSpend(space.ID, spend); err != nil {
		log.Print("hosted model: could not post a charge")
		return
	}
	h.recordAs(context.Background(), row.person, record.WorkstreamChanged, "posted spend", orgObjectWorkstream+space.ID,
		strconv.FormatFloat(usd, 'f', -1, 64)+" USD by a hosted chat")
	h.publishWorkstreamPart(space, api.PartSpend, "", row.person, 0, false)
}

func (h *Handler) modelAccessEnded(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="model"`)
	if h.config.Hosted.CompleterFor == nil {
		modelRefuse(w, http.StatusUnauthorized, modelEnded)
		return
	}
	writeHostedModelError(w, http.StatusUnauthorized, modelEnded)
}

func writeHostedModelError(w http.ResponseWriter, status int, message string) {
	kind := "invalid_request_error"
	switch status {
	case http.StatusPaymentRequired:
		kind = "insufficient_quota"
	case http.StatusTooManyRequests:
		kind = "rate_limit"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// THE DOOR SIGNS ITS OWN SENTENCES. A hosted chat's client reads a 402 from
	// anywhere as "the account cannot pay" and names the vendor, which blamed
	// OpenRouter and the person for a limit the organization chose. The mark is
	// how the client tells this server's sentence, which it shows as written, from
	// a provider's refusal passed through untouched, which keeps its own wording;
	// it never has to guess from the prose (internal/provider/orgdoor.go).
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"message": message, "type": kind, "code": status, provider.DoorKey: provider.OrganizationDoor},
	})
}

// limitMoney writes a limit the way the person who set it wrote it, and never as
// a different number. A whole number of cents keeps its two places ($5.00, $0.25,
// $20.00); a limit finer than a cent is spelled as chosen by the one door that
// spells a cap (teams.Money: $0.001). The sentence used to round everything to the
// cent, so a limit of a tenth of a cent read "$0.00" — a refusal quoting a limit
// of nothing, for a chat that had been given one.
func limitMoney(usd float64) string {
	if cents := usd * 100; math.Abs(cents-math.Round(cents)) < 1e-9 {
		return fmt.Sprintf("$%.2f", usd)
	}
	return teams.Money(usd)
}

// hostedModelWriter keeps the local API's reservation language and dollars
// on the server. It passes each SSE write through as it arrives.
type hostedModelWriter struct {
	http.ResponseWriter
	call        *hostedModelCall
	capUSD      float64
	replaceBody bool
}

func (w *hostedModelWriter) WriteHeader(status int) {
	switch {
	case status == http.StatusPaymentRequired && !w.call.called.Load():
		w.replaceBody = true
		writeHostedModelError(w.ResponseWriter, status, fmt.Sprintf(hostedChatLimit, limitMoney(w.capUSD)))
	case status == http.StatusTooManyRequests && w.Header().Get(modelapi.HeldHeader) != "":
		w.replaceBody = true
		w.Header().Del(modelapi.HeldHeader)
		writeHostedModelError(w.ResponseWriter, status, hostedModelBusy)
	case status == http.StatusUnauthorized:
		// A stop can close the in-process server just before it admits a call.
		w.replaceBody = true
		w.Header().Set("WWW-Authenticate", `Bearer realm="model"`)
		writeHostedModelError(w.ResponseWriter, status, modelEnded)
	case status == http.StatusBadGateway && w.call.rejected.Load() != nil:
		w.ResponseWriter.WriteHeader(http.StatusUnauthorized)
	default:
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *hostedModelWriter) Write(p []byte) (int, error) {
	if w.replaceBody {
		return len(p), nil
	}
	// modelapi writes each JSON object in one Write, with SSE framing in
	// separate writes. Leave that framing and keepalive writes untouched.
	data := p
	if len(p) != 0 && p[0] == '{' {
		data = w.hostedJSON(p)
	}
	n, err := w.ResponseWriter.Write(data)
	if n == len(data) {
		return len(p), err
	}
	return min(n, len(p)), err
}

func (w *hostedModelWriter) hostedJSON(p []byte) []byte {
	if bytes.Contains(p, []byte(`"usage":`)) || w.call.rejected.Load() != nil {
		var fields map[string]json.RawMessage
		if json.Unmarshal(p, &fields) == nil {
			changed := false
			var usage map[string]json.RawMessage
			if json.Unmarshal(fields["usage"], &usage) == nil && usage["cost"] != nil {
				delete(usage, "cost")
				fields["usage"], _ = json.Marshal(usage)
				changed = true
			}
			if message := w.call.rejected.Load(); message != nil && fields["error"] != nil {
				fields["error"], _ = json.Marshal(map[string]any{"code": http.StatusUnauthorized, "message": *message})
				changed = true
			}
			if changed {
				data, _ := json.Marshal(fields)
				if p[len(p)-1] == '\n' {
					data = append(data, '\n')
				}
				p = data
			}
		}
	}
	if key := w.call.key; key != "" && (key != "keyless" || w.call.own) {
		p = scrubHostedModelKey(p, key)
	}
	return p
}

func scrubHostedModelKey(p []byte, key string) []byte {
	encoded, _ := json.Marshal(key)
	if !bytes.Contains(p, encoded[1:len(encoded)-1]) {
		return p
	}
	// An upstream error or answer can echo a key. Redact only JSON strings so
	// short keys cannot change field names, numbers or the response framing.
	var value any
	decoder := json.NewDecoder(bytes.NewReader(p))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return p
	}
	clean, _ := json.Marshal(scrubHostedModelValue(value, key))
	if p[len(p)-1] == '\n' {
		clean = append(clean, '\n')
	}
	return clean
}

func scrubHostedModelValue(value any, key string) any {
	switch value := value.(type) {
	case string:
		return strings.ReplaceAll(value, key, "[redacted]")
	case []any:
		for i, child := range value {
			value[i] = scrubHostedModelValue(child, key)
		}
	case map[string]any:
		for field, child := range value {
			value[field] = scrubHostedModelValue(child, key)
		}
	}
	return value
}

func (w *hostedModelWriter) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
