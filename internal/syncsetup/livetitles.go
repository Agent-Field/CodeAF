package syncsetup

import (
	"context"
	"strings"

	"github.com/Agent-Field/codeaf/internal/filesdoor"
)

func (s *Sync) peerFilesSource(id, name string) *filesdoor.Source {
	return filesdoor.NewSource(filesdoor.SourceConfig{
		Service:         s.Relay,
		Identity:        s.Identity,
		Device:          s.Device,
		TargetID:        id,
		TargetName:      name,
		RetryWithin:     liveRetryWithin,
		NoMachineWithin: liveNoMachineWithin,
	})
}

func (s *Sync) serveChatTitle(id string) string {
	if !s.LiveTransfer() {
		return ""
	}
	c, _, ok := liveChatFinder(s.Home, id)
	if !ok {
		return ""
	}
	return strings.TrimSpace(titleOf(c.Root))
}

// PeerChatTitles reads a bounded batch of names from the paired holder while
// storage is off. Callers may keep listing stale or unnamed rows if it is
// offline or runs an older engine.
func (s *Sync) PeerChatTitles(ctx context.Context, holder string, ids []string) (map[string]string, error) {
	if !s.LiveTransfer() {
		return nil, nil
	}
	source := s.peerFilesSource(holder, "")
	defer source.Close()
	return source.ChatTitles(ctx, ids)
}
