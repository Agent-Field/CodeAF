// Package pool is the surface's provider seam: the model-switchable client a
// slot holds, the per-model clients a pinned job is served by, and the billing
// that makes every structuring call land on the same rail its leaves land on.
//
// It sits beside internal/provider rather than inside it because the thing it
// switches is a configured client: it reads config.Config to build one, hands
// back router.Client so a panel keeps its rungs, and journals store.NodeUsage
// so the day's total is honest. All three of those packages already depend on
// internal/provider, so the seam that composes them cannot live there — a
// sub-package is the same shelf without the cycle.
package pool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Client is a model-switchable completion client. Long-running leaves take a
// snapshot so one measurement has one model; structuring consumers hold this
// handle directly, so a swap takes effect on their next call.
//
// It is also the one seam every structuring call in this surface passes
// through — the head's routing loop, the compiler, the delivery gate, the
// revision sentinel, the narrator, the distiller. None of that spend reached
// the journal: the daily rail is summed from the usage table and nowhere else,
// so it systematically understated the bill by the entire cost of thinking
// about the work. The headless path has journaled its own preparation spend
// since it existed; chat is what forgot. Billing here rather than at each of a
// dozen call sites is what makes it hard to forget again.
type Client struct {
	settings config.Config
	mu       sync.RWMutex
	model    string
	client   router.Client
	journal  func(store.NodeUsage)
}

// spendNodeKey carries the node a structuring call is about. Most of them are
// about a specific piece of work — the gate judging one deliverable, the
// sentinel revising one plan — and attributing those to the job rather than to
// the spine is the difference between a job's cost being its whole cost and
// being only what its leaves happened to burn.
type spendNodeKey struct{}

// WithSpendNode names the work a structuring call belongs to.
func WithSpendNode(ctx context.Context, nodeID string) context.Context {
	if strings.TrimSpace(nodeID) == "" {
		return ctx
	}
	return context.WithValue(ctx, spendNodeKey{}, nodeID)
}

// SpendNode reads back what WithSpendNode named.
func SpendNode(ctx context.Context) string {
	if id, ok := ctx.Value(spendNodeKey{}).(string); ok {
		return id
	}
	// The spine is the honest home for work that belongs to no job: routing a
	// message, composing an arrival brief, consolidating the notebook. It is
	// not a job root, so it lands on the day's rail without inventing spend for
	// an errand that never asked for it.
	return store.RootID
}

// New builds the ordinary client: one slot, one model, resolved through the
// settings the process was started with.
func New(settings config.Config, model string) (*Client, error) {
	client, err := settings.ClientFor(model)
	if err != nil {
		return nil, err
	}
	return Adopt(settings, model, client), nil
}

// Adopt wraps a client somebody else already built. It is the same object New
// returns and the door for a caller that holds its own transport — a pinned
// pool entry, a scripted provider in a test — so there is exactly one shape of
// switchable client in the system rather than two.
func Adopt(settings config.Config, model string, client router.Client) *Client {
	return &Client{settings: settings, model: model, client: client}
}

// WithUsageJournal wires the durable rail. It is set after the graph opens
// rather than at construction because the clients exist first; until it is set
// a client simply does not bill, which is the old behaviour and the right one
// for a client that has no store to bill to.
func (l *Client) WithUsageJournal(journal func(store.NodeUsage)) *Client {
	if l == nil {
		return l
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.journal = journal
	return l
}

func (l *Client) usageJournal() func(store.NodeUsage) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.journal
}

// recordStructuringSpend bills one completed structuring call. A response with
// no usage block is not billed: guessing at a number is worse than a gap, and
// the gap is visible as a run the rail did not see rather than as money the
// rail invented.
func (l *Client) recordStructuringSpend(ctx context.Context, model string, response *ai.Response) {
	journal := l.usageJournal()
	if journal == nil || response == nil || response.Usage == nil {
		return
	}
	usage := store.NodeUsage{
		NodeID:           SpendNode(ctx),
		PromptTokens:     response.Usage.PromptTokens,
		CompletionTokens: response.Usage.CompletionTokens,
		Model:            model,
	}
	if response.Usage.Cost != nil {
		usage.Cost = *response.Usage.Cost
	}
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.Cost == 0 {
		return
	}
	journal(usage)
}

// CompleteWithMessages is the one seam every structuring call passes through,
// which makes it the one place two facts about a turn are both in hand: what it
// cost, and how it ended. The cost is journaled here because a dozen call sites
// would otherwise each have to remember to. How it ended is NOT journaled here,
// and the asymmetry is deliberate: spend belongs to the day's rail no matter
// who spent it, while an unfinished turn belongs to the message that turn
// produced — and this seam does not know which message that is, or whether
// there will be one. So the end mark rides back out on the response, and TurnEnd
// below is how a caller reads it in one line at the moment it posts.
func (l *Client) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	slot, client := l.Snapshot()
	response, err := client.CompleteWithMessages(ctx, messages, options...)
	// The served model, not the slot: a panel picks a rung and an escalation
	// moves one, and the row should name whoever actually answered.
	model := slot
	if served := provider.CallFrom(ctx).Model(); served != "" {
		model = served
	}
	l.recordStructuringSpend(ctx, model, response)
	return response, err
}

