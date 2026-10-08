package config

// ChatChoiceLabel translates only the display. Raw registry values and writers
// retain their compatibility semantics, including inverse ui.hints.
func ChatChoiceLabel(key, raw, fallback string) string {
	switch key {
	case KeyHints:
		if raw == "on" {
			return "off"
		}
		if raw == "off" {
			return "on"
		}
	case KeyTaskStart:
		switch raw {
		case "sized":
			return "assess while working"
		case "single":
			return "skip assessment"
		}
	case KeyTaskSettle:
		switch raw {
		case "ask":
			return "ask me"
		case "auto":
			return "let the chat decide"
		}
	case KeyToolApprovalMode:
		switch raw {
		case "allow":
			return "allow by default"
		case "prompt":
			return "ask by default"
		case "deny":
			return "block by default"
		}
	case KeyWork:
		switch raw {
		case "fold":
			return "collapsed"
		case "open":
			return "expanded"
		}
	case KeyModelPool:
		switch raw {
		case "on":
			return "use and contribute"
		case "read":
			return "use only"
		}
	}
	return fallback
}
