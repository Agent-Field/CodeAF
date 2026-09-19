package workspace

import (
	"fmt"
	"unicode/utf8"
)

const (
	ParticipantActive   = "active"
	ParticipantPaused   = "paused"
	ParticipantArchived = "archived"
	ActorKindChat       = "chat"
	ActorKindRole       = "role"
	ActorKindFolder     = "folder"
	ScopeSelected       = "selected"
	ScopeFolderDynamic  = "folder-dynamic"
	DeliveryPending     = "pending"
	DeliveryAccepted    = "accepted"
	DeliveryRecorded    = "recorded"
	DeliveryProcessed   = "processed"
	PatternDirect       = "direct"
	PatternFanout       = "fan-out"
	PatternDiscussion   = "discussion"
)

const participantColumns = "id,discussion_id,actor_id,kind,role,source_chat_id,status,scope_kind,folder_id,snapshot_json,origin,actor,created_at,updated_at"
const deliveryColumns = "id,cause_id,from_chat_id,to_chat_id,pattern,state,body,origin,actor_id,discussion_id,idempotency_key,attempt,created_at,accepted_at,recorded_at,processed_at,updated_at"

// Participant is one software-minted actor in a discussion. Scope lives on the
// coordinator row; inviting never writes a grant.
type Participant struct {
	ID, DiscussionID, ActorID, Kind, Role, SourceChatID, Status string
	ScopeKind, FolderID, SnapshotJSON                           string
	Origin, Actor                                               string
	CreatedAt, UpdatedAt                                        string
}

// Delivery is one durable envelope. Direct, fan-out and discussion rows share
// this table: fan-out mints one id per recipient and reuses CauseID.
type Delivery struct {
	ID, CauseID, FromChatID, ToChatID, Pattern, State, Body   string
	Origin, ActorID, DiscussionID, IdempotencyKey             string
	Attempt                                                   int
	CreatedAt, AcceptedAt, RecordedAt, ProcessedAt, UpdatedAt string
}

func validParticipantStatus(status string) bool {
	return status == ParticipantActive || status == ParticipantPaused || status == ParticipantArchived
}

func validActorKind(kind string) bool {
	return kind == ActorKindChat || kind == ActorKindRole || kind == ActorKindFolder
}

func validScopeKind(kind string) bool {
	return kind == "" || kind == ScopeSelected || kind == ScopeFolderDynamic
}

func validDeliveryPattern(pattern string) bool {
	return pattern == PatternDirect || pattern == PatternFanout || pattern == PatternDiscussion
}

func knownAckState(state string) bool {
	return state == DeliveryPending || deliveryAckColumn(state) != ""
}

func nextDeliveryState(state string) string {
	switch state {
	case DeliveryPending:
		return DeliveryAccepted
	case DeliveryAccepted:
		return DeliveryRecorded
	case DeliveryRecorded:
		return DeliveryProcessed
	default:
		return ""
	}
}

func deliveryAckColumn(state string) string {
	switch state {
	case DeliveryAccepted:
		return "accepted_at"
	case DeliveryRecorded:
		return "recorded_at"
	case DeliveryProcessed:
		return "processed_at"
	default:
		return ""
	}
}

func validOptionalText(s string, limit int) bool {
	return len(s) <= limit && utf8.ValidString(s)
}

func skipStateError(from, to string) error {
	return fmt.Errorf("%w: delivery cannot skip state from %s to %s", ErrInvalid, from, to)
}
