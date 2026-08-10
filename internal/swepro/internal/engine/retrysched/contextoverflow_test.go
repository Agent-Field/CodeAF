package retrysched

import "testing"

func TestContextOverflowProviderPredicate(t *testing.T) {
	// Finding 1: hard provider failures use provider/error.ts's message,
	// status, and response-body overflow classifications.
	status400 := float64(400)
	status413 := float64(413)
	for _, test := range []struct {
		name    string
		message string
		status  *float64
		body    *string
		want    bool
	}{
		{name: "message signature", message: "maximum context length is 128000 tokens", status: &status400, want: true},
		{name: "entity too large status", message: "payload rejected", status: &status413, want: true},
		{name: "response code", message: "bad request", status: &status400, body: stringAddress(`{"error":{"code":"context_length_exceeded"}}`), want: true},
		{name: "ordinary bad request", message: "invalid schema", status: &status400, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			retryable := false
			err := Err{Name: "APIError", Data: ErrData{
				Message: &test.message, StatusCode: test.status,
				IsRetryable: &retryable, ResponseBody: test.body,
			}}
			if got := IsContextOverflow(err); got != test.want {
				t.Fatalf("IsContextOverflow(%+v) = %v, want %v", err, got, test.want)
			}
		})
	}
}

func stringAddress(value string) *string { return &value }
