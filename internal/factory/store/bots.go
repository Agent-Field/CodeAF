package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// The removal receipts retain only a hash so a removed bot's credentials
// can be recognized after its bot and token rows have been deleted.
const botsSchema = `
CREATE TABLE IF NOT EXISTS bots (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL COLLATE NOCASE,
	creator TEXT NOT NULL,
	scope TEXT NOT NULL,
	created_at TEXT NOT NULL,
	disabled_at TEXT,
	disabled_why TEXT,
	UNIQUE (creator, name)
);
CREATE TABLE IF NOT EXISTS bot_tokens (
	id TEXT PRIMARY KEY,
	bot_id TEXT NOT NULL,
	token_hash TEXT NOT NULL UNIQUE,
	label TEXT NOT NULL,
	hint TEXT NOT NULL,
	created_at TEXT NOT NULL,
	expires_at TEXT,
	revoked_at TEXT,
	last_used_at TEXT
);
CREATE INDEX IF NOT EXISTS bot_tokens_bot ON bot_tokens (bot_id);
CREATE TABLE IF NOT EXISTS removed_bot_token_hashes (
	token_hash TEXT PRIMARY KEY
);
`

const (
	BotsPerPerson = 20
	BotLiveTokens = 3
	// Fixed-width UTC times make comparisons in the database chronological.
	botTime = "2006-01-02T15:04:05.000000000Z"
)

var (
	ErrBotNameTaken  = errors.New("you already have a bot with that name")
	ErrBotLimit      = errors.New("you have reached the limit for bots")
	ErrBotTokenLimit = errors.New("this bot has reached the limit for live tokens")
	ErrBotGone       = errors.New("bot not found")
	ErrBotTokenGone  = errors.New("bot token not found")
	ErrBotDisabled   = errors.New("that bot was switched off because its creator left the organization")
)

// Bot and BotToken contain only the metadata that may be returned in a list.
type Bot struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Creator     string     `json:"creator"`
	Scope       string     `json:"scope,omitempty"`
	CreatedAt   string     `json:"created_at"`
	DisabledAt  *string    `json:"disabled_at,omitempty"`
	DisabledWhy *string    `json:"-"`
	Tokens      []BotToken `json:"tokens"`
}

type BotToken struct {
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	Hint       string  `json:"hint"`
	CreatedAt  string  `json:"created_at"`
	ExpiresAt  *string `json:"expires_at,omitempty"`
	RevokedAt  *string `json:"revoked_at,omitempty"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
}

type BotTokenState string

const (
	BotTokenLive     BotTokenState = ""
	BotTokenUnknown  BotTokenState = "unknown"
	BotTokenRevoked  BotTokenState = "revoked"
	BotTokenExpired  BotTokenState = "expired"
	BotTokenRemoved  BotTokenState = "bot_removed"
	BotTokenDisabled BotTokenState = "bot_disabled"
)

const botColumns = `b.id, b.name, b.creator, b.scope, b.created_at, b.disabled_at, b.disabled_why`
const botTokenColumns = `t.id, t.label, t.hint, t.created_at, t.expires_at, t.revoked_at, t.last_used_at`

func (b *Bot) fields() []any {
	return []any{&b.ID, &b.Name, &b.Creator, &b.Scope, &b.CreatedAt, &b.DisabledAt, &b.DisabledWhy}
}

// CreateBot stores the initial token with the bot so a failed mint never
// leaves the person with a bot they cannot use.
func (st *Store) CreateBot(b Bot, token BotToken, hash string, now time.Time) (Bot, error) {
	db, err := st.wsDB(true)
	if err != nil {
		return Bot{}, err
	}
	tx, err := db.Begin()
	if err != nil {
		return Bot{}, err
	}
	defer tx.Rollback()

	names, err := tx.Query(`SELECT name FROM bots WHERE creator = ?`, b.Creator)
	if err != nil {
		return Bot{}, err
	}
	count, err := countBotNames(names, b.Name)
	if err != nil {
		return Bot{}, err
	}
	if count >= BotsPerPerson {
		return Bot{}, ErrBotLimit
	}
	b.CreatedAt = now.UTC().Format(botTime)
	_, err = tx.Exec(`INSERT INTO bots (id, name, creator, scope, created_at) VALUES (?, ?, ?, ?, ?)`,
		b.ID, b.Name, b.Creator, b.Scope, b.CreatedAt)
	if err != nil {
		return Bot{}, err
	}
	token.CreatedAt = b.CreatedAt
	if err := insertBotToken(tx, b.ID, token, hash); err != nil {
		return Bot{}, err
	}
	b.Tokens = []BotToken{token}
	return b, tx.Commit()
}

func countBotNames(rows *sql.Rows, name string) (int, error) {
	defer rows.Close()
	count := 0
	for rows.Next() {
		var existing string
		if err := rows.Scan(&existing); err != nil {
			return 0, err
		}
		// SQLite's NOCASE collation covers ASCII only.
		if strings.EqualFold(existing, name) {
			return 0, ErrBotNameTaken
		}
		count++
	}
	return count, rows.Err()
}

func insertBotToken(tx *sql.Tx, bot string, token BotToken, hash string) error {
	_, err := tx.Exec(`INSERT INTO bot_tokens (id, bot_id, token_hash, label, hint, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, token.ID, bot, hash, token.Label, token.Hint, token.CreatedAt, token.ExpiresAt)
	return err
}

