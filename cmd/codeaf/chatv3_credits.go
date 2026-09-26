package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/provider"
)

var errCreditsSuperseded = errors.New("credit read superseded by a changed key")

// v3ReadAndStoreCredits commits only a reading for the key still in force.
func v3ReadAndStoreCredits(ctx context.Context, profileDir, base, key string) (credits.Reading, error) {
	reading, err := credits.Read(ctx, nil, base, key)
	if err != nil {
		return credits.Reading{}, err
	}
	if key != config.APIKeyAt(profileDir) {
		return credits.Reading{}, errCreditsSuperseded
	}
	if err := config.WriteCreditsReading(profileDir, key, reading); err != nil {
		return credits.Reading{}, err
	}
	return reading, nil
}

// v3CreditReader binds the key and address at read time. A key pasted after
// first run must be the one the lookup asks about and fingerprints on disk.
func v3CreditReader(proc *v3Process) func(context.Context) (credits.Reading, error) {
	return func(ctx context.Context) (credits.Reading, error) {
		key, sources := proc.currentAccount()
		if strings.TrimSpace(key) == "" {
			return credits.Reading{}, errors.New("no default provider key")
		}
		base := sources.Default().Address
		if base == "" {
			base = proc.Settings.BaseURL
		}
		return v3ReadAndStoreCredits(ctx, proc.ProfileDir, base, key)
	}
}

// v3LocalCreditReader uses the terminal's resolved account on the engine road.
func v3LocalCreditReader(settings config.Config) func(context.Context) (credits.Reading, error) {
	return func(ctx context.Context) (credits.Reading, error) {
		key := config.APIKeyAt(settings.ProfileDir)
		if key == "" {
			return credits.Reading{}, errors.New("no default provider key")
		}
		base := settings.Sources.Default().Address
		if base == "" {
			base = settings.BaseURL
		}
		return v3ReadAndStoreCredits(ctx, settings.ProfileDir, base, key)
	}
}

type v3CreditListener struct{ call func() }

// v3CreditWatcher owns the provider's one hook for the process lifetime. An
// engine has no surface listener; its completed record is the surface's door.
type v3CreditWatcher struct {
	proc     *v3Process
	trigger  *credits.Trigger
	mu       sync.Mutex
	listener *v3CreditListener
	stopHook func()
	closed   bool
	wait     sync.WaitGroup
}

func newV3CreditWatcher(proc *v3Process) *v3CreditWatcher {
	w := &v3CreditWatcher{proc: proc, trigger: credits.NewTrigger(time.Now)}
	w.stopHook = provider.SetPaymentRequiredHook(w.refused)
	return w
}

func (w *v3CreditWatcher) refused(base string) {
	if !v3DefaultCreditBase(w.proc, base) {
		return
	}
	if !w.begin() {
		return
	}
	guard.Go("chatv3/payment-credits", func() {
		defer w.wait.Done()
		defer w.trigger.End()
		ctx := w.proc.processCtx
		if ctx == nil {
			ctx = context.Background()
		}
		_, err := v3CreditReader(w.proc)(ctx)
		if err != nil {
			return
		}
		if listener := w.currentListener(); listener != nil {
			listener.call()
		}
	})
}

func (w *v3CreditWatcher) begin() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || !w.trigger.Begin(credits.Refusal) {
		return false
	}
	w.wait.Add(1)
	return true
}

func (w *v3CreditWatcher) currentListener() *v3CreditListener {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.listener
}

func (w *v3CreditWatcher) listen(wake func()) func() {
	listener := &v3CreditListener{call: wake}
	w.setListener(listener)
	return func() { w.clearListener(listener) }
}

func (w *v3CreditWatcher) setListener(listener *v3CreditListener) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed {
		w.listener = listener
	}
}

func (w *v3CreditWatcher) clearListener(listener *v3CreditListener) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.listener == listener {
		w.listener = nil
	}
}

func (w *v3CreditWatcher) markClosed() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	w.listener = nil
}

func (w *v3CreditWatcher) close() {
	w.markClosed()
	w.stopHook()
	w.wait.Wait()
}

// v3PaymentRefusals lets a local surface hear a completed process read.
func v3PaymentRefusals(proc *v3Process) func(func()) func() {
	return func(wake func()) func() {
		return proc.creditWatcher.listen(wake)
	}
}

func v3DefaultCreditBase(proc *v3Process, base string) bool {
	_, sources := proc.currentAccount()
	current := sources.Default().Address
	if current == "" {
		current = proc.Settings.BaseURL
	}
	return strings.TrimRight(base, "/") == strings.TrimRight(current, "/")
}

// v3FreshDefault is the launch a NEW conversation opens on, asked again of the
// balance check at the moment it opens rather than at the moment the process
// booted (#1439). A key read low after boot must not hand /new the paid default
// the boot resolved, and a top-up read after boot must not keep handing out the
// free one.
//
// IT MOVES ONLY THE BUILD'S OWN DEFAULT. A --model flag, CODEAF_MODEL, or a
// saved talk row is somebody's choice and the launch keeps it exactly as it was
// — including the older behaviour that /new opens on the model the process
// booted on — so the only thing this changes is which of the two implicit
// defaults a fresh conversation starts on.
//
// AND THE WINDOW MOVES WITH THE MODEL. The launch's ContextWindow was measured
// for the model the boot chose; a conversation opened on the other default with
// that figure would compact against a window its model does not have (the free
// default takes 262k, the paid one 1.31M). The catalog's answer for the new
// model replaces it, and nothing — which leaves the session's conservative
// default — when the catalog cannot say.
func v3FreshDefault(launch *v3Launch, flag string) *v3Launch {
	if launch == nil || strings.TrimSpace(flag) != "" || strings.TrimSpace(env.Get(config.ModelEnv)) != "" {
		return launch
	}
	profileDir := launch.Settings.ProfileDir
	if config.ChatModelAt(profileDir) != "" {
		return launch
	}
	if launch.Model != config.DefaultModel && launch.Model != config.FreeChatModel {
		return launch
	}
	want := config.ChatDefaultAt(profileDir)
	if want == launch.Model {
		return launch
	}
	current := *launch
	current.Model = want
	current.Config.Model = want
	current.Config.ContextWindow = 0
	if current.Config.ContextWindowFor != nil {
		current.Config.ContextWindow = current.Config.ContextWindowFor(want)
	}
	return &current
}
