//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// carriedSecondSignalEnv marks the process this file's test starts as the one
// that is signalled.
const carriedSecondSignalEnv = "CODEAF_TEST_CARRIED_SECOND_SIGNAL"

// A SECOND CTRL-C LEAVES A SHELL RUN AT ONCE. The first one stops the run, and
// the run then waits for the program's grace, its last calls and the price of
// a call the stop cut short — up to about a minute and a half. The signals were
// held for all of it, so a person who pressed ctrl-c again was ignored. The
// first signal now gives the terminal its ordinary ctrl-c back.
//
// It is proved in a process of its own, because the proof is that process
// dying of the second signal.
func TestASecondInterruptLeavesAShellRunAtOnce(t *testing.T) {
	if os.Getenv(carriedSecondSignalEnv) == "1" {
		ctx, stop := carriedSignals()
		defer stop()
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			os.Exit(3)
		}
		// The release runs beside the cancellation; give it a moment.
		time.Sleep(200 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
		time.Sleep(5 * time.Second)
		// Still here: the second ctrl-c was swallowed.
		os.Exit(0)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestASecondInterruptLeavesAShellRunAtOnce$")
	command.Env = append(os.Environ(), carriedSecondSignalEnv+"=1")
	began := time.Now()
	err := command.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("the signalled process ended with %v after %v, want it killed by the second ctrl-c", err, time.Since(began))
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGINT {
		t.Fatalf("the signalled process ended %v after %v, want it killed by the second ctrl-c", exit, time.Since(began))
	}
}

// carriedHangupEnv marks the process the hangup test starts as the one that
// is hung up on.
const carriedHangupEnv = "CODEAF_TEST_CARRIED_HANGUP"

// A HANGUP STOPS A SHELL RUN THE WAY CTRL-C DOES. A closed terminal or a
// dropped ssh connection sent SIGHUP, which nothing caught: the host died on
// the spot with its program still working and its folder left unfinished.
// Now the first hangup ends the run's context — the program is stopped and the
// folder finished — and a second, which a shell passes on to its jobs as it
// exits, is not allowed to kill that finishing halfway.
//
// It is proved in a process of its own, because the failure is that process
// dying of the signal.
func TestAHangupStopsAShellRunLikeCtrlC(t *testing.T) {
	if os.Getenv(carriedHangupEnv) == "1" {
		ctx, stop := carriedSignals()
		defer stop()
		_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			os.Exit(3)
		}
		time.Sleep(200 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestAHangupStopsAShellRunLikeCtrlC$")
	command.Env = append(os.Environ(), carriedHangupEnv+"=1")
	if err := command.Run(); err != nil {
		t.Fatalf("the hung-up process ended with %v, want it to hear the hangup as a stop and live through a second", err)
	}
}
