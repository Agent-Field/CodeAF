package tui3

import (
	"path/filepath"
	"slices"
	"time"
)

// saveTakeDraftAt keeps the arriving words durable before their source is cleared.
func (a *app) saveTakeDraftAt(conv Conversation, state composerState) error {
	path := draftKeepPath(conv.DraftFile)
	keep, _ := readDraftKeep(path)
	workspace := conv.Workspace
	if workspace == "" {
		workspace = a.workspace
	}
	owner := draftOwnerOf(a.host, workspace, conv.SessionFile)
	keep.Version, keep.Owner, keep.At = draftKeepVersion, owner, a.now().UTC().Format(time.RFC3339Nano)
	keep.Slots = slices.DeleteFunc(keep.Slots, func(slot draftKeepSlot) bool {
		return slot.Owner == owner && slot.Task == 0 && slot.Run == "" && slot.Guest == ""
	})
	slot := keptSlot(mainRecipient, state)
	slot.Owner = owner
	keep.Slots = append(keep.Slots, slot)
	return (draftSave{keepPath: path, textPath: conv.DraftFile, keep: keep, text: slot.Text, rev: claimDraftRev(path)}).commit()
}

// moveTakeDraftCopies retires matching copies left by earlier windows of this chat.
func (a *app) moveTakeDraftCopies(state composerState) {
	if a.draftFile == "" {
		return
	}
	want := keptSlot(mainRecipient, state)
	owner := a.draftKeepOwner()
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(a.draftFile), draftPrefix(a.workspace)+"*.json"))
	for _, file := range files {
		if file == draftKeepPath(a.draftFile) || draftWindowAlive(file) {
			continue
		}
		keep, how := readDraftKeep(file)
		if how != draftKeepFound {
			continue
		}
		changed := false
		slots := keep.Slots[:0]
		for _, slot := range keep.Slots {
			if slot.Owner == owner && slot.Task == 0 && slot.Run == "" && slot.Guest == "" &&
				slot.Text == want.Text && slices.Equal(slot.Pastes, want.Pastes) && slices.Equal(slot.Chips, want.Chips) {
				slot.Text, slot.Caret, slot.Pastes, slot.Chips = "", 0, nil, nil
				changed = true
			}
			if !slot.empty() {
				slots = append(slots, slot)
			}
		}
		if !changed {
			continue
		}
		keep.Slots = slots
		gone := draftKeepFile{path: file, keep: keep}
		if keep.Owner == owner && readDraft(draftExportPath(file)) == want.Text {
			gone.export = draftExportPath(file)
		}
		if err := gone.prune(); err != nil {
			a.noteDraftKeepFailed(err)
		}
	}
}
