// Package prompt holds the Builder's prompt pack and the pure logic around it:
// rendering the three calls (plan, write, fix), parsing what the model sends
// back, applying plan-card edits, and the plan → write → check → fix policy.
// It performs no I/O; the wiring lane owns the model and the server.
package prompt

import (
	"embed"
	"fmt"
	"sort"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl templates/runs.txt
var templateFS embed.FS

var templates = template.Must(template.New("prompt").
	Option("missingkey=error").
	Funcs(template.FuncMap{
		"join": strings.Join, "runRules": runRules,
		"lineLimit": lineLimit, "nameLimit": nameLimit,
	}).
	ParseFS(templateFS, "templates/*.tmpl"))

// Kind names one of the Builder's three calls.
type Kind string

const (
	KindPlan  Kind = "plan"
	KindWrite Kind = "write"
	KindFix   Kind = "fix"
)

// Inputs is everything a call may be given. Each Kind requires a subset; see
// require. Catalog and Schemas arrive as text so this package does not depend
// on the contract lane's embedded files.
type Inputs struct {
	Paragraph  string
	Space      string
	Budget     string
	Connectors []string
	Catalog    string
	Schemas    string
	Plan       string // plan.json text; a revision input for plan, the source for write
	// Brief is the design brief the PLAN call wrote (Plan.Brief): plan shows
	// it on a revision, write and fix read it so every file serves one design.
	Brief   string
	Request string // a plan-card edit or Shape request, for plan revisions
	Files   map[string]string
	Report  string
	// AgentModel is the model id the app's own agent runs on (write only).
	AgentModel string
	// Only narrows a write to these files, so one call writes one screen and
	// the calls run side by side. Files then holds what is already written.
	Only []string
}

// Wants reports whether this write covers a file under prefix: every file when
// Only is empty, otherwise the files Only names.
func (in Inputs) Wants(prefix string) bool {
	if len(in.Only) == 0 {
		return true
	}
	for _, path := range in.Only {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// File is one current app file as the fix prompt shows it.
type File struct{ Path, Content string }

type view struct {
	Inputs
	Example Example
}

// ExampleReply is the few-shot WRITE reply cut to the kinds of file this call
// writes, so a single-screen call is not shown the whole CRM.
func (v view) ExampleReply() (string, error) {
	return exampleReply(func(path string) bool {
		return v.Wants(path) || v.Wants(exampleGroup(path))
	})
}

// KitHints is the one-line when-to-use hint for each catalog part that has one.
func (v view) KitHints() string { return kitHints(v.Catalog) }

// SortedFiles lists the files by path so a prompt is byte-stable.
func (v view) SortedFiles() []File {
	files := make([]File, 0, len(v.Files))
	for path, content := range v.Files {
		files = append(files, File{path, content})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

// Render fills the template for kind, or says which input is missing.
func Render(kind Kind, in Inputs) (string, error) {
	if err := in.require(kind); err != nil {
		return "", err
	}
	example, err := loadExample()
	if err != nil {
		return "", err
	}
	var out strings.Builder
	if err := templates.ExecuteTemplate(&out, string(kind)+".tmpl", view{in, example}); err != nil {
		return "", fmt.Errorf("prompt: render %s: %w", kind, err)
	}
	return out.String(), nil
}

func (in Inputs) require(kind Kind) error {
	var missing []string
	need := func(name, value string) {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	switch kind {
	case KindPlan:
		need("Space", in.Space)
		need("Budget", in.Budget)
		need("Catalog", in.Catalog)
		if strings.TrimSpace(in.Plan) == "" || strings.TrimSpace(in.Request) == "" {
			need("Paragraph", in.Paragraph)
		}
	case KindWrite:
		need("Plan", in.Plan)
		need("Catalog", in.Catalog)
		need("Schemas", in.Schemas)
		need("Space", in.Space)
		need("Budget", in.Budget)
	case KindFix:
		need("Report", in.Report)
		if len(in.Files) == 0 {
			missing = append(missing, "Files")
		}
	default:
		return fmt.Errorf("prompt: unknown kind %q", kind)
	}
	if len(missing) > 0 {
		return fmt.Errorf("prompt: %s needs %s", kind, strings.Join(missing, ", "))
	}
	return nil
}

func runRules() (string, error) {
	text, err := templateFS.ReadFile("templates/runs.txt")
	return string(text), err
}
