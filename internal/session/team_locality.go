package session

import (
	"context"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/teams"
)

type teamUnreached struct {
	member   teams.Member
	computer string
}

// Only remote identities or a successful ownership reading can refuse local
// work. Failed discovery leaves the existing local delivery path available.
func (a *Agent) teamDeliveryTargets(ctx context.Context, targets []teams.Member) ([]teams.Member, []teamUnreached) {
	if !config.TeamsShareAt(a.config.teamProfile()) {
		return targets, nil
	}
	names := map[string]string{}
	if a.config.TeamComputers != nil {
		got, err := a.config.TeamComputers(ctx, targets)
		if err != nil {
			teams.SharingError(a.config.teamProfile(), err)
		} else {
			names = got
		}

	}
	var local []teams.Member
	var remote []teamUnreached
	for _, member := range targets {
		name := names[member.Key]
		if name == "" {
			_, sentinel := teams.RemoteCell(member.Key)
			if (sentinel || teams.OnOtherComputer(member.Key)) && teamCursorDevice() != "" {
				name = "the other computer"
			}
		}
		if name == "" {
			local = append(local, member)
		} else {
			remote = append(remote, teamUnreached{member, name})
		}
	}
	return local, remote
}

func (a *Agent) teamDeliveryRefusal(ctx context.Context, targets []teams.Member) string {
	_, remote := a.teamDeliveryTargets(ctx, targets)
	if len(remote) > 0 {
		return teams.DeliveryRefusal(remote[0].member, remote[0].computer)
	}
	return ""
}

// A mixed audience still receives one local line. The receipt distinguishes
// remote recipients without changing the delivery rules or rewriting addresses.
func (a *Agent) teamSendPreflight(ctx context.Context, targets []teams.Member) (string, string, []teams.Member) {
	local, remote := a.teamDeliveryTargets(ctx, targets)
	if len(local) == 0 && len(remote) > 0 {
		return teams.DeliveryRefusal(remote[0].member, remote[0].computer), "", nil
	}
	var missed []string
	for _, r := range remote {
		who := r.member.Word
		if r.member.Handle != "" {
			who = "@" + r.member.Handle
		}
		if who == "" {
			who = "This chat"
		}
		missed = append(missed, who+" (on "+r.computer+")")
	}
	if len(missed) > 0 {
		return "", " Not reached: " + strings.Join(missed, ", ") + ".", local
	}
	return "", "", local
}
