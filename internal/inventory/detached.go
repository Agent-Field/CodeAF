package inventory

import "regexp"

// hint is one kind of command that starts something outside its own process
// tree, and the command that ends it. It is data: a row is added by naming two
// patterns, and nothing else changes. A hint only ever adds a line to Detached;
// it never gates or runs anything.
type hint struct {
	start, stop *regexp.Regexp
}

func row(start, stop string) hint {
	return hint{regexp.MustCompile(start), regexp.MustCompile(stop)}
}

// detachedHints is the five kinds the record knows.
var detachedHints = []hint{
	row(`^docker\s+compose\b.*\bup\b`, `^docker\s+compose\b.*\b(down|stop|rm)\b`),
	row(`^docker\s+run\b.*\s(-d|--detach)\b`, `^docker\s+(stop|rm|kill)\b`),
	row(`^docker\s+start\b`, `^docker\s+(stop|rm|kill)\b`),
	row(`^brew\s+services\s+start\b`, `^brew\s+services\s+stop\b`),
	row(`^systemctl\b.*\bstart\b`, `^systemctl\b.*\bstop\b`),
}

// applyDetached folds one command that succeeded into the list. A start
// replaces the earlier start of its own kind, so the list holds the latest of
// each; a stop removes the start of its kind, so a service that was brought
// down is not reported as left up. A command that is neither changes nothing.
func applyDetached(list []Detached, command, cwd string) []Detached {
	for _, h := range detachedHints {
		switch {
		case h.start.MatchString(command):
			return append(withoutKind(list, h), Detached{Command: command, Cwd: cwd})
		case h.stop.MatchString(command):
			return withoutKind(list, h)
		}
	}
	return list
}

// withoutKind is list without the starts a hint stands for.
func withoutKind(list []Detached, h hint) []Detached {
	var out []Detached
	for _, d := range list {
		if !h.start.MatchString(d.Command) {
			out = append(out, d)
		}
	}
	return out
}
