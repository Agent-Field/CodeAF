//go:build !windows

package msgmodel

// FilterCompacted projects a session onto what the model should see: a
// newest-first walk over the messages that stops at the last completed
// compaction, then (when the compaction names a `tail_start_id` that sits
// BEFORE it) rotates the summary block in front of the retained tail.
//
// `msgs` must arrive newest-first; the result is chronological.
//
// The step loop mutates the returned parts in place (WrapLateUserText), so
// the caller needs parts it owns. This function does NOT deep-copy; the
// storage layer that feeds it must hand over fresh values.
func FilterCompacted(msgs []WithParts) []WithParts {
	result := []WithParts{}
	completed := map[string]bool{}
	var retain *string

	for _, msg := range msgs {
		result = append(result, msg)
		if retain != nil {
			if msg.Info.MessageID() == *retain {
				break
			}
			continue
		}
		if user, ok := msg.Info.(User); ok && completed[user.ID] {
			part := findCompactionPart(msg.Parts)
			if part == nil {
				continue
			}
			if part.TailStartID == nil || *part.TailStartID == "" {
				// An empty tail id counts as no tail.
				break
			}
			retain = part.TailStartID
			if msg.Info.MessageID() == *retain {
				break
			}
			continue
		}
		if assistant, ok := msg.Info.(Assistant); ok &&
			boolValue(assistant.Summary) &&
			assistant.Finish != nil && *assistant.Finish != "" &&
			assistant.Error == nil {
			completed[assistant.ParentID] = true
		}
	}

	reverseWithParts(result)

	compactionIndex := -1
	for i := len(result) - 1; i >= 0; i-- {
		if _, ok := result[i].Info.(User); !ok {
			continue
		}
		if findCompactionWithTail(result[i].Parts) != nil {
			compactionIndex = i
			break
		}
	}
	if compactionIndex < 0 {
		return result
	}
	compaction := result[compactionIndex]
	part := findCompactionWithTail(compaction.Parts)

	summaryIndex := -1
	for i, msg := range result {
		if i <= compactionIndex {
			continue
		}
		assistant, ok := msg.Info.(Assistant)
		if !ok {
			continue
		}
		// The same rule that marked the compaction completed above: an
		// errored summary attempt (a transport failure, a rejected draft) is
		// never the boundary. Without this check a failed first attempt
		// sitting before the accepted one would be picked here, and the tail
		// would be rotated in front of the real summary instead of after it.
		if boolValue(assistant.Summary) && assistant.Error == nil &&
			assistant.ParentID == compaction.Info.MessageID() {
			summaryIndex = i
			break
		}
	}

	tailIndex := -1
	if part != nil && part.TailStartID != nil && *part.TailStartID != "" {
		for i, msg := range result {
			if msg.Info.MessageID() == *part.TailStartID {
				tailIndex = i
				break
			}
		}
	}

	if tailIndex >= 0 && tailIndex < compactionIndex && summaryIndex > compactionIndex {
		out := make([]WithParts, 0, len(result))
		out = append(out, result[compactionIndex:summaryIndex+1]...)
		out = append(out, result[tailIndex:compactionIndex]...)
		out = append(out, result[summaryIndex+1:]...)
		return out
	}
	return result
}

func findCompactionPart(parts Parts) *CompactionPart {
	for _, raw := range parts {
		if part, ok := raw.(CompactionPart); ok {
			return &part
		}
	}
	return nil
}

// findCompactionWithTail finds a compaction part whose tail id is present at
// all; unlike the walk above it accepts an empty-string tail id.
func findCompactionWithTail(parts Parts) *CompactionPart {
	for _, raw := range parts {
		part, ok := raw.(CompactionPart)
		if ok && part.TailStartID != nil {
			return &part
		}
	}
	return nil
}

func reverseWithParts(s []WithParts) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
