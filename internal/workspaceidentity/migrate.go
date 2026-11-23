package workspaceidentity

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
)

// migrate reads legacy receipts exactly once, under the database's transaction.
// Original files remain untouched, including after a failed or committed import.
func (r *Registry) migrate(tx *sql.Tx) (string, error) {
	root, err := os.OpenRoot(r.root)
	if err != nil {
		return "", err
	}
	defer root.Close()
	namespace, err := legacyRead(root, "namespace")
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return "", err
	}
	scope := string(namespace)
	if !missing && !validID(scope) {
		return "", errors.New("invalid legacy workspace namespace; migration refused")
	}
	directory, err := root.Open(".")
	if err != nil {
		return "", err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return "", err
	}
	var records []Record
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if missing {
			return "", errors.New("legacy workspace records have no namespace; migration refused")
		}
		raw, err := legacyRead(root, entry.Name())
		if err != nil {
			return "", err
		}
		var record Record
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil {
			return "", err
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return "", errors.New("legacy workspace record has trailing data")
		}
		if !validURI(record.URI) || !validRole(record.Role) {
			return "", errors.New("malformed legacy workspace record")
		}
		parts := strings.Split(strings.TrimPrefix(record.URI, "codeaf-workspace://"), "/")
		key := parts[1]
		if parts[0] != scope {
			digest := sha256.Sum256([]byte(record.URI))
			key = hex.EncodeToString(digest[:])
		}
		if entry.Name() != key+".json" {
			return "", errors.New("legacy workspace record does not match its identity")
		}
		if err := validateResolvedBinding(record.Role, record.Path); err != nil {
			return "", err
		}
		if err := insert(tx, record); err != nil {
			return "", err
		}
		records = append(records, record)
	}
	for _, record := range records {
		links := Links{OwnerURI: record.OwnerURI, ProjectURI: record.ProjectURI}
		if _, err := validatedLinks(tx, links); err != nil {
			return "", err
		}
	}
	if missing {
		return randomID()
	}
	return scope, nil
}
func legacyRead(root *os.Root, name string) ([]byte, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 65536 {
		return nil, errors.New("invalid legacy workspace registry file")
	}
	return io.ReadAll(io.LimitReader(file, 65537))
}
