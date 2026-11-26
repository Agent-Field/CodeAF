package appwire

import "encoding/json"

// RunAppCommand shares the desktop decoder and implementation for owner
// commands. Direct record and connector calls use their own scoped doors.
func RunAppCommand(engine Engine, verb string, params json.RawMessage) (any, error) {
	switch verb {
	case "residency.show", "residency.preview", "residency.cutover":
		return methods[verb].call(engine, params)
	case "new", "plan", "build", "check", "say", "build_state", "checkpoints",
		"restore_checkpoint", "preview", "look", "discard", "change", "kit",
		"reference", "parts", "common_types", "bind_common_type",
		"match_candidates", "match_scan", "match_accept", "match_reject", "match_merge", "match_unmerge", "list", "get", "screen", "versions", "mark", "presence":
	case "add_type", "add_field", "configure", "recipe", "markup", "claim", "set_run", "set_agent", "set_sample":
		return nil, Errorf(CodeForbidden, "Start an app build to run this command in its scoped session.")
	default:
		return nil, Errorf(CodeUnknownMethod, "Unknown app command %q.", verb)
	}
	m, ok := methods["apps."+verb]
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "App command %q is unavailable.", verb)
	}
	return m.call(engine, params)
}
