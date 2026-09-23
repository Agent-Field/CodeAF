package modelapi

// The server: one per run of a program, on this machine's loopback, opened by
// one token, closed when the run ends.
//
// ── EVERY CALL IS A TURN OF A CONVERSATION A PERSON CAN READ ────────────────
//
// To the program this is a model backend like any other. To codeaf the program
// is a very particular person asking it things, so every call is written down
// as one turn of that conversation (delegate.Turn, in the task's own record
// folder): once when it starts, so the task page can show a call in flight, and
// once when it ends, under the same number.
//
// ── MONEY IS METERED HERE, CALL BY CALL, AND NOWHERE ELSE ───────────────────
//
// The funnel tells whoever armed the call what each answer cost the moment it
// decodes it (provider.WithBilling), and a receipt fetched later for a stream
// that was cut before its usage block (provider.WithReconcile). Both reach the
// run through [Config.Bank] as they happen, so the run's ceiling, the task's
// spend rows and the machine's spending ledger all see a call's dollars before
// the program does. The program's own account of what it spent is never read.
//
// ── THE CEILING IS A REFUSAL BEFORE THE CALL ────────────────────────────────
//
// A call made once the run's metered spend has reached its dollar ceiling is
// never made: it is answered 402 in the router's own shape and written down as
// a refused turn. A call already in flight when the ceiling is crossed is not
// cut here — the run's supervisor ends the program for that, the way it ends
// any worker whose run has spent its allowance.

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/guard"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// Completer is the funnel one call goes out through. It is provider.Client's
// own method, and internal/session's Completer is the same one method, so a
// run hands this the conversation's completer as it is and a test hands it a
// script.
type Completer interface {
	CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error)
}

// Charge is one priced answer, as the funnel billed it.
type Charge struct {
	// Model is the model that answered, in the funnel's own spelling.
	Model     string
	TokensIn  int
	TokensOut int
	Cached    int
	CostUSD   float64
	// Spent is the run's metered total with this charge in it. It only rises,
	// and charges are told one at a time in the order they were metered, so a
	// bank that keeps the latest figure is always right.
	Spent float64
	// Late says the charge is a receipt the provider fetched after its call had
	// already returned — a stream cut before its usage block.
	Late bool
}

// Config is one run's API.
type Config struct {
	// TaskDir is the task's record folder, where the conversation log is kept
	// (delegate.ConversationFile). Empty keeps no log.
	TaskDir string
	// CompleterFor answers the funnel a call on model goes out through. Nil is
	// a run with no model road: every call is answered with the sentence that
	// says so, and none is made.
	CompleterFor func(model string) Completer
	// Serves answers whether one of this person's services can take a call on
	// model — the account pool's own test, handed in (internal/session's
	// ServesModel). Nil answers yes for every model.
	Serves func(model string) bool
	// Seat is the run's own work seat: the model a call falls to when the one
	// the program asked for cannot be served on this machine ([Resolve]).
	Seat string
	// Ceiling is the run's dollar ceiling, zero for none.
	Ceiling float64
	// Bank is told every charge as it is metered. It is called one charge at a
	// time and must not block on the program.
	Bank func(Charge)
	// Unbilled is told a call the provider charged for and could put no figure
	// on — a cut stream whose receipt never came.
	Unbilled func(model string)
	// Role is the lane role the calls ride: an unattended leaf when nobody is
	// reading, which is a run's worker, and an attended one for a shell run a
	// person is watching. Empty is unattended.
	Role lanes.Role
	// Node names the work the calls belong to in the model-call log — the
	// program's name — so `codeaf logs --node <name>` reads one program's calls.
	// Their tag is `task`, the word every call made inside a piece of work
	// carries (internal/session's purposeTask).
	Node string
	// Keepalive overrides [DefaultKeepalive], for a test that must not wait
	// fifteen seconds to see one.
	Keepalive time.Duration
}

