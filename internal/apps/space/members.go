package space

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (m *members) Authenticate(ctx context.Context, token string) (svc.Caller, error) {
	if m.s.ws == nil {
		return svc.Caller{}, bridge.NotBuilt("members.authenticate")
	}
	if strings.TrimSpace(token) == "" {
		return svc.Caller{}, denied("sign in again")
	}
	w := m.s.ws
	var row memberRow
	w.mu.Lock()
	err := w.tx(ctx, func(tx *sql.Tx) error {
		var member, expires string
		err := tx.QueryRow(`SELECT member, expires FROM sessions WHERE token_hash = ?`, hashSecret(token)).Scan(&member, &expires)
		if errors.Is(err, sql.ErrNoRows) {
			return denied("sign in again")
		}
		if err != nil {
			return err
		}
		at, err := parseStamp(expires)
		if err != nil {
			return err
		}
		if !w.now().Before(at) {
			_, _ = tx.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hashSecret(token))
			return denied("sign in again")
		}
		row, err = memberByID(tx, member)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE members SET last_seen = ? WHERE id = ?`, stamp(w.now()), row.ID)
		return err
	})
	w.mu.Unlock()
	if err != nil {
		return svc.Caller{}, err
	}
	return svc.Caller{Member: row.ID, Space: w.name, WS: svc.WorkspaceRole(row.Role)}, nil
}

func (m *members) List(ctx context.Context, caller svc.Caller) (bridge.MembersListResult, error) {
	if m.s.ws == nil {
		return bridge.MembersListResult{Members: []bridge.Member{personalMember()}, Invites: []bridge.Invite{}}, nil
	}
	if _, err := m.must(ctx, caller.Member); err != nil {
		return bridge.MembersListResult{}, err
	}
	return m.s.ws.snapshot(ctx), nil
}

func (m *members) Invite(ctx context.Context, caller svc.Caller, p bridge.InviteParams) (bridge.Invite, error) {
	w, actor, err := m.actor(ctx, caller)
	if err != nil {
		return bridge.Invite{}, err
	}
	role := svc.WorkspaceRole(p.Role)
	if err := canInvite(svc.WorkspaceRole(actor.Role), role); err != nil {
		return bridge.Invite{}, err
	}
	email := strings.ToLower(strings.TrimSpace(p.Email))
	if email != "" && !strings.Contains(email, "@") {
		return bridge.Invite{}, invalid("use an email address")
	}
	id, err := randomID("inv_")
	if err != nil {
		return bridge.Invite{}, err
	}
	secret, err := randomToken()
	if err != nil {
		return bridge.Invite{}, err
	}
	var out bridge.Invite
	w.mu.Lock()
	err = w.tx(ctx, func(tx *sql.Tx) error {
		if _, err := memberByID(tx, actor.ID); err != nil {
			return err
		}
		expires := w.now().Add(inviteTTL)
		_, err := tx.Exec(`INSERT INTO invites (id, secret_hash, email, role, by_handle, expires, used, cancelled) VALUES (?, ?, ?, ?, ?, ?, 0, 0)`,
			id, hashSecret(secret), email, string(role), actor.Handle, stamp(expires))
		if err != nil {
			return err
		}
		w.secrets[id] = secret
		out = bridge.Invite{ID: id, Email: email, Role: string(role), Link: buildInvite(w.publicURL, id, secret), By: actor.Handle, Expires: expires, State: "pending"}
		return nil
	})
	w.mu.Unlock()
	if err != nil {
		return bridge.Invite{}, err
	}
	target := email
	if target == "" {
		target = "someone"
	}
	m.s.audit(ctx, actor.ID, "members.invite", "invite/"+id, "@"+actor.Handle+" invited "+target+" as "+roleTitle(string(role)))
	m.s.publish(w.snapshot(ctx))
	return out, nil
}

func (m *members) CancelInvite(ctx context.Context, caller svc.Caller, p bridge.InviteRef) (bridge.Empty, error) {
	w, actor, err := m.actor(ctx, caller)
	if err != nil {
		return bridge.Empty{}, err
	}
	if svc.WorkspaceRole(actor.Role) != svc.WorkspaceOwner && svc.WorkspaceRole(actor.Role) != svc.WorkspaceAdmin {
		return bridge.Empty{}, denied("this call needs workspace Owner or Admin")
	}
	id, _, err := splitInviteRef(p.Invite)
	if err != nil {
		return bridge.Empty{}, err
	}
	var email string
	w.mu.Lock()
	err = w.tx(ctx, func(tx *sql.Tx) error {
		row, err := inviteByID(tx, id)
		if err != nil {
			return err
		}
		if row.Used {
			return inviteExpired(row.By)
		}
		_, err = tx.Exec(`UPDATE invites SET cancelled = 1 WHERE id = ?`, id)
		email = row.Email
		delete(w.secrets, id)
		return err
	})
	w.mu.Unlock()
	if err != nil {
		return bridge.Empty{}, err
	}
	target := email
	if target == "" {
		target = "an invite"
	}
	m.s.audit(ctx, actor.ID, "members.invite.cancel", "invite/"+id, "@"+actor.Handle+" cancelled the invite for "+target)
	m.s.publish(w.snapshot(ctx))
	return bridge.Empty{}, nil
}

// Resend rotates the secret and the expiry. The gateway still answers
// members.invite.resend with not_built until that registration calls this.
func (m *members) Resend(ctx context.Context, caller svc.Caller, p bridge.InviteRef) (bridge.Invite, error) {
	w, actor, err := m.actor(ctx, caller)
	if err != nil {
		return bridge.Invite{}, err
	}
	if svc.WorkspaceRole(actor.Role) != svc.WorkspaceOwner && svc.WorkspaceRole(actor.Role) != svc.WorkspaceAdmin {
		return bridge.Invite{}, denied("this call needs workspace Owner or Admin")
	}
	id, _, err := splitInviteRef(p.Invite)
	if err != nil {
		return bridge.Invite{}, err
	}
	secret, err := randomToken()
	if err != nil {
		return bridge.Invite{}, err
	}
	var out bridge.Invite
	w.mu.Lock()
	err = w.tx(ctx, func(tx *sql.Tx) error {
		row, err := inviteByID(tx, id)
		if err != nil {
			return err
		}
		if row.Used || row.Cancelled {
			return inviteExpired(row.By)
		}
		expires := w.now().Add(inviteTTL)
		_, err = tx.Exec(`UPDATE invites SET secret_hash = ?, expires = ?, cancelled = 0 WHERE id = ?`, hashSecret(secret), stamp(expires), id)
		if err != nil {
			return err
		}
		w.secrets[id] = secret
		out = bridge.Invite{ID: id, Email: row.Email, Role: row.Role, Link: buildInvite(w.publicURL, id, secret), By: row.By, Expires: expires, State: "pending"}
		return nil
	})
	w.mu.Unlock()
	if err != nil {
		return bridge.Invite{}, err
	}
	target := out.Email
	if target == "" {
		target = "an invite"
	}
	m.s.audit(ctx, actor.ID, "members.invite.resend", "invite/"+id, "@"+actor.Handle+" resent the invite for "+target)
	m.s.publish(w.snapshot(ctx))
	return out, nil
}

func (m *members) SetRole(ctx context.Context, caller svc.Caller, p bridge.MemberRoleParams) (bridge.Member, error) {
	w, actor, err := m.actor(ctx, caller)
	if err != nil {
		return bridge.Member{}, err
	}
	var updated memberRow
	w.mu.Lock()
	err = w.tx(ctx, func(tx *sql.Tx) error {
		target, err := memberByID(tx, p.Member)
		if err != nil {
			return err
		}
		owners, err := countRole(tx, svc.WorkspaceOwner)
		if err != nil {
			return err
		}
		to := svc.WorkspaceRole(p.Role)
		if err := canSetRole(svc.WorkspaceRole(actor.Role), svc.WorkspaceRole(target.Role), to, owners, w.name); err != nil {
			return err
		}
		if to == svc.WorkspaceOwner && target.Role != string(svc.WorkspaceOwner) {
			if _, err := tx.Exec(`UPDATE members SET role = ? WHERE role = ? AND id != ?`, string(svc.WorkspaceAdmin), string(svc.WorkspaceOwner), target.ID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`UPDATE members SET role = ? WHERE id = ?`, string(to), target.ID); err != nil {
			return err
		}
		target.Role = string(to)
		updated = target
		return nil
	})
	w.mu.Unlock()
	if err != nil {
		return bridge.Member{}, err
	}
	m.s.audit(ctx, actor.ID, "members.role", "member/"+updated.ID, "@"+actor.Handle+" made @"+updated.Handle+" "+roleTitle(updated.Role))
	m.s.publish(w.snapshot(ctx))
	return updated.wire(), nil
}

func (m *members) Remove(ctx context.Context, caller svc.Caller, p bridge.MemberRef) (bridge.Empty, error) {
	return m.drop(ctx, caller, p.Member, false)
}

func (m *members) Leave(ctx context.Context, caller svc.Caller) (bridge.Empty, error) {
	if m.s.ws == nil {
		return bridge.Empty{}, bridge.NotBuilt("members.leave")
	}
	return m.drop(ctx, caller, caller.Member, true)
}

func (m *members) drop(ctx context.Context, caller svc.Caller, member string, leaving bool) (bridge.Empty, error) {
	w, actor, err := m.actor(ctx, caller)
	if err != nil {
		return bridge.Empty{}, err
	}
	var target memberRow
	w.mu.Lock()
	err = w.tx(ctx, func(tx *sql.Tx) error {
		var err error
		target, err = memberByID(tx, member)
		if err != nil {
			return err
		}
		owners, err := countRole(tx, svc.WorkspaceOwner)
		if err != nil {
			return err
		}
		self := actor.ID == target.ID
		if leaving || self {
			if target.Role == string(svc.WorkspaceOwner) && owners <= 1 {
				return errLastOwnerOf(w.name)
			}
		} else if err := canRemove(svc.WorkspaceRole(actor.Role), svc.WorkspaceRole(target.Role), owners, false, w.name); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM sessions WHERE member = ?`, target.ID); err != nil {
			return err
		}
		_, err = tx.Exec(`DELETE FROM members WHERE id = ?`, target.ID)
		return err
	})
	w.mu.Unlock()
	if err != nil {
		return bridge.Empty{}, err
	}
	if leaving || actor.ID == target.ID {
		m.s.audit(ctx, target.ID, "members.leave", "member/"+target.ID, "@"+target.Handle+" left "+w.name)
	} else {
		m.s.audit(ctx, actor.ID, "members.remove", "member/"+target.ID, "@"+actor.Handle+" removed @"+target.Handle)
	}
	m.s.publish(w.snapshot(ctx))
	m.s.finishGone(ctx, target.ID)
	return bridge.Empty{}, nil
}

func (m *members) actor(ctx context.Context, caller svc.Caller) (*workspace, memberRow, error) {
	if m.s.ws == nil {
		return nil, memberRow{}, denied("this call is only on a workspace")
	}
	row, err := m.must(ctx, caller.Member)
	return m.s.ws, row, err
}

func (m *members) must(ctx context.Context, id string) (memberRow, error) {
	w := m.s.ws
	var row memberRow
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.tx(ctx, func(tx *sql.Tx) error {
		var err error
		row, err = memberByID(tx, id)
		return err
	})
	return row, err
}
