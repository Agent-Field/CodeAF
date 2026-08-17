package connect

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// How many messages a search answers with. defaultMessages is what a caller
// asking for nothing gets; maxMessages stops a caller who asked for a thousand
// from waiting for a thousand.
const (
	defaultMessages = 10
	maxMessages     = 25
	// searchWorkers is how many message summaries are fetched at once. The
	// list only gives identifiers, so a summary is one request per message,
	// and doing them strictly in turn makes a ten-hit search feel like a
	// stall. A handful at a time is quick without looking like a flood.
	searchWorkers = 5
)

// message is the part of a mail service's answer this package reads.
type message struct {
	ID           string `json:"id"`
	Snippet      string `json:"snippet"`
	InternalDate string `json:"internalDate"`
	Payload      part   `json:"payload"`
}

// part is one piece of a message: a set of headers, possibly some body, and
// possibly more parts underneath.
type part struct {
	MimeType string   `json:"mimeType"`
	Filename string   `json:"filename"`
	Headers  []header `json:"headers"`
	Body     struct {
		Data string `json:"data"`
	} `json:"body"`
	Parts []part `json:"parts"`
}

type header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// find returns the named header's value, or nothing when the message did not
// carry one.
func (p part) find(name string) string {
	for _, h := range p.Headers {
		if strings.EqualFold(h.Name, name) {
			return collapse(h.Value)
		}
	}
	return ""
}

// GmailSearch answers a mailbox query with a list of matching messages, one
// block each: the identifier to read it with, when it arrived, who sent it, its
// subject and its opening line.
//
// query is the service's own search language, passed through untouched — "from:
// alice is:unread", "has:attachment newer_than:7d" — because a model that knows
// that language should not have it taken away, and one that does not can still
// pass plain words.
func GmailSearch(ctx context.Context, client *http.Client, query string, max int) (string, error) {
	max = clampMessages(max)
	parameters := url.Values{}
	parameters.Set("maxResults", strconv.Itoa(max))
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		parameters.Set("q", trimmed)
	}

	var listing struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	address := gmailBaseURL + "/users/me/messages?" + parameters.Encode()
	if err := getJSON(ctx, client, address, &listing); err != nil {
		return "", fmt.Errorf("search mail: %w", err)
	}
	if len(listing.Messages) == 0 {
		return noMatches(query), nil
	}

	summaries := make([]*message, len(listing.Messages))
	errs := make([]error, len(listing.Messages))
	work := make(chan int)
	var group sync.WaitGroup
	for worker := 0; worker < searchWorkers && worker < len(listing.Messages); worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range work {
				summaries[index], errs[index] = summarize(ctx, client, listing.Messages[index].ID)
			}
		}()
	}
	for index := range listing.Messages {
		work <- index
	}
	close(work)
	group.Wait()

	var builder strings.Builder
	shown := 0
	for index, summary := range summaries {
		if summary == nil {
			// One summary that could not be fetched costs its own block
			// and nothing else. Failing the whole search over it would
			// throw away the hits that did arrive.
			_ = errs[index]
			continue
		}
		if shown > 0 {
			builder.WriteString("\n")
		}
		writeSummary(&builder, summary)
		shown++
	}
	if shown == 0 {
		return "", fmt.Errorf("search mail: %d messages matched but none could be read: %w", len(listing.Messages), firstError(errs))
	}
	return bound(matchLine(shown, query) + "\n\n" + builder.String()), nil
}

// GmailRead answers with one whole message: the headers worth reading and the
// body as plain sentences.
func GmailRead(ctx context.Context, client *http.Client, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("read mail: no message named")
	}
	address := gmailBaseURL + "/users/me/messages/" + url.PathEscape(id) + "?format=full"
	var full message
	if err := getJSON(ctx, client, address, &full); err != nil {
		return "", fmt.Errorf("read mail: %w", err)
	}

	var builder strings.Builder
	// THE EMPTINESS LAW: a header the message does not carry is not printed
	// as an empty label. Only what is there is shown.
	for _, name := range []string{"From", "To", "Cc", "Subject"} {
		if value := full.Payload.find(name); value != "" {
			fmt.Fprintf(&builder, "%s: %s\n", name, value)
		}
	}
	if when := received(&full); when != "" {
		fmt.Fprintf(&builder, "Date: %s\n", when)
	}
	body := readableBody(full.Payload)
	if body == "" {
		body = collapse(full.Snippet)
	}
	if body != "" {
		builder.WriteString("\n")
		builder.WriteString(body)
		builder.WriteString("\n")
	}
	return bound(strings.TrimRight(builder.String(), "\n")), nil
}

