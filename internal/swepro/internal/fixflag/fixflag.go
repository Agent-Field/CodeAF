// Package fixflag provides live opt-in checks for bug-for-bug compatibility
// fixes. A fix is enabled only while its environment variable is exactly "1".
package fixflag

import "os"

var all = []string{
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

// Enabled reports whether name is enabled right now.
func Enabled(name string) bool {
	return os.Getenv(name) == "1"
}

// All returns every known compatibility-fix flag name.
func All() []string {
	flags := make([]string, len(all))
	copy(flags, all)
	return flags
}
