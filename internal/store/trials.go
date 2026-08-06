package store

import (
	"encoding/json"
	"fmt"
)

// TrialStatus says whether a fired experiment has produced a notebook
// verdict. Pending means its trial-marked subtree has not consumed the pair.
type TrialStatus string

const (
	TrialPending      TrialStatus = "pending"
	TrialSettled      TrialStatus = "settled"
	TrialInconclusive TrialStatus = "inconclusive"
)

// TrialOutcome connects one trial-marked splice to the fact that consumed its
// unsettled pair. Body is the winning rule or the carried-forward pair.
type TrialOutcome struct {
	NodeID         string
	TrialOf        int64
	Status         TrialStatus
	ReplacementSeq int64
	Kind           FactKind
	Body           string
}

// TrialStats answers whether the experiment loop has fired and what each run
// settled. Counts are derived from journal events rather than process state.
type TrialStats struct {
	Fired        int
	Settled      int
	Inconclusive int
	Pending      int
	Outcomes     []TrialOutcome
}

// TrialStats returns all trial-marked splices in admission order and resolves
// their verdicts through fact supersession events written by the trial root.
func (s *Store) TrialStats() (TrialStats, error) {
	events, err := s.Events(0, 0)
	if err != nil {
		return TrialStats{}, err
	}
	facts := make(map[int64]factPayload)
	replacements := make(map[int64]int64)
	stats := TrialStats{}
	for _, event := range events {
		switch event.Kind {
		case EventSubtreeSpliced:
			var payload splicedPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return TrialStats{}, fmt.Errorf("decode trial splice event %d: %w", event.Seq, err)
			}
			if payload.Provenance.TrialOf > 0 {
				stats.Outcomes = append(stats.Outcomes, TrialOutcome{
					NodeID: payload.Root, TrialOf: payload.Provenance.TrialOf, Status: TrialPending,
				})
			}
		case EventFactLearned:
			var payload factPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return TrialStats{}, fmt.Errorf("decode trial fact event %d: %w", event.Seq, err)
			}
			facts[event.Seq] = payload
		case EventFactSuperseded:
			var payload factSupersededPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return TrialStats{}, fmt.Errorf("decode trial supersession event %d: %w", event.Seq, err)
			}
			if _, exists := replacements[payload.FactSeq]; !exists {
				replacements[payload.FactSeq] = payload.BySeq
			}
		}
	}

	stats.Fired = len(stats.Outcomes)
	for index := range stats.Outcomes {
		outcome := &stats.Outcomes[index]
		replacementSeq := replacements[outcome.TrialOf]
		replacement, ok := facts[replacementSeq]
		if replacementSeq == 0 || !ok || replacement.NodeID != outcome.NodeID {
			stats.Pending++
			continue
		}
		outcome.ReplacementSeq = replacementSeq
		outcome.Kind = replacement.Kind
		outcome.Body = replacement.Body
		if replacement.Kind == FactUnsettled {
			outcome.Status = TrialInconclusive
			stats.Inconclusive++
			continue
		}
		outcome.Status = TrialSettled
		stats.Settled++
	}
	return stats, nil
}
