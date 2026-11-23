package main

import (
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// SaysEntryAuthor says this engine's transcript entries carry author
// ([appwire.FeatureEntryAuthor]).
func (b *appBackend) SaysEntryAuthor() bool { return true }

// appEntryAuthor is who wrote one transcript entry's words, from what the
// record's producer said about it, never from a reading of what it says:
//
//   - a user line is the person's, unless it opens with codeaf's carry-on lead
//     ([session.IsCarryOn]: a checkpoint's nudge, a factory step's "you stopped
//     without calling stage_result" reminder), which is the engine's in the
//     person's seat;
//   - a factory runner's progress line is the engine's, as is every quiet line
//     the session wrote (a note, a compaction's summary); a crew's delivery is
//     its members', so the agents';
//   - the model's answers, updates and calls are the agent's.
//
// A factory step's opening instructions are the one engine line this cannot
// see from the line alone; [appMarkStepBrief] marks it from the conversation's
// own owner record.
func appEntryAuthor(entry appwire.Entry, item session.DisplayEntry) string {
	switch entry.Kind {
	case appwire.EntryUser:
		if item.Person == nil && session.IsCarryOn(item.Text) {
			return appwire.AuthorEngine
		}
		return appwire.AuthorPerson
	case appwire.EntryAnswer, appwire.EntryUpdate:
		if item.FactoryProgress {
			return appwire.AuthorEngine
		}
		return appwire.AuthorAgent
	case appwire.EntryTool:
		return appwire.AuthorAgent
	case appwire.EntryNotice:
		if len(item.Team) > 0 {
			return appwire.AuthorAgent
		}
		return appwire.AuthorEngine
	case appwire.EntrySummary:
		return appwire.AuthorEngine
	}
	return ""
}

// appMarkStepBrief marks a factory step conversation's first message as the
// engine's: the step maker opens the conversation on the step's brief before
// anyone can type into it (factory_stage.go's Open), so the record's first
// user line is that brief, and its owner record beside the journal says the
// conversation is a step's ([readFactoryOwner]'s Step). A job's own manager
// conversation has no step and its first line is the person's.
func appMarkStepBrief(page []appwire.Entry, transcript string) {
	owner, ok := readFactoryOwner(transcript)
	if !ok || owner.Step == "" {
		return
	}
	first := appUserEntryID(0)
	for i := range page {
		if page[i].ID == first && page[i].Kind == appwire.EntryUser {
			page[i].Author = appwire.AuthorEngine
			return
		}
	}
}
