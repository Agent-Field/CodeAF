// Package judge is the model-backed [route.Judge]: it asks a cheap model one
// rule's yes/no question ([route.Match.Ask]) about one item, and reads the
// answer strictly into a [route.Answer].
//
// IT HOLDS NO MODEL CLIENT, as internal/factory/triage and scout hold none.
// The caller hands it a [Call] that sends one request and answers the text
// with what it cost (cmd/codeaf's factory_judge.go builds it on the provider
// and books every call on the spend ledger under `factory filter`, exactly as
// the arrival read books `factory triage`), so a test drives the whole judge
// with a string and a fake clock.
//
// A FAILURE IS NEVER A YES. Every way the judge can fail — no model, a refused
// call, a timeout, a reply that is not exactly {"yes":bool,"why":"…"} — is an
// error and a zero Answer, and the intake reads an error as no.
//
// docs/design/factory-general/DESIGN.md §1b is the contract this fills.
package judge

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/route"
)

// Timeout is how long one attempt may take before it is abandoned: twenty
// seconds, which a one-sentence answer from a cheap model fits many times
// over and which still lets the intake move on within a minute when a model
// hangs. RetryAfter is the pause before the ONE retry a transient failure
// earns, so the longest a question can take is 2×Timeout+RetryAfter.
const (
	Timeout    = 20 * time.Second
	RetryAfter = 2 * time.Second
	Tries      = 2
)

// Request is one question as it goes to the model: the model it is asked of,
// the fixed [System] instruction, and the [User] message carrying the
// question and the untrusted item.
type Request struct {
	Model  string
	System string
	User   string
}

// Reply is what one call answered: the text, the model that actually served
// it (empty means the one asked), and what the call cost in dollars, which a
// Call reports EVEN WHEN IT FAILED, because a provider can charge for a
// generation that then errored.
type Reply struct {
	Text  string
	Model string
	USD   float64
}

// Call sends one request to a model.
type Call func(ctx context.Context, req Request) (Reply, error)

// Options is everything the judge is built from. Call is required; every
// other field has a default.
type Options struct {
	Call Call
	// DefaultModel is the factory's default model (factory.Spending's
	// DefaultModel); "" or nil says there is none.
	DefaultModel func() string
	// ReadModel is the model the arrival read (internal/factory/triage) runs
	// on, the last road when nothing else names one.
	ReadModel func() (string, error)
	// Spent is told every attempt's cost that is above zero, failed attempts
	// included, so a floor can note what its filter costs beside what its
	// reads cost. The ledger itself is the Call's to write.
	Spent func(usd float64)
	// Transient says a failed attempt is worth the one retry. Nil uses
	// [Transient] alone; a caller that knows its provider's errors adds to it.
	Transient func(error) bool
	// After is the clock: a channel that fires after d. Nil is time.After.
	After func(d time.Duration) <-chan time.Time
	// Timeout overrides [Timeout] for one attempt; zero keeps it.
	Timeout time.Duration
}

// ErrNoModel is a judge with no road to a model: no rule model, no factory
// default and no read model.
var ErrNoModel = errors.New("judge: no model to ask")

// ErrTimeout is an attempt that outran its [Timeout].
var ErrTimeout = errors.New("judge: the model did not answer in time")

// ErrNoQuestion is a rule with an empty Ask; there is nothing to judge.
var ErrNoQuestion = errors.New("judge: the rule asks no question")

// New builds the judge. A nil Call answers every question with an error,
// which reads as no.
func New(opts Options) route.Judge {
	if opts.After == nil {
		opts.After = time.After
	}
	if opts.Timeout <= 0 {
		opts.Timeout = Timeout
	}
	return &judge{opts: opts}
}

type judge struct{ opts Options }

type ruleModelKey struct{}

// WithRuleModel names the model a rule asks its question of, for the call
// made with ctx. It wins over the factory's default: it is the seam for a
// rule's own `model`, which route.Match does not carry yet, so the intake can
// pass one without the [route.Judge] interface changing.
func WithRuleModel(ctx context.Context, model string) context.Context {
	return context.WithValue(ctx, ruleModelKey{}, strings.TrimSpace(model))
}

// Model is the model a question asked with ctx goes to: THE RULE'S, ELSE THE
// FACTORY'S DEFAULT, ELSE THE ARRIVAL READ'S.
func (j *judge) Model(ctx context.Context) (string, error) {
	if m, _ := ctx.Value(ruleModelKey{}).(string); m != "" {
		return m, nil
	}
	if j.opts.DefaultModel != nil {
		if m := strings.TrimSpace(j.opts.DefaultModel()); m != "" {
			return m, nil
		}
	}
	if j.opts.ReadModel != nil {
		m, err := j.opts.ReadModel()
		if err != nil {
			return "", err
		}
		if m = strings.TrimSpace(m); m != "" {
			return m, nil
		}
	}
	return "", ErrNoModel
}

// Judge asks ask about it. The answer carries the model that answered and
// Key = route.AnswerKey(ask, it), so a later edit of the item's words makes
// it stale.
func (j *judge) Judge(ctx context.Context, ask string, it factory.Item) (route.Answer, error) {
	if strings.TrimSpace(ask) == "" {
		return route.Answer{}, ErrNoQuestion
	}
	if j.opts.Call == nil {
		return route.Answer{}, ErrNoModel
	}
	model, err := j.Model(ctx)
	if err != nil {
		return route.Answer{}, err
	}
	req := Request{Model: model, System: System, User: User(ask, it)}
	var reply Reply
	for try := 1; ; try++ {
		reply, err = j.attempt(ctx, req)
		if err == nil || try >= Tries || ctx.Err() != nil || !j.transient(err) {
			break
		}
		select {
		case <-ctx.Done():
			return route.Answer{}, ctx.Err()
		case <-j.opts.After(RetryAfter):
		}
	}
	if err != nil {
		return route.Answer{}, err
	}
	yes, why, err := Parse(reply.Text)
	if err != nil {
		return route.Answer{}, err
	}
	served := strings.TrimSpace(reply.Model)
	if served == "" {
		served = model
	}
	return route.Answer{Yes: yes, Why: why, Model: served, Key: route.AnswerKey(ask, it)}, nil
}

// attempt is one call under the attempt timeout, its cost told to Spent.
func (j *judge) attempt(ctx context.Context, req Request) (Reply, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-j.opts.After(j.opts.Timeout):
			cancel(ErrTimeout)
		case <-stop:
		}
	}()
	reply, err := j.opts.Call(ctx, req)
	if reply.USD > 0 && j.opts.Spent != nil {
		j.opts.Spent(reply.USD)
	}
	if errors.Is(context.Cause(ctx), ErrTimeout) {
		return reply, ErrTimeout
	}
	if err == nil && strings.TrimSpace(reply.Text) == "" {
		err = errNothing
	}
	return reply, err
}

func (j *judge) transient(err error) bool {
	if Transient(err) {
		return true
	}
	return j.opts.Transient != nil && j.opts.Transient(err)
}

// Transient is the failures any call may have that a second try can cure: the
// attempt timed out, or the network failed under it. A reply that is not an
// answer is NOT transient — the same prompt at temperature zero writes it
// again — and neither is a refusal of the request itself.
func Transient(err error) bool {
	if errors.Is(err, ErrTimeout) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

// errNothing is a call that succeeded with no text in it.
var errNothing = errors.New("judge: the model answered nothing")
