package appwire

import (
	"encoding/json"
	"testing"
)

type accountProtocolEngine struct {
	fakeEngine
	provider, attempt string
}

func (e *accountProtocolEngine) StartSignIn(provider string) (ProviderSignInStart, error) {
	e.provider = provider
	return ProviderSignInStart{URL: "https://example.invalid/login", Attempt: "opaque-attempt"}, nil
}
func (e *accountProtocolEngine) CancelSignIn(provider, attempt string) error {
	e.provider, e.attempt = provider, attempt
	return nil
}

func TestAccountSignInProtocolKeepsOperationIdentityAndOptionalCapability(t *testing.T) {
	engine := &accountProtocolEngine{}
	result, err := methods["providers.sign_in_start"].call(engine, json.RawMessage(`{"provider":"Codex"}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var start ProviderSignInStart
	if err := json.Unmarshal(raw, &start); err != nil {
		t.Fatal(err)
	}
	if engine.provider != "codex" || start.Attempt != "opaque-attempt" {
		t.Fatalf("operation lost: %+v", start)
	}
	if _, err := methods["providers.sign_in_cancel"].call(engine, json.RawMessage(`{"provider":"Codex","attempt":"opaque-attempt"}`)); err != nil {
		t.Fatal(err)
	}
	if engine.provider != "codex" || engine.attempt != "opaque-attempt" {
		t.Fatal("cancel lost operation identity")
	}
	if _, err := methods["providers.sign_in_cancel"].call(engine, json.RawMessage(`{"provider":"codex"}`)); codeOf(err) != CodeBadParams {
		t.Fatalf("missing operation accepted: %v", err)
	}
	for _, method := range []string{"providers.sign_in_start", "providers.sign_in_cancel"} {
		if _, err := methods[method].call(&fakeEngine{}, json.RawMessage(`{"provider":"codex","attempt":"a"}`)); codeOf(err) != CodeUnknownMethod {
			t.Fatalf("missing capability %s: %v", method, err)
		}
	}
}
