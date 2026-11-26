package appwire

import "encoding/json"

// RunAppCommand uses the desktop method's decoder and implementation. This
// door deliberately has no access to data.*, connectors, or conversations.
func RunAppCommand(engine Engine, verb string, params json.RawMessage) (any, error) {
	switch verb {
	case "new", "plan", "build", "check", "say", "build_state", "checkpoints",
		"restore_checkpoint", "preview", "look", "discard", "change", "kit",
		"reference", "parts", "list", "get", "screen", "versions", "mark", "presence",
		"render", "outputs", "output_file":
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
