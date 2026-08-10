package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Rebuild discards and reconstructs both materialized views solely by replaying
// the immutable event journal. The replacement happens in one transaction, so
// readers never observe a half-rebuilt graph.
func (s *Store) Rebuild() error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("rebuild: %w", err)
	}
	defer tx.Rollback()

	events, err := readEvents(tx)
	if err != nil {
		return fmt.Errorf("rebuild: %w", err)
	}
	if len(events) == 0 {
		return fmt.Errorf("rebuild: event journal has no spine event")
	}
	if _, err := tx.Exec(`DELETE FROM graph_fts`); err != nil {
		return fmt.Errorf("rebuild graph index: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM edges`); err != nil {
		return fmt.Errorf("rebuild edges: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM nodes`); err != nil {
		return fmt.Errorf("rebuild nodes: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM messages_fts`); err != nil {
		return fmt.Errorf("rebuild conversation index: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM messages`); err != nil {
		return fmt.Errorf("rebuild messages: %w", err)
	}
	// Sessions are derived from the messages that name them, so they are
	// discarded with the messages and minted again by the replay.
	if _, err := tx.Exec(`DELETE FROM sessions`); err != nil {
		return fmt.Errorf("rebuild sessions: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM agent_questions`); err != nil {
		return fmt.Errorf("rebuild agent questions: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM commands`); err != nil {
		return fmt.Errorf("rebuild commands: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM usage`); err != nil {
		return fmt.Errorf("rebuild usage: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM task_budgets`); err != nil {
		return fmt.Errorf("rebuild task budgets: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM surprises`); err != nil {
		return fmt.Errorf("rebuild surprises: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM self_inquiry_lines`); err != nil {
		return fmt.Errorf("rebuild self inquiry lines: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM self_receipts`); err != nil {
		return fmt.Errorf("rebuild self receipts: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM charters_fts`); err != nil {
		return fmt.Errorf("rebuild charter index: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM charters`); err != nil {
		return fmt.Errorf("rebuild charters: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM services_fts`); err != nil {
		return fmt.Errorf("rebuild service index: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM services`); err != nil {
		return fmt.Errorf("rebuild services: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM scope_aliases`); err != nil {
		return fmt.Errorf("rebuild scope aliases: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM question_practices`); err != nil {
		return fmt.Errorf("rebuild question practices: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM facts`); err != nil {
		return fmt.Errorf("rebuild facts: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM facts_fts`); err != nil {
		return fmt.Errorf("rebuild facts index: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM retrospective_watermark`); err != nil {
		return fmt.Errorf("rebuild retrospective watermark: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM resident_watermarks`); err != nil {
		return fmt.Errorf("rebuild resident watermarks: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM meta_parameters`); err != nil {
		return fmt.Errorf("rebuild meta parameters: %w", err)
	}
	for _, event := range events {
		if err := replayEvent(tx, event); err != nil {
			return fmt.Errorf("replay event %d (%s): %w", event.Seq, event.Kind, err)
		}
	}

	var roots, spine int
	if err := tx.QueryRow(`SELECT COUNT(*), COUNT(*) FILTER (WHERE id = ?) FROM nodes WHERE parent_id IS NULL`, RootID).Scan(&roots, &spine); err != nil {
		return fmt.Errorf("validate rebuilt spine: %w", err)
	}
	if roots != 1 || spine != 1 {
		return fmt.Errorf("validate rebuilt spine: got %d roots (%d permanent)", roots, spine)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("rebuild: %w", err)
	}
	return nil
}

func readEvents(tx *sql.Tx) ([]Event, error) {
	rows, err := tx.Query(`SELECT seq, ts, node_id, kind, payload FROM events ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]Event, 0)
	for rows.Next() {
		var event Event
		var timestamp, payload string
		if err := rows.Scan(&event.Seq, &timestamp, &event.NodeID, &event.Kind, &payload); err != nil {
			return nil, err
		}
		parsed, err := parseTime(timestamp)
		if err != nil {
			return nil, fmt.Errorf("parse event %d time: %w", event.Seq, err)
		}
		event.Time = parsed
		event.Payload = json.RawMessage(payload)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func replayEvent(tx *sql.Tx, event Event) error {
	switch event.Kind {
	case EventSpineCreated:
		var payload spinePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		_, err := tx.Exec(`
			INSERT INTO nodes (
			    id, parent_id, brief, stage, status, origin, session_id,
			    intent, created_seq, created_order, updated_seq, started_at
			) VALUES (?, NULL, ?, 0, ?, ?, ?, ?, ?, 0, ?, ?)`,
			payload.ID, payload.Brief, Running, payload.Provenance.Origin,
			nullIfEmpty(payload.Provenance.SessionID), payload.Provenance.Intent,
			event.Seq, event.Seq, formatTime(event.Time))
		if err != nil {
			return err
		}
		return refreshGraphFTS(tx, payload.ID)

	case EventSpineRepaired:
		return applySpineRepair(tx, event.Seq, event.Time)

	case EventServicePromoted:
		var payload servicePromotedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyServicePromoted(tx, payload, event.Seq)
	case EventServiceAdopted:
		var payload serviceAdoptedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyServiceAdoptedPayload(tx, event.NodeID, payload, event.Seq)
	case EventServiceStopped:
		return applyServiceStatus(tx, event.NodeID, ServiceStopped, event.Seq, 0)
	case EventServiceFailed:
		var payload serviceFailedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyServiceStatus(tx, event.NodeID, ServiceFailed, event.Seq, payload.RestartCount)
	case EventServiceRestarted:
		var payload serviceRestartedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyServiceRestarted(tx, event.NodeID, payload, event.Seq)
	case EventServiceRested:
		var payload serviceFailedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyServiceStatus(tx, event.NodeID, ServiceResting, event.Seq, payload.RestartCount)

	case EventSubtreeSpliced:
		var payload splicedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applySpliceView(tx, payload, event.Seq)

	case EventNodeClaimed:
		var payload claimPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return replayUpdate(tx, event.NodeID, `
			UPDATE nodes
			SET status = ?, owner = ?, claim_token = ?, attempt = attempt + 1, updated_seq = ?
			WHERE id = ?`, Claimed, payload.Owner, payload.Token, event.Seq, event.NodeID)

	case EventNodeStarted:
		var payload claimPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return replayUpdate(tx, event.NodeID, `
			UPDATE nodes
			SET status = ?, owner = ?, claim_token = ?, started_at = ?, updated_seq = ?
			WHERE id = ?`, Running, payload.Owner, payload.Token,
			formatTime(event.Time), event.Seq, event.NodeID)

	case EventNodeCompleted:
		var payload completePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		if err := replayUpdate(tx, event.NodeID, `
			UPDATE nodes
			SET status = ?, owner = ?, claim_token = ?, summary = ?, finished_at = ?, updated_seq = ?
			WHERE id = ?`, Done, payload.Owner, payload.Token, payload.Summary,
			formatTime(event.Time), event.Seq, event.NodeID); err != nil {
			return err
		}
		return refreshGraphFTS(tx, event.NodeID)

	case EventNodeFailed:
		var payload failPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return replayUpdate(tx, event.NodeID, `
			UPDATE nodes
			SET status = ?, owner = ?, claim_token = ?, error = ?, finished_at = ?, updated_seq = ?
			WHERE id = ?`, Failed, payload.Owner, payload.Token, payload.Message,
			formatTime(event.Time), event.Seq, event.NodeID)

	case EventNodeReleased:
		var payload releasePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return replayUpdate(tx, event.NodeID, `
			UPDATE nodes
			SET status = ?, owner = '', claim_token = ?, started_at = NULL, updated_seq = ?
			WHERE id = ?`, Pending, payload.NextToken, event.Seq, event.NodeID)

	case EventSubtreeFolded:
		var payload foldPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		pointers, err := json.Marshal(payload.Pointers)
		if err != nil {
			return err
		}
		return applyFoldView(tx, event.NodeID, payload.Digest, string(pointers), event.Seq)

	case EventEdgeAdded:
		var payload edgeAddedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyEdgeAddedView(tx, payload.From, payload.To, payload.Kind, event.Seq)

	case EventEdgeRemoved:
		var payload edgeRemovedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyEdgeRemovedView(tx, payload.From, payload.To, payload.Kind)

	case EventNodeAmended:
		var payload nodeAmendedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyNodeAmendedView(tx, event.NodeID, payload.Brief, payload.Title, event.Seq)

	case EventNodeReparented:
		var payload nodeReparentedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyNodeReparentedView(tx, event.NodeID, payload.Parent, event.Seq)

	case EventNodeCancelled:
		var payload nodeCancelledPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyNodeCancelledView(tx, event.NodeID, payload.Reason, payload.Partial, event.Seq, formatTime(event.Time))

	case EventNodeCancelRequested:
		if err := decodeNodeControlPayload(event.Payload); err != nil {
			return err
		}
		return applyNodeCancelRequestedView(tx, event.NodeID, event.Seq)

	case EventNodeHeld, EventNodeResumed:
		if err := decodeNodeControlPayload(event.Payload); err != nil {
			return err
		}
		return applyNodeHoldView(tx, event.NodeID, event.Kind == EventNodeHeld, event.Seq)

	case EventNodePriorityChanged:
		var payload nodePriorityPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyNodePriorityView(tx, event.NodeID, payload.Priority, event.Seq)

	case EventNodeWorkerChanged:
		var payload nodeWorkerPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyNodeWorkerView(tx, event.NodeID, payload.Subharness, event.Seq)

	case EventMessagePosted:
		var payload messagePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyMessageView(tx, payload, event.Seq, event.Time)

	case EventCommandRequested:
		var payload commandPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyCommandView(tx, payload, event.Seq, event.Time)

	case EventCommandResolved:
		var payload commandResolvedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyCommandResolution(tx, payload, event.Seq)

	case EventSeenTouched:
		// Seen watermarks are journal-native. Rebuild validates their payload;
		// LastSeen reads the newest event directly.
		var payload seenPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		if strings.TrimSpace(payload.Surface) == "" ||
			(payload.State != SeenAttached && payload.State != SeenDetached) {
			return fmt.Errorf("invalid seen watermark")
		}
		return nil

	case EventAgentQuestionQueued:
		var payload agentQuestionPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyAgentQuestionView(tx, payload, event.Seq, event.Time)

	case EventAgentQuestionSurfaced:
		var payload agentQuestionSurfacedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyAgentQuestionSurfaced(tx, payload, event.Seq, event.Time)

	case EventAgentQuestionResolved:
		var payload agentQuestionResolvedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyAgentQuestionResolution(tx, payload, event.Seq, event.Time)

	case EventStandingWatchOffered, EventStandingWatchEnabled,
		EventStandingWatchDeclined, EventStandingWatchStoodDown, EventStandingWatchPass:
		// Standing-watch state is journal-native. Validate it on replay; status
		// and the never-ask-twice gate read event order directly.
		return decodeStandingWatchEvent(event.Kind, event.Payload)

	case EventUsageRecorded:
		var payload NodeUsage
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyUsageView(tx, payload, event.Seq, event.Time)

	case EventSurpriseRecorded:
		var payload NodeSurprise
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applySurpriseView(tx, payload, event.Seq, event.Time)

	case EventSelfReceipt:
		var payload SelfReceipt
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applySelfReceiptView(tx, payload, event.Seq, event.Time)

	case EventSelfInquiryRetired:
		var payload SelfInquiryRetirement
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applySelfInquiryRetirement(tx, payload, event.Seq)

	case EventRailRaised:
		// Rail raises have no materialized view: their event timestamps define
		// "today", so replay only validates the policy record.
		var payload RailAdjustment
		return json.Unmarshal(event.Payload, &payload)

	case EventTaskCeilingSet:
		var payload TaskCeiling
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyTaskCeilingView(tx, payload, event.Seq, event.Time)

	case EventTaskRailAsked:
		// The ask has no materialized view: its own sequence is the marker, and
		// replay only validates that the record decodes.
		var payload TaskRailAsk
		return json.Unmarshal(event.Payload, &payload)

	case EventOverrunDeferred:
		var payload DeferredOverrun
		return json.Unmarshal(event.Payload, &payload)

	case EventOverrunResumed:
		// Deferred continuations are journal-native; replay validates both sides
		// while PendingOverruns derives their current state from event order.
		var payload overrunResumed
		return json.Unmarshal(event.Payload, &payload)

	case EventDeliveryGate:
		// Gate evidence is journal-native and has no materialized view. Decode it
		// during reconstruction so a corrupt payload still fails loudly.
		var payload DeliveryGate
		return json.Unmarshal(event.Payload, &payload)

	case EventFactLearned:
		var payload factPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyFactView(tx, payload, event.Seq, event.Time)

	case EventFactActivated:
		var payload factActivatedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyFactActivation(tx, payload)

	case EventFactSuperseded:
		var payload factSupersededPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyFactSupersession(tx, payload, event.Seq)

	case EventFactInjected:
		var payload factInjectionPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return replayFactInjection(tx, event.NodeID, payload)

	case EventFactQuarantined:
		var payload factStatusPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		if !validFactChangeOrigin(payload.Origin) {
			return fmt.Errorf("invalid fact quarantine origin %q", payload.Origin)
		}
		return applyFactQuarantine(tx, payload, event.Seq)

	case EventFactRestored:
		var payload factStatusPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		if !validFactChangeOrigin(payload.Origin) {
			return fmt.Errorf("invalid fact restoration origin %q", payload.Origin)
		}
		return applyFactRestore(tx, payload, event.Seq)

	case EventQuestionStatusChanged:
		var payload questionStatusPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyQuestionStatus(tx, payload, event.Seq)

	case EventQuestionPracticeStarted:
		var payload QuestionPracticeStarted
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyQuestionPracticeStarted(tx, payload, event.Seq)

	case EventQuestionPracticeCompleted:
		var payload questionPracticeCompleted
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyQuestionPracticeCompleted(tx, payload, event.Seq)

	case EventScopeAliased:
		var payload scopeAliasedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyScopeAlias(tx, payload, event.Seq)

	case EventRetrospectiveCheckpointed:
		var payload retrospectiveCheckpointPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyRetrospectiveCheckpoint(tx, payload, event.Seq, event.Time)

	case EventResidentWatermarked:
		var payload residentWatermarkPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyResidentWatermark(tx, payload, event.Seq, event.Time)

	case EventAssumedWithDefault:
		var payload assumedWithDefaultPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		if payload.Category == "" || strings.TrimSpace(payload.Default) == "" {
			return fmt.Errorf("invalid assumed-with-default event")
		}
		return nil

	case EventPlanGraph:
		// A journaled plan has no materialized view on purpose — it is read by
		// node id, the newest event is the answer, and the event itself remains
		// the source of truth on rebuild. Validating the payload rather than
		// falling through to the silent default is what keeps that a stated
		// boundary instead of an accident nobody would notice.
		var payload PlanGraph
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		if len(payload.Graph) == 0 {
			return fmt.Errorf("invalid plan-graph event")
		}
		return nil

	case EventParameterChanged:
		var payload ParameterChange
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyParameterChange(tx, payload, event.Seq)

	case EventCharterCreated, EventCharterRevised, EventCharterStatusChanged,
		EventCharterWatchAdvanced, EventCharterWoken, EventSentinelChecked,
		EventCharterFired, EventCharterFiringBlocked, EventCharterFiringDeferred,
		EventCharterProposalDeclined, EventCharterFiringProposed, EventCharterFiringDeclined,
		EventCharterFiringReviewed, EventCharterPromoted, EventCharterDemoted:
		return replayCharterEvent(tx, event)

	default:
		// The journal is expected to gain accounting and artifact events that do
		// not affect the materialized views. Unknown kinds therefore remain
		// durable but are intentionally a no-op during reconstruction.
		return nil
	}
}

func replayUpdate(tx *sql.Tx, nodeID, statement string, args ...any) error {
	result, err := tx.Exec(statement, args...)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("event targets missing node %q", nodeID)
	}
	return nil
}
