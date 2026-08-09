// Package revert ports src/session/revert.ts:1-170 from swe-pro commit
// 3b25a1a. Snapshot-backed file restoration is the same explicit no-op as the
// source; revert records a message boundary and cleanup removes forward data.
package revert

import (
	"context"

	"github.com/Agent-Field/swe-pro-go/internal/bus"
	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
)

type Input struct {
	SessionID string  `json:"sessionID"`
	MessageID string  `json:"messageID"`
	PartID    *string `json:"partID,omitempty"`
}

type RevertInfo struct {
	MessageID string  `json:"messageID"`
	PartID    *string `json:"partID,omitempty"`
	Snapshot  *string `json:"snapshot,omitempty"`
	Diff      *string `json:"diff,omitempty"`
}

type Summary struct {
	Additions float64 `json:"additions"`
	Deletions float64 `json:"deletions"`
	Files     int     `json:"files"`
}

type Session struct {
	ID      string      `json:"id"`
	Revert  *RevertInfo `json:"revert,omitempty"`
	Summary Summary     `json:"summary"`
}

type SetRevertInput struct {
	SessionID string
	Revert    RevertInfo
	Summary   Summary
}

type Sessions interface {
	Messages(ctx context.Context, sessionID string) ([]msgmodel.WithParts, error)
	Get(ctx context.Context, sessionID string) (Session, error)
	SetRevert(ctx context.Context, input SetRevertInput) error
	ClearRevert(ctx context.Context, sessionID string) error
}

type RunState interface {
	AssertNotBusy(sessionID string) error
}

type SummaryComputer interface {
	ComputeDiff(
		ctx context.Context, messages []msgmodel.WithParts,
	) ([]msgmodel.FileDiff, error)
}

type DiffStore interface {
	Write(key []string, content any) error
}

type Publisher interface {
	Publish(bus.Definition, any, ...bus.PublishOptions)
}

type Sync interface {
	Run(ctx context.Context, event string, properties any) error
}

type Dependencies struct {
	Sessions Sessions
	State    RunState
	Summary  SummaryComputer
	Storage  DiffStore
	Bus      Publisher
	Sync     Sync
}

type DiffProperties struct {
	SessionID string              `json:"sessionID"`
	Diff      []msgmodel.FileDiff `json:"diff"`
}

var DiffEvent = bus.Define("session.diff", DiffProperties{})

type Service struct {
	deps Dependencies
}

func New(dependencies Dependencies) *Service {
	return &Service{deps: dependencies}
}

func (service *Service) Revert(ctx context.Context, input Input) (Session, error) {
	if err := service.deps.State.AssertNotBusy(input.SessionID); err != nil {
		return Session{}, err
	}
	all, err := service.deps.Sessions.Messages(ctx, input.SessionID)
	if err != nil {
		return Session{}, err
	}
	session, err := service.deps.Sessions.Get(ctx, input.SessionID)
	if err != nil {
		return Session{}, err
	}

	var lastUser msgmodel.Info
	var boundary *RevertInfo
	for _, message := range all {
		if message.Info.MessageRole() == "user" {
			lastUser = message.Info
		}
		remaining := make([]msgmodel.Part, 0, len(message.Parts))
		for _, part := range message.Parts {
			if boundary != nil {
				continue
			}
			targetMessage := message.Info.MessageID() == input.MessageID &&
				input.PartID == nil
			targetPart := input.PartID != nil && part.Base().ID == *input.PartID
			if targetMessage || targetPart {
				partID := input.PartID
				hasContent := false
				for _, prior := range remaining {
					if prior.PartType() == msgmodel.PartTypeText ||
						prior.PartType() == msgmodel.PartTypeTool {
						hasContent = true
						break
					}
				}
				if !hasContent {
					partID = nil
				}
				messageID := message.Info.MessageID()
				if partID == nil && lastUser != nil {
					messageID = lastUser.MessageID()
				}
				boundary = &RevertInfo{MessageID: messageID, PartID: partID}
			}
			remaining = append(remaining, part)
		}
	}
	if boundary == nil {
		return session, nil
	}

	if session.Revert != nil && session.Revert.Snapshot != nil {
		boundary.Snapshot = session.Revert.Snapshot
	}
	selected := make([]msgmodel.WithParts, 0, len(all))
	for _, message := range all {
		if message.Info.MessageID() >= boundary.MessageID {
			selected = append(selected, message)
		}
	}
	diffs, err := service.deps.Summary.ComputeDiff(ctx, selected)
	if err != nil {
		return Session{}, err
	}
	if service.deps.Storage != nil {
		_ = service.deps.Storage.Write(
			[]string{"session_diff", input.SessionID}, diffs,
		)
	}
	if service.deps.Bus != nil {
		service.deps.Bus.Publish(DiffEvent, DiffProperties{
			SessionID: input.SessionID, Diff: diffs,
		})
	}
	summary := Summary{Files: len(diffs)}
	for _, diff := range diffs {
		summary.Additions += float64(diff.Additions)
		summary.Deletions += float64(diff.Deletions)
	}
	if err := service.deps.Sessions.SetRevert(ctx, SetRevertInput{
		SessionID: input.SessionID, Revert: *boundary, Summary: summary,
	}); err != nil {
		return Session{}, err
	}
	return service.deps.Sessions.Get(ctx, input.SessionID)
}

func (service *Service) Unrevert(
	ctx context.Context, sessionID string,
) (Session, error) {
	if err := service.deps.State.AssertNotBusy(sessionID); err != nil {
		return Session{}, err
	}
	session, err := service.deps.Sessions.Get(ctx, sessionID)
	if err != nil || session.Revert == nil {
		return session, err
	}
	if err := service.deps.Sessions.ClearRevert(ctx, sessionID); err != nil {
		return Session{}, err
	}
	return service.deps.Sessions.Get(ctx, sessionID)
}

func (service *Service) Cleanup(ctx context.Context, session Session) error {
	if session.Revert == nil {
		return nil
	}
	messages, err := service.deps.Sessions.Messages(ctx, session.ID)
	if err != nil {
		return err
	}
	messageID := session.Revert.MessageID
	remove := []msgmodel.WithParts{}
	var target *msgmodel.WithParts
	for index := range messages {
		message := &messages[index]
		if message.Info.MessageID() < messageID {
			continue
		}
		if message.Info.MessageID() > messageID {
			remove = append(remove, *message)
			continue
		}
		if session.Revert.PartID != nil {
			target = message
			continue
		}
		remove = append(remove, *message)
	}
	for _, message := range remove {
		if err := service.deps.Sync.Run(ctx, msgmodel.EventMessageRemoved, msgmodel.RemovedEvent{
			SessionID: session.ID, MessageID: message.Info.MessageID(),
		}); err != nil {
			return err
		}
	}
	if session.Revert.PartID != nil && target != nil {
		partID := *session.Revert.PartID
		index := -1
		for i, part := range target.Parts {
			if part.Base().ID == partID {
				index = i
				break
			}
		}
		if index >= 0 {
			removeParts := target.Parts[index:]
			target.Parts = target.Parts[:index]
			for _, part := range removeParts {
				if err := service.deps.Sync.Run(
					ctx, msgmodel.EventMessagePartRemoved, msgmodel.PartRemovedEvent{
						SessionID: session.ID, MessageID: target.Info.MessageID(),
						PartID: part.Base().ID,
					},
				); err != nil {
					return err
				}
			}
		}
	}
	return service.deps.Sessions.ClearRevert(ctx, session.ID)
}
