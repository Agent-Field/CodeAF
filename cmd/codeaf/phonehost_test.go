package main

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// A normal app has no verb for opening an ask. The fixture switch is what
// makes testing.open_ask exist, and this is the door a person hits when it
// is left off.
func TestTestingOpenAskIsAbsentWithoutTheFixtureSwitch(t *testing.T) {
	t.Setenv("CODEAF_PHONE_FIXTURE", "")
	backend := newAppBackend(newAppRecorder(), nil)
	defer backend.Close()
	_, err := appwire.Call(backend, "testing.open_ask", json.RawMessage(`{}`))
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeUnknownMethod {
		t.Fatalf("testing.open_ask answered %v", err)
	}
}
