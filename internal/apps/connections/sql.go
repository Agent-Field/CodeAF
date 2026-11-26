package connections

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func database(kind Kind, secret string) (*sql.DB, error) {
	switch kind {
	case Postgres:
		cfg, err := pgx.ParseConfig(secret)
		if err == nil {
			// Use bound parameters in text format. The server infers the
			// column type and interprets values using that type.
			cfg.DefaultQueryExecMode = pgx.QueryExecModeExec
			return stdlib.OpenDB(*cfg), nil
		}
	case MySQL:
		cfg, err := mysql.ParseDSN(secret)
		if err == nil {
			cfg.Logger = log.New(io.Discard, "", 0)
			cfg.MultiStatements = false
			cfg.InterpolateParams = false
			cfg.AllowAllFiles = false
			cfg.MaxAllowedPacket = maxBytes + maxCell
			if driver, err := mysql.NewConnector(cfg); err == nil {
				return sql.OpenDB(driver), nil
			}
		}
	}
	return nil, errors.New("the database credential is not a valid connection string")
}

func (k Kind) quote(name string) string {
	quote := `"`
	if k == MySQL {
		quote = "`"
	}
	return quote + name + quote
}

func (k Kind) table(name string) string {
	parts := strings.Split(name, ".")
	for i, part := range parts {
		parts[i] = k.quote(part)
	}
	return strings.Join(parts, ".")
}

func (k Kind) placeholder(n int) string {
	if k == Postgres {
		return "$" + strconv.Itoa(n)
	}
	return "?"
}

func sqlRows(ctx context.Context, kind Kind, secret string, read Read, match map[string]any, limit int) ([]map[string]any, error) {
	db, err := database(kind, secret)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, ErrUnavailable
	}
	defer tx.Rollback()

	return scanSQLRows(ctx, tx, kind, read, match, limit, "", false, "")
}