// Bots lists one creator's bots, or all bots when creator is empty.
func (st *Store) Bots(creator string) ([]Bot, error) {
	out := []Bot{}
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return out, err
	}
	rows, err := db.Query(`SELECT `+botColumns+`,
		COALESCE(t.id, ''), COALESCE(t.label, ''), COALESCE(t.hint, ''), COALESCE(t.created_at, ''),
		t.expires_at, t.revoked_at, t.last_used_at
		FROM bots b LEFT JOIN bot_tokens t ON t.bot_id = b.id
		WHERE ? = '' OR b.creator = ?
		ORDER BY b.creator, b.name, b.id, t.created_at, t.id`, creator, creator)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var b Bot
		var tok BotToken
		if err := rows.Scan(append(b.fields(), &tok.ID, &tok.Label, &tok.Hint, &tok.CreatedAt,
			&tok.ExpiresAt, &tok.RevokedAt, &tok.LastUsedAt)...); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].ID != b.ID {
			b.Tokens = []BotToken{}
			out = append(out, b)
		}
		if tok.ID != "" {
			last := &out[len(out)-1]
			last.Tokens = append(last.Tokens, tok)
		}
	}
	return out, rows.Err()
}

func (st *Store) Bot(id string) (Bot, error) {
	var b Bot
	db, err := st.wsDB(false)
	if err != nil {
		return b, err
	}
	if db == nil {
		return b, ErrBotGone
	}
	err = db.QueryRow(`SELECT `+botColumns+` FROM bots b WHERE b.id = ?`, id).Scan(b.fields()...)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrBotGone
	}
	return b, err
}

// AddBotToken checks the live-token limit inside the same write as the mint.
func (st *Store) AddBotToken(id string, token BotToken, hash string, now time.Time, expires *time.Time) (BotToken, error) {
	db, err := st.wsDB(false)
	if err != nil {
		return BotToken{}, err
	}
	if db == nil {
		return BotToken{}, ErrBotGone
	}
	tx, err := db.Begin()
	if err != nil {
		return BotToken{}, err
	}
	defer tx.Rollback()
	var disabled *string
	err = tx.QueryRow(`SELECT disabled_at FROM bots WHERE id = ?`, id).Scan(&disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return BotToken{}, ErrBotGone
	}
	if err != nil {
		return BotToken{}, err
	}
	if disabled != nil {
		return BotToken{}, ErrBotDisabled
	}
	token.CreatedAt = now.UTC().Format(botTime)
	if expires != nil {
		stamp := expires.UTC().Format(botTime)
		token.ExpiresAt = &stamp
	}
	var count int
	err = tx.QueryRow(`SELECT COUNT(*) FROM bot_tokens
		WHERE bot_id = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)`, id, token.CreatedAt).Scan(&count)
	if err != nil {
		return BotToken{}, err
	}
	if count >= BotLiveTokens {
		return BotToken{}, ErrBotTokenLimit
	}
	if err := insertBotToken(tx, id, token, hash); err != nil {
		return BotToken{}, err
	}
	return token, tx.Commit()
}

