package serve_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// checkoutFloor is the v2 jobs floor whose checkout door answers states, by
// repository, and missing (with GitHub not connected) for any other.
func checkoutFloor(t *testing.T, states map[string]factory.CheckoutState) *v2Jobs {
	t.Helper()
	f := newV2Jobs(t)
	f.h.Close()
	f.seam.CheckoutOf = func(_ context.Context, repo string) (factory.CheckoutState, error) {
		if s, ok := states[repo]; ok && s != factory.CheckoutMissing {
			return s, nil
		}
		return factory.CheckoutMissing, factory.NeedsCheckout{Repo: repo, Why: factory.CheckoutNeedsGitHub}
	}
	f.h = serve.New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	return f
}

const cannotClone = "acme/api isn't checked out on this machine · " + factory.CheckoutNeedsGitHub

// A START THAT CANNOT GET A CHECKOUT IS REFUSED BEFORE IT WRITES ANYTHING: 409
// needs_checkout in the sentence naming why, and the job's version, mark,
// budget and failure exactly as the person read them, from every door that
// starts (run, start_now, run_again) and from a new job started at once.
func TestAStartThatCannotCloneIsRefusedBeforeAnyWrite(t *testing.T) {
	f := checkoutFloor(t, nil)
	f.h.Close()
	f.seam.Launch = func(int) error { t.Fatal("a job with no checkout was launched"); return nil }
	f.seam.New = func(string, string) (int, error) {
		t.Fatal("a job that could not start was made to start")
		return 0, nil
	}
	f.h = serve.New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	fresh := f.create(t, factory.Item{Num: 3, State: factory.StateNew, Assignee: "sam"})
	failed := f.create(t, factory.Item{Num: 4, State: factory.StateNeedsYou, Assignee: "sam",
		Failure: &factory.Failure{Step: "write", Code: factory.CodeToolError, Detail: "it broke"}})
	for _, c := range []struct {
		it   factory.Item
		verb string
	}{{fresh, "start_now"}, {fresh, "run"}, {failed, "run_again"}} {
		was, err := f.st.Revision(c.it.ID)
		if err != nil {
			t.Fatal(err)
		}
		w := verbRequest(f.h, "POST", "/v1/jobs/"+strconv.Itoa(c.it.ID)+"/"+c.verb, `{}`)
		if words, reason := refusal(t, w); w.Code != 409 || words != cannotClone || reason != api.ReasonNeedsCheckout {
			t.Fatalf("%s = %d %q %q", c.verb, w.Code, reason, words)
		}
		if now, _ := f.st.Revision(c.it.ID); now != was {
			t.Fatalf("%s moved the job's version from %d to %d", c.verb, was, now)
		}
		got, _ := f.st.Get(c.it.ID)
		if got.Marked || got.Cap != 0 || (c.it.Failure != nil && got.Failure == nil) {
			t.Fatalf("%s wrote the job: %+v", c.verb, got)
		}
	}
	w := verbRequest(f.h, "POST", "/v1/jobs", `{"repo": "acme/api", "title": "new work", "autostart": true}`)
	if words, reason := refusal(t, w); w.Code != 409 || words != cannotClone || reason != api.ReasonNeedsCheckout {
		t.Fatalf("a new job started at once = %d %s", w.Code, w.Body)
	}
}

// A JOB THE RUNNER WILL CLONE STARTS: the checkout door passes it, and the
// launch is the one write after the mark and the budget.
func TestAStartTheRunnerWillCloneForStarts(t *testing.T) {
	f := checkoutFloor(t, map[string]factory.CheckoutState{"acme/api": factory.CheckoutWillClone})
	it := f.create(t, factory.Item{Num: 3, State: factory.StateNew, Assignee: "sam"})
	f.post(t, it, "start_now", `{}`, 200)
	if got, _ := f.st.Get(it.ID); got.State != factory.StateQueued || !got.Marked || got.Cap != 4 {
		t.Fatalf("a start the runner will clone for = %+v", got)
	}
}

// A LAUNCH THAT STILL REFUSES TAKES THE START'S MARK BACK, so the queue does
// not keep a start that never happened at its top; and a launch that refuses
// for want of a checkout is 409 needs_checkout wherever it surfaces, typed and
// never matched by its words.
func TestALaunchThatRefusesTakesTheMarkBack(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	f.seam.Launch = func(int) error {
		return factory.NeedsCheckout{Repo: "acme/api", Why: factory.CheckoutNeedsGitHub}
	}
	f.h = serve.New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	it := f.create(t, factory.Item{Num: 3, State: factory.StateNew, Assignee: "sam"})
	w := verbRequest(f.h, "POST", "/v1/jobs/"+strconv.Itoa(it.ID)+"/start_now", `{}`)
	if words, reason := refusal(t, w); w.Code != 409 || words != cannotClone || reason != api.ReasonNeedsCheckout {
		t.Fatalf("a launch refused for its checkout = %d %q %q", w.Code, reason, words)
	}
	if got, _ := f.st.Get(it.ID); got.Marked {
		t.Fatalf("a start that did not start kept its mark: %+v", got)
	}

	f.h.Close()
	f.seam.Launch = func(int) error { return errors.New("the desk could not be written") }
	f.h = serve.New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	w = verbRequest(f.h, "POST", "/v1/jobs/"+strconv.Itoa(it.ID)+"/start_now", `{}`)
	if words, _ := refusal(t, w); w.Code != 500 || words != "the desk could not be written" {
		t.Fatalf("a launch that failed = %d %s", w.Code, w.Body)
	}
	if got, _ := f.st.Get(it.ID); got.Marked {
		t.Fatalf("a start that failed kept its mark: %+v", got)
	}
}

// THE JOB'S READ SAYS HOW A RUN GETS ITS CHECKOUT, here, will_clone or
// missing, before anyone presses Start, and says nothing on a factory whose
// seam does not know its machine's checkouts.
func TestTheJobReadSaysWhereItsCheckoutComesFrom(t *testing.T) {
	f := checkoutFloor(t, map[string]factory.CheckoutState{"acme/api": factory.CheckoutHere, "acme/web": factory.CheckoutWillClone})
	for repo, want := range map[string]string{"acme/api": "here", "acme/web": "will_clone", "acme/docs": "missing"} {
		it := f.create(t, factory.Item{Repo: repo, Num: 3, State: factory.StateNew, Assignee: "sam"})
		if got := f.read(t, it).Checkout; got != want {
			t.Errorf("%s reads checkout %q, want %q", repo, got, want)
		}
	}
	plain := newV2Jobs(t)
	it := plain.create(t, factory.Item{Num: 3, State: factory.StateNew, Assignee: "sam"})
	if got := plain.read(t, it).Checkout; got != "" {
		t.Fatalf("a factory that does not know its checkouts says %q", got)
	}
}