// GmailSend writes one message and sends it, and answers with what left: the
// subject, who it went to, and the identifier it now has in the mailbox — the
// same "id …" a search prints, so the sent message can be opened straight back.
//
// to and cc are comma-separated addresses; cc may be empty. A message needs
// somebody to go to and something to say, and nothing else here is required.
//
// THE MESSAGE IS BUILT ONE HEADER PER LINE AND EVERY VALUE IS FOLDED FLAT.
// A subject or an address carrying a line break would end that header and start
// one of the caller's own — a blind copy nobody asked for, a reply-to somewhere
// else — so every value that goes onto a header line is collapsed to a single
// line before it does, and the body starts only after the one blank line that
// separates it.
func GmailSend(ctx context.Context, client *http.Client, to, cc, subject, body string) (string, error) {
	recipients := addresses(to)
	if len(recipients) == 0 {
		return "", fmt.Errorf("send mail: nobody to send it to")
	}
	copies := addresses(cc)
	subject = collapse(subject)
	body = strings.TrimRight(squeeze(body), "\n")
	if subject == "" && body == "" {
		return "", fmt.Errorf("send mail: nothing to say")
	}

	var message strings.Builder
	writeHeader(&message, "To", strings.Join(recipients, ", "))
	if len(copies) > 0 {
		writeHeader(&message, "Cc", strings.Join(copies, ", "))
	}
	// The subject is encoded only when it needs to be, which is what the
	// encoder does with a line that is already plain ASCII.
	writeHeader(&message, "Subject", mime.QEncoding.Encode("UTF-8", subject))
	writeHeader(&message, "MIME-Version", "1.0")
	writeHeader(&message, "Content-Type", `text/plain; charset="UTF-8"`)
	message.WriteString("\r\n")
	message.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))

	sent := struct {
		ID string `json:"id"`
	}{}
	payload := map[string]string{"raw": base64.RawURLEncoding.EncodeToString([]byte(message.String()))}
	if err := postJSON(ctx, client, gmailBaseURL+"/users/me/messages/send", payload, &sent); err != nil {
		return "", fmt.Errorf("send mail: %w", err)
	}

	line := "Sent to " + strings.Join(recipients, ", ")
	if len(copies) > 0 {
		line += ", copying " + strings.Join(copies, ", ")
	}
	if subject != "" {
		line += ": " + clip(subject, 120)
	}
	line += "."
	if id := strings.TrimSpace(sent.ID); id != "" {
		line += "\nid " + id
	}
	return line, nil
}

// writeHeader puts one header on its own line, flattened. THE EMPTINESS LAW
// holds here too: a header with nothing to say is left out rather than sent as
// a bare label.
func writeHeader(message *strings.Builder, name, value string) {
	value = collapse(value)
	if value == "" {
		return
	}
	message.WriteString(name)
	message.WriteString(": ")
	message.WriteString(value)
	message.WriteString("\r\n")
}

// addresses reads a comma-separated list into the addresses in it, dropping the
// blanks a trailing comma or a double one leaves behind.
func addresses(list string) []string {
	var out []string
	for _, field := range strings.Split(list, ",") {
		if address := collapse(field); address != "" {
			out = append(out, address)
		}
	}
	return out
}

// summarize fetches only the fields a listing shows, which is a much smaller
// answer than the whole message and the difference between a fast search and a
// slow one.
func summarize(ctx context.Context, client *http.Client, id string) (*message, error) {
	parameters := url.Values{}
	parameters.Set("format", "metadata")
	parameters["metadataHeaders"] = []string{"From", "Subject", "Date"}
	address := gmailBaseURL + "/users/me/messages/" + url.PathEscape(id) + "?" + parameters.Encode()
	var summary message
	if err := getJSON(ctx, client, address, &summary); err != nil {
		return nil, err
	}
	if summary.ID == "" {
		summary.ID = id
	}
	return &summary, nil
}

// writeSummary renders one hit: an identifier line that carries everything a
// person or a model scans by, then the subject, then the opening line.
func writeSummary(builder *strings.Builder, summary *message) {
	fields := []string{"id " + summary.ID}
	if when := received(summary); when != "" {
		fields = append(fields, when)
	}
	if from := summary.Payload.find("From"); from != "" {
		fields = append(fields, from)
	}
	builder.WriteString(strings.Join(fields, " | "))
	builder.WriteString("\n")
	if subject := summary.Payload.find("Subject"); subject != "" {
		builder.WriteString(subject)
		builder.WriteString("\n")
	}
	if snippet := collapse(summary.Snippet); snippet != "" {
		builder.WriteString(clip(snippet, 200))
		builder.WriteString("\n")
	}
}

// received is when the message arrived, as a plain local timestamp. The
// service's own arrival stamp is preferred over the Date header, which is
// written by the sender and is wrong more often than anyone expects.
func received(m *message) string {
	if raw := strings.TrimSpace(m.InternalDate); raw != "" {
		if milliseconds, err := strconv.ParseInt(raw, 10, 64); err == nil && milliseconds > 0 {
			return time.UnixMilli(milliseconds).Local().Format("2006-01-02 15:04")
		}
	}
	return m.Payload.find("Date")
}

// readableBody walks the message for something a person can read, preferring
// plain text over markup and ignoring anything that arrived as an attachment.
func readableBody(root part) string {
	if text := findPart(root, "text/plain"); text != "" {
		return squeeze(text)
	}
	if markup := findPart(root, "text/html"); markup != "" {
		return htmlToText(markup)
	}
	return ""
}

// findPart is a depth-first search for the first body of the wanted type.
func findPart(p part, want string) string {
	if strings.EqualFold(p.MimeType, want) && strings.TrimSpace(p.Filename) == "" {
		if decoded := decodeBase64(p.Body.Data); decoded != "" {
			return decoded
		}
	}
	for _, child := range p.Parts {
		if found := findPart(child, want); found != "" {
			return found
		}
	}
	return ""
}

// matchLine is the one-line summary above a list of hits.
func matchLine(count int, query string) string {
	noun := "messages"
	if count == 1 {
		noun = "message"
	}
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		return fmt.Sprintf("%d %s match %q.", count, noun, trimmed)
	}
	return fmt.Sprintf("%d most recent %s.", count, noun)
}

// noMatches is what an empty mailbox search says. It is a full sentence rather
// than an empty answer, because "nothing matched" is itself the finding.
func noMatches(query string) string {
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		return fmt.Sprintf("No messages match %q.", trimmed)
	}
	return "No messages."
}

func clampMessages(max int) int {
	switch {
	case max <= 0:
		return defaultMessages
	case max > maxMessages:
		return maxMessages
	}
	return max
}

// firstError picks a representative failure out of a batch, for the case where
// every one of them failed.
func firstError(errs []error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
