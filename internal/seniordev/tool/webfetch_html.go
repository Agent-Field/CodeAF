//go:build !windows

package tool

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var webHTMLWhitespace = regexp.MustCompile(`\s+`)
var (
	webMarkdownLeadingEquals = regexp.MustCompile(`^(=+)`)
	webMarkdownLeadingHash   = regexp.MustCompile(`^(#{1,6}) `)
	webMarkdownLeadingNumber = regexp.MustCompile(`^(\d+)\. `)
)

func extractWebHTMLText(source string) (string, error) {
	document, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return "", err
	}
	var output strings.Builder
	var walk func(*html.Node, bool)
	walk = func(node *html.Node, skip bool) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "script", "style", "noscript", "iframe", "object", "embed":
				skip = true
			}
		}
		if node.Type == html.TextNode && !skip {
			output.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, skip)
		}
	}
	walk(document, false)
	// Text chunks are concatenated with only a final trim; no separators are
	// invented between elements.
	return strings.TrimSpace(output.String()), nil
}

func convertWebHTMLToMarkdown(source string) (string, error) {
	document, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return "", err
	}
	markdown := renderWebMarkdown(document, markdownRenderState{})
	markdown = strings.ReplaceAll(markdown, "\u00a0", " ")
	markdown = regexp.MustCompile(`[ \t]+\n`).ReplaceAllString(markdown, "\n")
	markdown = regexp.MustCompile(`\n{3,}`).ReplaceAllString(markdown, "\n\n")
	return strings.TrimSpace(markdown), nil
}

type markdownRenderState struct {
	pre       bool
	code      bool
	listDepth int
}

func renderWebMarkdown(node *html.Node, state markdownRenderState) string {
	if node.Type == html.TextNode {
		if state.pre {
			return node.Data
		}
		text := webHTMLWhitespace.ReplaceAllString(node.Data, " ")
		if state.code {
			return text
		}
		return escapeWebMarkdownText(text)
	}
	if node.Type != html.ElementNode && node.Type != html.DocumentNode {
		return ""
	}
	if node.Type == html.ElementNode {
		switch node.Data {
		case "script", "style", "meta", "link":
			// These four elements are dropped outright (rather than all head
			// content).
			return ""
		}
	}

	childState := state
	if node.Type == html.ElementNode && node.Data == "pre" {
		childState.pre = true
	}
	if node.Type == html.ElementNode && node.Data == "code" {
		childState.code = true
	}
	if node.Type == html.ElementNode && (node.Data == "ul" || node.Data == "ol") {
		childState.listDepth++
	}
	var content strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		content.WriteString(renderWebMarkdown(child, childState))
	}
	inner := content.String()
	if node.Type != html.ElementNode {
		return inner
	}

	switch node.Data {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level, _ := strconv.Atoi(node.Data[1:])
		return "\n\n" + strings.Repeat("#", level) + " " + strings.TrimSpace(inner) + "\n\n"
	case "p", "div", "section", "article", "header", "footer", "main", "aside", "nav", "figure", "figcaption":
		if strings.TrimSpace(inner) == "" {
			return ""
		}
		return "\n\n" + strings.TrimSpace(inner) + "\n\n"
	case "br":
		return "  \n"
	case "hr":
		return "\n\n---\n\n"
	case "strong", "b":
		return "**" + strings.TrimSpace(inner) + "**"
	case "em", "i":
		return "*" + strings.TrimSpace(inner) + "*"
	case "del", "s", "strike":
		return "~~" + strings.TrimSpace(inner) + "~~"
	case "code":
		if state.pre {
			return inner
		}
		return "`" + strings.TrimSpace(inner) + "`"
	case "pre":
		return "\n\n```\n" + strings.Trim(inner, "\n") + "\n```\n\n"
	case "a":
		href := webHTMLAttribute(node, "href")
		if href == "" {
			return inner
		}
		title := webHTMLAttribute(node, "title")
		if title != "" {
			href += ` "` + title + `"`
		}
		return "[" + strings.TrimSpace(inner) + "](" + href + ")"
	case "img":
		source := webHTMLAttribute(node, "src")
		if source == "" {
			return ""
		}
		title := webHTMLAttribute(node, "title")
		if title != "" {
			source += ` "` + title + `"`
		}
		return "![" + webHTMLAttribute(node, "alt") + "](" + source + ")"
	case "blockquote":
		value := strings.TrimSpace(inner)
		return "\n\n> " + strings.ReplaceAll(value, "\n", "\n> ") + "\n\n"
	case "ul", "ol":
		return "\n\n" + strings.Trim(inner, "\n") + "\n\n"
	case "li":
		prefix := "-   "
		if node.Parent != nil && node.Parent.Data == "ol" {
			index := 1
			for sibling := node.PrevSibling; sibling != nil; sibling = sibling.PrevSibling {
				if sibling.Type == html.ElementNode && sibling.Data == "li" {
					index++
				}
			}
			prefix = fmt.Sprintf("%d.  ", index)
		}
		indent := strings.Repeat("    ", max(0, state.listDepth-1))
		value := strings.TrimSpace(inner)
		value = strings.ReplaceAll(value, "\n", "\n"+indent+"    ")
		return "\n" + indent + prefix + value
	case "table", "thead", "tbody", "tfoot", "tr":
		return "\n" + strings.TrimSpace(inner) + "\n"
	case "th", "td":
		return strings.TrimSpace(inner) + "\t"
	default:
		return inner
	}
}

// escapeWebMarkdownText is Turndown 7.2.0's ordered escape list. Applying it
// only to ordinary text nodes keeps markup emitted by element rules intact.
func escapeWebMarkdownText(text string) string {
	text = strings.ReplaceAll(text, `\`, `\\`)
	text = strings.ReplaceAll(text, `*`, `\*`)
	if strings.HasPrefix(text, "-") {
		text = `\` + text
	}
	if strings.HasPrefix(text, "+ ") {
		text = `\` + text
	}
	text = webMarkdownLeadingEquals.ReplaceAllString(text, `\$1`)
	text = webMarkdownLeadingHash.ReplaceAllString(text, `\$1 `)
	text = strings.ReplaceAll(text, "`", "\\`")
	if strings.HasPrefix(text, "~~~") {
		text = `\` + text
	}
	text = strings.ReplaceAll(text, "[", `\[`)
	text = strings.ReplaceAll(text, "]", `\]`)
	if strings.HasPrefix(text, ">") {
		text = `\` + text
	}
	text = strings.ReplaceAll(text, "_", `\_`)
	return webMarkdownLeadingNumber.ReplaceAllString(text, `$1\. `)
}

func webHTMLAttribute(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}
