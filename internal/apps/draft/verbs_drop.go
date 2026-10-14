package draft

import "strings"

// dropper knows how to find and remove one kind of piece.
type dropper struct {
	ids    func(State) []string
	remove func(*State, string)
}

var droppers = map[string]dropper{
	"record": {
		func(st State) []string { return sortedKeys(st.Records) },
		func(st *State, id string) { delete(st.Records, id); delete(st.FieldOrder, id) },
	},
	"field": {
		func(st State) []string {
			return dotted(st, func(r string) []string { return sortedKeys(st.Records[r].Fields) })
		},
		func(st *State, id string) {
			if rec, name, ok := splitDot(id); ok && st.Records[rec] != nil {
				delete(st.Records[rec].Fields, name)
				st.FieldOrder[rec] = without(st.FieldOrder[rec], name)
			}
		},
	},
	"constant": {
		func(st State) []string {
			return dotted(st, func(r string) []string { return sortedKeys(st.Records[r].Constants) })
		},
		func(st *State, id string) {
			if rec, name, ok := splitDot(id); ok && st.Records[rec] != nil {
				delete(st.Records[rec].Constants, name)
			}
		},
	},
	"screen": {
		func(st State) []string { return sortedKeys(st.Screens) },
		func(st *State, id string) { delete(st.Screens, id) },
	},
	"run": {
		func(st State) []string { return sortedKeys(st.Runs) },
		func(st *State, id string) { delete(st.Runs, id) },
	},
	"agent": {
		func(st State) []string {
			if st.Agent == nil {
				return nil
			}
			return []string{"agent"}
		},
		func(st *State, _ string) { st.Agent = nil },
	},
	"connect": {
		func(st State) []string { return sortedKeys(st.Connectors) },
		func(st *State, id string) { delete(st.Connectors, id) },
	},
	"trigger": {
		func(st State) []string { return triggerIDs(st) },
		func(st *State, id string) {
			kept := st.Triggers[:0:0]
			for _, t := range st.Triggers {
				if t.ID != id {
					kept = append(kept, t)
				}
			}
			st.Triggers = kept
		},
	},
	"command": {
		func(st State) []string { return commandIDs(st) },
		func(st *State, id string) {
			kept := st.Commands[:0:0]
			for _, c := range st.Commands {
				if c.ID != id {
					kept = append(kept, c)
				}
			}
			st.Commands = kept
		},
	},
}

func triggerIDs(st State) []string {
	out := make([]string, len(st.Triggers))
	for i, t := range st.Triggers {
		out[i] = t.ID
	}
	return out
}

func commandIDs(st State) []string {
	out := make([]string, len(st.Commands))
	for i, c := range st.Commands {
		out[i] = c.ID
	}
	return out
}

// dotted lists "<record>.<name>" ids for the pieces that live inside a record.
func dotted(st State, names func(record string) []string) []string {
	var out []string
	for _, rec := range sortedKeys(st.Records) {
		for _, n := range names(rec) {
			out = append(out, rec+"."+n)
		}
	}
	return out
}

func without(list []string, drop string) []string {
	kept := list[:0:0]
	for _, v := range list {
		if v != drop {
			kept = append(kept, v)
		}
	}
	return kept
}

func splitDot(id string) (record, name string, ok bool) {
	return strings.Cut(id, ".")
}

type dropArgs struct{ kind, id string }

func init() {
	register("drop", def[dropArgs]{
		Usage: "codeaf app drop <record|field|constant|screen|run|agent|connect|trigger|command> <id>   (field and constant ids are <record>.<name>; agent takes no id)",
		Decode: func(a argv, _ string) (dropArgs, error) {
			if len(a.pos) == 1 && a.pos[0] == "agent" {
				return dropArgs{"agent", "agent"}, nil
			}
			if err := a.exactly(2); err != nil {
				return dropArgs{}, err
			}
			return dropArgs{a.pos[0], a.pos[1]}, nil
		},
		Piece: func(d dropArgs) Piece { return Piece{Kind: "drop", ID: d.kind + ":" + d.id} },
		Check: func(st State, _ Entry, d dropArgs) error {
			dr, ok := droppers[d.kind]
			if !ok {
				return usage("drop kind must be one of %s, got %q", ids(sortedKeys(droppers)), d.kind)
			}
			if !contains(dr.ids(st), d.id) {
				return refuse("there is no %s %q to drop (%s: %s)", d.kind, d.id, d.kind, ids(dr.ids(st)))
			}
			return nil
		},
		Apply: func(st *State, d dropArgs) {
			if dr, ok := droppers[d.kind]; ok {
				dr.remove(st, d.id)
			}
		},
		Line: func(_ State, d dropArgs) string { return "Removed " + d.kind + " " + d.id },
	})
}
