package outputs

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

const (
	pdfPageBox = "[0 0 595.28 841.89]"
	pdfMargin  = 51
	pdfTop     = 790
	pdfWidth   = 493
	pdfEm      = 1000
)

type pdfChar struct {
	cid   uint16
	glyph sfnt.GlyphIndex
	width int
}

type pdfFont struct {
	font  *sfnt.Font
	buf   sfnt.Buffer
	chars map[rune]pdfChar
	order []rune
}

func (f *pdfFont) char(r rune) (pdfChar, error) {
	if ch, ok := f.chars[r]; ok {
		return ch, nil
	}
	if len(f.order) == 65535 {
		return pdfChar{}, errors.New("PDF needs too many distinct characters")
	}
	glyph, err := f.font.GlyphIndex(&f.buf, r)
	if err == nil && glyph == 0 {
		glyph, err = f.font.GlyphIndex(&f.buf, '?')
	}
	if err != nil {
		return pdfChar{}, err
	}
	width, err := f.font.GlyphAdvance(&f.buf, glyph, fixed.I(pdfEm), font.HintingNone)
	if err != nil {
		return pdfChar{}, err
	}
	ch := pdfChar{cid: uint16(1 + len(f.order)), glyph: glyph, width: width.Round()}
	f.order = append(f.order, r)
	f.chars[r] = ch
	return ch, nil
}

func writePDF(w io.Writer, title string, sections []string) error {
	face, err := sfnt.Parse(goregular.TTF)
	if err != nil {
		return err
	}
	layout := pdfLayout{
		font: pdfFont{font: face, chars: map[rune]pdfChar{}},
		y:    pdfTop,
	}
	if err := layout.paragraph(title, 18, 24); err != nil {
		return err
	}
	layout.y -= 8
	for _, section := range sections {
		if err := layout.paragraph(section, 11, 15); err != nil {
			return err
		}
		layout.y -= 7
	}
	layout.pages = append(layout.pages, layout.page.String())
	return layout.document(w)
}

type pdfLayout struct {
	font  pdfFont
	page  strings.Builder
	pages []string
	y     int
}

// A paragraph wraps to the actual font advances. Keeping each page's
// coordinates in points makes the same input paginate identically.
func (l *pdfLayout) paragraph(text string, size, leading int) error {
	maxWidth := pdfWidth * pdfEm / size
	text = strings.ReplaceAll(text, "\t", "    ")
	for _, source := range strings.Split(text, "\n") {
		chars := make([]pdfChar, 0, len(source))
		for _, r := range source {
			ch, err := l.font.char(r)
			if err != nil {
				return err
			}
			chars = append(chars, ch)
		}
		for len(chars) > 0 {
			n, width, space := 0, 0, 0
			for n < len(chars) && width+chars[n].width <= maxWidth {
				if chars[n] == l.font.chars[' '] {
					space = n + 1
				}
				width += chars[n].width
				n++
			}
			if n < len(chars) && space > 0 {
				n = space
			}
			n = max(n, 1)
			l.line(chars[:n], size, leading)
			chars = chars[n:]
		}
		if source == "" {
			l.y -= leading
		}
	}
	return nil
}

func (l *pdfLayout) line(chars []pdfChar, size, leading int) {
	if l.y < pdfMargin {
		l.pages = append(l.pages, l.page.String())
		l.page.Reset()
		l.y = pdfTop
	}
	fmt.Fprintf(&l.page, "BT /F %d Tf %d %d Td <", size, pdfMargin, l.y)
	for _, ch := range chars {
		fmt.Fprintf(&l.page, "%04X", ch.cid)
	}
	l.page.WriteString("> Tj ET\n")
	l.y -= leading
}

func (l *pdfLayout) document(w io.Writer) error {
	// The Catalog and Pages objects have stable numbers because both the page
	// tree and its page objects refer to each other.
	const catalog, pages = 1, 2
	d := pdfDocument{objects: []string{
		fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages),
		"",
	}}
	fontID, err := d.embedFont(&l.font)
	if err != nil {
		return err
	}
	var kids strings.Builder
	for _, page := range l.pages {
		content := d.stream("", []byte(page))
		id := d.add(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox %s /Resources << /Font << /F %d 0 R >> >> /Contents %d 0 R >>",
			pages, pdfPageBox, fontID, content))
		fmt.Fprintf(&kids, " %d 0 R", id)
	}
	d.objects[pages-1] = fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s ] >>", len(l.pages), kids.String())
	return d.write(w, catalog)
}

