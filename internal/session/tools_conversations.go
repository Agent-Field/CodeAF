package session

// search_conversations is the model's door onto WHAT WAS ACTUALLY SAID, in
// every earlier conversation on this machine.
//
// Every user message, every reply and every tool result is already posted into
// the store as it lands (chatlog.go), and internal/store/thread_search.go has
// kept an FTS index over all of it — bm25 ranked, recency as the tiebreak, each
// hit bounded to 400 bytes and stamped with its age. Until this tool the whole
// index had no reader in this package at all: the transcript was written,
// indexed, bounded, tested and UNREACHABLE, so "what did we decide about the
// flag last week" was answered out of a model's imagination, or refused.
//
// THE NAME IS NOT `recall`, AND THAT IS NOT A PREFERENCE. `recall` is already
// one of the three working-state hands (state.go: track, commit, recall), it is
// on every belt unconditionally, and two tools of one name on one wire is a
// model choosing between them by coin toss. This one is named for the question
// a person asks it — "search my old conversations".
//
// WHY THE VERBATIM LINE AND NOT THE REMEMBERED ONE. The `<memory>` block holds a
// handful of durable extracted facts, which is a different thing and a much
// lossier one: on LongMemEval, retrieving verbatim chunks scores 67.4% against
// 45.4% for LLM-extracted artifacts over the same conversations — +22.0pp, p <
// 10⁻¹⁵ (arXiv 2601.00821) — and MemGPT measures the same gap from the other
// end, 92.5% for paging over retained text against 32.1% for recursive
// summarization (arXiv 2310.08560). The same work shows the two tiers are not
// rivals: artifacts ALONGSIDE the text cost nothing measurable (42.5% vs 43.9%,
// p = 0.39). It is replacement that loses. So `remember` keeps its lines and
// this tool hands back the words they were extracted from.
//
// AND IT IS ABSENT RATHER THAN BROKEN. No store is no index, so a session with
// memory off — and a task node, and --once — is not given the verb at all,
// exactly as `stand` and `remember` are withheld (tools.go). A model told it can
// search earlier conversations will plan a whole answer around one.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	// conversationLimitDefault is how many lines one search answers with when nobody
	// asked for a number. Eight is the store's own default and about what a
	// question of this shape ever needs: the hits are ranked, so the answer is
	// almost always in the first two or three, and a wall of near-misses is the
	// one thing a retrieved block must not become — a single top-ranked
	// non-answer costs 18 to 20 percent relative on the reader (Cuconasu et
	// al., SIGIR 2024).
	conversationLimitDefault = 8

	// conversationLimitMax is the ceiling on that, whatever was asked for. It is a
	// bound on the ANSWER rather than on the search: twenty bounded excerpts is
	// already a long tool result, and the honest way to see more of one
	// conversation is to read its transcript.
	conversationLimitMax = 20
)

// searchConversationsDescription is what makes the model reach for this rather than
// answering from memory, so it says the gesture out loud in the person's own
// terms — the same sentence the system prompt uses for `tasks`.
const searchConversationsDescription = "Search earlier conversations verbatim — every message of every conversation on this machine, in the words they were actually said in. USE IT BEFORE ANSWERING ANYTHING ABOUT WHAT WAS SAID OR DECIDED IN ANOTHER SESSION: \"what did we decide about the retry limit\", \"what did I tell you about the deploy\", \"the name we picked for that flag\" are one gesture and none of them are answered from memory. Search with the person's own words. Each hit is one bounded excerpt with its age, the conversation it came from and that conversation's transcript, which `read` opens when the excerpt is not enough."

// It is a var and not a const because the two bounds are interpolated from the
// constants the code enforces: a schema that spelled its own numbers would be
// the one place they could drift from what the tool actually does.
var searchConversationsSchemaJSON = `{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "What to look for, in the words the person used. Matched against the text of every message ever said in any conversation on this machine."
    },
    "limit": {
      "type": "integer",
      "description": "How many excerpts to answer with. Default ` + strconv.Itoa(conversationLimitDefault) + `, maximum ` + strconv.Itoa(conversationLimitMax) + `."
    }
  },
  "required": ["query"]
}`

// conversationTools is the belt's episodic half — one tool, present only where there
// is a store behind it (tools.go), because the index lives in the store and a
// session with memory off has opened none.
func (a *Agent) conversationTools() []bare.Tool {
	// THE BRAIN AND NOT THE FIELD, because everything below dereferences the
	// brain. [Config.hasStore] is the same fact asked of a config, which is all
	// the render step has when it composes this tool's sentence
	// (beltfacts.go); newAgent builds the brain from exactly that field, and
	// prompt_belt_test.go pins the two answers together for every shape this
	// package builds.
	if !a.remembers() {
		return nil
	}
	return []bare.Tool{{
		Name:        "search_conversations",
		Description: searchConversationsDescription,
		Schema:      json.RawMessage(searchConversationsSchemaJSON),
		Execute:     a.searchConversationsTool,
	}}
}

