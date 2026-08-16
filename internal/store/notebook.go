package store

import (
	"database/sql"
	"fmt"
	"time"
)

// The two reads the notebook page needed and nobody had written.
//
// Everything else that page draws already had a read — Facts, TasteRules,
// SkillFacts, Questions, CompetenceMap, SelfSpendToday — and the rule this file
// was added under is that a surface may not invent a query. These two are here
// because the alternative was worse in a specific way, stated per function.

// RecentQuestionPractices is every practice round of the newest rounds, in ONE
// query, so a page listing thirty questions costs one read instead of thirty.
//
// [Store.QuestionPractices] answers per question and stays the right read for
// the resident, which is always working on one gap at a time. A surface is not:
// it draws the whole band on every journal move, and thirty indexed queries per
// move is thirty round trips to answer a question the table can answer once.
// The caller buckets by [QuestionPractice.QuestionSeq].
//
// Newest first, because a window that has to drop rounds should drop the oldest.
func (s *Store) RecentQuestionPractices(limit int) ([]QuestionPractice, error) {
	if limit <= 0 {
		limit = 500
	}
	return s.questionPractices(`1 = 1 ORDER BY start_seq DESC LIMIT ?`, limit)
}

// PracticedToday is the wall clock spent on self-directed practice since local
// midnight — the day receipt's "42m practiced".
//
// It is a read rather than a derivation because the derivation is a WHOLE GRAPH
// SCAN: the old self page answered this by walking Snapshot() for roots whose
// group is [PracticeGroup], which is every node in the store to find a handful.
// The query below is those roots and nothing else.
//
// WALL CLOCK, NOT SUM. Two practice roots that ran at the same time cost one
// stretch of clock, not two, and a day receipt that added them would tell the
// reader the machine practised for longer than the day was: the intervals are
// merged before they are totalled, the same rule store.SubtreeReceipts follows
// for a job's elapsed. A root still running is counted up to now, because it is
// still practising; a root with no start is not counted at all, because a node
// that never started spent no time (10.2.8 — absent is not zero, and here the
// absence genuinely means nothing was spent).
func (s *Store) PracticedToday(now time.Time) (time.Duration, error) {
	if now.IsZero() {
		now = time.Now()
	}
	start, end := localDayBounds(now)
	rows, err := s.db.Query(`SELECT started_at, finished_at FROM nodes
		WHERE parent_id = ? AND grp = ? AND started_at IS NOT NULL AND started_at != ''
		AND started_at < ? ORDER BY started_at`, RootID, PracticeGroup, formatTime(end))
	if err != nil {
		return 0, fmt.Errorf("practiced today: %w", err)
	}
	defer rows.Close()
	type span struct{ from, to time.Time }
	spans := make([]span, 0, 8)
	for rows.Next() {
		var startedAt string
		var finishedAt sql.NullString
		if err := rows.Scan(&startedAt, &finishedAt); err != nil {
			return 0, fmt.Errorf("practiced today: %w", err)
		}
		from, err := parseTime(startedAt)
		if err != nil {
			continue
		}
		to := now.UTC()
		if finishedAt.Valid && finishedAt.String != "" {
			if parsed, err := parseTime(finishedAt.String); err == nil {
				to = parsed
			}
		}
		// Clamped into the day at both ends: a round that began yesterday and
		// landed this morning contributes this morning, and nothing else.
		if from.Before(start) {
			from = start
		}
		if to.After(end) {
			to = end
		}
		if !to.After(from) {
			continue
		}
		spans = append(spans, span{from: from, to: to})
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("practiced today: %w", err)
	}
	total := time.Duration(0)
	var open span
	for _, s := range spans {
		switch {
		case open.to.IsZero():
			open = s
		case !s.from.After(open.to):
			if s.to.After(open.to) {
				open.to = s.to
			}
		default:
			total += open.to.Sub(open.from)
			open = s
		}
	}
	if !open.to.IsZero() {
		total += open.to.Sub(open.from)
	}
	return total, nil
}
