package subharness

import (
	"fmt"
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
