package sessionhost

// Report is the operator-facing feasibility report, with a compact projection
// served by the health endpoint.
// abstractScopeABI is the first Landlock version that can scope abstract Unix
// sockets (Linux 6.12); internal/executor refuses a required fence below it too.
const abstractScopeABI = 6

// requireUIDSetting is the exact operator setting that accepts process-only
// isolation. Both refusals that offer it spell it from here so they cannot
// drift apart, and README.md in deploy/org quotes the same words.
const requireUIDSetting = "CODEAF_SESSIONHOST_REQUIRE=uid"

type Report struct {
	OK          bool           `json:"ok"`
	Mode        string         `json:"mode"`
	Reason      string         `json:"reason"`
	Kernel      string         `json:"kernel"`
	EUID        int            `json:"euid"`
	CapEff      string         `json:"cap_eff"`
	CapBnd      string         `json:"cap_bnd"`
	CapNamesEff []string       `json:"cap_names_eff"`
	NoNewPrivs  int            `json:"no_new_privs"`
	Seccomp     int            `json:"seccomp"`
	UIDSwitch   SwitchReport   `json:"uid_switch"`
	Landlock    LandlockReport `json:"landlock"`
	UserNS      bool           `json:"userns"`
	PtraceScope string         `json:"ptrace_scope"`
	FenceMode   string         `json:"fence_mode"`
	Require     string         `json:"require"`
}

type SwitchReport struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

type LandlockReport struct {
	Available bool `json:"available"`
	ABI       int  `json:"abi"`
}

func (r Report) decide() Report {
	r.FenceMode = "none"
	if r.Landlock.Available {
		r.FenceMode = "landlock"
		if r.UserNS {
			r.FenceMode = "full"
		}
	}
	r.OK, r.Mode = false, modeRefused
	switch {
	case !r.UIDSwitch.OK:
		r.Reason = r.UIDSwitch.Error
	case r.Require == modeUID:
		r.OK, r.Mode, r.Reason = true, modeUID, ""
	case r.FenceMode == "none":
		r.Reason = "command restrictions are unavailable on this machine; to start chats with the user switch alone, set " + requireUIDSetting
	case r.Landlock.ABI < abstractScopeABI:
		// Abstract Unix sockets belong to no user, so only the fence can keep one
		// chat from connecting to another's, and only a kernel from Linux 6.12 can
		// make the fence do that. An operator who accepts the gap says so with
		// CODEAF_SESSIONHOST_REQUIRE=uid, which is checked above. THE SENTENCE NAMES
		// THAT SETTING EXACTLY, because an operator who meets this refusal on a
		// machine with an older kernel has no other way to learn what to type.
		r.Reason = "command restrictions cannot keep one chat's local sockets from another's on this kernel (Linux 6.12 or newer can); to start chats here anyway, set " + requireUIDSetting
	default:
		r.OK, r.Mode, r.Reason = true, modeFenced, ""
	}
	return r
}
