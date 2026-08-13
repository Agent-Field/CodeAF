package provider

import (
	"errors"
	"fmt"
	"testing"
)

// A refusal is a value now, and the two things about it that are facts rather
// than prose — the status and the provider's own sentence — are fields. What
// used to read them out of the formatted string can still read the formatted
// string: Error() is byte-for-byte what it always was, and the retry taxonomy
// depends on that.
func TestAPIErrorCarriesTheStatusAndTheMessageAndPrintsWhatItAlwaysDid(t *testing.T) {
	body := `{"error":{"message":"No endpoints found that support tool use.","code":404}}`
	err := apiError(404, []byte(body))

	if got := err.Error(); got != "API error (404): No endpoints found that support tool use." {
		t.Fatalf("the printed form changed: %q", got)
	}
	var refused *APIError
	if !errors.As(err, &refused) {
		t.Fatal("a provider refusal is not recoverable as a value")
	}
	if refused.Status != 404 {
		t.Fatalf("status = %d", refused.Status)
	}
	if refused.Message != "No endpoints found that support tool use." {
		t.Fatalf("message = %q", refused.Message)
	}
	if refused.Body != body {
		t.Fatalf("the undecoded body was dropped: %q", refused.Body)
	}
	// It survives being wrapped, which is the whole point: the executor stamps
	// its node and its attempt count on the way up, and the sentence a person
	// reads has to be readable from the far end of that chain.
	wrapped := fmt.Errorf("node craft-3799~launch: after 3 node call attempts: %w", err)
	refused = nil
	if !errors.As(wrapped, &refused) || refused.Message == "" {
		t.Fatalf("the refusal did not survive wrapping: %v", wrapped)
	}
}

// THE REGRESSION THAT PUT A JSON BLOB IN SOMEBODY'S ROOM. OpenRouter types its
// error codes as numbers; the SDK's error struct types them as strings; and
// json.Unmarshal fails the whole object on that one field. So every OpenRouter
// refusal fell through to the raw-payload arm and arrived with the readable
// sentence trapped inside a blob — including the routing 404s, which are the
// refusals a person most needs explained.
func TestANumericErrorCodeDoesNotTrapTheMessageInABlob(t *testing.T) {
	err := apiError(404, []byte(`{"error":{"message":"No endpoints found that support tool use.","code":404}}`))
	var refused *APIError
	if !errors.As(err, &refused) {
		t.Fatal("a provider refusal is not recoverable as a value")
	}
	if refused.Message != "No endpoints found that support tool use." {
		t.Fatalf("a numeric code swallowed the message: %q", refused.Message)
	}
	if got := err.Error(); got != "API error (404): No endpoints found that support tool use." {
		t.Fatalf("the printed form still carries the blob: %q", got)
	}
}

// A body this client does not know the shape of is kept whole rather than
// guessed at, and it still prints exactly as it used to.
func TestAPIErrorKeepsAnUndecodableBodyWhole(t *testing.T) {
	err := apiError(502, []byte("upstream is unavailable"))
	if got := err.Error(); got != "API error (502): upstream is unavailable" {
		t.Fatalf("the printed form changed: %q", got)
	}
	var refused *APIError
	if !errors.As(err, &refused) {
		t.Fatal("a provider refusal is not recoverable as a value")
	}
	if refused.Message != "" {
		t.Fatalf("a message was invented: %q", refused.Message)
	}
	if refused.Body != "upstream is unavailable" {
		t.Fatalf("body = %q", refused.Body)
	}
}
