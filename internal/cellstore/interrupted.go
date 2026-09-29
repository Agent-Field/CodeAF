package cellstore

import (
	"encoding/json"
	"fmt"
	"strings"
)

// briefMax is how much of a call's arguments a line about it carries.
const briefMax = 60

// briefKeys are the argument names that say what a call is about, in the order
// they are looked for.
var briefKeys = []string{"command", "path", "file_path", "pattern", "url", "query"}

// briefOf is the few words of a call's JSON arguments a person recognises it by,
// or nothing when no argument reads as one.
func briefOf(args []byte) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(args, &fields) != nil {
		return ""
	}
	for _, key := range briefKeys {
		var text string
		if json.Unmarshal(fields[key], &text) == nil && strings.TrimSpace(text) != "" {
			return clip(text)
		}
	}
	return ""
}

// clip puts text on one line and cuts it to briefMax.
func clip(text string) string {
	line := strings.Join(strings.Fields(text), " ")
	if r := []rune(line); len(r) > briefMax {
		return string(r[:briefMax-1]) + "…"
	}
	return line
}

// what is the call as one phrase: the tool, and its brief when it has one.
func (i Intent) what() string {
	if i.Brief == "" {
		return i.Tool
	}
	return i.Tool + ": " + i.Brief
}

// said is the sentence a person reads about the call.
func (i Intent) said() string {
	return i.what() + " — stopped when codeaf was killed — not run again. Ask me to run it again if you still want it."
}

// interrupted is what the calls a crash left unfinished look like to a person
// (Lines), to the model (Note), and once both have been told (Close).
type interrupted struct{ rec *Recorder }

// Lines is one plain sentence per interrupted call, oldest first.
func (v interrupted) Lines() []string {
	var lines []string
	for _, in := range v.rec.Incomplete() {
		lines = append(lines, in.said())
	}
	return lines
}

// Note is the fact the model needs so it does not take the calls for done. It is
// empty when nothing was interrupted.
func (v interrupted) Note() string {
	calls := v.rec.Incomplete()
	if len(calls) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("These tool calls were running when codeaf was killed. Their results are unknown: each may have stopped part way or finished, and none was run again. Do not assume any of them succeeded; look at what they touch before relying on it, and run one again only if the person asks.")
	for _, in := range calls {
		fmt.Fprintf(&b, "\n- %s (%s)", in.what(), in.SideEffect)
	}
	return b.String()
}

// Close counts every interrupted call as dealt with: the person and the model
// have both been told, and the cell can be rewound again.
func (v interrupted) Close() error {
	for _, in := range v.rec.Incomplete() {
		if err := v.rec.Resolve(in); err != nil {
			return err
		}
	}
	return nil
}
