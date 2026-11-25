package outputs

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

const (
	contentTypes = "http://schemas.openxmlformats.org/package/2006/content-types"
	packageRels  = "http://schemas.openxmlformats.org/package/2006/relationships"
	officeRels   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	wordNS       = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	presNS       = "http://schemas.openxmlformats.org/presentationml/2006/main"
	drawNS       = "http://schemas.openxmlformats.org/drawingml/2006/main"
)

type officePart struct {
	path, contentType, xml string
}

type relationship struct {
	kind, target string
}

func relationships(path string, links ...relationship) officePart {
	var body strings.Builder
	fmt.Fprintf(&body, `<Relationships xmlns="%s">`, packageRels)
	for i, link := range links {
		fmt.Fprintf(&body, `<Relationship Id="rId%d" Type="%s/%s" Target="%s"/>`, i+1, officeRels, link.kind, link.target)
	}
	body.WriteString("</Relationships>")
	return officePart{path: path, xml: body.String()}
}

func writeOffice(w io.Writer, main string, parts []officePart) error {
	var types strings.Builder
	fmt.Fprintf(&types, `<Types xmlns="%s"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`, contentTypes)
	for _, part := range parts {
		if part.contentType != "" {
			fmt.Fprintf(&types, `<Override PartName="/%s" ContentType="%s"/>`, part.path, part.contentType)
		}
	}
	types.WriteString("</Types>")
	parts = append(parts, officePart{path: "[Content_Types].xml", xml: types.String()}, relationships("_rels/.rels", relationship{"officeDocument", main}))
	z := zip.NewWriter(w)
	for _, part := range parts {
		f, err := z.Create(part.path)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(f, xml.Header+part.xml); err != nil {
			return err
		}
	}
	return z.Close()
}

func xmlText(s string) string {
	var escaped strings.Builder
	_ = xml.EscapeText(&escaped, []byte(s))
	return escaped.String()
}

func writeDOCX(w io.Writer, title string, sections []string) error {
	var body strings.Builder
	fmt.Fprintf(&body, `<w:document xmlns:w="%s"><w:body>`, wordNS)
	writeParagraph := func(text string, heading bool) {
		body.WriteString("<w:p><w:r>")
		if heading {
			body.WriteString(`<w:rPr><w:b/><w:sz w:val="36"/></w:rPr>`)
		}
		for i, line := range strings.Split(text, "\n") {
			if i > 0 {
				body.WriteString("<w:br/>")
			}
			for j, tab := range strings.Split(line, "\t") {
				if j > 0 {
					body.WriteString("<w:tab/>")
				}
				fmt.Fprintf(&body, `<w:t xml:space="preserve">%s</w:t>`, xmlText(tab))
			}
		}
		body.WriteString("</w:r></w:p>")
	}
	writeParagraph(title, true)
	for _, section := range sections {
		writeParagraph(section, false)
	}
	body.WriteString("</w:body></w:document>")
	const main = "word/document.xml"
	return writeOffice(w, main, []officePart{{main, "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml", body.String()}})
}