// DefaultKeepalive is how often a waiting answer says it is still coming. It
// is well inside the two-minute idle timeout a program's HTTP client keeps, so
// a model thinking for half an hour never looks like a dead connection.
const DefaultKeepalive = 15 * time.Second

// basePath is the version segment every OpenAI-style base URL ends in, and the
// route is appended to it exactly as a client appends it ([ChatURL]).
const basePath = "/v1"

// closeWait bounds how long [Server.Close] waits for calls already in flight
// to write their last record. Their contexts are ended first, so an honest
// funnel returns at once; the bound is for one that does not.
const closeWait = 10 * time.Second

// Server is one run's model API.
type Server struct {
	config   Config
	listener net.Listener
	server   *http.Server
	base     string
	// ctx ends every call in flight when the run's API closes.
	ctx    context.Context
	cancel context.CancelFunc
	calls  sync.WaitGroup

	// mu guards the token, the ending, the meter, the turn numbers and the
	// threads' memory — everything a call reads and writes that another call
	// may be reading at the same moment.
	mu      sync.Mutex
	token   string
	closed  bool
	spent   float64
	seq     int
	threads threads
	// refused counts the calls answered 402 at the ceiling.
	refused int

	// bankMu keeps charges in the order they were metered, one at a time, and
	// logMu keeps two turns from sharing one write of the log.
	bankMu sync.Mutex
	logMu  sync.Mutex
}

// Open starts one run's API on an OS-chosen 127.0.0.1 port and mints its
// token.
//
// 127.0.0.1 AND NEVER 0.0.0.0, for the file door's reason: the token is the
// only thing between a caller and the person's model account, and a listener
// on every interface hands that account to anybody on the same network who
// can guess a port.
func Open(config Config) (*Server, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("modelapi: %w", err)
	}
	token, err := mint(32)
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("modelapi: mint the run's token: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{config: config, listener: listener, token: token, ctx: ctx, cancel: cancel,
		base: "http://" + listener.Addr().String() + basePath}
	mux := http.NewServeMux()
	mux.HandleFunc(basePath+chatRoute, s.serveChat)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "the model API answers "+basePath+chatRoute+" and nothing else")
	})
	s.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	guard.Go("modelapi/serve", func() { _ = s.server.Serve(listener) })
	return s, nil
}

// API is the address and the token a program is started with
// (delegate.ChildEnv). After [Server.Close] the token is empty: a closed API
// has nothing to hand out.
func (s *Server) API() delegate.ModelAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	return delegate.ModelAPI{BaseURL: s.base, Token: s.token}
}

// Spent is the run's metered total so far.
func (s *Server) Spent() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.spent
}

// RefusedAtCeiling is how many calls were refused because the run's dollar
// ceiling had been reached.
//
// IT IS WHAT TELLS THE CEILING FROM A CRASH. A program that budgets by its own
// sum of each answer's cost can be refused before that sum reaches the
// ceiling it was given — codeaf's meter counts every answer the funnel was
// charged for, retries included — and senior-dev then ends its run as
// `crashed`. The run was stopped by the limit a person set, and the worker
// reads this to say so (internal/run's DelegateWorker).
func (s *Server) RefusedAtCeiling() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refused
}

// refuse counts one call refused at the ceiling.
func (s *Server) refuse() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refused++
}

// Close ends the API: THE TOKEN DIES WITH THE RUN. The token is forgotten,
// every call in flight is ended, the listener and every connection are closed,
// and the calls that were running are given [closeWait] to write their last
// record. A grandchild the program left behind can no longer spend.
func (s *Server) Close() error {
	if !s.end() {
		return nil
	}
	s.cancel()
	err := s.server.Close()
	drained := make(chan struct{})
	guard.Go("modelapi/close", func() {
		s.calls.Wait()
		close(drained)
	})
	select {
	case <-drained:
	case <-time.After(closeWait):
	}
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return err
}

