// Package cellindex rebuilds what the harness keeps ABOUT a session from what
// the sealed cell holds (docs/ARCHITECTURE.md 14, docs/PLAN.md 0.6).
//
// THE PRINCIPLE. The sealed cell (transcript, turns, receipts) is the only
// truth. Every index the harness keeps beside it is a citation of that record,
// so a cell materialized on a machine that never ran it can have its indexes
// made again, and a lost or deleted index is a slow open and never a lost
// conversation.
//
// THE SHAPE. An [Index] is one derived thing: it can say whether it is present
// and can be built again from a [session.Digest], the transcript read as facts.
// [Indexes] is the registry; [Rebuild] walks it and builds each index that is
// missing, so adding an index is adding one type and one registry line.
//
// WHAT IS NOT HERE, and why, is the table in the lane report: state.json and
// tasks.json are the model's and the scheduler's own working state (truth, they
// ride the sealed folder), card.json is a model-extracted summary the next turn
// refills, and the fields of meta.json the transcript cannot know (effort,
// approval, referred places, working copies, archive marks) are truth that
// still lives only in that file.
package cellindex
