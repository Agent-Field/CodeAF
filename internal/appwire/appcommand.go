package appwire

import (
	"encoding/json"
	"strings"
)

// RunAppCommand shares the desktop decoder and implementation for owner
// commands. Direct record and connector calls use their own scoped doors.
func RunAppCommand(engine Engine, verb string, params json.RawMessage) (any, error) {
	switch verb {
	case "residency.show", "residency.preview", "residency.cutover":
		return methods[verb].call(engine, params)
	case "agent_launch", "agent_review", "agent_approval", "agent_stop", "agent_steer":
		return methods["agents."+strings.TrimPrefix(verb, "agent_")].call(engine, params)
	case "new", "plan", "build", "check", "say", "build_state", "checkpoints",
		"restore_checkpoint", "preview", "look", "discard", "change", "kit",
		"reference", "parts", "kit_parts", "promote_part", "kit_steps", "promote_step", "common_types", "bind_common_type",
		"match_candidates", "match_scan", "match_accept", "match_reject", "match_merge", "match_unmerge", "list", "get", "screen", "versions", "mark", "presence",
		"render", "outputs", "output_file",
		"pack", "install", "fork", "update", "lineage", "resolve_package", "apply", "keep":
	case "add_type", "add_field", "configure", "recipe", "markup", "claim", "set_run", "set_agent", "set_sample", "set_custom_part", "set_custom_step", "use_kit_part", "use_kit_step":
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
