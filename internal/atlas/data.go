// Package atlas is the architecture map of codeaf's two-computer work:
// pairing, devices, cells, sync and the furrow engine, drawn as boxes and
// arrows in the terminal. `codeaf atlas` opens it.
//
// THE DATA IS ONE PACKAGE. Every box, arrow, flow and file reference on the
// map lives in [Pairing] and the other files beside it, and nowhere else —
// the view never hard-codes content,
// The Go map here is the only version of the diagram; the retired TypeScript
// twin was removed and everything reads from this package. When the code
// moves, this file moves in the same change; data_test.go fails the build
// when a files[].Path no longer exists.
package atlas

// Kind is what a box is; it drives its colour and its legend entry.
type Kind string

const (
	KindMachine Kind = "machine"
	KindGo      Kind = "go"
	KindService Kind = "service"
	KindEngine  Kind = "engine"
	KindSpec    Kind = "spec"
)

// FileRef is a real file in the repository, relative to the repo root.
type FileRef struct {
	Path    string
	Symbols []string // key functions and types worth knowing
	Note    string
}

// Node is one box on the map.
type Node struct {
	ID      string
	Label   string // box title, kept under ~18 columns
	Short   string // one-line subtitle drawn inside the box when there is room
	Kind    Kind
	X, Y    float64 // centre of the box as fractions (0..1) of the map area
	Summary string  // plain-words description of what it does
	Details []string
	Files   []FileRef
}

// Edge is one arrow: who calls or talks to whom, and over what.
type Edge struct {
	ID     string
	From   string
	To     string
	Label  string // a few words
	Detail string // what crosses, and whether it is encrypted
}

// Step is one move of a flow.
type Step struct {
	From    string
	To      string
	Message string // what is sent, in plain words
	Edge    string // id of the overview edge this step travels along
	Note    string // why, or where in the code
}

// Flow is a step-through story over the map.
type Flow struct {
	ID      string
	Title   string
	Summary string
	Steps   []Step
}

// Map is one whole diagram: the single source the view renders from. Every
// registered map is grounded in the code: each Files[].Path must exist
// relative to the repository root; data_test.go checks that for every map.
type Map struct {
	Name        string // the name a person types: `codeaf atlas <name>`
	Title       string
	Description string // one line, shown in the picker
	Nodes       []Node
	Edges       []Edge
	Flows       []Flow
}
