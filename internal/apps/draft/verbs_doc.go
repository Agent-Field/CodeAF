package draft

import (
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// docKind is what differs between the three whole-document verbs: the file a
// piece lives in, how a body is judged alone, and where its title is.
type docKind struct {
	verb    string
	usage   string
	hasID   bool
	judge   func(st State, id, body string) []string
	title   func(body string) string
	store   func(st *State, id, body string)
	display string // the owner's word for the piece
}

var docKinds = []docKind{
	{
		verb: "screen", usage: "codeaf app screen <id> < screen.json", hasID: true,
		judge:   judgeScreen,
		title:   func(body string) string { return jsonTitle(body, "title") },
		store:   func(st *State, id, body string) { st.Screens[id] = json.RawMessage(body) },
		display: "Screen",
	},
	{
		verb: "run", usage: "codeaf app run <id> < run.yaml", hasID: true,
		judge:   judgeRun,
		title:   runTitle,
		store:   func(st *State, id, body string) { st.Runs[id] = []byte(body) },
		display: "Run",
	},
	{
		verb: "agent", usage: "codeaf app agent < agent.md",
		judge:   judgeAgent,
		title:   agentTitle,
		store:   func(st *State, _, body string) { st.Agent = []byte(body) },
		display: "Agent",
	},
}

func init() {
	for _, k := range docKinds {
		register(k.verb, k.def())
	}
}

type docArgs struct{ id, body string }

func (k docKind) def() def[docArgs] {
	return def[docArgs]{
		Usage: k.usage,
		Body:  true,
		Decode: func(a argv, body string) (docArgs, error) {
			want := 0
			if k.hasID {
				want = 1
			}
			if err := a.exactly(want); err != nil {
				return docArgs{}, err
			}
			if strings.TrimSpace(body) == "" {
				return docArgs{}, usage("the document must come on stdin, for example with a heredoc — usage: %s", k.usage)
			}
			id := ""
			if k.hasID {
				id = a.pos[0]
			}
			return docArgs{id, body}, nil
		},
		Piece: func(d docArgs) Piece { return Piece{Kind: k.verb, ID: d.id} },
		Check: func(st State, _ Entry, d docArgs) error {
			if k.hasID {
				if err := checkSlug(k.verb, d.id); err != nil {
					return err
				}
			}
			if lines := k.judge(st, d.id, d.body); len(lines) > 0 {
				return refuseAll(lines)
			}
			return nil
		},
		Apply: func(st *State, d docArgs) { k.store(st, d.id, d.body) },
		Line: func(_ State, d docArgs) string {
			if title := k.title(d.body); title != "" {
				return k.display + " · " + title
			}
			return k.display + " · " + orElse(d.id, "agent")
		},
	}
}

// judgeScreen checks one screen alone: its schema, then its parts, props,
// bindings and field names against the draft's records.
func judgeScreen(st State, id, body string) []string {
	file := "screens/" + id + ".json"
	if issues := spec.ValidateFile(file, []byte(body)); len(issues) > 0 {
		return issueLines(file, issues, "codeaf app schema screen")
	}
	var screen spec.Screen
	if err := json.Unmarshal([]byte(body), &screen); err != nil {
		return []string{file + ": " + err.Error()}
	}
	return lintScreen(st, id, &screen)
}

// judgeRun checks one run alone: its schema and references, then the record
// types, fields and queries it names against the draft.
func judgeRun(st State, id, body string) []string {
	file := "runs/" + id + ".yaml"
	if issues := spec.ValidateFile(file, []byte(body)); len(issues) > 0 {
		return issueLines(file, issues, "codeaf app schema run")
	}
	asJSON, err := spec.YAMLToJSON([]byte(body))
	if err != nil {
		return []string{file + ": " + err.Error()}
	}
	var run spec.Run
	if err := json.Unmarshal(asJSON, &run); err != nil {
		return []string{file + ": " + err.Error()}
	}
	return lintRun(st, id, &run)
}

func judgeAgent(_ State, _, body string) []string {
	if _, err := spec.ParseAgent([]byte(body)); err != nil {
		return []string{err.Error() + " — see: codeaf app schema agent"}
	}
	return nil
}

func jsonTitle(body, key string) string {
	var doc map[string]any
	_ = json.Unmarshal([]byte(body), &doc)
	title, _ := doc[key].(string)
	return title
}

func runTitle(body string) string {
	asJSON, err := spec.YAMLToJSON([]byte(body))
	if err != nil {
		return ""
	}
	return jsonTitle(string(asJSON), "title")
}

func agentTitle(body string) string {
	doc, err := spec.ParseAgent([]byte(body))
	if err != nil {
		return ""
	}
	return doc.Name
}
