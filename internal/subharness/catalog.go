package subharness

import (
	"fmt"
	"strconv"
	"strings"
)

// catalogWidth is the column the guide's kind catalog wraps at: the width the
// document holds itself to everywhere else.
const catalogWidth = 78

// Catalog is the kind catalog the designer's guide teaches from, rendered
// from the registry itself: the fields the model is told about are the fields
// Validate will enforce, in the order kinds.go declares them. A kind added to
// the registry appears here, and a field renamed changes the guide and the
// law in one edit — the guide cannot describe a field the page refuses, nor
// refuse one the guide never mentioned.
func Catalog() string {
	kindCol, fieldCol := 0, 0
	for _, name := range kindOrder {
		k := kinds[name]
		if len(k.Name)+1 > kindCol {
			kindCol = len(k.Name) + 1
		}
		for _, s := range k.Specs {
			if len(s.name)+1 > fieldCol {
				fieldCol = len(s.name) + 1
			}
		}
	}
	var out strings.Builder
	for _, name := range kindOrder {
		k := kinds[name]
		indent := strings.Repeat(" ", kindCol)
		head := fmt.Sprintf("%-*s", kindCol, k.Name)
		desc := wrapCatalog(k.Desc, catalogWidth-len(head))
		out.WriteString(head + desc[0] + "\n")
		for _, line := range desc[1:] {
			out.WriteString(indent + line + "\n")
		}
		for _, s := range k.Specs {
			head := indent + fmt.Sprintf("%-*s", fieldCol, s.name)
			lines := wrapCatalog(s.describe(), catalogWidth-len(head))
			out.WriteString(head + lines[0] + "\n")
			for _, line := range lines[1:] {
				out.WriteString(indent + strings.Repeat(" ", fieldCol) + line + "\n")
			}
		}
	}
	return out.String()
}

// describe is one field's catalog line: its marker, its words or its cap, and
// its about.
func (s spec) describe() string {
	var b strings.Builder
	if s.required {
		b.WriteString("REQUIRED")
	} else {
		b.WriteString("optional")
	}
	if len(s.words) > 0 {
		b.WriteString(": ")
		b.WriteString(strings.Join(s.words, " | "))
	}
	if s.max > 0 {
		fmt.Fprintf(&b, " integer, 1..%d", s.max)
		if s.def > 0 {
			fmt.Fprintf(&b, " (default %d)", s.def)
		}
	}
	if s.about != "" {
		b.WriteString(". ")
		b.WriteString(strings.ToUpper(s.about[:1]) + s.about[1:])
	}
	return b.String()
}

// wrapCatalog breaks text at spaces, greedily, each line at most width cells.
func wrapCatalog(text string, width int) []string {
	if width < 20 {
		width = 20
	}
	var lines []string
	for len(text) > width {
		at := strings.LastIndex(text[:width+1], " ")
		if at < 0 {
			at = width
		}
		lines = append(lines, text[:at])
		text = strings.TrimLeft(text[at:], " ")
	}
	return append(lines, text)
}

// BeltEntry is one line of the tool list a designer is shown: a name and a
// sentence. Each door brings its own belt — the standalone rig's few tools, the
// chat's wire tools plus whichever media verbs this machine has models for — and
// everything else in the guide's machinery is this package's.
type BeltEntry struct {
	Name  string
	About string
}

// Machinery is every value the designer's guide leaves a hole for: this
// package's caps, both ladders, the kind catalog and the tool belt. It is the ONE
// map both doors that render the guide read — cmd/harness-design and
// internal/session — and it lives HERE, beside the numbers, because when each
// door kept its own copy a placeholder renamed in the guide was fixed in one and
// silently broke the other: the standalone tool could not render a brief for a
// fortnight after «kinds» arrived and «max_turns» left. prompts.Render refuses a
// hole nobody filled and a value nothing reads, so this map and that guide cannot
// drift apart without the next render saying so — and now there is one map to
// drift.
//
// The name column is sized from the belt rather than fixed, because
// generate_image is thirteen characters and a fixed width turns the list the
// designer reads into a ragged one the moment a media verb is present.
func Machinery(belt []BeltEntry) map[string]string {
	width := 0
	for _, tool := range belt {
		if len(tool.Name) > width {
			width = len(tool.Name)
		}
	}
	lines := make([]string, 0, len(belt))
	for _, tool := range belt {
		lines = append(lines, fmt.Sprintf("%-*s %s", width, tool.Name, tool.About))
	}
	return map[string]string{
		"kinds":         strings.TrimRight(Catalog(), "\n"),
		"max_nodes":     strconv.Itoa(MaxNodes),
		"max_id_bytes":  strconv.Itoa(MaxIdBytes),
		"max_dyn_cap":   strconv.Itoa(MaxDynCap),
		"verify_ladder": strings.Join(VerifyLadder(), " < "),
		"dyn_ladder":    strings.Join(DynLadder(), " < "),
		"tools":         strings.Join(lines, "\n"),
	}
}