// end marks the API closed and forgets its token, and answers whether this was
// the call that closed it.
func (s *Server) end() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.closed, s.token = true, ""
	return true
}

// enter counts one call in, unless the API has closed. It is taken under the
// same lock Close sets the ending under, so no call is counted after Close has
// begun to wait.
func (s *Server) enter() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.calls.Add(1)
	return true
}

// authorized reports whether a request carries this run's token. The compare
// takes the same time whatever the guess, so a token cannot be read off how
// long a refusal takes.
func (s *Server) authorized(r *http.Request) bool {
	token := s.liveToken()
	if token == "" {
		return false
	}
	given, ok := strings.CutPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ")
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(given)), []byte(token)) == 1
}

// liveToken is the run's token, empty once the API has closed.
func (s *Server) liveToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ""
	}
	return s.token
}

// serveChat is the one route.
func (s *Server) serveChat(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "that token does not open this run's model API")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "the model API answers POST")
		return
	}
	if !s.enter() {
		writeError(w, http.StatusServiceUnavailable, "this run has ended")
		return
	}
	defer s.calls.Done()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("the request is larger than the %d bytes one call may carry", maxRequestBytes))
			return
		}
		writeError(w, http.StatusBadRequest, "the request body could not be read")
		return
	}
	request, err := decodeRequest(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// A PROGRAM MAY NAME ITS LINEAGE IN A HEADER ONLY. The router's
	// session-affinity header is the same fact as the body's prompt_cache_key —
	// which warm instance this conversation belongs on — and codeaf's adapter
	// sends its own header from the key it is handed, so a header with no key
	// beside it is read as the key.
	if request.cacheKey == "" {
		if affinity := strings.TrimSpace(r.Header.Get(affinityHeader)); affinity != "" {
			request.cacheKey, request.thread = affinity, affinity
		}
	}
	s.serve(w, r, request)
}

// affinityHeader is the router's session-affinity header, which a program
// written for OpenRouter sends beside its prompt_cache_key.
const affinityHeader = "X-Session-Affinity"

// record is one call's turn as it stands, shared with the receipt that can
// arrive after the call has ended.
type record struct {
	mu    sync.Mutex
	turn  delegate.Turn
	ended bool
}

// serveOn says the call went out on the seat instead of the ask.
func (r *record) serveOn(seat string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turn.Served = seat
}

// close writes the call's ending onto its turn and answers the turn as it now
// stands, for the log.
func (r *record) close(fill func(turn *delegate.Turn)) delegate.Turn {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turn.Ended = time.Now()
	fill(&r.turn)
	r.ended = true
	return r.turn
}

// open numbers one call, opens its turn with what the thread had not said
// before, names the working the program handed back by the field its thread's
// working last arrived on, and answers the run's spend at the moment the call
// arrived — the figure its ceiling is asked against.
func (s *Server) open(request *call, thread, served string) (*record, float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	entry := &record{turn: delegate.Turn{Seq: s.seq, Thread: thread, Started: time.Now(), Model: request.asked, Served: served}}
	entry.turn.Sent, entry.turn.Restarted = s.threads.delta(thread, request.messages)
	request.reasoning = s.threads.name(thread, request.reasoning)
	return entry, s.spent
}

// arrived remembers the field a thread's working came in on.
func (s *Server) arrived(thread, field string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threads.arrived(thread, field)
}

