package api

import (
	"net/http"
	"testing"
)

// The worker and the factory read one rule for when a worker request may be
// sent again: the server must not cache an answer the worker will resend, and
// the worker must not give up on an answer the server will not keep.
func TestAWorkerResendsOnlyWhenTheFactoryWasDownSlowOrBusy(t *testing.T) {
	for status, want := range map[int]bool{
		http.StatusOK:                  false,
		http.StatusBadRequest:          false,
		http.StatusConflict:            false,
		http.StatusUnprocessableEntity: false,
		http.StatusRequestTimeout:      true,
		http.StatusTooManyRequests:     true,
		http.StatusInternalServerError: true,
		http.StatusBadGateway:          true,
		http.StatusServiceUnavailable:  true,
	} {
		if got := WorkerResendable(status); got != want {
			t.Errorf("WorkerResendable(%d) = %v, want %v", status, got, want)
		}
	}
}

// A report the factory refused as invalid committed nothing, so it is checked
// again when sent again rather than answered from a cached refusal.
func TestOnlyAnInvalidReportIsCheckedAgain(t *testing.T) {
	for status, want := range map[int]bool{
		http.StatusUnprocessableEntity: true,
		http.StatusBadRequest:          false,
		http.StatusOK:                  false,
		http.StatusInternalServerError: false,
	} {
		if got := ReportCommittedNothing(status); got != want {
			t.Errorf("ReportCommittedNothing(%d) = %v, want %v", status, got, want)
		}
	}
}
