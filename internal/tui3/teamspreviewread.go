package tui3

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
)

// Read backwards to the latest human prompt, skipping record bodies while
// retaining bounded prefixes. A long answer must not turn a last-turn preview
// into its tail, and an unchanged journal pays only the existing stat check.
// This runs on the Teams reading command, never on the frame's goroutine.
func teamsReadExchange(f *os.File, size int64, preview *teamsPreview) error {
	var chunk [teamsPreviewBytes]byte
	end, recordEnd := size, size
	var reply teamsPreviewMessage
	reply.role = "assistant"
	var fallback []teamsPreviewMessage
	skip := 0
	readRecord := func(start, end int64) (bool, error) {
		if end <= start {
			return false, nil
		}
		n := end - start
		if n > teamsPreviewBytes {
			n = teamsPreviewBytes
		}
		data, err := io.ReadAll(io.NewSectionReader(f, start, n))
		if err != nil {
			return false, err
		}
		clipped := end-start > n
		if clipped {
			data, err = teamsPreviewRecordPrefix(data, io.NewSectionReader(f, start+n, end-start-n))
			if err != nil {
				return false, err
			}
		}
		var header struct {
			Type, Role string
			Dropped    int
		}
		if json.Unmarshal(data, &header) != nil {
			return false, nil
		}
		if header.Type == "rewind" {
			skip += max(header.Dropped, 0)
			return false, nil
		}
		if header.Type == "compaction" {
			return true, nil
		}
		if header.Type == "message" && header.Role != "" && skip > 0 {
			skip--
			return false, nil
		}
		messages := []teamsPreviewMessage{}
		for _, e := range session.ReadTranscriptBytes(data).Entries {
			messages = append(messages, teamsPreviewMessageOf(e)...)
		}
		for i := len(messages) - 1; i >= 0; i-- {
			m := messages[i]
			m.clipped = m.clipped || clipped
			if len(fallback) == 0 {
				fallback = teamsLatestExchange([]teamsPreviewMessage{m})
			}
			if m.role == "assistant" {
				if preview.text == "" {
					preview.text = strings.Join(strings.Fields(m.text), " ")
				}
				text := m.text
				if reply.text != "" {
					text += "\n\n" + reply.text
				}
				reply.text, reply.clipped = teamsPreviewPrefix(text, teamsPreviewBytes, reply.clipped || m.clipped)
				reply.interrupted = reply.interrupted || m.interrupted
			}
			if m.role == "user" || m.role == "correction" {
				for _, piece := range teamsLatestExchange([]teamsPreviewMessage{m, reply}) {
					preview.messages[preview.count] = piece
					preview.count++
				}
				return true, nil
			}
		}
		return false, nil
	}
	for end > 0 {
		start := max(end-int64(len(chunk)), 0)
		n, err := f.ReadAt(chunk[:end-start], start)
		if err != nil && err != io.EOF {
			return err
		}
		for i := n - 1; i >= 0; i-- {
			if chunk[i] != '\n' {
				continue
			}
			at := start + int64(i)
			found, err := readRecord(at+1, recordEnd)
			if err != nil {
				return err
			}
			if found {
				if preview.count == 0 {
					for _, m := range fallback {
						preview.messages[preview.count] = m
						preview.count++
					}
				}
				return nil
			}
			recordEnd = at
		}
		end = start
	}
	found, err := readRecord(0, recordEnd)
	if err != nil || found {
		return err
	}
	for _, m := range fallback {
		preview.messages[preview.count] = m
		preview.count++
	}
	return nil
}

// Oversized content is clipped while its following audience/origin metadata
// is preserved. Escapes are consumed as JSON bytes, so the clipped prefix
// cannot turn a session-authored note into a human prompt.
func teamsPreviewRecordPrefix(data []byte, rest io.Reader) ([]byte, error) {
	field := []byte(`"content":`)
	at := bytes.Index(data, field)
	if at < 0 {
		return nil, nil
	}
	start := at + len(field)
	for start < len(data) && (data[start] == ' ' || data[start] == '\t') {
		start++
	}
	if start >= len(data) || data[start] != '"' {
		return nil, nil
	}
	reader := bufio.NewReader(io.MultiReader(bytes.NewReader(data[start+1:]), rest))
	prefix := append([]byte(nil), data[:start+1]...)
	escaped, escapeStart, unicodeLeft := false, -1, 0
	clipped := false
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == '"' && !escaped && unicodeLeft == 0 {
			if escapeStart >= 0 && clipped {
				prefix = prefix[:escapeStart]
			}
			prefix = append(prefix, '"')
			break
		}
		if len(prefix) < teamsPreviewBytes && !clipped {
			prefix = append(prefix, b)
		} else {
			if !clipped && escapeStart >= 0 {
				prefix = prefix[:escapeStart]
				escapeStart = -1
			}
			clipped = true
		}
		if unicodeLeft > 0 {
			unicodeLeft--
			if unicodeLeft == 0 {
				escapeStart = -1
			}
		} else if escaped {
			escaped = false
			if b == 'u' {
				unicodeLeft = 4
			} else {
				escapeStart = -1
			}
		} else if b == '\\' {
			escaped = true
			if !clipped {
				escapeStart = len(prefix) - 1
			}
		}
		// A prefix ending inside an escape drops the entire unfinished escape.
		if clipped && escapeStart >= 0 {
			prefix = prefix[:escapeStart]
			escapeStart = -1
		}
	}
	metadata, err := io.ReadAll(io.LimitReader(reader, teamsPreviewBytes+1))
	if err != nil {
		return nil, err
	}
	if len(metadata) > teamsPreviewBytes {
		return nil, nil
	}
	prefix = append(prefix, metadata...)
	if !json.Valid(prefix) {
		return nil, nil
	}
	return prefix, nil
}