func scanSQLRows(ctx context.Context, tx *sql.Tx, kind Kind, read Read, match map[string]any, limit int, checkpoint string, includeDeleted bool, upper string) ([]map[string]any, error) {
	wanted := append(columnNames(read.Fields), read.Key)
	if read.Version != "" {
		wanted = append(wanted, read.Version)
	}
	if includeDeleted {
		wanted = append(wanted, read.Residency.Tombstone)
	}
	slices.Sort(wanted)
	wanted = slices.Compact(wanted)
	projection := make([]string, 0, len(wanted))
	cast := "TEXT"
	if kind == MySQL {
		cast = "CHAR"
	}
	for _, name := range wanted {
		// Truncate one character past the allowed byte count at the source.
		// The scan rejects that sentinel length, so no truncated value escapes.
		projection = append(projection, fmt.Sprintf("SUBSTRING(CAST(%s AS %s), 1, %d)", kind.quote(name), cast, maxCell+1))
	}
	query := "SELECT " + strings.Join(projection, ", ") + " FROM " + kind.table(read.Table)
	var predicates []string
	var args []any
	for _, name := range slices.Sorted(maps.Keys(match)) {
		args = append(args, sqlValue(match[name]))
		predicates = append(predicates, kind.quote(name)+" = "+kind.placeholder(len(args)))
	}
	if r := read.Residency; r != nil {
		if !includeDeleted {
			predicates = append(predicates, "NOT "+kind.quote(r.Tombstone))
		}
		if checkpoint != "" {
			args = append(args, checkpoint)
			predicates = append(predicates, kind.quote(r.UpdatedAt)+" >= "+kind.placeholder(len(args))+"::timestamptz - interval '5 minutes'")
		}
		if upper != "" {
			args = append(args, upper)
			predicates = append(predicates, kind.quote(r.UpdatedAt)+" <= "+kind.placeholder(len(args))+"::timestamptz")
		}
	}
	if len(predicates) != 0 {
		query += " WHERE " + strings.Join(predicates, " AND ")
	}
	query += " ORDER BY "
	if read.OrderBy != "" && read.OrderBy != read.Key {
		query += kind.quote(read.OrderBy) + ", "
	}
	query += kind.quote(read.Key) + " LIMIT " + strconv.Itoa(limit)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	var out []map[string]any
	var byteCount int
	vals := make([]sql.NullString, len(wanted))
	targets := make([]any, len(vals))
	for i := range vals {
		targets[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(targets...); err != nil {
			return nil, ErrInvalidRows
		}
		item := make(map[string]any, len(wanted))
		for i, name := range wanted {
			v := vals[i]
			byteCount += len(v.String)
			if len(v.String) > maxCell || byteCount > maxBytes {
				return nil, ErrTooLarge
			}
			item[name] = nil
			if v.Valid {
				item[name] = v.String
			}
		}
		out = append(out, item)
	}
	if rows.Err() != nil {
		return nil, ErrUnavailable
	}
	return out, nil
}

func sqlValue(value any) any {
	if n, ok := value.(json.Number); ok {
		return n.String()
	}
	return value
}

func sqlWrite(ctx context.Context, kind Kind, secret string, read Read, key, version string, values map[string]any, check func() error) (string, error) {
	rev, err := strconv.ParseInt(version, 10, 64)
	if err != nil || rev < 0 || rev == (1<<63)-1 {
		return "", ErrRejected
	}
	db, err := database(kind, secret)
	if err != nil {
		return "", err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", ErrUnavailable
	}
	defer tx.Rollback()
	if kind == MySQL {
		schema, table := "", read.Table
		if first, rest, qualified := strings.Cut(table, "."); qualified {
			schema, table = first, rest
		}
		var engine string
		// Only allow the update where rollback can undo a failed precondition.
		err := tx.QueryRowContext(ctx, "SELECT ENGINE FROM information_schema.TABLES WHERE TABLE_SCHEMA = COALESCE(NULLIF(?, ''), DATABASE()) AND TABLE_NAME = ?", schema, table).Scan(&engine)
		if err != nil || engine != "InnoDB" {
			return "", ErrRejected
		}
	}
	if read.Residency != nil {
		if _, err := tx.ExecContext(ctx, "LOCK TABLE "+kind.table(read.Table)+" IN ROW EXCLUSIVE MODE"); err != nil {
			return "", ErrUnavailable
		}
	}
	if err := check(); err != nil {
		return "", err
	}
	if err := updateSQLRow(ctx, tx, kind, read, key, rev, values); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", ErrUncertain
	}
	return strconv.FormatInt(rev+1, 10), nil
}

func updateSQLRow(ctx context.Context, tx *sql.Tx, kind Kind, read Read, key string, rev int64, values map[string]any) error {
	columns := make([]string, 0, len(values)+1)
	args := make([]any, 0, len(values)+2)
	for _, column := range slices.Sorted(maps.Keys(values)) {
		args = append(args, sqlValue(values[column]))
		columns = append(columns, kind.quote(column)+" = "+kind.placeholder(len(args)))
	}
	v := kind.quote(read.Version)
	columns = append(columns, v+" = "+v+" + 1")
	if read.Residency != nil {
		col := kind.quote(read.Residency.UpdatedAt)
		columns = append(columns, col+" = clock_timestamp()")
	}
	statement := "UPDATE " + kind.table(read.Table) + " SET " + strings.Join(columns, ", ") +
		" WHERE " + kind.quote(read.Key) + " = " + kind.placeholder(len(args)+1) +
		" AND " + v + " = " + kind.placeholder(len(args)+2)
	if read.Residency != nil {
		statement += " AND NOT " + kind.quote(read.Residency.Tombstone)
	}
	args = append(args, key, rev)
	result, err := tx.ExecContext(ctx, statement, args...)
	if err != nil {
		return ErrRejected
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return ErrConflict
	}
	return nil
}
