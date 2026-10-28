package workspaceidentity

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Agent-Field/codeaf/internal/sqliteuri"
)

// ResolveExisting verifies an already admitted binding without creating a
// directory, database, schema, namespace, alias, or migration. Missing local
// state is unknown, never permission to admit an incoming identity.
func ResolveExisting(root, uri string) (Record, error) {
	if !validURI(uri) {
		return Record{}, errors.New("malformed workspace identity")
	}
	if !filepath.IsAbs(root) {
		return Record{}, errors.New("workspace registry requires an absolute path")
	}
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, ErrUnknown
	}
	if err != nil {
		return Record{}, err
	}
	if !info.IsDir() || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return Record{}, errors.New("workspace registry directory must be private")
	}
	path := filepath.Join(root, "registry.sqlite")
	info, err = os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, ErrUnknown
	}
	if err != nil {
		return Record{}, err
	}
	if !info.Mode().IsRegular() {
		return Record{}, errors.New("workspace registry database is not a regular file")
	}
	address := sqliteuri.File(path)
	address.RawQuery = "mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", address.String())
	if err != nil {
		return Record{}, err
	}
	defer db.Close()
	record, err := byURI(db, uri)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrUnknown
	}
	if err != nil {
		return Record{}, err
	}
	if err := validateResolvedBinding(record.Role, record.Path); err != nil {
		return Record{}, err
	}
	return record, nil
}
