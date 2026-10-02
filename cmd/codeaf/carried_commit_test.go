package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// The shell's finishing commit reads the same outcome as its last line,
// including a terminal that arrives after the person stopped the program.
func TestCarriedCommitOnlyOmitsTheEndingAfterPassingChecks(t *testing.T) {
	passed := &delegate.Terminal{Status: delegate.StatusPass, Message: "the checks passed", Data: map[string]json.RawMessage{"status": json.RawMessage(`"pass"`)}}
	for _, row := range []struct {
		name       string
		terminal   *delegate.Terminal
		result     delegate.Result
		err        error
		limited    bool
		wantPassed bool
		wantWords  string
	}{
		{name: "passed", terminal: passed, wantPassed: true, wantWords: "fake finished: the checks passed"},
		{name: "no-check", terminal: &delegate.Terminal{Status: delegate.StatusPass}, wantWords: "fake finished"},
		{name: "unverified", terminal: &delegate.Terminal{Status: delegate.StatusPass, Message: "nothing finished checking", Data: map[string]json.RawMessage{"status": json.RawMessage(`"pass-unverified"`)}}, wantWords: "fake finished: nothing finished checking"},
		{name: "own-ceiling", terminal: &delegate.Terminal{Status: delegate.StatusBudget, Message: "out of time", Data: map[string]json.RawMessage{"submitted": json.RawMessage("true"), "status": json.RawMessage(`"pass"`)}}, wantWords: "fake stopped at its ceiling: out of time"},
		{name: "handed-in-failed-check", terminal: &delegate.Terminal{Status: delegate.StatusFail, Message: "its check did not pass", Data: map[string]json.RawMessage{"submitted": json.RawMessage("true"), "status": json.RawMessage(`"fail"`)}}, wantWords: "fake finished: its check did not pass"},
		{name: "handed-in-unfinished-check", terminal: &delegate.Terminal{Status: delegate.StatusFail, Message: "nothing finished checking", Data: map[string]json.RawMessage{"submitted": json.RawMessage("true"), "status": json.RawMessage(`"pass-unverified"`)}}, wantWords: "fake finished: nothing finished checking"},
		{name: "handed-in-passed-check", terminal: &delegate.Terminal{Status: delegate.StatusFail, Message: "the checks passed", Data: map[string]json.RawMessage{"submitted": json.RawMessage("true"), "status": json.RawMessage(`"pass"`)}}, wantPassed: true, wantWords: "fake finished: the checks passed"},
		{name: "nothing-handed-in", terminal: &delegate.Terminal{Status: delegate.StatusFail, Message: "nothing submitted", Data: map[string]json.RawMessage{"status": json.RawMessage(`"pass"`)}}, wantWords: "fake did not finish: nothing submitted"},
		{name: "stopped", terminal: passed, result: delegate.Result{Stopped: true}, wantWords: "fake finished: the checks passed"},
		{name: "limit", terminal: passed, limited: true, wantWords: "fake finished: the checks passed"},
		{name: "crashed", terminal: passed, err: errors.New("broken pipe"), wantWords: "fake finished: the checks passed"},
		{name: "no-terminal", result: delegate.Result{ExitCode: 3}, wantWords: "fake exited 3 without saying how it ended"},
	} {
		t.Run(row.name, func(t *testing.T) {
			view := &carriedView{inv: &delegate.Invocation{Program: delegate.Delegate{Name: "fake"}}, terminal: row.terminal}
			words := view.finishingWords(row.result, row.limited)
			passed := view.commitPassed(row.result, row.err, row.limited)
			if passed != row.wantPassed || words != row.wantWords {
				t.Fatalf("finishing = %q, passed %v; want %q, %v", words, passed, row.wantWords, row.wantPassed)
			}
			if strings.TrimSpace(words) == "" {
				t.Fatal("incomplete shell ending has no words for its commit")
			}
		})
	}
}