// serve answers one decoded call: the model decided, the turn opened, the
// ceiling asked, the funnel called with keepalives while it thinks, the model
// fallen back to the seat when the machine could not serve the ask, and the
// answer written in the shape the call asked for.
func (s *Server) serve(w http.ResponseWriter, r *http.Request, request *call) {
	model, served := Resolve(request.asked, s.config.Serves, s.config.Seat)
	thread := request.thread
	if thread == "" {
		thread = delegate.MainThread
	}
	entry, spent := s.open(request, thread, served)

	if ceiling := s.config.Ceiling; ceiling > 0 && spent >= ceiling {
		// 402 AND NOTHING THAT READS AS PASSING: a program's client retries a
		// 408, a 409, a 429 and a 5xx as the weather, and a ceiling is not
		// weather — asked again it answers the same.
		s.refuse()
		refused := ceilingSentence(ceiling, spent)
		s.log(entry.close(func(turn *delegate.Turn) { turn.Refused = refused }))
		writeError(w, http.StatusPaymentRequired, refused)
		return
	}
	s.log(entry.turn)

	ctx, stop := s.callContext(r.Context())
	defer stop()
	out := &reply{w: w, stream: request.stream, id: "gen-" + mustMint(12), created: time.Now().Unix()}
	bill := &tally{}
	catch := &catcher{}
	slot := &provider.ServedEndpoint{}
	response, err := s.complete(ctx, out, request, model, bill, catch, slot, entry)
	// THE ONE FAILURE THE SEAT CAN CURE: the machine could not serve the model
	// the program asked for — no key for its service, or a router that carries
	// no such model — though the account pool believed it could. The call goes
	// out once more, on the seat, and the turn says so.
	if err != nil && served == "" && ctx.Err() == nil && unknownHere(err) {
		if fallback, seat := Resolve(request.asked, without(s.config.Serves, model), s.config.Seat); seat != "" {
			model = fallback
			entry.serveOn(seat)
			catch = &catcher{}
			response, err = s.complete(ctx, out, request, model, bill, catch, slot, entry)
		}
	}

	var said answer
	status, sentence := 0, ""
	if err != nil {
		status, sentence = s.failure(err, r.Context(), model)
	} else {
		said = answerOf(response, model, bill, catch, slot, out)
		s.arrived(thread, said.reasoning.field)
	}
	s.log(entry.close(func(turn *delegate.Turn) {
		turn.TokensIn, turn.TokensOut, turn.Cached, turn.CostUSD = bill.figures()
		turn.Failed = sentence
		if err == nil {
			turn.Reply, turn.Calls = said.text, toolUses(said.calls)
		}
	}))

	if r.Context().Err() != nil {
		// The program stopped waiting; there is nobody to write the answer to.
		return
	}
	if err != nil {
		out.fail(status, sentence, model)
		return
	}
	out.answer(said)
}

// complete makes one call through the funnel and waits for it, saying the
// answer is still coming every [Config.Keepalive] while it does.
func (s *Server) complete(ctx context.Context, out *reply, request *call, model string, bill *tally, catch *catcher, slot *provider.ServedEndpoint, entry *record) (*ai.Response, error) {
	var completer Completer
	if s.config.CompleterFor != nil {
		completer = s.config.CompleterFor(model)
	}
	if completer == nil {
		return nil, errNoRoad
	}
	options := append(append([]ai.Option(nil), request.options...), ai.WithModel(model))
	ctx = s.settings(ctx, request, bill, catch, slot, entry)
	type outcome struct {
		response *ai.Response
		err      error
	}
	done := make(chan outcome, 1)
	guard.Go("modelapi/call", func() {
		// THE ANSWER IS SENT ON EVERY PATH, a fault included: the handler is
		// waiting on this channel, and a funnel that panicked must come back as
		// a failed call rather than a handler that waits for ever.
		result := outcome{err: errFault}
		defer func() { done <- result }()
		result.response, result.err = completer.CompleteWithMessages(ctx, request.messages, options...)
	})
	interval := s.config.Keepalive
	if interval <= 0 {
		interval = DefaultKeepalive
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case result := <-done:
			if result.err == nil && result.response == nil {
				return nil, errEmpty
			}
			return result.response, result.err
		case <-ticker.C:
			out.keepalive()
		}
	}
}