// searchConversationsTool searches and renders. Everything it can be asked badly is an
// ordinary tool result rather than a Go error, the way every other tool on this
// belt answers: a query the model shaped wrongly is a query it can shape again.
func (a *Agent) searchConversationsTool(_ context.Context, args json.RawMessage) (string, bool, error) {
	var parsed struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if len(args) > 0 {
		if err := decodeToolArguments(args, &parsed); err != nil {
			return "Invalid arguments: " + err.Error(), true, nil
		}
	}
	query := strings.TrimSpace(parsed.Query)
	if query == "" {
		return "Invalid arguments: query is required — the words the person used", true, nil
	}
	limit := parsed.Limit
	if limit <= 0 {
		limit = conversationLimitDefault
	}
	if limit > conversationLimitMax {
		limit = conversationLimitMax
	}
	// The empty session filter is the point of the read: this is the question
	// "we talked about this once", and a search scoped to the room it is being
	// asked in would answer it out of the window the model already has.
	hits, err := a.memory.store.SearchMessages(query, "", limit)
	if err != nil {
		return "Could not search earlier conversations: " + err.Error(), true, nil
	}
	if len(hits) == 0 {
		// A MISS IS AN ANSWER AND IT IS SAID. Hostile FTS syntax comes back here
		// too — the store treats a query it cannot parse as finding nothing
		// (thread_search.go) — and the honest report of both is the same: this
		// was looked for and it is not there.
		return "Nothing said in any earlier conversation matches " + strconv.Quote(query) + ".", false, nil
	}
	return a.conversationHitsText(hits), false, nil
}

// conversationHitsText renders the hits. ONE HIT IS ONE LINE — its age, the conversation
// it was said in, who said it and the bounded words — with the transcript it
// came from indented under it, exactly as a `tasks` row prints its transcript
// URI (tools_tasks.go).
//
// A hit is NOT widened to its neighbouring rows, though `m.seq` is the primary
// key and the range scan would be cheap. SECOM (arXiv 2502.05589) measures a
// coherent topic segment beating a lone turn — LoCoMo GPT4Score 71.57 against
// 54.15 for the full history — so this is worth doing and is deliberately not
// done yet: the store exposes no bounded reader for a seq window, [store.Store.Messages]
// hands back whole message bodies up to 16 KiB each, and a widening built on it
// would turn an eight-line answer into a hundred kilobytes of transcript. The
// widening belongs in the store, beside the bound it has to respect.
func (a *Agent) conversationHitsText(hits []store.MessageHit) string {
	// One session is usually several hits, so the room's name and its
	// transcript are each resolved once per conversation rather than per line.
	names := make(map[string]string, len(hits))
	transcripts := make(map[string]string, len(hits))
	var out strings.Builder
	for _, hit := range hits {
		room, ok := names[hit.SessionID]
		if !ok {
			room = a.conversationRoom(hit.SessionID)
			names[hit.SessionID] = room
		}
		parts := make([]string, 0, 3)
		if hit.Age != "" {
			parts = append(parts, hit.Age)
		}
		if room != "" {
			parts = append(parts, room)
		}
		parts = append(parts, conversationSpeaker(hit.Role)+": "+conversationOneLine(hit.Body))
		fmt.Fprintf(&out, "%s\n", strings.Join(parts, " · "))
		uri, ok := transcripts[hit.SessionID]
		if !ok {
			uri = a.conversationTranscriptURI(hit.SessionID)
			transcripts[hit.SessionID] = uri
		}
		// A LINE THAT NAMES NO TRANSCRIPT IS LEFT OFF RATHER THAN WRITTEN EMPTY.
		// The conversation is still on this disk somewhere for a session written
		// in the flat layout or under another home; what this must never do is
		// print a path to a file that is not there.
		if uri != "" {
			out.WriteString("  transcript " + uri + "\n")
		}
	}
	out.WriteString("\nEach line is one excerpt and not the exchange around it — read a transcript for the rest.\n")
	return out.String()
}

// conversationRoom is the conversation's own name, or its id when it never settled on
// one. The id is worth printing either way: it is the folder the conversation
// lives in, so a person and the model can both go and look.
func (a *Agent) conversationRoom(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	if session, ok, err := a.memory.store.Session(sessionID); err == nil && ok {
		if title := strings.TrimSpace(session.Title); title != "" {
			return "'" + title + "'"
		}
	}
	return sessionID
}

// conversationTranscriptURI is where that conversation's journal can be read, or ""
// when there is no journal at that path.
//
// THE FOLDER'S NAME IS THE SESSION'S ID (place.go), and the store's thread id is
// that same id (chatlog.go), so a sibling of this session's own folder is the
// whole of the arithmetic. It is checked against the disk before it is printed,
// for the reason the chat log spills bytes before naming them: the one thing a
// pointer must never do is claim a record exists where it does not.
func (a *Agent) conversationTranscriptURI(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	dir := strings.TrimSpace(a.config.Place.Dir)
	if sessionID == "" || dir == "" {
		return ""
	}
	path := Place{Dir: filepath.Join(filepath.Dir(dir), sessionID)}.Transcript()
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return ""
	}
	return taskURI(path)
}

// conversationSpeaker is who said the line, in the grammar the model is being asked to
// quote it in: the person, itself in an earlier conversation, or a tool result
// somebody was looking at.
func conversationSpeaker(role store.Role) string {
	switch role {
	case store.RoleUser:
		return "them"
	case store.RoleAgent:
		return "you"
	default:
		return "a tool result"
	}
}

// conversationOneLine flattens an excerpt onto the row it belongs to. The store has
// already bounded it to 400 bytes; what is left is that a transcript line can
// be a paragraph or a diff, and a hit that spilled over four rows would bury
// the seven hits under it.
func conversationOneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
