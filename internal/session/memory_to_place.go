package session

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/reflex"
)

// A decided memory becomes a knows line through the same decider the session
// memory store already uses. reflex.Decide answers add, update, supersede, or
// skip; the place writer accepts those words (update refines, supersede
// replaces) and writes a line. This file does not also write the memory row.
// The caller that wants both calls the memory door and this door.

// reflexPlaceDecider is the store memory decider, closed over the candidate
// the extractor already returned. The knows line does not store applicability
// or rationale, but the decider's prompt still reads them, so the original
// extract result is what gets decided — not a title and a body alone.
type reflexPlaceDecider struct {
	ctx       context.Context
	completer reflex.Completer
	candidate reflex.ExtractResult
}

func (d reflexPlaceDecider) Decide(_ placegraph.MemoryCandidate, neighbors []placegraph.MemoryNeighbor) (placegraph.MemoryVerdict, error) {
	near := make([]reflex.Neighbor, len(neighbors))
	for i, n := range neighbors {
		near[i] = reflex.Neighbor{ID: n.ID, Title: n.Title, Text: n.Text}
	}
	ctx := d.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	decided, err := reflex.Decide(ctx, d.completer, d.candidate, near)
	if err != nil {
		return placegraph.MemoryVerdict{}, err
	}
	return placegraph.MemoryVerdict{
		Op:       decided.Op,
		TargetID: decided.TargetID,
		Title:    decided.Title,
		Text:     decided.Text,
	}, nil
}

// MemoryToPlace settles candidate for placeID and writes the knows line the
// decider names. A nil client, a nil graph, a blank place, or an extract that
// found nothing to keep (Mem is not 1) writes nothing: there is no second
// writer that adds a line the decider did not settle. chatID and at are the
// said-in-chat provenance; unknown values stay absent.
func MemoryToPlace(ctx context.Context, client reflex.Completer, graph *placegraph.Store, placeID, chatID string, at time.Time, candidate reflex.ExtractResult) (placegraph.Promotion, error) {
	if client == nil || graph == nil || strings.TrimSpace(placeID) == "" || candidate.Mem != 1 {
		return placegraph.Promotion{Skipped: true}, nil
	}
	return graph.PromoteMemory(placeID, placegraph.MemoryCandidate{
		Title:  candidate.Title,
		Text:   candidate.Text,
		ChatID: chatID,
		At:     at,
	}, reflexPlaceDecider{ctx: ctx, completer: client, candidate: candidate})
}

// LearnPlaceAnswers writes the learned line when one kind has been answered
// the same way three times in the place. A nil graph is a wiring error, not
// an empty place: the caller asked to record answers and there is nowhere to
// put them.
func LearnPlaceAnswers(graph *placegraph.Store, placeID string, answers []placegraph.KindAnswer) (placegraph.Promotion, error) {
	if graph == nil {
		return placegraph.Promotion{}, errors.New("session: no place graph to learn into")
	}
	return graph.LearnAnswers(placeID, answers)
}
