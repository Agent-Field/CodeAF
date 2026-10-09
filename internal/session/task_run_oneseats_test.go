package session

import "testing"

// A one-model session with no roles source keeps every seat on the
// conversation's model, which is what the flag has always meant.
func TestOneModelSeatsWithoutASourceAreTheConversationsModel(t *testing.T) {
	a := &Agent{config: Config{OneModel: true}}
	work, plan, check := a.oneModelSeats("chat/model")
	if work != "chat/model" || plan != "chat/model" || check != "chat/model" {
		t.Fatalf("seats %q %q %q", work, plan, check)
	}
}

// A surface that lets the person choose a model per job (the desktop) supplies a
// source, and each seat then answers from its own role.
func TestOneModelSeatsFollowTheChosenRoles(t *testing.T) {
	pins := map[string]string{"roles.worker": "w/model", "roles.auditor": "c/model"}
	a := &Agent{config: Config{OneModel: true, RolesSource: func(key string) (string, bool) {
		value, ok := pins[key]
		return value, ok
	}}}
	work, plan, check := a.oneModelSeats("chat/model")
	if work != "w/model" || plan != "chat/model" || check != "c/model" {
		t.Fatalf("seats %q %q %q", work, plan, check)
	}
}
