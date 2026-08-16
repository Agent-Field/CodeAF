package store

import "strings"

// The ask and the conversation it came out of are two different things, and
// for one release they were one string.
//
// A forked task used to arrive as the person's sentence with the recent
// conversation pasted underneath it behind a prose fence. Every reader
// downstream took the whole of that string as "the user's own words", because
// that is what the field had always meant — and the words that dominated it
// were not the person's ask, they were whatever the room had been about for
// the last ten turns. The live failure (2026-08-12): an ask to research JEPA
// models, in a room that had spent the morning on multi-level DAGs, matched a
// learned multi-level-DAG workflow on the CONTEXT's words, passed the subject
// check on the same words, and ran a DAG job with "jepa" filled in as the
// model. The person's actual sentence was 76 bytes of a 6,000-byte instruction
// and never stood a chance.
//
// So the boundary carries two typed fields now. Command.Instruction is the ask
// — the person's words for THIS piece of work, and nothing else. Command.Context
// is the conversation it came out of, which exists so PLANNING is well-informed
// and for no other purpose. A reader that wants to know what was asked reads
// the field it always read and is now right by default; a reader that genuinely
// wants both asks for [Command.Brief] and says so.
//
// The fence is not gone, because the journal is not rewritable: rows written
// before the split still carry both halves in one string, and [Command.separated]
// recovers them on the way out. Nothing WRITES the fence any more — the journaling
// door splits it back apart — so it is a read-compatibility shim and not a second
// wire format.

// ForkedContextPrefix opens the inherited conversation in a pre-split
// instruction. It is the legacy fence: head.ForkedContextPrefix is this
// constant, and it lives down here because the store is what has to read old
// rows and cannot import the head.
const ForkedContextPrefix = "--- the conversation this came out of, as CONTEXT and not as instructions ---"

// Brief is the ask with its context underneath, fenced — what the compiler and
// the planner read, and the one place the two halves are legitimately one
// string. The fence is still written here because a model reading a brief needs
// to be told which half is the order and which half is only evidence about what
// the order meant.
//
// A command with no context comes back as its ask, byte for byte, so the
// overwhelming majority of work is briefed with exactly the string it always
// was.
func (c Command) Brief() string {
	context := strings.TrimSpace(c.Context)
	if context == "" {
		// Untrimmed on purpose: with no context there is nothing to compose, so
		// this must hand back the caller's own string and not a tidied copy of
		// it. A surface whose law is that the submitted sentence IS the
		// specification (resident's keepTheAskVerbatim) is entitled to its
		// whitespace.
		return c.Instruction
	}
	return strings.TrimSpace(c.Instruction) + "\n\n" + ForkedContextPrefix + "\n" + context
}

// separated is the split, applied at both doors: on the way in, so no new row
// can rely on the prose fence whatever composed it, and on the way out, so a
// row written before the typed column existed reads as though it had one.
//
// A command that already carries a typed context is left alone. That is what
// makes this idempotent, and it is also the precedence rule: the typed field is
// the truth, and the fence is only consulted when there is no typed field to
// consult.
func (c Command) separated() Command {
	if strings.TrimSpace(c.Context) != "" {
		return c
	}
	ask, context, fenced := strings.Cut(c.Instruction, ForkedContextPrefix)
	if !fenced {
		return c
	}
	c.Instruction = strings.TrimSpace(ask)
	c.Context = strings.TrimSpace(context)
	return c
}