// settings are the per-call facts the funnel reads off the call's context:
// who the call is for, what it is called in the log, the program's own cache
// lineage and reasoning depth, the working it handed back, and the four
// sinks that meter it, catch its working and name its server.
func (s *Server) settings(ctx context.Context, request *call, bill *tally, catch *catcher, slot *provider.ServedEndpoint, entry *record) context.Context {
	role := s.config.Role
	if role == "" {
		role = lanes.RoleLeafUnattended
	}
	ctx = provider.WithRole(ctx, role)
	ctx = provider.WithCallTag(ctx, "task")
	ctx = provider.WithCallNode(ctx, s.config.Node)
	ctx = provider.WithCacheKey(ctx, request.cacheKey)
	// THE PROGRAM ASKED FOR ITS DEPTH IN SO MANY WORDS, which is what a person's
	// configured level is: sent even to a model the catalog cannot vouch for,
	// and dropped by the adapter's own repair if the model refuses it. A rung
	// rides the ladder's one translation (xhigh and max as a thinking budget);
	// the two words that are not rungs ride the adapter's own.
	switch {
	case request.depth.rung != effort.None:
		ctx = provider.WithConfiguredEffortRung(ctx, request.depth.rung)
	case request.depth.word != provider.EffortNone:
		ctx = provider.WithConfiguredReasoningEffort(ctx, request.depth.word)
	}
	ctx = provider.WithMessageReasoning(ctx, request.reasoning)
	ctx = provider.WithBilling(ctx, func(billed provider.Billed) { s.charge(bill, billed, false) })
	ctx = provider.WithReconcile(ctx, func(receipt provider.Reconciled) { s.receipt(bill, entry, receipt) })
	ctx = provider.WithStreamObserver(ctx, catch.observe)
	return provider.WithServedEndpoint(ctx, slot)
}

// callContext is the call's own context: the request's, which ends when the
// program stops waiting, ended as well when the run's API closes.
func (s *Server) callContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	unhook := context.AfterFunc(s.ctx, cancel)
	return ctx, func() {
		unhook()
		cancel()
	}
}

// charge meters one billed answer: onto the call's own tally, onto the run's
// total, and to the bank, one charge at a time.
func (s *Server) charge(bill *tally, billed provider.Billed, late bool) {
	if billed.Empty() {
		return
	}
	bill.add(billed)
	s.bankMu.Lock()
	defer s.bankMu.Unlock()
	spent := s.meter(billed.Cost)
	if s.config.Bank != nil {
		s.config.Bank(Charge{
			Model: strings.TrimSpace(billed.Model), TokensIn: billed.PromptTokens, TokensOut: billed.CompletionTokens,
			Cached: billed.CachedTokens, CostUSD: billed.Cost, Spent: spent, Late: late,
		})
	}
}

// meter adds one charge to the run's total and answers the total.
func (s *Server) meter(cost float64) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spent += cost
	return s.spent
}

// receipt is a cut stream's late answer. A found receipt is the same real
// money and is metered like any charge, and the call's turn is written again
// with it, so the page's figure for that call is the true one; a receipt that
// never came is told as a call nobody could price.
func (s *Server) receipt(bill *tally, entry *record, receipt provider.Reconciled) {
	if !receipt.Found || receipt.Billed.Empty() {
		if s.config.Unbilled != nil {
			s.config.Unbilled(strings.TrimSpace(receipt.Model))
		}
		return
	}
	s.charge(bill, receipt.Billed, true)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if !entry.ended {
		// The call has not written its ending yet; the tally it reads carries
		// this receipt already.
		return
	}
	entry.turn.TokensIn, entry.turn.TokensOut, entry.turn.Cached, entry.turn.CostUSD = bill.figures()
	s.log(entry.turn)
}

// log writes one turn. A log that cannot be written costs the record and never
// the call: the program is owed its answer whatever the disk does.
func (s *Server) log(turn delegate.Turn) {
	if strings.TrimSpace(s.config.TaskDir) == "" {
		return
	}
	s.logMu.Lock()
	defer s.logMu.Unlock()
	_ = delegate.AppendTurn(s.config.TaskDir, turn)
}

// ── the call's own figures ──────────────────────────────────────────────────

