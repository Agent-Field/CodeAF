package appwire

import (
	"encoding/json"
	"slices"
	"testing"
)

type dismissEngine struct {
	fakeEngine
	dismissed []string
}

func (e *dismissEngine) DismissInbox(id string) error {
	e.dismissed = append(e.dismissed, id)
	return nil
}

// inbox.dismiss takes {id} and answers {}; an engine without the door says it
// does not serve it, and an empty id is refused before anything is closed.
func TestInboxDismissIsServedOnlyWithItsDoor(t *testing.T) {
	e := &dismissEngine{}
	result, err := methods["inbox.dismiss"].call(e, json.RawMessage(`{"id":"a1/ask:9"}`))
	if err != nil || len(e.dismissed) != 1 || e.dismissed[0] != "a1/ask:9" {
		t.Fatalf("dismiss = %v %v, engine saw %v", result, err, e.dismissed)
	}
	if raw, _ := json.Marshal(result); string(raw) != "{}" {
		t.Fatalf("dismiss answered %s, want {}", raw)
	}
	if _, err := methods["inbox.dismiss"].call(e, json.RawMessage(`{"id":" "}`)); codeOf(err) != CodeBadParams {
		t.Fatalf("an empty id = %v", err)
	}
	if _, err := methods["inbox.dismiss"].call(&fakeEngine{}, json.RawMessage(`{"id":"a1/ask:9"}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatalf("an engine without the door = %v", err)
	}
}

// hello says inbox.dismiss exactly when the engine has the door, so a client
// offers Dismiss only where it is served.
func TestHelloSaysInboxDismissOnlyWithItsDoor(t *testing.T) {
	if FeatureInboxDismiss != "inbox.dismiss" {
		t.Fatalf("the feature word is %q, which clients do not check for", FeatureInboxDismiss)
	}
	if hello := withInboxDismissFeature(Hello{}, &dismissEngine{}); !slices.Contains(hello.Features, FeatureInboxDismiss) {
		t.Fatalf("an engine with the door says %v", hello.Features)
	}
	if hello := withInboxDismissFeature(Hello{}, &fakeEngine{}); slices.Contains(hello.Features, FeatureInboxDismiss) {
		t.Fatalf("an engine without the door says %v", hello.Features)
	}
}
