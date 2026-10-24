package clusterstate

import "errors"

// SchemaVersion versions commands, results and application rows together. A
// member must stop application on an unknown schema, rather than skip an entry.
const SchemaVersion uint16 = 1

// LeaseTTLMillis is shared with the legacy directory without importing clocks.
const LeaseTTLMillis int64 = 90_000

// MaxSafeInteger is the v1 fence ceiling and the limit for numeric JSON values
// that cross JavaScript. V2 uint64 fields instead use decimal strings.
const MaxSafeInteger uint64 = 1<<53 - 1

// These sentinels retain the existing directory error identity through aliases.
var (
	ErrNotFound   = errors.New("directory: not found")
	ErrExists     = errors.New("directory: exists")
	ErrLeaseHeld  = errors.New("directory: lease held by another device")
	ErrFenceStale = errors.New("directory: fence superseded")
	ErrHeadMoved  = errors.New("directory: head moved")
	ErrCAS        = errors.New("directory: compare-and-swap failed")
	ErrSelfRevoke = errors.New("directory: a device cannot revoke itself")
)

// Command binds an original signed request to one identity cluster and durable
// client sequence. Signature authentication and committed authorization belong
// to ingress and apply respectively; decoding alone grants no authority.
type Command struct {
	Certificate       string     `json:"certificate,omitempty"`
	V1                *V1Request `json:"v1_request,omitempty"`
	WorkerIncarnation uint64     `json:"worker_incarnation,omitempty"`
	Schema            uint16     `json:"schema"`
	Cluster           string     `json:"cluster"`
	Actor             string     `json:"actor"`
	Session           string     `json:"session"`
	Sequence          uint64     `json:"sequence"`
	RequestDigest     string     `json:"request_digest"`
	Ack               uint64     `json:"ack,omitempty"`
	Now               int64      `json:"now"`
	Change            Mutation   `json:"change"`
	Signature         []byte     `json:"signature"`
}

// Mutation is a closed union: exactly one field is present. Future packets add
// their typed operations here; unknown fields fail decoding on older members.
// There is no arbitrary JSON payload in which task text or secrets can hide.
type Mutation struct {
	Assign             *Assign             `json:"assign,omitempty"`
	Reassign           *Assign             `json:"reassign,omitempty"`
	ClaimAssignment    *ClaimAssignment    `json:"claim_assignment,omitempty"`
	FinishAssignment   *FinishAssignment   `json:"finish_assignment,omitempty"`
	CancelAssignment   *CancelAssignment   `json:"cancel_assignment,omitempty"`
	EnrollWorker       *EnrollWorker       `json:"enroll_worker,omitempty"`
	RegisterWorker     *RegisterWorker     `json:"register_worker,omitempty"`
	WorkerBeatBatch    *WorkerBeatBatch    `json:"worker_beat_batch,omitempty"`
	DrainWorker        *DrainWorker        `json:"drain_worker,omitempty"`
	RenewHolds         *RenewHolds         `json:"renew_holds,omitempty"`
	PrepareQuiescence  *PrepareQuiescence  `json:"prepare_quiescence,omitempty"`
	CompleteLeadership *CompleteLeadership `json:"complete_leadership,omitempty"`
	RequestLeadership  *RequestLeadership  `json:"request_leadership,omitempty"`
	RestoreAnchors     *RestoreAnchors     `json:"restore_anchors,omitempty"`
	SettleLeadership   *SettleLeadership   `json:"settle_leadership,omitempty"`
	InitMembership     *InitMembership     `json:"init_membership,omitempty"`
	EnrollMember       *EnrollMember       `json:"enroll_member,omitempty"`
	RemoveMember       *RemoveMember       `json:"remove_member,omitempty"`
	SettleMembership   *SettleMembership   `json:"settle_membership,omitempty"`
	RecordUpload       *UploadReceipt      `json:"record_upload,omitempty"`
	AdmitJoin          *AdmitJoin          `json:"admit_join,omitempty"`
	CompleteJoin       *CompleteJoin       `json:"complete_join,omitempty"`
	Rotate             *RotateRequest      `json:"rotate,omitempty"`
	CloseSession       *CloseSession       `json:"close_session,omitempty"`
	DecideJoin         *JoinDecision       `json:"decide_join,omitempty"`
	Create             *CreateCell         `json:"create,omitempty"`
	Acquire            *AcquireCell        `json:"acquire,omitempty"`
	Beat               *BeatCell           `json:"beat,omitempty"`
	BeatBatch          *BeatBatch          `json:"beat_batch,omitempty"`
	Publish            *PublishCell        `json:"publish,omitempty"`
	Release            *ReleaseCell        `json:"release,omitempty"`
	Archive            *CellID             `json:"archive,omitempty"`
	PutDevice          *PutDevice          `json:"put_device,omitempty"`
	Revoke             *DeviceID           `json:"revoke,omitempty"`
	SetVault           *VaultCAS           `json:"set_vault,omitempty"`
	AdvanceTime        *AdvanceTime        `json:"advance_time,omitempty"`
	DeviceSeen         *DeviceSeen         `json:"device_seen,omitempty"`
}

