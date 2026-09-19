package tui3

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Folder detail (Wave 2): standing instructions, software indexing, organizer
// why-here. Preview and hover still launch no AI. Filing from the organizer
// is quiet — the next beat draws the placement, and no approval card is
// raised. Snapshot, guidance and index stay on the home beat, never in View.

func (a *app) readFolderDetail(ctx context.Context, next *homeFoldersReading, open string) {
	if idx, err := a.folders.IndexProgress(ctx); err == nil {
		next.index = idx
	} else {
		next.index = a.home.folders.index
	}
	if open == "" {
		return
	}
	items, err := a.folders.FolderGuidance(ctx, open)
	if err != nil {
		if strings.TrimSpace(a.home.folders.open.ID) == open {
			next.guidance = a.home.folders.guidance
		}
		return
	}
	next.guidance = items
}

// folderIndexCopy is the folders heading clause. It is software counters and
// delayed/degraded labels. A catch-up state never draws 100% or claims the
// workspace was checked.
func folderIndexCopy(idx FolderIndex) string {
	if !folderIndexBusy(idx) {
		return ""
	}
	var parts []string
	if idx.Delayed {
		parts = append(parts, folderDelayedWord)
	}
	if idx.Degraded {
		parts = append(parts, folderDegradedWord)
	}
	if d := strings.TrimSpace(idx.Detail); d != "" && !folderIndexForbidden(d) {
		parts = append(parts, d)
	}
	if idx.Passages > 0 {
		parts = append(parts, itoa(idx.Passages)+" passages")
	}
	if idx.Vectors > 0 {
		parts = append(parts, itoa(idx.Vectors)+" vectors")
	}
	return strings.Join(parts, " · ")
}

func folderIndexBusy(idx FolderIndex) bool {
	if idx.Delayed || idx.Degraded {
		return true
	}
	if strings.TrimSpace(idx.Detail) != "" {
		return true
	}
	return idx.Passages > 0 && idx.Vectors < idx.Passages
}

func folderIndexForbidden(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(s, "100%") || strings.Contains(lower, "checked")
}

// folderInstructSub is the folder-detail instructions section, glued to `back`
// so an 80-col squeeze that keeps one content row still shows the heading.
func folderInstructLines(items []FolderInstruction) []homeLine {
	body := folderInstructBody(items)
	if body == "" {
		body = folderInstructWhisper
	}
	return []homeLine{
		{kind: homeSwitchHead, cell: &homeCell{kind: cellGroup, panel: panelFolders, title: folderInstructHead}},
		{kind: homeSwitchHead, cell: &homeCell{kind: cellWhisper, panel: panelFolders, title: body}},
	}
}

func folderInstructBody(items []FolderInstruction) string {
	var parts []string
	for _, item := range items {
		if text := strings.TrimSpace(item.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " · ")
}

func folderOrganizerWhy(why FolderWhy) string {
	parts := []string{folderOrganizerOrigin}
	for _, p := range []string{why.Reason, why.Evidence, why.Actor, why.At} {
		if s := strings.TrimSpace(p); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

func (a *app) instructThisFolder(line homeLine) tea.Cmd {
	return a.instructFolder(a.folderIDOf(line), strings.TrimSpace(a.home.box.String()), true)
}

func (a *app) instructNamedFolder(rest string) tea.Cmd {
	if a.foldersUnavailable() {
		return nil
	}
	name, text := splitFoldersInstruct(rest)
	if name == "" || text == "" {
		a.folderNote(folderInstructUsage)
		return nil
	}
	folder, ok := a.resolveFolder(name)
	if !ok {
		a.folderNote("no folder called " + name)
		return nil
	}
	return a.instructFolder(folder.ID, text, false)
}

func splitFoldersInstruct(rest string) (name, text string) {
	rest = strings.TrimSpace(rest)
	name, text, _ = strings.Cut(rest, " ")
	return strings.TrimSpace(name), strings.TrimSpace(text)
}

func (a *app) instructFolder(id, text string, clearBox bool) tea.Cmd {
	if a.foldersUnavailable() {
		return nil
	}
	id = strings.TrimSpace(id)
	text = strings.TrimSpace(text)
	if id == "" {
		a.folderNote(folderNoStandWord)
		return nil
	}
	if text == "" {
		a.folderNote(folderInstructUsage)
		return nil
	}
	if err := a.folders.InstructFolder(a.folderCtx(), id, text); err != nil {
		a.folderNote("could not instruct this folder")
		return nil
	}
	if clearBox {
		a.home.box.setText("")
	}
	a.refreshFolderMemo()
	return nil
}
