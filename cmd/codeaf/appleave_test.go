package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"sync/atomic"
	"testing"
	"time"
)

// THE APP'S PROCESS ENDING IS THE APP LEAVING. The parent is the one the
// engine was started under; once it reads differently (the orphan's adopter),
// the engine leaves, exactly once.
func TestAnEngineWhoseAppProcessEndsLeaves(t *testing.T) {
	var parent atomic.Int64
	parent.Store(4242)
	var gone atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchAppParent(context.Background(), func() int { return int(parent.Load()) }, time.Millisecond, func() { gone.Add(1) })
	}()
	time.Sleep(20 * time.Millisecond)
	if gone.Load() != 0 {
		t.Fatal("the engine left while the app was still there")
	}
	parent.Store(1)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the engine never noticed its app's process had ended")
	}
	if gone.Load() != 1 {
		t.Fatalf("left %d times", gone.Load())
	}
}

// An engine the system started itself has no app to outlive, and an engine
// that is already leaving stops watching.
func TestTheParentWatchStandsDownWhenThereIsNothingToWatch(t *testing.T) {
	gone := func() { t.Error("left without its app ending") }
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		watchAppParent(context.Background(), func() int { return 1 }, time.Millisecond, gone)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("a process started by the system watched a parent it never had")
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished = make(chan struct{})
	go func() {
		defer close(finished)
		watchAppParent(ctx, func() int { return 4242 }, time.Millisecond, gone)
	}()
	cancel()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the watch outlived the engine's leaving")
	}
}

// LEAVING IS BOUNDED. Nothing ends the process before it starts leaving; once
// Close has begun (or a signal or the app's going cancelled the context), a
// wrap-up that has not finished by the bound is ended.
func TestALeavingThatOutrunsItsBoundIsEnded(t *testing.T) {
	for _, road := range []string{"close", "context"} {
		t.Run(road, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			left := make(chan struct{})
			ended := make(chan time.Time, 1)
			go leaveWithin(ctx, left, make(chan struct{}), 30*time.Millisecond, func() { ended <- time.Now() })
			select {
			case <-ended:
				t.Fatal("the process was ended before it started leaving")
			case <-time.After(60 * time.Millisecond):
			}
			began := time.Now()
			if road == "close" {
				close(left)
			} else {
				cancel()
			}
			select {
			case at := <-ended:
				if at.Sub(began) < 30*time.Millisecond {
					t.Fatalf("ended after %s, before its bound", at.Sub(began))
				}
			case <-time.After(5 * time.Second):
				t.Fatal("a wrap-up that outran its bound was never ended")
			}
		})
	}
}

// A WRAP-UP THAT FINISHED IS NOT ENDED. The door's own deferred cancel starts
// the clock as the door returns, and a process that lives on after it, as a
// test binary does, was ended a bound later in the middle of another test.
// Once the door has returned the watch stands down, whether it returned
// before leaving began or during the wrap-up.
func TestADoorThatReturnedIsNotEndedByItsBound(t *testing.T) {
	for _, when := range []string{"before leaving", "while leaving"} {
		t.Run(when, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			returned := make(chan struct{})
			ended := make(chan struct{}, 1)
			watched := make(chan struct{})
			go func() {
				leaveWithin(ctx, make(chan struct{}), returned, 30*time.Millisecond, func() { ended <- struct{}{} })
				close(watched)
			}()
			if when == "while leaving" {
				cancel()
				close(returned)
			} else {
				close(returned)
				cancel()
			}
			select {
			case <-watched:
			case <-time.After(5 * time.Second):
				t.Fatal("the watch outlived the door it bounds")
			}
			select {
			case <-ended:
				t.Fatal("a door that had returned was ended by its bound")
			default:
			}
		})
	}
}

// The bound is built from the stop waits it has to cover, so changing one of
// them cannot leave the bound shorter than the wrap-up it ends.
func TestTheLeavingBoundCoversEveryStopWait(t *testing.T) {
	if appLeaveWait < 2*appLocalStopWait+appReleaseWait+appQuitGraceS*time.Second {
		t.Fatalf("appLeaveWait %s is shorter than the wrap-up it bounds", appLeaveWait)
	}
}

// THE ENGINE'S DOOR TAKES THE ONE LEAVING ROAD, WATCHES ITS PARENT AND BOUNDS
// ITS LEAVING. A runApp that spelled its own signal set swallowed every SIGTERM
// behind a Close that never finished; this reads the door itself, because
// nothing short of running the binary reaches its wiring.
func TestTheAppDoorLeavesByTheOneRoadAndInBoundedTime(t *testing.T) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "app.go", nil, 0)
	if err != nil {
		t.Fatalf("read app.go: %v", err)
	}
	var door *ast.FuncDecl
	for _, declaration := range parsed.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == "runApp" {
			door = function
		}
	}
	if door == nil {
		t.Fatal("runApp is gone from app.go; this test has stopped watching the app's door")
	}
	seen := map[string]bool{}
	ast.Inspect(door.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch called := call.Fun.(type) {
		case *ast.Ident:
			seen[called.Name] = true
		case *ast.SelectorExpr:
			if receiver, ok := called.X.(*ast.Ident); ok {
				seen[receiver.Name+"."+called.Sel.Name] = true
			}
		}
		return true
	})
	for _, want := range []string{"leave.On", "watchAppParent", "leaveWithin"} {
		if !seen[want] {
			t.Errorf("runApp does not call %s", want)
		}
	}
	for _, banned := range []string{"signal.NotifyContext", "signal.Notify"} {
		if seen[banned] {
			t.Errorf("runApp spells its own signal set with %s instead of leave.On", banned)
		}
	}
}
