package store

import (
	"encoding/json"
	"fmt"
	"time"
)

// AgeFactsBounded is AgeFacts with the journal read bounded to the two event
// kinds that can record a fact access, in one pass for the whole notebook
// instead of one whole-journal read per belief. The retention verdict for every
// fact is the one AgeFacts reaches; only the reading is different.
//
// The unbounded original grows with tenure forever — the resident calls this on
// a timer, and its cost must follow the notebook, not the journal.
func (s *Store) AgeFactsBounded(now time.Time) (int, error) {
	facts, err := s.ActiveFacts("", 10000)
	if err != nil {
		return 0, err
	}
	aging := make([]Fact, 0, len(facts))
	for _, fact := range facts {
		if fact.Kind == FactTrait || fact.Kind == FactQuestion {
			continue
		}
		aging = append(aging, fact)
	}
	if len(aging) == 0 {
		return 0, nil
	}
	accesses, err := s.factAccesses(aging)
	if err != nil {
		return 0, err
	}
	// One belief kind has one learned decay, and quarantine does not change the
	// revisit intervals it is estimated from, so it is read once per kind.
	decays := make(map[FactKind]float64, 4)
	threshold := s.Parameter(ParameterBeliefRetentionThreshold)
	aged := 0
	for _, fact := range aging {
		decay, known := decays[fact.Kind]
		if !known {
			decay, _, err = s.LearnedDecay(fact.Kind)
			if err != nil {
				continue
			}
			decays[fact.Kind] = decay
		}
		if BaseLevelActivation(append([]time.Time{fact.Time}, accesses[fact.Seq]...), now, decay) >= threshold {
			continue
		}
		if err := s.QuarantineFact(fact.Seq, 0, FactOriginConsolidator); err != nil {
			return aged, err
		}
		aged++
	}
	return aged, nil
}

// factAccesses collects every journaled revisit of the given beliefs. The
// window opens at the oldest belief in the batch — nothing before a fact was
// learned can be an access to it — and only the two kinds that carry a fact
// pointer are read at all.
func (s *Store) factAccesses(facts []Fact) (map[int64][]time.Time, error) {
	wanted := make(map[int64]bool, len(facts))
	oldest := int64(0)
	for _, fact := range facts {
		wanted[fact.Seq] = true
		if oldest == 0 || fact.Seq < oldest {
			oldest = fact.Seq
		}
	}
	rows, err := s.db.Query(`SELECT seq, ts, kind, payload FROM events
		WHERE seq > ? AND kind IN (?, ?) ORDER BY seq`,
		oldest, EventFactInjected, EventFactLearned)
	if err != nil {
		return nil, fmt.Errorf("read fact accesses: %w", err)
	}
	defer rows.Close()
	accesses := make(map[int64][]time.Time, len(facts))
	for rows.Next() {
		var seq int64
		var kind, timestamp, payload string
		if err := rows.Scan(&seq, &timestamp, &kind, &payload); err != nil {
			return nil, fmt.Errorf("read fact accesses: %w", err)
		}
		at, err := parseTime(timestamp)
		if err != nil {
			return nil, fmt.Errorf("parse event %d time: %w", seq, err)
		}
		// One access per pointer list, not per repetition inside it: an
		// injection names a belief once however many times it lists it, and an
		// unsettled pair names it once per approach.
		record := func(pointers []int64) {
			counted := make(map[int64]bool, len(pointers))
			for _, factSeq := range pointers {
				// A belief cannot be accessed by an event that predates it.
				if !wanted[factSeq] || factSeq >= seq || counted[factSeq] {
					continue
				}
				counted[factSeq] = true
				accesses[factSeq] = append(accesses[factSeq], at)
			}
		}
		switch EventKind(kind) {
		case EventFactInjected:
			var injection factInjectionPayload
			if json.Unmarshal([]byte(payload), &injection) != nil {
				continue
			}
			record(injection.FactSeqs)
		case EventFactLearned:
			var learned factPayload
			if json.Unmarshal([]byte(payload), &learned) != nil || learned.Unsettled == nil {
				continue
			}
			for _, approach := range learned.Unsettled.Approaches {
				record(approach.Evidence)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read fact accesses: %w", err)
	}
	return accesses, nil
}
