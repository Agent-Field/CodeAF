package connect

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const (
	// maxToolText is the ceiling on what one helper hands back. The reader
	// is a model with a finite context and a job to do; a mailbox dumped
	// whole would crowd out the reasoning it was fetched for. Twelve
	// kilobytes is roughly a long article — enough for a real answer, small
	// enough that three of them still fit beside a conversation.
	maxToolText = 12 * 1024
	// maxResponseBytes is how much of a service's answer is read before
	// giving up on it, so that a runaway response cannot exhaust memory.
	maxResponseBytes = 8 << 20
)

// getJSON performs one read against a service and decodes the answer.
//
// The client is the account-bearing one from [Manager.Client]; this function
// never sees a key, never writes one down and never puts one in an error.
func getJSON(ctx context.Context, client *http.Client, address string, out any) error {
	if client == nil {
		return errors.New("no connected account for this request")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("%s: %s", response.Status, apiMessage(body))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("unreadable answer from %s: %w", request.URL.Host, err)
	}
	return nil
}

// apiMessage digs the human sentence out of a service's refusal, falling back
// to a short piece of whatever it actually sent when there is no such sentence.
func apiMessage(body []byte) string {
	var wrapper struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &wrapper); err == nil {
		if message := strings.TrimSpace(wrapper.Error.Message); message != "" {
			return message
		}
	}
	return clip(collapse(string(body)), 200)
}

// bound caps a helper's answer at [maxToolText] and says so when it cuts.
//
// THE CUT IS ANNOUNCED, ALWAYS. A model handed a silently truncated mailbox
// will answer confidently about mail it was never shown; a model told that it
// is looking at the first part of something larger will narrow its search
// instead.
func bound(text string) string {
	if len(text) <= maxToolText {
		return text
	}
	cut := text[:maxToolText]
	// Prefer to end on a line boundary, but not at the cost of throwing away
	// most of what was asked for.
	if index := strings.LastIndexByte(cut, '\n'); index > maxToolText/2 {
		cut = cut[:index]
	}
	cut = strings.ToValidUTF8(cut, "")
	return strings.TrimRight(cut, "\n") +
		fmt.Sprintf("\n\n[Shortened here. This is the first %d of %d characters.]", len(cut), len(text))
}

// clip shortens one line to at most limit characters, marking the cut.
func clip(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return strings.TrimRight(string(runes[:limit]), " ") + "…"
}

// collapse folds every run of whitespace into one space and trims the ends —
// what a value pulled out of a header or a snippet needs before it is put on a
// line of its own.
func collapse(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// squeeze folds runs of blank lines into one and trims the ends, so a body that
// arrived with a page of padding does not spend the caller's budget on it.
func squeeze(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, line)
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}

// decodeBase64 decodes the URL-safe, sometimes unpadded encoding mail bodies
// arrive in. A part that will not decode is treated as no part at all rather
// than as an error: one unreadable attachment must not cost the reader the
// message it was attached to.
func decodeBase64(data string) string {
	data = strings.NewReplacer("-", "+", "_", "/", "\n", "", "\r", "", " ", "").Replace(data)
	if remainder := len(data) % 4; remainder != 0 {
		data += strings.Repeat("=", 4-remainder)
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return ""
	}
	return string(raw)
}

// htmlToText renders a markup-only message as the sentences it was trying to
// say: scripts and styles dropped, block boundaries turned into line breaks,
// entities unescaped.
func htmlToText(markup string) string {
	node, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return squeeze(collapse(markup))
	}
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			builder.WriteString(n.Data)
			return
		case html.ElementNode:
			switch n.Data {
			case "script", "style", "head":
				return
			case "br":
				builder.WriteString("\n")
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote":
				builder.WriteString("\n")
			}
		}
	}
	walk(node)
	// Every line is folded on its own so that markup indentation does not
	// survive as ragged leading space.
	lines := strings.Split(builder.String(), "\n")
	for i, line := range lines {
		lines[i] = collapse(line)
	}
	return squeeze(strings.Join(lines, "\n"))
}
