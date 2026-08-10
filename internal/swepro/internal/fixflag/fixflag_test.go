package fixflag

import (
	"reflect"
	"testing"
)

func TestEnabledReadsEnvironmentLive(t *testing.T) {
	const name = "CODEAF_GO_FIX_LUBY_GUARDS"

	t.Setenv(name, "")
	if Enabled(name) {
		t.Fatal("empty value enabled the flag")
	}
	t.Setenv(name, "1")
	if !Enabled(name) {
		t.Fatal(`value "1" did not enable the flag`)
	}
	t.Setenv(name, "true")
	if Enabled(name) {
		t.Fatal(`value "true" enabled the flag`)
	}
}

func TestAll(t *testing.T) {
	want := []string{
		"CODEAF_GO_FIX_AIMD_NAN_CAP",
		"CODEAF_GO_FIX_HEFT_SLOTS_CAP",
		"CODEAF_GO_FIX_LOOPGUARD_CYCLE_CAP",
		"CODEAF_GO_FIX_LUBY_GUARDS",
		"CODEAF_GO_FIX_ORCLIENT_TOOLCALL_INDEX",
		"CODEAF_GO_FIX_ORCLIENT_TOOLCALL_RESEND",
		"CODEAF_GO_FIX_ORFETCH_CANCEL",
		"CODEAF_GO_FIX_RETRY_ATTEMPT_CAP",
		"CODEAF_GO_FIX_RETRY_DELAY_CAP",
		"CODEAF_GO_FIX_RETRY_MESSAGE_GUARD",
		"CODEAF_GO_FIX_ROUTER_INFLIGHT_LEAK",
	}
	got := All()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("All() = %v, want %v", got, want)
	}

	got[0] = "mutated"
	if reflect.DeepEqual(All(), got) {
		t.Fatal("All returned mutable package state")
	}
}
