package tui3

import "github.com/Agent-Field/codeaf/internal/config"

// settingValueWord changes only the reading. Editors and persistence keep the
// existing values, and search accepts both the reading and the saved spelling.
func settingValueWord(row config.Setting) string {
	value := row.Reading()
	switch row.Key {
	case config.KeyTaskStart:
		switch row.Value() {
		case "sized":
			return "assess the brief"
		case "single":
			return "start directly"
		}
	case config.KeyTaskSettle:
		switch row.Value() {
		case "ask":
			return "ask me"
		case "auto":
			return "let the chat decide"
		}
	case config.KeyToolApprovalMode:
		switch row.Value() {
		case "allow":
			return "allow by default"
		case "prompt":
			return "ask each time"
		case "deny":
			return "block by default"
		}
	case config.KeyWork:
		switch row.Value() {
		case "fold":
			return "collapsed"
		case "open":
			return "expanded"
		}
	case config.KeyModelPool:
		switch row.Value() {
		case "on":
			return "use and contribute"
		case "read":
			return "use only"
		}
	}
	return value
}
