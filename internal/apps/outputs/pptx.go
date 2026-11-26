package outputs

import (
	"fmt"
	"io"
	"strings"
)

const (
	pptType  = "application/vnd.openxmlformats-officedocument.presentationml."
	group    = `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>`
	colorMap = `bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"`
)

func presentationXML(tag, body string) string {
	return fmt.Sprintf(`<p:%s xmlns:p="%s" xmlns:a="%s" xmlns:r="%s">%s</p:%s>`, tag, presNS, drawNS, officeRels, body, tag)
}

func writePPTX(w io.Writer, title string, sections []string, slideTitles bool) error {
	const main = "ppt/presentation.xml"
	const master = "ppt/slideMasters/slideMaster1.xml"
	const layout = "ppt/slideLayouts/slideLayout1.xml"
	const theme = "ppt/theme/theme1.xml"
	tree := "<p:cSld><p:spTree>" + group + "</p:spTree></p:cSld>"
	parts := []officePart{
		{master, pptType + "slideMaster+xml", presentationXML("sldMaster", tree+
			"<p:clrMap "+colorMap+`/><p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst>`)},
		relationships("ppt/slideMasters/_rels/slideMaster1.xml.rels", relationship{"slideLayout", "../slideLayouts/slideLayout1.xml"}, relationship{"theme", "../theme/theme1.xml"}),
		{layout, pptType + "slideLayout+xml", presentationXML("sldLayout", tree)},
		relationships("ppt/slideLayouts/_rels/slideLayout1.xml.rels", relationship{"slideMaster", "../slideMasters/slideMaster1.xml"}),
		{theme, "application/vnd.openxmlformats-officedocument.theme+xml", slideTheme()},
	}

	var ids strings.Builder
	ids.WriteString(`<p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst>`)
	links := []relationship{{"slideMaster", "slideMasters/slideMaster1.xml"}}
	if len(sections) == 0 {
		sections = []string{""}
	}
	for _, section := range sections {
		heading := title
		if slideTitles {
			heading, section, _ = strings.Cut(section, "\n")
		}
		for _, lines := range slidePages(section) {
			n := len(links)
			if n > 2000 {
				return fmt.Errorf("output needs more than 2000 slides")
			}
			file := fmt.Sprintf("slides/slide%d.xml", n)
			fmt.Fprintf(&ids, `<p:sldId id="%d" r:id="rId%d"/>`, 255+n, n+1)
			links = append(links, relationship{"slide", file})
			shapes := slideText(2, "Title", 480000, 720000, 3200, []string{heading}) +
				slideText(3, "Body", 1320000, 4790000, 2000, lines)
			parts = append(parts, officePart{"ppt/" + file, pptType + "slide+xml",
				presentationXML("sld", "<p:cSld><p:spTree>"+group+shapes+"</p:spTree></p:cSld>")},
				relationships(fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", n), relationship{"slideLayout", "../slideLayouts/slideLayout1.xml"}))
		}
	}
	ids.WriteString(`</p:sldIdLst><p:sldSz cx="12192000" cy="6858000"/><p:notesSz cx="6858000" cy="9144000"/>`)
	parts = append(parts, officePart{main, pptType + "presentation.main+xml", presentationXML("presentation", ids.String())},
		relationships("ppt/_rels/presentation.xml.rels", links...))
	return writeOffice(w, main, parts)
}

func slidePages(section string) [][]string {
	const charsPerLine = 64
	const linesPerSlide = 12
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(section, "\t", "    "), "\n") {
		chars := []rune(line)
		for len(chars) > charsPerLine {
			end := charsPerLine
			// Break at the last space when possible without losing it.
			for i := end - 1; i > 0; i-- {
				if chars[i] == ' ' {
					end = i + 1
					break
				}
			}
			lines = append(lines, string(chars[:end]))
			chars = chars[end:]
		}
		lines = append(lines, string(chars))
	}
	var pages [][]string
	for len(lines) > 0 {
		n := min(len(lines), linesPerSlide)
		pages = append(pages, lines[:n])
		lines = lines[n:]
	}
	return pages
}

func slideText(id int, name string, y, height, size int, lines []string) string {
	var body strings.Builder
	fmt.Fprintf(&body, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="650000" y="%d"/><a:ext cx="10892000" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr><p:txBody><a:bodyPr><a:normAutofit/></a:bodyPr><a:lstStyle/>`, id, name, y, height)
	for _, line := range lines {
		fmt.Fprintf(&body, `<a:p><a:r><a:rPr lang="en" sz="%d"><a:solidFill><a:srgbClr val="202020"/></a:solidFill></a:rPr><a:t>%s</a:t></a:r></a:p>`, size, xmlText(line))
	}
	body.WriteString("</p:txBody></p:sp>")
	return body.String()
}

func slideTheme() string {
	var theme strings.Builder
	fmt.Fprintf(&theme, `<a:theme xmlns:a="%s" name="App output"><a:themeElements><a:clrScheme name="App output">`, drawNS)
	for _, color := range []struct{ key, rgb string }{
		{"dk1", "202020"}, {"lt1", "FFFFFF"}, {"dk2", "363636"}, {"lt2", "F2F2F2"},
		{"accent1", "345B85"}, {"accent2", "47765C"}, {"accent3", "8A6040"}, {"accent4", "74618D"},
		{"accent5", "397480"}, {"accent6", "944E55"}, {"hlink", "185ABD"}, {"folHlink", "753E81"},
	} {
		fmt.Fprintf(&theme, `<a:%s><a:srgbClr val="%s"/></a:%s>`, color.key, color.rgb, color.key)
	}
	theme.WriteString(`</a:clrScheme><a:fontScheme name="Arial">`)
	for _, font := range []string{"majorFont", "minorFont"} {
		fmt.Fprintf(&theme, `<a:%s><a:latin typeface="Arial"/><a:ea typeface=""/><a:cs typeface=""/></a:%s>`, font, font)
	}
	const fill = `<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>`
	theme.WriteString(`</a:fontScheme><a:fmtScheme name="App output"><a:fillStyleLst>` + strings.Repeat(fill, 3) + `</a:fillStyleLst><a:lnStyleLst>`)
	for _, width := range []int{6350, 12700, 19050} {
		fmt.Fprintf(&theme, `<a:ln w="%d">%s</a:ln>`, width, fill)
	}
	theme.WriteString(`</a:lnStyleLst><a:effectStyleLst>` + strings.Repeat("<a:effectStyle><a:effectLst/></a:effectStyle>", 3) +
		`</a:effectStyleLst><a:bgFillStyleLst>` + strings.Repeat(fill, 3) + `</a:bgFillStyleLst></a:fmtScheme></a:themeElements></a:theme>`)
	return theme.String()
}
