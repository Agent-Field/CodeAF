package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// WHAT A CONVERSATION COST HAS TO BE READABLE WITHOUT OPENING ITS TRANSCRIPT.
//
// The journal's usage lines are the record and stay the record, but home reads
// every session on the machine as a folder, a meta.json and a presence file
// (world.go) — so the running total is stamped on meta.json at the end of every
// turn, and these tests pin that it is the SESSION'S OWN talking, that a turn
// which spent nothing leaves no figure, and that a journal written before the
// field existed gets one on the way back in.

// stampedMeta is one session folder's identity file, read back off disk.
func stampedMeta(t *testing.T, dir string) Meta {
	t.Helper()
	meta, err := LoadMeta(dir)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	return meta
}

// Every turn leaves the total behind, and the total is cumulative: two turns of
// forty cents are eighty on the file, not forty twice.
func TestEachTurnStampsWhatTheConversationHasSpentOnItsMeta(t *testing.T) {
	dir := t.TempDir()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return pricedResponse("forty cents", 0.40), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return pricedResponse("forty more", 0.40), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "session.jsonl")
		config.Place = Place{Dir: dir}
	})

	collect(t, mustSubmit(t, agent, "what does this cost?"))
	first := stampedMeta(t, dir)
	if first.SpentUSD < 0.40 {
		t.Fatalf("meta.json says the conversation spent %v, want at least the turn's 0.40", first.SpentUSD)
	}
	if first.Tokens != 15 {
		t.Fatalf("meta.json says %d tokens, want the turn's input plus output (15)", first.Tokens)
	}

	collect(t, mustSubmit(t, agent, "and again?"))
	second := stampedMeta(t, dir)
	if second.SpentUSD < 0.80 {
		t.Fatalf("after two turns meta.json says %v, want the running total (0.80)", second.SpentUSD)
	}
	// AT LEAST the two turns' own 30. The total is [Agent.Usage], which counts
	// the auxiliary calls beside a turn as well — the namer, the guardian — and
	// that is the honest answer to "what has this conversation cost me".
	if second.Tokens < 30 {
		t.Fatalf("after two turns meta.json says %d tokens, want at least the turns' 30", second.Tokens)
	}
	// AND THE REST OF THE IDENTITY SURVIVES THE STAMP. It is the same file a
	// picker reads for the title and the last-active stamp, and a write that
	// dropped either would take every resumed conversation's name with it.
	if second.LastUserAt.IsZero() || second.ID == "" {
		t.Fatalf("the spend stamp lost the rest of the identity: %+v", second)
	}
}

// A TURN THAT SPENT NOTHING LEAVES NO FIGURE. The emptiness law reaches the
// file, exactly as it does the journal's usage lines: a provider that reports
// no accounting leaves a meta.json with no spend on it, and home draws nothing
// rather than `$0.00`.
func TestAConversationThatSpentNothingStampsNoFigure(t *testing.T) {
	dir := t.TempDir()
	agent, _ := newTestAgent(t, quietCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(dir, "session.jsonl")
		config.Place = Place{Dir: dir}
	})
	collect(t, mustSubmit(t, agent, "say something free"))

	meta := stampedMeta(t, dir)
	if meta.SpentUSD != 0 || meta.Tokens != 0 {
		t.Fatalf("a conversation that spent nothing stamped %v / %d tokens", meta.SpentUSD, meta.Tokens)
	}
}

// A JOURNAL WRITTEN BEFORE THE FIELD EXISTED GETS ITS TOTAL ON THE WAY BACK IN,
// off the replay that was already read — never a second pass over the file.
func TestAResumedConversationFoldsItsTranscriptIntoTheStampOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	write(t, path,
		`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"what did that cost?","timestamp":"t"}`,
		`{"type":"usage","usage":{"model":"m","input":10,"output":5,"costUsd":0.25,"calls":1},"timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"a quarter","timestamp":"t"}`,
	)
	if err := SaveMeta(dir, Meta{ID: "abc", Workspace: t.TempDir()}); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}

	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = path
		config.Place = Place{Dir: dir}
	})
	_ = agent

	meta := stampedMeta(t, dir)
	if meta.SpentUSD != 0.25 || meta.Tokens != 15 {
		t.Fatalf("a resumed conversation stamped %v / %d tokens, want the transcript's 0.25 and 15",
			meta.SpentUSD, meta.Tokens)
	}
}

// A META THAT ALREADY CARRIES A FIGURE IS NOT REWRITTEN BY MERELY OPENING THE
// SESSION. The stamp on the way in exists for old journals; a window that only
// looked at a conversation must not touch its file.
func TestOpeningAConversationThatAlreadyHasATotalRewritesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	write(t, path,
		`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"usage","usage":{"model":"m","input":10,"output":5,"costUsd":0.25,"calls":1},"timestamp":"t"}`,
	)
	if err := SaveMeta(dir, Meta{ID: "abc", Workspace: t.TempDir(), SpentUSD: 9, Tokens: 99}); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}

	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = path
		config.Place = Place{Dir: dir}
	})
	_ = agent

	meta := stampedMeta(t, dir)
	if meta.SpentUSD != 9 || meta.Tokens != 99 {
		t.Fatalf("opening a conversation overwrote its total with %v / %d", meta.SpentUSD, meta.Tokens)
	}
}

// AND THE WORLD READS IT BACK. This is the whole point of the field: the row
// home draws carries the conversation's own spending beside its tasks', without
// this layer opening a single transcript.
func TestTheWorldCarriesWhatTheConversationItselfSpent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "-tmp-alpha", "aaaa000000000001")
	if err := SaveMeta(dir, Meta{
		ID: "aaaa000000000001", Workspace: "/tmp/alpha", Title: "Pricing",
		LastUserAt: time.Now(), SpentUSD: 1.25, Tokens: 34000,
	}); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	write(t, Place{Dir: dir}.Transcript(), `{"type":"session","version":1,"id":"aaaa000000000001","timestamp":"t"}`)

	world := ReadWorld(root)
	if len(world.Projects) != 1 || len(world.Projects[0].Sessions) != 1 {
		t.Fatalf("the world read %d projects, want the one written", len(world.Projects))
	}
	row := world.Projects[0].Sessions[0]
	if row.Spend != 1.25 || row.Tokens != 34000 {
		t.Fatalf("the row carries %v / %d tokens, want meta.json's 1.25 and 34000", row.Spend, row.Tokens)
	}
}