// tally is what one call cost, across every answer the funnel was charged for
// on its way to the one it returned.
type tally struct {
	mu        sync.Mutex
	in, out   int
	cached    int
	cost      float64
	billed    bool
	lastModel string
}

func (t *tally) add(billed provider.Billed) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.in += billed.PromptTokens
	t.out += billed.CompletionTokens
	t.cached += billed.CachedTokens
	t.cost += billed.Cost
	t.billed = true
	if model := strings.TrimSpace(billed.Model); model != "" {
		t.lastModel = model
	}
}

func (t *tally) figures() (in, out, cached int, cost float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.in, t.out, t.cached, t.cost
}

// usage is the call's usage block: the metered figures when the funnel billed
// anything, and the answer's own usage block otherwise.
func (t *tally) usage(response *ai.Response) usageBlock {
	block, billed := t.metered()
	if !billed && response != nil && response.Usage != nil {
		block = usageBlock{
			PromptTokens: response.Usage.PromptTokens, CompletionTokens: response.Usage.CompletionTokens,
			PromptTokensDetails: promptDetail{CachedTokens: response.Usage.CacheReadTokens()},
		}
		if response.Usage.Cost != nil {
			block.Cost = *response.Usage.Cost
		}
	}
	block.TotalTokens = block.PromptTokens + block.CompletionTokens
	return block
}

// metered is the tally as a usage block, and whether the funnel billed
// anything at all.
func (t *tally) metered() (usageBlock, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return usageBlock{PromptTokens: t.in, CompletionTokens: t.out, Cost: t.cost, PromptTokensDetails: promptDetail{CachedTokens: t.cached}}, t.billed
}

// answerOf is the funnel's response as the program is handed it.
func answerOf(response *ai.Response, model string, bill *tally, catch *catcher, slot *provider.ServedEndpoint, out *reply) answer {
	said := answer{
		id: out.id, provider: slot.Name(), model: model, created: out.created,
		text: response.Text(), calls: response.ToolCalls(), finish: finishOf(response),
		reasoning: catch.caught(), usage: bill.usage(response),
	}
	if answered := strings.TrimSpace(response.Model); answered != "" {
		said.model = answered
	}
	return said
}

// toolUses is the answer's tool calls as the log writes them.
func toolUses(calls []ai.ToolCall) []delegate.ToolUse {
	var uses []delegate.ToolUse
	for _, call := range calls {
		uses = append(uses, delegate.ToolUse{Name: call.Function.Name, Args: call.Function.Arguments})
	}
	return uses
}

// ── failures ────────────────────────────────────────────────────────────────

var (
	// errNoRoad is a run started with no funnel at all.
	errNoRoad = errors.New("this run was started with no road to a model")
	// errFault is a funnel that panicked; the fault itself is in the log guard
	// writes.
	errFault = errors.New("the model road failed inside codeaf")
	// errEmpty is a funnel that answered nothing and said nothing.
	errEmpty = errors.New("the model road answered nothing")
)

// failure is one failed call's status and sentence, in the words the program
// is answered with and the turn is written with.
//
// AN ACCOUNT REFUSED UPSTREAM IS NOT THE PROGRAM'S TOKEN BEING WRONG. A 401 or
// 403 from the model's service is codeaf's own account being refused, and on
// this API those two statuses mean the run's token; the program is told 502,
// a gateway whose far side said no, with the far side's sentence.
func (s *Server) failure(err error, request context.Context, model string) (int, string) {
	switch {
	case s.ctx.Err() != nil:
		return http.StatusServiceUnavailable, "the run ended before the answer came back"
	case request.Err() != nil:
		return 499, "the program stopped waiting for the answer"
	case errors.Is(err, errNoRoad):
		return http.StatusServiceUnavailable, err.Error()
	case errors.Is(err, provider.ErrNoAPIKey):
		return http.StatusServiceUnavailable, "no service on this machine can answer " + quoted(model) + ": it has no key for the service that model is on"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, firstLine(err.Error())
	}
	if refusal, ok := provider.RefusalFrom(err); ok {
		status := refusal.Status
		if status == http.StatusUnauthorized || status == http.StatusForbidden || status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		return status, firstLine(refusal.Error())
	}
	return http.StatusBadGateway, firstLine(err.Error())
}