type CellID struct {
	Cell string `json:"cell"`
}

type DeviceID struct {
	Device string `json:"device"`
}

// V1 is protocol provenance supplied by authenticated v1 ingress; v2 request
// bodies must never select it. Old clients retain their upload ordering.
type CreateCell struct {
	V1      bool     `json:"v1,omitempty"`
	Cell    string   `json:"cell"`
	Init    CellInit `json:"init"`
	Receipt string   `json:"receipt"`
}

// AcquireCell always names the observed tuple on v2. The legacy adapter will
// normalize older requests before they become commands in the same log.
type AcquireCell struct {
	Cell   string           `json:"cell"`
	Force  bool             `json:"force"`
	Expect LeaseExpectation `json:"expect"`
	Legacy bool             `json:"legacy,omitempty"`
	V1     bool             `json:"v1,omitempty"`
}

type BeatCell struct {
	Cell string `json:"cell"`
	Beat Beat   `json:"beat"`
}

// BeatBatch has a record envelope so an empty item set cannot collapse the
// operation's presence into a null union member during JSON decoding.
type BeatBatch struct {
	Items []BeatCell `json:"items"`
}

type PublishCell struct {
	V1      bool    `json:"v1,omitempty"`
	Cell    string  `json:"cell"`
	Publish Publish `json:"publish"`
	Receipt string  `json:"receipt"`
}

type ReleaseCell struct {
	Cell  string `json:"cell"`
	Fence uint64 `json:"fence"`
	Quiet bool   `json:"quiet"`
}

type PutDevice struct {
	ID     string `json:"id"`
	Device Device `json:"device"`
}

type VaultCAS struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// AdvanceTime has no host clock dependency: Command.Now supplies its sample.
type AdvanceTime struct{}

type DeviceSeen struct {
	Device string `json:"device"`
}

// Result stores the exact application response for deduplication. Codes carry
// no free-form diagnostic text, so logging a refusal cannot disclose payloads.
// Now and Version are application time/version, never a local clock/Raft index.
type Result struct {
	Assignment *Assignment       `json:"assignment,omitempty"`
	Workers    []WorkerResult    `json:"workers,omitempty"`
	Index      uint64            `json:"index,omitempty"`
	Membership *MembershipResult `json:"membership,omitempty"`
	Schema     uint16            `json:"schema"`
	Code       Code              `json:"code"`
	Now        int64             `json:"now"`
	Version    uint64            `json:"version"`
	View       *CellView         `json:"view,omitempty"`
	Items      []ItemResult      `json:"items,omitempty"`
}

type ItemResult struct {
	Cell string    `json:"cell"`
	Code Code      `json:"code"`
	View *CellView `json:"view,omitempty"`
}

type Code string

const (
	OK             Code = "ok"
	NotFound       Code = "not_found"
	Exists         Code = "exists"
	LeaseHeld      Code = "lease_held"
	FenceStale     Code = "fence_stale"
	HeadMoved      Code = "head_moved"
	CAS            Code = "cas"
	Revoked        Code = "revoked"
	SelfRevoke     Code = "self_revoke"
	Unauthorized   Code = "unauthorized"
	Rotated        Code = "rotated"
	Conflict       Code = "conflict"
	RotationStep   Code = "rotation_step"
	BadGrace       Code = "bad_grace"
	RequestGone    Code = "request_gone"
	AlreadyDecided Code = "already_decided"
	BadRequest     Code = "bad_request"
	Capacity       Code = "capacity"
	UpdateRequired Code = "update_required"
)

// Row encodes one application record for paged reads and streamed snapshots.
// Logical time, visible version and applied index belong to the host's atomic
// application transaction; they must not be hidden in Listing's json:"-" field.
type Row struct {
	Schema   uint16       `json:"schema"`
	Key      string       `json:"key"`
	Identity *IdentityRec `json:"identity,omitempty"`
	Device   *Device      `json:"device,omitempty"`
	Cell     *Cell        `json:"cell,omitempty"`
}

// CloseSession retains a tombstone so an old sequence can never execute again.
type CloseSession struct{}

// RotateRequest uses milliseconds so the shared evaluator needs no time package.
type RotateRequest struct {
	Op      string `json:"op"`
	GraceMS int64  `json:"grace_ms,omitempty"`
}

// JoinDecision records only sealed pairing data. The committed request binds
// its certificate key; outbox delivery follows the authorization transaction.
type JoinDecision struct {
	Code   string `json:"code"`
	Device string `json:"device"`
	Record Device `json:"record"`
	Grant  string `json:"grant"`
	Cert   string `json:"cert"`
	Deny   bool   `json:"deny"`
}
