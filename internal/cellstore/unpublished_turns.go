package cellstore

import (
	"bufio"
	"encoding/json"
	"io"
	"os"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// unpublishedConversationTurns counts exchanges rather than intermediate saves or tool records.
func unpublishedConversationTurns(c cell.Cell, span Range) (uint32, error) {
	if span.End <= span.Start {
		return 0, nil
	}
	file, err := os.Open(rel(c, cell.TranscriptPath))
	if err != nil {
		return 0, err
	}
	defer file.Close()
	scan := bufio.NewScanner(io.NewSectionReader(file, span.Start, span.End-span.Start))
	scan.Buffer(make([]byte, 4096), 8<<20)
	var questions, answers uint32
	leadingReply := false
	for scan.Scan() {
		var record struct {
			Type      string            `json:"type"`
			Role      string            `json:"role"`
			Content   string            `json:"content"`
			ToolCalls []json.RawMessage `json:"toolCalls"`
		}
		if err := json.Unmarshal(scan.Bytes(), &record); err != nil {
			return 0, err
		}
		if record.Type != "message" {
			continue
		}
		switch record.Role {
		case "user":
			questions++
		case "assistant":
			leadingReply = leadingReply || questions == 0
			if len(record.ToolCalls) == 0 && record.Content != "" {
				answers++
			}
		case "tool":
			leadingReply = leadingReply || questions == 0
		}
	}
	if questions == 0 {
		// Older reply-only journals still describe their exchanges by their answers.
		return answers, scan.Err()
	}
	if leadingReply {
		// A reply whose question was already shared still has one unpublished tail.
		questions++
	}
	return questions, scan.Err()
}