// ceilingSentence is the refusal a call made past the run's ceiling gets.
func ceilingSentence(ceiling, spent float64) string {
	return "the run's dollar ceiling of " + dollars(ceiling) + " is reached (" + dollars(spent) + " spent), so codeaf made no call"
}

// dollars writes an amount the way a person reads one: cents, and four places
// under a cent so a small run is not written as nothing.
func dollars(amount float64) string {
	if amount > 0 && amount < 0.01 {
		return fmt.Sprintf("$%.4f", amount)
	}
	return fmt.Sprintf("$%.2f", amount)
}

func quoted(model string) string {
	if strings.TrimSpace(model) == "" {
		return "the default model"
	}
	return model
}

// firstLine is an error's first line, because a refusal is one sentence.
func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return line
}

// writeError answers a call that has not begun its reply, in the router's own
// error envelope.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Error: errorDetail{Message: message, Code: status}})
}

// ── the reply ───────────────────────────────────────────────────────────────

// reply is one call's side of the response: nothing is written until the
// answer is ready or the first keepalive is due, so a call that fails fast is
// answered with its real status; after that the status is 200 and a failure
// travels in the body, the way the router sends one.
type reply struct {
	w         http.ResponseWriter
	stream    bool
	id        string
	created   int64
	committed bool
}

func (r *reply) commit() {
	if r.committed {
		return
	}
	header := r.w.Header()
	if r.stream {
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache")
	} else {
		header.Set("Content-Type", "application/json")
	}
	r.w.WriteHeader(http.StatusOK)
	r.committed = true
}

// keepalive says the answer is still coming: an event-stream comment on a
// stream, and on a whole body the whitespace JSON allows before its value, so
// a client's idle timer is fed either way.
func (r *reply) keepalive() {
	r.commit()
	if r.stream {
		_, _ = io.WriteString(r.w, ": keepalive\n\n")
	} else {
		_, _ = io.WriteString(r.w, "\n")
	}
	r.flush()
}

// answer writes the finished answer in the shape the call asked for.
func (r *reply) answer(said answer) {
	r.commit()
	if !r.stream {
		_ = json.NewEncoder(r.w).Encode(said.whole())
		r.flush()
		return
	}
	for _, event := range said.chunks() {
		r.event(event)
	}
	_, _ = io.WriteString(r.w, "data: [DONE]\n\n")
	r.flush()
}

// fail writes a failure: its own status when nothing has been written yet,
// and in the body when a keepalive already sent the 200.
func (r *reply) fail(status int, message, model string) {
	if !r.committed {
		writeError(r.w, status, message)
		return
	}
	if !r.stream {
		_ = json.NewEncoder(r.w).Encode(errorBody{Error: errorDetail{Message: message, Code: status}})
		r.flush()
		return
	}
	r.event(failedChunk(r.id, model, r.created, status, message))
	_, _ = io.WriteString(r.w, "data: [DONE]\n\n")
	r.flush()
}

func (r *reply) event(event chunk) {
	encoded, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = io.WriteString(r.w, "data: ")
	_, _ = r.w.Write(encoded)
	_, _ = io.WriteString(r.w, "\n\n")
	r.flush()
}

func (r *reply) flush() {
	if flusher, ok := r.w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// mint is n random bytes as hex.
func mint(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// mustMint is an id that only has to be unlikely to repeat; a machine whose
// random source failed gets a clock reading instead.
func mustMint(n int) string {
	if id, err := mint(n); err == nil {
		return id
	}
	return fmt.Sprintf("%x", time.Now().UnixNano())
}