type pdfDocument struct {
	objects []string
}

func (d *pdfDocument) add(value string) int {
	d.objects = append(d.objects, value)
	return len(d.objects)
}

func (d *pdfDocument) stream(dict string, data []byte) int {
	var compressed bytes.Buffer
	z := zlib.NewWriter(&compressed)
	// A bytes.Buffer cannot fail a write or close.
	_, _ = z.Write(data)
	_ = z.Close()
	return d.add(fmt.Sprintf("<< /Length %d /Filter /FlateDecode %s >>\nstream\n%s\nendstream", compressed.Len(), dict, compressed.Bytes()))
}

func (d *pdfDocument) write(w io.Writer, root int) error {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, len(d.objects))
	for i, object := range d.objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := buf.Len()
	size := len(d.objects) + 1
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", size)
	for _, offset := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", size, root, xref)
	_, err := w.Write(buf.Bytes())
	return err
}

// PDF can select the same glyph for several Unicode characters. Distinct
// character codes keep text extraction faithful even when they look alike.
func (d *pdfDocument) embedFont(f *pdfFont) (int, error) {
	bounds, err := f.font.Bounds(&f.buf, fixed.I(pdfEm), font.HintingNone)
	if err != nil {
		return 0, err
	}
	metrics, err := f.font.Metrics(&f.buf, fixed.I(pdfEm), font.HintingNone)
	if err != nil {
		return 0, err
	}
	program := d.stream(fmt.Sprintf("/Length1 %d", len(goregular.TTF)), goregular.TTF)
	descriptor := d.add(fmt.Sprintf("<< /Type /FontDescriptor /FontName /Go-Regular /Flags 4 /FontBBox [%d %d %d %d] /Ascent %d /Descent %d /CapHeight %d /ItalicAngle 0 /StemV 80 /FontFile2 %d 0 R >>",
		bounds.Min.X.Floor(), -bounds.Max.Y.Ceil(), bounds.Max.X.Ceil(), -bounds.Min.Y.Floor(), metrics.Ascent.Round(), -metrics.Descent.Round(), metrics.CapHeight.Round(), program))
	var widths strings.Builder
	glyphs := make([]byte, 2*(len(f.order)+1))
	for _, r := range f.order {
		ch := f.chars[r]
		binary.BigEndian.PutUint16(glyphs[2*int(ch.cid):], uint16(ch.glyph))
		fmt.Fprintf(&widths, "%d ", ch.width)
	}
	glyphMap := d.stream("", glyphs)
	unicodeMap := d.stream("", unicodeCMap(f.order))
	cid := d.add(fmt.Sprintf("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Go-Regular /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor %d 0 R /CIDToGIDMap %d 0 R /W [1 [%s]] >>",
		descriptor, glyphMap, widths.String()))
	return d.add(fmt.Sprintf("<< /Type /Font /Subtype /Type0 /BaseFont /Go-Regular /Encoding /Identity-H /DescendantFonts [%d 0 R] /ToUnicode %d 0 R >>", cid, unicodeMap)), nil
}

func unicodeCMap(chars []rune) []byte {
	var cmap bytes.Buffer
	cmap.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n")
	cmap.WriteString("/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n")
	cmap.WriteString("/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	for i := 0; i < len(chars); i += 100 {
		end := min(i+100, len(chars))
		fmt.Fprintf(&cmap, "%d beginbfchar\n", end-i)
		for j, r := range chars[i:end] {
			fmt.Fprintf(&cmap, "<%04X> <", i+j+1)
			for _, unit := range utf16.Encode([]rune{r}) {
				fmt.Fprintf(&cmap, "%04X", unit)
			}
			cmap.WriteString(">\n")
		}
		cmap.WriteString("endbfchar\n")
	}
	cmap.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
	return cmap.Bytes()
}
