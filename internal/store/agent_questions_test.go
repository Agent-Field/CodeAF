package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAgentQuestionLifecycleIsJournaledExactlyOnceAndRebuildable(t *testing.T) {
	graph := openThreadStore(t)
	expires := time.Now().UTC().Add(time.Hour).Round(0)
	queued, err := graph.AskQuestion(AgentQuestion{
		SessionID: "questions", Text: "Which release channel should I use?",
		Urgency: QuestionNextNaturalMoment, ExpiresAt: expires,
		Options: []QuestionOption{{Label: "stable", Value: "stable"}, {Label: "beta", Value: "beta"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != QuestionPending || queued.Seq == 0 || queued.CreatedAt.IsZero() {
		t.Fatalf("queued question = %+v", queued)
	}
	// Nothing above labeled a class, so the conservative default must have
	// landed: unlabeled always reads back consent, never informational.
	if queued.Class != QuestionConsent {
		t.Fatalf("unlabeled question class = %q, want consent", queued.Class)
	}
	pending, err := graph.PendingQuestions("questions", 0)
	if err != nil || len(pending) != 1 || pending[0].Seq != queued.Seq {
		t.Fatalf("pending questions = %+v err=%v", pending, err)
	}

	ask, err := graph.SurfaceQuestion(queued.Seq)
	if err != nil {
		t.Fatal(err)
	}
	if ask.Role != RoleAgent || ask.QuestionSeq != queued.Seq || ask.Body != queued.Text ||
		!reflect.DeepEqual(ask.Options, queued.Options) {
		t.Fatalf("surfaced message = %+v", ask)
	}
	if _, err := graph.SurfaceQuestion(queued.Seq); !errors.Is(err, ErrInvalid) {
		t.Fatalf("second surface error = %v, want ErrInvalid", err)
	}
	if pending, err := graph.PendingQuestions("questions", 0); err != nil || len(pending) != 0 {
		t.Fatalf("surfaced question stayed pending: %+v err=%v", pending, err)
	}

	answer, err := graph.PostMessage(Message{
		SessionID: "questions", Role: RoleUser, Body: "Use beta.", QuestionSeq: queued.Seq,
	})
	if err != nil {
		t.Fatal(err)
	}
	matched, found, err := graph.QuestionForAnswer("questions", answer.Seq, answer.QuestionSeq)
	if err != nil || !found || matched.Seq != queued.Seq {
		t.Fatalf("matched question = %+v found=%t err=%v", matched, found, err)
	}
	if err := graph.ResolveQuestion(queued.Seq, QuestionAnswered, answer.Body, answer.Seq); err != nil {
		t.Fatal(err)
	}
	if err := graph.ResolveQuestion(queued.Seq, QuestionExpired, "too late"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("double resolution error = %v, want ErrInvalid", err)
	}
	resolved, found, err := graph.AgentQuestionBySeq(queued.Seq)
	if err != nil || !found || resolved.Status != QuestionAnswered || resolved.Resolution != answer.Body ||
		resolved.AnswerMessageSeq != answer.Seq || resolved.AskedMessageSeq != ask.Seq || resolved.ResolvedAt.IsZero() {
		t.Fatalf("resolved question = %+v found=%t err=%v", resolved, found, err)
	}

	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, found, err := graph.AgentQuestionBySeq(queued.Seq)
	if err != nil || !found || !reflect.DeepEqual(rebuilt, resolved) {
		t.Fatalf("rebuilt question = %+v, want %+v found=%t err=%v", rebuilt, resolved, found, err)
	}
}

func TestAgentQuestionRecencyDoesNotCaptureLaterConversation(t *testing.T) {
	graph := openThreadStore(t)
	question, err := graph.AskQuestion(AgentQuestion{
		SessionID: "recency", Text: "Use the compact layout?", Urgency: QuestionWhenever,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SurfaceQuestion(question.Seq); err != nil {
		t.Fatal(err)
	}
	first, err := graph.PostMessage(Message{SessionID: "recency", Role: RoleUser, Body: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := graph.QuestionForAnswer("recency", first.Seq, 0); err != nil || !found {
		t.Fatalf("first reply match found=%t err=%v", found, err)
	}
	later, err := graph.PostMessage(Message{SessionID: "recency", Role: RoleUser, Body: "unrelated"})
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := graph.QuestionForAnswer("recency", later.Seq, 0); err != nil || found {
		t.Fatalf("later reply captured old question: found=%t err=%v", found, err)
	}
	if explicit, found, err := graph.QuestionForAnswer("recency", later.Seq, question.Seq); err != nil || !found || explicit.Seq != question.Seq {
		t.Fatalf("explicit reference did not win: %+v found=%t err=%v", explicit, found, err)
	}
}

// A lens listing what is waiting on the user must read the questions, not the
// surfacing decision. A blocking question crosses into the thread the instant
// it is asked, so the unsurfaced set is blind to exactly the ones a running job
// is stuck behind.
func TestOpenQuestionsIncludeTheOnesAlreadyInTheThread(t *testing.T) {
	graph := openThreadStore(t)
	quiet, err := graph.AskQuestion(AgentQuestion{
		SessionID: "open", Text: "Use a table or bullets?", Urgency: QuestionWhenever,
	})
	if err != nil {
		t.Fatal(err)
	}
	asked, err := graph.AskQuestion(AgentQuestion{
		SessionID: "open", Text: "Which branch should the fix target?", Urgency: QuestionBlocking,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SurfaceQuestion(asked.Seq); err != nil {
		t.Fatal(err)
	}
	open, err := graph.OpenQuestions("open", 0)
	if err != nil || len(open) != 2 {
		t.Fatalf("open questions = %+v err=%v", open, err)
	}
	if open[0].Seq != quiet.Seq || open[1].Seq != asked.Seq {
		t.Fatalf("open questions are out of order: %+v", open)
	}
	if err := graph.ResolveQuestion(asked.Seq, QuestionAnswered, "main"); err != nil {
		t.Fatal(err)
	}
	if open, err := graph.OpenQuestions("open", 0); err != nil || len(open) != 1 ||
		open[0].Seq != quiet.Seq {
		t.Fatalf("a settled question stayed open: %+v err=%v", open, err)
	}
}

// QuestionForAnswer takes the newest of these, and that is a coin toss the
// moment there are two. The caller has to be able to see the whole set before
// it spends one of them.
func TestQuestionsForAnswerReturnsEveryPlausibleTarget(t *testing.T) {
	graph := openThreadStore(t)
	first, err := graph.AskQuestion(AgentQuestion{
		SessionID: "plausible", Text: "Which branch should the fix target?",
		Urgency: QuestionBlocking, Options: []QuestionOption{{Label: "main"}, {Label: "release/2.4"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := graph.AskQuestion(AgentQuestion{
		SessionID: "plausible", Text: "Start the migration plan?",
		Urgency: QuestionBlocking, Options: []QuestionOption{{Label: "start it"}, {Label: "hold it"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, seq := range []int64{first.Seq, second.Seq} {
		if _, err := graph.SurfaceQuestion(seq); err != nil {
			t.Fatal(err)
		}
	}
	reply, err := graph.PostMessage(Message{SessionID: "plausible", Role: RoleUser, Body: "1"})
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := graph.QuestionsForAnswer("plausible", reply.Seq)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("candidates = %+v err=%v", candidates, err)
	}
	if candidates[0].Seq != second.Seq || candidates[1].Seq != first.Seq {
		t.Fatalf("candidates are not newest first: %+v", candidates)
	}
	matched, found, err := graph.QuestionForAnswer("plausible", reply.Seq, 0)
	if err != nil || !found || matched.Seq != candidates[0].Seq {
		t.Fatalf("recency rule disagrees with the candidate list: %+v found=%t err=%v", matched, found, err)
	}
}

// The class axis's whole reason to exist is a conservative default that holds
// under every way of reaching it: unlabeled stays consent through the write
// path, a read back, and a rebuild from the journal; an explicit label
// round-trips; and an invalid label is rejected rather than silently
// defaulted, so a producer's typo cannot masquerade as "not yet decided".
func TestAgentQuestionClassDefaultsToConsentUnlessLabeled(t *testing.T) {
	graph := openThreadStore(t)

	unlabeled, err := graph.AskQuestion(AgentQuestion{
		SessionID: "class", Text: "Should I proceed?", Urgency: QuestionWhenever,
	})
	if err != nil {
		t.Fatal(err)
	}
	if unlabeled.Class != QuestionConsent {
		t.Fatalf("unlabeled question class = %q, want consent", unlabeled.Class)
	}
	reread, found, err := graph.AgentQuestionBySeq(unlabeled.Seq)
	if err != nil || !found || reread.Class != QuestionConsent {
		t.Fatalf("reread unlabeled question = %+v found=%t err=%v", reread, found, err)
	}

	informational, err := graph.AskQuestion(AgentQuestion{
		SessionID: "class", Text: "FYI — already done this the usual way.",
		Urgency: QuestionWhenever, Class: QuestionInformational,
	})
	if err != nil {
		t.Fatal(err)
	}
	if informational.Class != QuestionInformational {
		t.Fatalf("labeled question class = %q, want informational", informational.Class)
	}
	rereadInformational, found, err := graph.AgentQuestionBySeq(informational.Seq)
	if err != nil || !found || rereadInformational.Class != QuestionInformational {
		t.Fatalf("reread informational question = %+v found=%t err=%v", rereadInformational, found, err)
	}

	if _, err := graph.AskQuestion(AgentQuestion{
		SessionID: "class", Text: "bad class", Urgency: QuestionWhenever, Class: "maybe",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid class error = %v, want ErrInvalid", err)
	}

	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuiltUnlabeled, found, err := graph.AgentQuestionBySeq(unlabeled.Seq)
	if err != nil || !found || rebuiltUnlabeled.Class != QuestionConsent {
		t.Fatalf("rebuilt unlabeled question = %+v found=%t err=%v", rebuiltUnlabeled, found, err)
	}
	rebuiltInformational, found, err := graph.AgentQuestionBySeq(informational.Seq)
	if err != nil || !found || rebuiltInformational.Class != QuestionInformational {
		t.Fatalf("rebuilt informational question = %+v found=%t err=%v", rebuiltInformational, found, err)
	}
}

// A resident database opened by a build that predates the class column must
// not treat its old questions as anything but consent — the migration's
// ALTER TABLE default and the read path's defensive default both have to
// agree on that for a row that never had an opinion.
func TestAgentQuestionClassDefaultsToConsentOnLegacyRowMissingColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := graph.AskQuestion(AgentQuestion{
		SessionID: "legacy", Text: "Still there?", Urgency: QuestionWhenever,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the table exactly as it looked before the class column existed,
	// carrying the row across without it.
	_, err = db.Exec(`
		ALTER TABLE agent_questions RENAME TO agent_questions_legacy;
		CREATE TABLE agent_questions (
		    seq                 INTEGER PRIMARY KEY REFERENCES events(seq),
		    session_id          TEXT NOT NULL,
		    text                TEXT NOT NULL,
		    origin_node_id      TEXT NOT NULL DEFAULT '',
		    origin_charter_id   TEXT NOT NULL DEFAULT '',
		    origin_command_seq  INTEGER NOT NULL DEFAULT 0,
		    urgency             TEXT NOT NULL CHECK (urgency IN ('blocking', 'next-natural-moment', 'whenever')),
		    status              TEXT NOT NULL CHECK (status IN ('pending', 'asked', 'answered', 'expired')),
		    options             JSON NOT NULL DEFAULT '[]' CHECK (json_valid(options)),
		    category            TEXT NOT NULL DEFAULT 'generic',
		    default_answer      TEXT NOT NULL DEFAULT '',
		    created_at          TEXT NOT NULL,
		    asked_at            TEXT,
		    resolved_at         TEXT,
		    expires_at          TEXT,
		    resolution          TEXT NOT NULL DEFAULT '',
		    asked_message_seq   INTEGER NOT NULL DEFAULT 0,
		    answer_message_seq  INTEGER NOT NULL DEFAULT 0,
		    updated_seq         INTEGER NOT NULL
		);
		INSERT INTO agent_questions SELECT seq, session_id, text, origin_node_id, origin_charter_id,
		    origin_command_seq, urgency, status, options, category, default_answer, created_at, asked_at,
		    resolved_at, expires_at, resolution, asked_message_seq, answer_message_seq, updated_seq
		FROM agent_questions_legacy;
		DROP TABLE agent_questions_legacy;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	legacy, found, err := reopened.AgentQuestionBySeq(queued.Seq)
	if err != nil || !found || legacy.Class != QuestionConsent {
		t.Fatalf("legacy question class = %+v found=%t err=%v, want consent", legacy, found, err)
	}
}
