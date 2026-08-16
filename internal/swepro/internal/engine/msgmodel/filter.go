package msgmodel

// FilterCompacted is message-v2.ts:1100-1147 — the newest-first walk over a
// session's messages that stops at the last completed compaction, then (when
// the compaction names a `tail_start_id` that sits BEFORE it) rotates the
// summary block in front of the retained tail.
//
// The TS takes an Iterable produced by `stream()`, which yields NEWEST first;
// `result.reverse()` at the end restores chronological order. This port keeps
// that orientation: `msgs` must arrive newest-first.
//
// ENGINE-DESIGN §2.5 site 5 mutates the returned parts in place, so the caller
// needs parts it owns. TS gets that for free because `stream()` rebuilds from
// the store on every loop iteration; this function does NOT deep-copy, so the
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
				// `!part.tail_start_id` is falsy on "" too.
				break
			}
			retain = part.TailStartID
			if msg.Info.MessageID() == *retain {
				break
			}
			continue
		}
		// :1118-1119 is dead: the guard above always `continue`s or `break`s
		// for a user message with completed[id]. Kept for fidelity.
		if user, ok := msg.Info.(User); ok && completed[user.ID] && hasCompaction(msg.Parts) {
			break
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
		if boolValue(assistant.Summary) && assistant.ParentID == compaction.Info.MessageID() {
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

func hasCompaction(parts Parts) bool { return findCompactionPart(parts) != nil }

// findCompactionWithTail is the `item.tail_start_id !== undefined` predicate
// (:1136, :1141) — note this one accepts an EMPTY-STRING tail id, unlike the
// falsy `!part.tail_start_id` test in the walk above.
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
