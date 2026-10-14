package space

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

func (w *workspace) sendCode(ctx context.Context, inviteRef, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return invalid("use an email address")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	w.mu.Lock()
	err = w.tx(ctx, func(tx *sql.Tx) error {
		row, _, err := w.loadInvite(tx, inviteRef, true)
		if err != nil {
			return err
		}
		if row.Email != "" && !strings.EqualFold(row.Email, email) {
			return conflict("Use the address the invite was sent to.")
		}
		_, err = tx.Exec(`INSERT INTO codes (email, code_hash, invite_id, expires, tries) VALUES (?, ?, ?, ?, 0)
			ON CONFLICT(email) DO UPDATE SET code_hash = excluded.code_hash, invite_id = excluded.invite_id, expires = excluded.expires, tries = 0`,
			email, hashSecret(code), row.ID, stamp(w.now().Add(codeTTL)))
		return err
	})
	w.mu.Unlock()
	if err != nil {
		return err
	}
	return w.mailer.SendCode(ctx, email, code)
}

type signedIn struct {
	Token   string
	Member  bridge.Member
	Expires time.Time
}

func (w *workspace) redeemCode(ctx context.Context, email, code string) (signedIn, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var out signedIn
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.tx(ctx, func(tx *sql.Tx) error {
		var hash, inviteID, expiresAt string
		var tries int
		err := tx.QueryRow(`SELECT code_hash, invite_id, expires, tries FROM codes WHERE email = ?`, email).Scan(&hash, &inviteID, &expiresAt, &tries)
		if errors.Is(err, sql.ErrNoRows) {
			return invalid("that code is not valid")
		}
		if err != nil {
			return err
		}
		at, err := parseStamp(expiresAt)
		if err != nil {
			return err
		}
		match := hashEqual(hash, strings.TrimSpace(code))
		expired := !w.now().Before(at)
		if !match || expired || tries >= codeTries {
			if !match && !expired && tries+1 < codeTries {
				_, _ = tx.Exec(`UPDATE codes SET tries = ? WHERE email = ?`, tries+1, email)
				return invalid("that code is not valid")
			}
			_, _ = tx.Exec(`DELETE FROM codes WHERE email = ?`, email)
			return invalid("that code is no longer valid")
		}
		inv, err := inviteByID(tx, inviteID)
		if err != nil {
			return err
		}
		if inv.dead(w.now()) {
			return inviteExpired(inv.By)
		}
		row, err := w.acceptInvite(tx, inv, email, "", w.now())
		if err != nil {
			return err
		}
		token, expires, err := issueSession(tx, row.ID, w.now())
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM codes WHERE email = ?`, email); err != nil {
			return err
		}
		delete(w.secrets, inv.ID)
		out = signedIn{Token: token, Member: row.wire(), Expires: expires}
		return nil
	})
	return out, err
}

func (w *workspace) preview(ctx context.Context, ref string) (inviteRow, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var row inviteRow
	err := w.tx(ctx, func(tx *sql.Tx) error {
		var err error
		row, _, err = w.loadInvite(tx, ref, true)
		return err
	})
	return row, err
}

func (w *workspace) beginOAuth(ctx context.Context, providerName, ref string) (string, error) {
	p := w.providers[providerName]
	if p == nil {
		return "", invalid("this workspace is not set up for " + providerName)
	}
	state, err := randomToken()
	if err != nil {
		return "", err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	err = w.tx(ctx, func(tx *sql.Tx) error {
		row, secret, err := w.loadInvite(tx, ref, true)
		if err != nil {
			return err
		}
		w.oauth[state] = oauthHold{ID: row.ID, Secret: secret, Expires: w.now().Add(codeTTL)}
		return nil
	})
	if err != nil {
		return "", err
	}
	return p.AuthCodeURL(state), nil
}

func (w *workspace) finishOAuth(ctx context.Context, providerName, state, code string) (signedIn, error) {
	p := w.providers[providerName]
	if p == nil {
		return signedIn{}, invalid("this workspace is not set up for " + providerName)
	}
	email, err := p.Email(ctx, code)
	if err != nil {
		return signedIn{}, invalid("the sign-in service did not return an email")
	}
	w.mu.Lock()
	hold, ok := w.oauth[state]
	if ok {
		delete(w.oauth, state)
	}
	w.mu.Unlock()
	if !ok || !w.now().Before(hold.Expires) {
		return signedIn{}, invalid("that sign-in has expired")
	}
	var out signedIn
	w.mu.Lock()
	defer w.mu.Unlock()
	err = w.tx(ctx, func(tx *sql.Tx) error {
		row, err := w.liveInvite(tx, hold.ID, hold.Secret)
		if err != nil {
			return err
		}
		member, err := w.acceptInvite(tx, row, email, "", w.now())
		if err != nil {
			return err
		}
		token, expires, err := issueSession(tx, member.ID, w.now())
		if err != nil {
			return err
		}
		delete(w.secrets, row.ID)
		out = signedIn{Token: token, Member: member.wire(), Expires: expires}
		return nil
	})
	return out, err
}

func (w *workspace) signinDescription(row inviteRow) bridge.SpaceSignin {
	hint := row.Email
	out := bridge.SpaceSignin{Kind: w.signIn, EmailHint: hint}
	if w.signIn == "email" {
		return out
	}
	return out
}