// TurnEnd reads how one completion ended, or nil when it ended on its own
// terms. It is the truncation law's read side for every pool caller — the
// compiler, the delivery gate, the revision sentinel, the narrator, the head —
// and it is a plain function over the value they already hold rather than
// state on the client, because a client is shared by concurrent callers and
// "the last finish reason" on a shared object is a race wearing a field name.
//
// A non-nil result belongs on the message that call produced, as
// store.EndedMark(*end). Dropping it is how a 600-token cap became a diagram
// that stopped mid-path and a journal that said nothing about it.
func TurnEnd(ctx context.Context, response *ai.Response) *store.EndedPart {
	return store.EndedFor(provider.FinishReason(response), provider.Streaming(ctx))
}

func (l *Client) Model() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.model
}

// Snapshot returns a model and client from the same instant, which keeps the
// profile key and the executor it describes inseparable.
func (l *Client) Snapshot() (string, router.Client) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.model, l.client
}

// Escalatable reports whether a failed leaf has somewhere stronger to go —
// the same condition the headless runner uses to grant one escalation.
func (l *Client) Escalatable() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if panel, ok := l.client.(*router.Router); ok {
		return panel.Rungs() > 1
	}
	return false
}

// Routed reports whether a panel is behind this slot, which is what decides
// whether a structured-output schema has a second rung to unlock.
func (l *Client) Routed() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	_, ok := l.client.(*router.Router)
	return ok
}

func (l *Client) SetModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model cannot be empty")
	}
	client, err := l.settings.ClientFor(model)
	if err != nil {
		return err
	}
	CloseReplaced(l.swap(model, client))
	return nil
}

// swap installs the new pair and returns the client it displaced, which the
// caller closes outside the lock — a client's Close flushes a ledger, and no
// model read should wait behind that.
func (l *Client) swap(model string, client router.Client) router.Client {
	l.mu.Lock()
	defer l.mu.Unlock()
	previous := l.client
	l.model, l.client = model, client
	return previous
}

// Close releases the underlying router client so its ledger flushes and its
// events handle is returned before the process exits.
func (l *Client) Close() {
	_, client := l.Snapshot()
	CloseReplaced(client)
}

// Pool keeps per-message chat overrides pinned to the exact model recorded on
// the durable user message. A later work-model change therefore cannot race an
// armed submission that the head has not tailed yet.
type Pool struct {
	settings config.Config
	journal  func(store.NodeUsage)
	mu       sync.Mutex
	clients  map[string]*Client
}

// NewPool builds an empty pool over the settings its clients are made from.
func NewPool(settings config.Config) *Pool {
	return &Pool{settings: settings, clients: make(map[string]*Client)}
}

// WithUsageJournal wires the rail every client this pool hands out bills to.
// Clients already pinned move with it, so the order of construction and wiring
// is not a correctness question.
func (p *Pool) WithUsageJournal(journal func(store.NodeUsage)) *Pool {
	if p == nil {
		return p
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.journal = journal
	for _, client := range p.clients {
		client.WithUsageJournal(journal)
	}
	return p
}

// ForMessage pins the exact model the durable user message recorded.
func (p *Pool) ForMessage(message store.Message) (*Client, error) {
	model := strings.TrimSpace(message.Model)
	if model == "" {
		return nil, errors.New("boost message has no model")
	}
	return p.ForModel(model)
}

// ForModel is the same pinning seam seen from the graph side: one client per
// exact model slug, shared by every leaf that asked for it.
func (p *Pool) ForModel(model string) (*Client, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, errors.New("no model named")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if client := p.clients[model]; client != nil {
		return client, nil
	}
	client, err := New(p.settings, model)
	if err != nil {
		return nil, err
	}
	client.WithUsageJournal(p.journal)
	if p.clients == nil {
		p.clients = make(map[string]*Client)
	}
	p.clients[model] = client
	return client, nil
}

// Adopt pins a client somebody else built under an exact model slug. The pool
// builds its own on a miss; this is the door for a caller that already holds
// the transport it wants that slug served by.
func (p *Pool) Adopt(model string, client router.Client) *Client {
	model = strings.TrimSpace(model)
	pinned := Adopt(p.settings, model, client)
	p.mu.Lock()
	defer p.mu.Unlock()
	pinned.WithUsageJournal(p.journal)
	if p.clients == nil {
		p.clients = make(map[string]*Client)
	}
	p.clients[model] = pinned
	return pinned
}

// Close releases every pinned per-model client the pool has handed out.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, client := range p.clients {
		client.Close()
	}
}

// CloseReplaced releases a client that has just been swapped out.
//
// A router is not a value: it owns the append handle on router-events.jsonl and
// a queue of graded observations the run has already paid for. Every model
// switch used to drop one on the floor, which leaks the handle for the life of
// the process and loses whatever had not reached the ledger file yet. Closing
// is best-effort and idempotent; a plain adapter has nothing to close and is
// left alone.
func CloseReplaced(client router.Client) {
	if closer, ok := client.(io.Closer); ok {
		_ = closer.Close()
	}
}
