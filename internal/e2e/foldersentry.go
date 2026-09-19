package e2e

// foldersentry.go is the J36–J49 + F09/F10 contract the Folders harness waits
// for. Live tmux against a real model is t-fe-validate (J36–J43) and
// t-rx-validate (J44–J49, F09/F10); this file commits the frozen names so the
// untagged word gate can fail a respelling on the pull request that moved it,
// and so the tagged driver has one place to quote them.
//
// PERSON-FACING STRINGS ARE QUOTED FROM CONTRACTS.md Folders-entry and Folders
// columns + reactive. The ui lane draws them; the runtime lane maps store job
// states and timings onto them. A needle that lived only in a comment would
// keep the word gate green while the surface stopped saying it, so the values
// below are string literals.

const (
	// foldersEntryNewFolderWord is the visible Folders-place action that
	// creates a logical folder at Root, or inside the selected folder.
	foldersEntryNewFolderWord = "New folder"
	// foldersEntryNewChatWord is the visible Folders-place action that starts
	// a chat at Root or in the selected folder. The start-page tab spells the
	// same two words; Esc before send creates no transcript.
	foldersEntryNewChatWord = "New chat"
	// foldersEntryOrganizeWord is the visible survey. It is not slash-only.
	foldersEntryOrganizeWord = "Organize existing chats"
	// foldersEntryOrganizeSlash is the optional typed door onto the same
	// survey. It is never the only door.
	foldersEntryOrganizeSlash = "/folders organize"
	// foldersEntryJobQueued is what a person sees for a store pending job.
	foldersEntryJobQueued = "queued"
	// foldersEntryJobRunning is what a person sees for a store leased job.
	foldersEntryJobRunning = "running"
	// foldersEntryJobDelayed is what a person sees for deferred or failed.
	foldersEntryJobDelayed = "delayed"
	// foldersEntryJobDone is what a person sees for a completed survey.
	foldersEntryJobDone = "done"
	// foldersEntryJobCancel is the visible cancel, and the person-facing
	// spelling of a store cancelled row.
	foldersEntryJobCancel = "cancel"
	// foldersEntryCoalesceKey is wsapi.OrganizeExistingKey once the organize
	// lane lands it. Quoted here so the harness can assert the job row without
	// importing a door that may still be missing.
	foldersEntryCoalesceKey = "organize_existing"
	// foldersEntryJobType is workspace.JobOrganize, the one scheduler. There
	// is no second job type for this survey.
	foldersEntryJobType = "observe_and_organize"
	// foldersEntryNoFoldersYet is the emptiness-law refusal. The empty folder
	// list keeps the heading and the whisper; it never says this.
	foldersEntryNoFoldersYet = "no folders yet"
	// foldersEntryChecked is the other banned progress word. Organize paints
	// delayed, never this, and never the store spellings pending/leased/
	// completed/deferred/cancelled.
	foldersEntryChecked = "checked"

	// foldersOrganizeThisChatWord is the visible targeted rerun. Chord t.
	foldersOrganizeThisChatWord = "Organize this chat"
	// foldersManageThisFolderWord is the details restable that opens an
	// ordinary scoped chat. Chord d. No manager entity.
	foldersManageThisFolderWord = "Manage this folder"
	// foldersAddExistingWord is the one Folders-place hook t-ux-add-old fills.
	// Chord b. Absent file means the row is absent.
	foldersAddExistingWord = "Add existing chats"
	// foldersCoordinateSelectedWord is the Folders-place / home-panel chord g.
	// It supersedes Folders-place c / coordinate these (that c stays New folder).
	foldersCoordinateSelectedWord = "Coordinate selected"
	// foldersOpenChatWord is the details restable that opens the existing
	// full conversation after a cached preview.
	foldersOpenChatWord = "Open chat"
	// foldersAlsoInWord is a shared object naming its other placements. The
	// trailing space is the join onto the other folder's name.
	foldersAlsoInWord = "also in "
	// foldersAddedToPrefix is the compact activity line before the real
	// collection name. Omit the line when there is no committed change.
	foldersAddedToPrefix = "Added to "
	// foldersWhyUndoTail is the rest of that compact line: Why and Undo.
	foldersWhyUndoTail = " · Why · Undo"
	// foldersShiftRightHint is the Folders-place hint that shift+→ opens the
	// verb strip. → itself drills.
	foldersShiftRightHint = "shift+→ actions"
	// foldersReactiveKey is config workspace.reactive. Unset is off. Visible
	// Organize existing chats that enqueues writes on.
	foldersReactiveKey = "workspace.reactive"
	// foldersEntrySurveyCap is organizeSurveyCap: a per-lease slice, not a
	// wall. F09 persists a cursor so Unfiled[0..] is not rescanned forever.
	foldersEntrySurveyCap = 8
)

// foldersEntryStoreWords are the job-table bytes a Folders place must never
// paint. The adapter maps them onto queued/running/delayed/done/cancel.
var foldersEntryStoreWords = []string{
	"pending", "leased", "completed", "deferred", "cancelled",
}
