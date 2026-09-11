package remote

import (
	"go/ast"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestEveryNotifiedMethodOwesNobodyAnOrder is the law [orderedLane]'s doc
// comment depends on, and it is written down because that comment is currently
// the only thing holding it up.
//
// THE UNCAPPED QUEUE IS SAFE FOR EXACTLY ONE REASON: every frame on it is a call
// the far end is waiting for synchronously under [callDeadline], so its depth is
// the number of calls one surface has outstanding. [Client.notify] is the one
// mechanism in this package that puts a frame on the wire with NOBODY waiting
// for it — and `c.notify(MethodSubmit, …)` is one line, compiles, and turns that
// bound from "a surface's outstanding calls" into unbounded.
//
// So a method sent through notify must be one that owes nobody an order: its
// class must run on its own goroutine, never on the ordered lane. That also
// keeps the two mechanisms' promises apart — a hint may be dropped
// ([Client.notify] drops one while another is on the wire), and a frame on the
// ordered lane may not be.
func TestEveryNotifiedMethodOwesNobodyAnOrder(t *testing.T) {
	root := repoRoot(t)
	found := 0
	walkGo(t, filepath.Join(root, "internal", "remote"), func(path string, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selected, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selected.Sel.Name != "notify" {
				return true
			}
			found++
			name, ok := call.Args[0].(*ast.Ident)
			if !ok {
				t.Errorf("%s: a notify whose method is not a named constant cannot be checked; name it",
					filepath.Base(path))
				return true
			}
			method, known := wireMethod(name.Name)
			if !known {
				t.Errorf("%s: notify sends %s, which is not a method constant this law can read",
					filepath.Base(path), name.Name)
				return true
			}
			if classify(method).road() != onItsOwn {
				t.Errorf("%s: notify sends %s, which owes an order — a frame nobody waits for may not ride the ordered lane",
					filepath.Base(path), name.Name)
			}
			return true
		})
	})
	if found == 0 {
		t.Error("this law found no notify call at all: either the mechanism was renamed or the walk stopped reaching it")
	}
}

// wireMethod resolves a method constant's NAME to its value, so the law above
// reads the same table [classify] does rather than a copy of it. Every constant
// in wire.go's block is a plain string and the compiler has them all here; this
// is the one place they are looked up by their Go identifier.
func wireMethod(identifier string) (string, bool) {
	method, ok := wireMethodsByName[identifier]
	return method, ok
}

// wireMethodsByName is every method a notify may name. It is deliberately SHORT
// rather than the whole wire: a method added here is somebody deciding that a
// frame of that kind may be sent with nobody waiting for it, which is exactly
// the decision this law exists to make visible.
var wireMethodsByName = map[string]string{
	"MethodTyping":  MethodTyping,
	"MethodPing":    MethodPing,
	"MethodSubmit":  MethodSubmit,
	"MethodSteer":   MethodSteer,
	"MethodCompact": MethodCompact,
}

// blockingPipe is a link that has accepted the connection and stopped reading:
// not dead, not reconnecting, just wedged. It is the case [Client.notify]'s law
// is about and the one `c.dead != nil || c.reconnecting` cannot see.
type blockingPipe struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *blockingPipe) Write(b []byte) (int, error) {
	p.once.Do(func() { close(p.entered) })
	<-p.release
	return len(b), nil
}

func (p *blockingPipe) Read([]byte) (int, error) { <-p.release; return 0, nil }
func (p *blockingPipe) Close() error             { return nil }

// TestAHintDoesNotWaitForAWedgedLink is [Client.notify]'s stated law, driven.
//
// THE CALLER IS THE GOROUTINE THAT DRAWS. internal/tui3's app.key calls
// [Agent.Typing] from Update, on every character, so a hint that waited on
// [Client.write] would stop the terminal echoing what the person is typing for
// as long as the far end was wedged — the ≤50 ms ruling, broken by the very
// mechanism meant to make a turn faster.
func TestAHintDoesNotWaitForAWedgedLink(t *testing.T) {
	pipe := &blockingPipe{entered: make(chan struct{}), release: make(chan struct{})}
	client := &Client{conn: pipe}
	t.Cleanup(func() { close(pipe.release) })

	// The first hint takes the wedged write and stays there.
	client.notify(MethodTyping, nil)
	select {
	case <-pipe.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the hint never reached the pipe")
	}

	// AND EVERY KEYSTROKE AFTER IT RETURNS AT ONCE. They are dropped rather
	// than queued, because a hint is free to lose and the person is about to
	// send another one anyway.
	done := make(chan time.Duration, 1)
	go func() {
		began := time.Now()
		for range 200 {
			client.notify(MethodTyping, nil)
		}
		done <- time.Since(began)
	}()
	select {
	case took := <-done:
		if took > 50*time.Millisecond {
			t.Fatalf("two hundred keystrokes behind a wedged link took %v", took)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a keystroke is waiting on the socket: the draw goroutine is blocked")
	}
}
