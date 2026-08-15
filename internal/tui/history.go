package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const historyLimit = 8

type historyEntry struct {
	hit    store.RecallHit
	status store.Status
}

type historyRow struct {
	line  int
	index int
}

type historyResultMsg struct {
	generation int
	terms      string
	entries    []historyEntry
	err        error
}

func (m *Model) openRecallHistory(terms string) tea.Cmd {
	terms = strings.TrimSpace(terms)
	m.historyGeneration++
	generation := m.historyGeneration
	m.historyTerms = terms
	m.historyVisible = true
	m.historyLoading = true
	m.historyErr = nil
	m.historyEntries = nil
	m.historySelection = 0
	m.historyOpen = -1
	// Recall reads the thread, so it takes the composer's focus and its words.
	// Escape back to the composer is where the draft is waiting.
	m.borrowDraft()
	m.palette = paletteNone
	m.paletteSelected = 0
	m.focus = focusChat
	m.inputFocused = false
	m.input.Blur()
	m.autoScroll = true
	m.setSize(m.width, m.height)

	backend := m.backend
	return func() tea.Msg {
		var hits []store.RecallHit
		var err error
		if terms == "" {
			var snapshot store.Snapshot
			snapshot, err = backend.Snapshot()
			if err == nil {
				hits = recentSettledHistory(snapshot, historyLimit)
			}
		} else {
			hits, err = backend.Recall(terms, nil, historyLimit)
		}
		entries := make([]historyEntry, 0, min(historyLimit, len(hits)))
		for _, hit := range hits {
			entry := historyEntry{hit: hit}
			if node, found, nodeErr := backend.Node(hit.NodeID); nodeErr == nil && found {
				entry.status = node.Status
			}
			entries = append(entries, entry)
			if len(entries) == historyLimit {
				break
			}
		}
		return historyResultMsg{generation: generation, terms: terms, entries: entries, err: err}
	}
}

func (m *Model) applyHistoryResult(result historyResultMsg) {
	if result.generation != m.historyGeneration {
		return
	}
	m.historyLoading = false
	m.historyErr = result.err
	m.historyEntries = append([]historyEntry(nil), result.entries...)
	m.historySelection = max(0, min(len(m.historyEntries)-1, m.historySelection))
	m.historyOpen = -1
	m.setSize(m.width, m.height)
	m.focusHistorySelection()
	m.refreshChat()
	m.chat.GotoBottom()
	m.autoScroll = true
}

func recentSettledHistory(snapshot store.Snapshot, limit int) []store.RecallHit {
	type candidate struct {
		hit        store.RecallHit
		status     store.Status
		updatedSeq int64
		at         time.Time
	}
	candidates := make([]candidate, 0)
	for _, node := range snapshot.Nodes {
		if node.ID == store.RootID || !nodeSettled(node) ||
			node.Group == store.TerritoryGroup || node.Group == charterGroupMarker ||
			(node.Parent != store.RootID && !node.FoldRoot) {
			continue
		}
		intent := strings.TrimSpace(node.Provenance.Intent)
		if intent == "" {
			intent = strings.TrimSpace(node.Brief)
		}
		digest := strings.TrimSpace(node.FoldDigest)
		if digest == "" {
			digest = strings.TrimSpace(node.Summary)
		}
		if digest == "" {
			digest = strings.TrimSpace(node.Error)
		}
		at := node.FinishedAt
		if at.IsZero() {
			at = node.StartedAt
		}
		candidates = append(candidates, candidate{
			hit: store.RecallHit{
				NodeID: node.ID, Intent: intent, Digest: digest,
				Pointers: append([]string(nil), node.FoldPointers...), Age: store.AgeLabel(at, time.Now().UTC()),
			},
			status: node.Status, updatedSeq: node.UpdatedSeq, at: at,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].updatedSeq != candidates[j].updatedSeq {
			return candidates[i].updatedSeq > candidates[j].updatedSeq
		}
		if !candidates[i].at.Equal(candidates[j].at) {
			return candidates[i].at.After(candidates[j].at)
		}
		return candidates[i].hit.NodeID < candidates[j].hit.NodeID
	})
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	hits := make([]store.RecallHit, len(candidates))
	for index, candidate := range candidates {
		hits[index] = candidate.hit
	}
	return hits
}