func (st *Store) RevokeBotToken(bot, id string, now time.Time) error {
	db, err := st.wsDB(false)
	if err != nil {
		return err
	}
	if db == nil {
		return ErrBotTokenGone
	}
	result, err := db.Exec(`UPDATE bot_tokens SET revoked_at = COALESCE(revoked_at, ?) WHERE bot_id = ? AND id = ?`,
		now.UTC().Format(botTime), bot, id)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil || count != 0 {
		return err
	}
	return ErrBotTokenGone
}

func (st *Store) RemoveBot(id string) error {
	db, err := st.wsDB(false)
	if err != nil {
		return err
	}
	if db == nil {
		return ErrBotGone
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`DELETE FROM bots WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil {
		return err
	} else if count == 0 {
		return ErrBotGone
	}
	if _, err := tx.Exec(`INSERT INTO removed_bot_token_hashes SELECT token_hash FROM bot_tokens WHERE bot_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM bot_tokens WHERE bot_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// DisableBotsOf keeps the bots disabled even if the creator rejoins later.
func (st *Store) DisableBotsOf(creator, why string, now time.Time) error {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := now.UTC().Format(botTime)
	if _, err := tx.Exec(`UPDATE bots SET disabled_at = ?, disabled_why = ? WHERE creator = ? AND disabled_at IS NULL`, stamp, why, creator); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE bot_tokens SET revoked_at = ? WHERE revoked_at IS NULL AND bot_id IN
		(SELECT id FROM bots WHERE creator = ?)`, stamp, creator); err != nil {
		return err
	}
	return tx.Commit()
}

func (st *Store) FindBotToken(hash string, now time.Time) (Bot, BotToken, BotTokenState, error) {
	var b Bot
	var tok BotToken
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return b, tok, BotTokenUnknown, err
	}
	err = db.QueryRow(`SELECT `+botColumns+`, `+botTokenColumns+`
		FROM bot_tokens t JOIN bots b ON b.id = t.bot_id WHERE t.token_hash = ?`, hash).
		Scan(append(b.fields(), &tok.ID, &tok.Label, &tok.Hint, &tok.CreatedAt, &tok.ExpiresAt, &tok.RevokedAt, &tok.LastUsedAt)...)
	if errors.Is(err, sql.ErrNoRows) {
		var removed bool
		err = db.QueryRow(`SELECT EXISTS (SELECT 1 FROM removed_bot_token_hashes WHERE token_hash = ?)`, hash).Scan(&removed)
		if removed {
			return b, tok, BotTokenRemoved, err
		}
		return b, tok, BotTokenUnknown, err
	}
	state := BotTokenLive
	switch {
	case b.DisabledAt != nil:
		state = BotTokenDisabled
	case tok.RevokedAt != nil:
		state = BotTokenRevoked
	case tok.ExpiresAt != nil && *tok.ExpiresAt <= now.UTC().Format(botTime):
		state = BotTokenExpired
	}
	return b, tok, state, err
}

// TouchBotToken uses the stored time in its update so even concurrent calls
// write at most once a minute. The caller can skip it on a recent read.
func (st *Store) TouchBotToken(id string, now time.Time) error {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return err
	}
	_, err = db.Exec(`UPDATE bot_tokens SET last_used_at = ?
		WHERE id = ? AND revoked_at IS NULL AND (last_used_at IS NULL OR last_used_at <= ?)`,
		now.UTC().Format(botTime), id, now.Add(-time.Minute).UTC().Format(botTime))
	return err
}
