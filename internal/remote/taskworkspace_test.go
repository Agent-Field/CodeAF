package remote

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

type workspaceFar struct {
	*fakeAgent
	mu                 sync.Mutex
	id                 uint64
	slot               string
	refuse             error
	entered, cancelled chan struct{}
}

func (f *workspaceFar) ReviewTaskWorkspaceRoot(ctx context.Context, id uint64, slot string) (session.TaskWorkspaceReview, error) {
	f.mu.Lock()
	f.id, f.slot = id, slot
	err := f.refuse
	f.mu.Unlock()
	return session.TaskWorkspaceReview{Slot: slot, Copy: "/private/task", Destination: "/project", Snapshot: "reviewed", Changed: []string{"result.txt"}, Diff: "+actual result"}, err
}
func (f *workspaceFar) ApplyTaskWorkspaceRoot(ctx context.Context, id uint64, slot string) (string, error) {
	f.mu.Lock()
	f.id, f.slot = id, slot
	err := f.refuse
	f.mu.Unlock()
	if f.entered != nil {
		close(f.entered)
		<-ctx.Done()
		close(f.cancelled)
		return "", ctx.Err()
	}
	return "selected snapshot applied", err
}
func workspaceLoop(t *testing.T, far WrappedAgent, watch bool) *Loop {
	t.Helper()
	loop, err := Loopback(Hello{Version: Version, Watch: watch}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far, SessionFile: "/srv/owned-task.jsonl"}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	return loop
}
func TestTaskWorkspaceReviewAndApplyCrossResidentConnection(t *testing.T) {
	far := &workspaceFar{fakeAgent: &fakeAgent{}}
	loop := workspaceLoop(t, far, false)
	agent := loop.Client.Agent()
	review, err := agent.ReviewTaskWorkspaceRoot(context.Background(), 7, "library")
	if err != nil {
		t.Fatal(err)
	}
	if review.Slot != "library" || review.Snapshot != "reviewed" || review.Diff != "+actual result" || review.Destination != "/project" {
		t.Fatalf("review changed across wire: %+v", review)
	}
	receipt, err := agent.ApplyTaskWorkspaceRoot(context.Background(), 7, "library")
	if err != nil || receipt != "selected snapshot applied" {
		t.Fatalf("apply receipt %q %v", receipt, err)
	}
	far.mu.Lock()
	id, slot := far.id, far.slot
	far.refuse = errors.New("original changed; private result retained")
	far.mu.Unlock()
	if id != 7 || slot != "library" {
		t.Fatalf("wrong root reached engine: %d %s", id, slot)
	}
	if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), 7, "library"); err == nil || !strings.Contains(err.Error(), "private result retained") {
		t.Fatalf("refusal lost: %v", err)
	}
}
func TestTaskWorkspaceCancellationReachesResidentOperation(t *testing.T) {
	far := &workspaceFar{fakeAgent: &fakeAgent{}, entered: make(chan struct{}), cancelled: make(chan struct{})}
	loop := workspaceLoop(t, far, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ended := make(chan error, 1)
	go func() { _, err := loop.Client.Agent().ApplyTaskWorkspaceRoot(ctx, 7, "work"); ended <- err }()
	select {
	case <-far.entered:
	case <-time.After(time.Second):
		t.Fatal("apply did not reach resident engine")
	}
	cancel()
	select {
	case err := <-ended:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("caller cancellation lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("caller did not return after cancellation")
	}
	select {
	case <-far.cancelled:
	case <-time.After(time.Second):
		t.Fatal("resident operation continued after caller cancellation")
	}
}
func TestTaskWorkspaceUnsupportedAndWatchingEnginesRefuse(t *testing.T) {
	loop := workspaceLoop(t, &fakeAgent{}, false)
	if _, err := loop.Client.Agent().ReviewTaskWorkspaceRoot(context.Background(), 7, "work"); err == nil {
		t.Fatal("unsupported engine accepted review")
	}
	far := &workspaceFar{fakeAgent: &fakeAgent{}}
	watch := workspaceLoop(t, far, true)
	if _, err := watch.Client.Agent().ApplyTaskWorkspaceRoot(context.Background(), 7, "work"); err == nil {
		t.Fatal("watch-only connection applied work")
	}
}

type slowWorkspaceFar struct {
	*workspaceFar
	delay    time.Duration
	deadline chan time.Time
}

func (f *slowWorkspaceFar) ApplyTaskWorkspaceRoot(ctx context.Context, id uint64, slot string) (string, error) {
	if deadline, ok := ctx.Deadline(); ok && f.deadline != nil {
		f.deadline <- deadline
	}
	timer := time.NewTimer(f.delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return "selected snapshot applied", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
func TestTaskWorkspaceApplyCanCompleteAfterGetterDeadline(t *testing.T) {
	far := &slowWorkspaceFar{workspaceFar: &workspaceFar{fakeAgent: &fakeAgent{}}, delay: 11 * time.Second, deadline: make(chan time.Time, 1)}
	loop := workspaceLoop(t, far, false)
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	receipt, err := loop.Client.Agent().ApplyTaskWorkspaceRoot(ctx, 7, "work")
	if err != nil || receipt != "selected snapshot applied" {
		t.Fatalf("durable operation lost its delayed receipt: %q %v", receipt, err)
	}
	select {
	case deadline := <-far.deadline:
		if deadline.Sub(started) <= 10*time.Second || deadline.Sub(started) > 121*time.Second {
			t.Fatalf("resident operation deadline is not bounded to longer task patience: %s", deadline.Sub(started))
		}
	default:
		t.Fatal("resident operation had no bounded deadline")
	}
}
func TestTaskWorkspaceEarlierCallerDeadlineStopsResidentOperation(t *testing.T) {
	far := &workspaceFar{fakeAgent: &fakeAgent{}, entered: make(chan struct{}), cancelled: make(chan struct{})}
	loop := workspaceLoop(t, far, false)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := loop.Client.Agent().ApplyTaskWorkspaceRoot(ctx, 7, "work"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("earlier caller deadline lost: %v", err)
	}
	select {
	case <-far.cancelled:
	case <-time.After(time.Second):
		t.Fatal("resident operation outlived earlier caller deadline")
	}
}

// A resident reply can beat the caller's cancellation notification. Keep the
// caller live so this test always exercises the error received over the wire.
func TestTaskWorkspaceResidentContextErrorsPreserveSentinels(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled} {
		t.Run(cause.Error(), func(t *testing.T) {
			far := &workspaceFar{fakeAgent: &fakeAgent{}, refuse: cause}
			loop := workspaceLoop(t, far, false)
			agent := loop.Client.Agent()
			if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), 7, "work"); !errors.Is(err, cause) {
				t.Fatalf("resident apply lost %v: %v", cause, err)
			}
			if _, err := agent.ReviewTaskWorkspaceRoot(context.Background(), 7, "work"); !errors.Is(err, cause) {
				t.Fatalf("resident review lost %v: %v", cause, err)
			}
		})
	}
}

// The engine's answer to a call whose caller's deadline has passed is that
// caller's context error, whichever end reported it first. An unrelated
// refusal, or the same words while the caller still has time, is left alone.
func TestAnEngineAnswerAtTheCallersDeadlineIsTheCallersContextError(t *testing.T) {
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	live, cancelLive := context.WithTimeout(context.Background(), time.Hour)
	defer cancelLive()
	for _, tc := range []struct {
		what   string
		ctx    context.Context
		answer string
		want   error
	}{
		{"the engine noticed the deadline", expired, "context deadline exceeded", context.DeadlineExceeded},
		{"the engine wrapped it", expired, "apply: context deadline exceeded", context.DeadlineExceeded},
		{"the caller cancelled", cancelled, "context canceled", context.Canceled},
		{"an unrelated refusal after the deadline", expired, "no task 7 in this session", nil},
		{"the same words while the caller has time", live, "context deadline exceeded", nil},
	} {
		t.Run(tc.what, func(t *testing.T) {
			answer := errors.New(tc.answer)
			got := callerDeadline(tc.ctx, answer)
			if got.Error() != tc.answer {
				t.Fatalf("the engine's words became %q, want %q", got.Error(), tc.answer)
			}
			if tc.want == nil {
				if got != answer {
					t.Fatalf("%v was attributed to the caller's context", got)
				}
				return
			}
			if !errors.Is(got, tc.want) {
				t.Fatalf("errors.Is(%v, %v) = false", got, tc.want)
			}
			if !errors.Is(got, answer) {
				t.Fatal("the engine's own error is no longer reachable")
			}
		})
	}
}
