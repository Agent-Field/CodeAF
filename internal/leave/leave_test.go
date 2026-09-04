package leave

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func TestASignalReachesTheLeavingWork(t *testing.T) {
	for _, received := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(received.String(), func(t *testing.T) {
			left := make(chan struct{}, 2)
			stop := On(func() { left <- struct{}{} }, nil)
			defer stop()

			sendSignal(t, received)
			select {
			case <-left:
			case <-time.After(time.Second):
				t.Fatalf("%s did not reach the leaving work", received)
			}
			select {
			case <-left:
				t.Fatalf("%s ran the leaving work more than once", received)
			case <-time.After(20 * time.Millisecond):
			}
		})
	}
}

func TestASecondSignalEndsItWhileTheFirstIsStillLeaving(t *testing.T) {
	for _, test := range []struct {
		first  syscall.Signal
		second syscall.Signal
		want   int
	}{
		{first: syscall.SIGTERM, second: syscall.SIGINT, want: 130},
		{first: syscall.SIGINT, second: syscall.SIGTERM, want: 143},
	} {
		t.Run(test.second.String(), func(t *testing.T) {
			leaving := make(chan struct{})
			release := make(chan struct{})
			exited := replaceExit(t)
			stop := On(func() {
				close(leaving)
				<-release
			}, nil)
			defer func() {
				close(release)
				stop()
			}()

			sendSignal(t, test.first)
			waitFor(t, leaving, "the first signal to start leaving")
			sendSignal(t, test.second)
			select {
			case code := <-exited:
				if code != test.want {
					t.Fatalf("a second %s exited with %d, want %d", test.second, code, test.want)
				}
			case <-time.After(time.Second):
				t.Fatalf("a second %s waited for leaving to finish", test.second)
			}
			select {
			case <-release:
				t.Fatal("the leaving work was released before the forced exit")
			default:
			}
		})
	}
}

func TestTheTerminalIsHandedBackBeforeTheForcedExit(t *testing.T) {
	t.Run("returned", func(t *testing.T) {
		leaving := make(chan struct{})
		release := make(chan struct{})
		order := make(chan string, 2)
		exited := replaceExit(t)
		stop := On(func() {
			close(leaving)
			<-release
		}, func() { order <- "handed back" })
		defer func() {
			close(release)
			stop()
		}()

		sendSignal(t, syscall.SIGINT)
		waitFor(t, leaving, "the first signal to start leaving")
		sendSignal(t, syscall.SIGINT)
		select {
		case code := <-exited:
			order <- "exit"
			if code != 130 {
				t.Fatalf("forced exit used status %d, want 130", code)
			}
		case <-time.After(time.Second):
			t.Fatal("the forced exit did not arrive")
		}
		if first := <-order; first != "handed back" {
			t.Fatalf("%q happened before the terminal was handed back", first)
		}
		if second := <-order; second != "exit" {
			t.Fatalf("the second event was %q, want exit", second)
		}
	})

	t.Run("blocked", func(t *testing.T) {
		leaving := make(chan struct{})
		releaseLeaving := make(chan struct{})
		handBackStarted := make(chan struct{})
		releaseHandBack := make(chan struct{})
		exited := replaceExit(t)
		stop := On(func() {
			close(leaving)
			<-releaseLeaving
		}, func() {
			close(handBackStarted)
			<-releaseHandBack
		})
		defer func() {
			close(releaseHandBack)
			close(releaseLeaving)
			stop()
		}()

		sendSignal(t, syscall.SIGTERM)
		waitFor(t, leaving, "the first signal to start leaving")
		sendSignal(t, syscall.SIGTERM)
		waitFor(t, handBackStarted, "the terminal hand-back to start")
		select {
		case code := <-exited:
			if code != 143 {
				t.Fatalf("forced exit used status %d, want 143", code)
			}
		// Two seconds bounds a wait that would otherwise be infinite; it is not
		// a measurement of the terminal hand-back grace.
		case <-time.After(2 * time.Second):
			t.Fatal("a blocked terminal hand-back held the forced exit past its grace")
		}
	})
}

func TestTheLeavingRoadStandsDownWhenItIsStopped(t *testing.T) {
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGHUP)
	defer signal.Stop(guard)

	left := make(chan struct{}, 1)
	stop := On(func() { left <- struct{}{} }, nil)
	stop()
	stop()
	sendSignal(t, syscall.SIGHUP)
	select {
	case <-guard:
	case <-time.After(time.Second):
		t.Fatal("the process guard did not receive SIGHUP")
	}
	select {
	case <-left:
		t.Fatal("a signal reached leaving after the road stood down")
	case <-time.After(20 * time.Millisecond):
	}
}

func replaceExit(t *testing.T) <-chan int {
	t.Helper()
	exited := make(chan int, 1)
	previous := exit
	exit = func(code int) { exited <- code }
	t.Cleanup(func() { exit = previous })
	return exited
}

func sendSignal(t *testing.T, received syscall.Signal) {
	t.Helper()
	if err := syscall.Kill(os.Getpid(), received); err != nil {
		t.Fatalf("send %s: %v", received, err)
	}
}

func waitFor(t *testing.T, event <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}
