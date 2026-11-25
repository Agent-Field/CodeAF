package provider

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const orgDoorSentence = "This chat has reached its spending limit of $0.001, so it is paused. Start a new chat to carry on."

// orgDoorBody is an organization model door's error as it writes it, with the
// mark under the shared field name.
func orgDoorBody(status int, kind, message string) []byte {
	return []byte(fmt.Sprintf(`{"error":{"code":%d,%q:%q,"message":%q,"type":%q}}`,
		status, DoorKey, OrganizationDoor, message, kind))
}

// THE FIELD THE SERVER WRITES IS THE FIELD THE CLIENT READS. The reader's JSON
// tag cannot be a constant, so this pins it to [DoorKey], which the server spells
// the mark through.
func TestTheDoorsFieldIsTheOneTheClientDecodes(t *testing.T) {
	field, ok := reflect.TypeOf(errorBody{}).Field(0).Type.FieldByName("Door")
	if !ok {
		t.Fatal("the error envelope has no field for who wrote the error")
	}
	if tag := strings.Split(field.Tag.Get("json"), ",")[0]; tag != DoorKey {
		t.Fatalf("the envelope decodes %q, the door writes %q", tag, DoorKey)
	}
}

func TestOnlyTheOrganizationsDoorIsReadAsTheOrganizationSpeaking(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    []byte
		wantOrg bool
		wantSay string
	}{
		{"the door's spending limit", 402, orgDoorBody(402, "insufficient_quota", orgDoorSentence), true, orgDoorSentence},
		{"a provider's own payment refusal", 402, []byte(`{"error":{"code":402,"message":"Insufficient credits."}}`), false, ""},
		{"a provider's insufficient_quota type is not the door's mark", 402,
			[]byte(`{"error":{"code":"insufficient_quota","type":"insufficient_quota","message":"You exceeded your current quota."}}`), false, ""},
		{"a pacing refusal the door signed", 429, orgDoorBody(429, "rate_limit", "This chat is already making as many requests as it may at once. Try again in a moment."), false, ""},
		{"a stopped chat the door signed", 401, orgDoorBody(401, "invalid_request_error", "This chat's model access has ended."), false, ""},
		{"a different door's mark", 402, []byte(`{"error":{"code":402,"door":"someone-else","message":"Insufficient credits."}}`), false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refusal, ok := RefusalFrom(apiError(tc.status, tc.body))
			if !ok {
				t.Fatal("no refusal")
			}
			said, org := refusal.OrgLimitWords()
			if org != tc.wantOrg || said != tc.wantSay {
				t.Fatalf("OrgLimitWords = %q, %v; want %q, %v", said, org, tc.wantSay, tc.wantOrg)
			}
		})
	}
}

// NOTHING ELSE ABOUT THE REFUSAL CHANGES. The door's limit is still a terminal
// empty-account refusal: no endpoint walk, no second model, no retry. Only the
// words a person reads moved.
func TestTheDoorsLimitIsStillTerminalForEveryReaderOfARefusal(t *testing.T) {
	err := apiError(http.StatusPaymentRequired, orgDoorBody(402, "insufficient_quota", orgDoorSentence))
	refusal, _ := RefusalFrom(err)
	if !refusal.AccountCannotPay() || !refusal.OurRequest() {
		t.Fatalf("the door's limit is no longer terminal: payment=%v ours=%v", refusal.AccountCannotPay(), refusal.OurRequest())
	}
	if failure, _ := RouteFailureOf(err); failure != RoutePayment {
		t.Fatalf("the route read the door's limit as %q", failure)
	}
	if refusal.Message != orgDoorSentence {
		t.Fatalf("the refusal's own sentence changed: %q", refusal.Message)
	}
}
