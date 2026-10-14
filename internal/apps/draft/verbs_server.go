package draft

import (
	"io"
	"strings"
)

// The server verbs need services only the running Builder has: sample rows,
// the full Check, the runtime. Here they only check their own arguments; the
// answer comes from the Builder over its socket.
func init() {
	add("check", remoteVerb("check", "codeaf app check [--json]", 0, nil, []string{"json"}))
	add("try", remoteVerb("try", "codeaf app try <run-id>", 1, nil, nil))
	add("done", remoteVerb("done", `codeaf app done "<one line>"`, 1, nonBlank, nil))
}

func nonBlank(a argv) error {
	if strings.TrimSpace(a.pos[0]) == "" {
		return usage("the closing line must not be empty — usage: %s", a.usage)
	}
	return nil
}

func remoteVerb(name, usageLine string, positionals int, extra func(argv) error, bools []string) Verb {
	return Verb{
		Usage:  usageLine,
		Remote: true,
		Parse: func(args []string, _ io.Reader) (Entry, error) {
			a, err := scanArgs(args, usageLine, nil, bools)
			if err != nil {
				return Entry{}, err
			}
			if err := a.exactly(positionals); err != nil {
				return Entry{}, err
			}
			if extra != nil {
				if err := extra(a); err != nil {
					return Entry{}, err
				}
			}
			return Entry{Verb: name, Args: args, Piece: Piece{Kind: name}}, nil
		},
	}
}
