package tui3

import "github.com/Agent-Field/codeaf/internal/config"

// settingValueWord changes only the reading. Editors and persistence keep the
// existing values, and search accepts both the reading and the saved spelling.
func settingValueWord(row config.Setting) string {
	return settingChoiceWord(row.Key, row.Value(), row.Reading())
}

func settingChoiceWord(key, raw, fallback string) string {
	switch key {
	case config.KeyTaskStart:
		switch raw {
		case "sized":
			return "assess while working"
		case "single":
			return "skip assessment"
		}
	case config.KeyTaskSettle:
		switch raw {
		case "ask":
			return "ask me"
		case "auto":
			return "let the chat decide"
		}
	case config.KeyToolApprovalMode:
		switch raw {
		case "allow":
			return "allow by default"
		case "prompt":
			return "ask by default"
		case "deny":
			return "block by default"
		}
	case config.KeyWork:
		switch raw {
		case "fold":
			return "collapsed"
		case "open":
			return "expanded"
		}
	case config.KeyModelPool:
		switch raw {
		case "on":
			return "use and contribute"
		case "read":
			return "use only"
		}
	}
	return fallback
}