func (m *Model) renderRecallHistory(width, atLine int, track bool) string {
	width = max(12, width)
	label := "history"
	if m.historyTerms != "" {
		label += " · “" + m.historyTerms + "”"
	}
	lines := []string{mutedStyle.Faint(true).Render(truncate(label, width))}
	if m.historyLoading {
		return strings.Join(append(lines, mutedStyle.Render("searching permanent graph memory…")), "\n")
	}
	if m.historyErr != nil {
		return strings.Join(append(lines, roseStyle.Render(
			truncate(m.historyErr.Error(), width))), "\n")
	}
	if len(m.historyEntries) == 0 {
		return strings.Join(append(lines, mutedStyle.Render("no settled work found")), "\n")
	}

	for index, entry := range m.historyEntries {
		disclosure := "▸"
		if m.historyOpen == index {
			disclosure = "▾"
		}
		age := strings.TrimSpace(entry.hit.Age)
		if age == "" {
			age = "—"
		}
		prefix := mutedStyle.Render(disclosure + " " + string(rune('1'+index)) + "  " + padANSI(age, 10) + "  ")
		glyph := historyStatusGlyph(entry.status)
		intentWidth := max(1, width-lipgloss.Width(prefix)-lipgloss.Width(glyph)-1)
		row := prefix + inputTextStyle.Render(truncate(firstLine(entry.hit.Intent), intentWidth)) + " " + glyph
		lines = append(lines, truncate(row, width))
		if track {
			m.historyRows = append(m.historyRows, historyRow{line: atLine + len(lines) - 1, index: index})
		}
		if m.historyOpen != index {
			continue
		}
		digest := strings.TrimSpace(entry.hit.Digest)
		if digest == "" {
			digest = "No digest was recorded."
		}
		for _, line := range strings.Split(wrapText(digest, max(1, width-4)), "\n") {
			lines = append(lines, mutedStyle.Faint(true).Render("    ")+mutedStyle.Render(line))
		}
		if workspace := m.workspaceDirectoryLink(entry.hit.NodeID); workspace != "" {
			lines = append(lines, mutedStyle.Faint(true).Render("    ")+workspace)
		}
	}
	return strings.Join(lines, "\n")
}

func historyStatusGlyph(status store.Status) string {
	switch status {
	case store.Done:
		return mintStyle.Render("✓")
	case store.Failed, store.Cancelled:
		return roseStyle.Render("✗")
	case store.Running, store.Claimed:
		return peachStyle.Render("◐")
	case store.Pending:
		return mutedStyle.Render("○")
	default:
		return mutedStyle.Render("·")
	}
}

func (m *Model) activateHistory(index int) bool {
	if index < 0 || index >= len(m.historyEntries) {
		return false
	}
	m.focus = focusChat
	m.inputFocused = false
	m.input.Blur()
	m.historySelection = index
	m.focusHistorySelection()
	if m.historyOpen == index {
		m.historyOpen = -1
	} else {
		m.historyOpen = index
	}
	offset := m.chat.YOffset
	m.refreshChat()
	m.chat.SetYOffset(offset)
	return true
}

func (m *Model) focusHistorySelection() {
	if m.historySelection < 0 || m.historySelection >= len(m.historyEntries) {
		return
	}
	targetLine := -1
	for _, row := range m.historyRows {
		if row.index == m.historySelection {
			targetLine = row.line
			break
		}
	}
	if targetLine < 0 {
		return
	}
	for index, line := range m.chatFocusLines() {
		if line == targetLine {
			m.chatFocusIndex = index
			return
		}
	}
}

func (m *Model) syncHistorySelection(line int) {
	for _, row := range m.historyRows {
		if row.line == line {
			m.historySelection = row.index
			return
		}
	}
}
