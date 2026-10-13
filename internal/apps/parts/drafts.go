package parts

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Drafts read through to the applied files, but every writable path is in the member's folder.
func (c *session) storageDir(appDir string) (string, error) {
	if c.scope.Data != svc.DataDraft {
		return appDir, nil
	}
	member := c.scope.Caller.Member
	if !validRelative(member) || filepath.Base(member) != member {
		return "", invalid("invalid draft member")
	}
	var err error
	c.realRoot, err = safePath(appDir, "content")
	if err != nil {
		return "", err
	}
	return safePath(appDir, "drafts/"+member)
}

// The read-only attachment cannot create journals or change the applied parts database.
// Earlier applied versions remain visible after the first draft edit forks a part's history.
func (c *session) attachApplied(ctx context.Context, appDir string) error {
	if c.scope.Data != svc.DataDraft {
		return nil
	}
	if _, err := c.db.ExecContext(ctx, "PRAGMA temp_store=MEMORY"); err != nil {
		return err
	}
	path, err := safePath(appDir, "parts.db")
	if err != nil {
		return err
	}
	if _, err = os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("mode", "ro")
	q.Set("immutable", "1")
	u.RawQuery = q.Encode()
	if _, err = c.db.ExecContext(ctx, "ATTACH DATABASE ? AS applied", u.String()); err != nil {
		return err
	}
	_, err = c.db.ExecContext(ctx, `CREATE TEMP VIEW readable_versions AS
 SELECT * FROM main.part_versions
 UNION ALL SELECT r.* FROM applied.part_versions r WHERE r.data='real'
 AND r.version < COALESCE((SELECT MIN(d.version) FROM main.part_versions d
 WHERE d.data='real' AND d.doc=r.doc AND d.part=r.part), 9223372036854775807);
 CREATE TEMP VIEW readable_sidecar AS
 SELECT * FROM main.part_sidecar
 UNION ALL SELECT r.* FROM applied.part_sidecar r WHERE r.data='real'
 AND NOT EXISTS(SELECT 1 FROM main.part_sidecar d WHERE d.data='real' AND d.doc=r.doc AND d.part=r.part);`)
	if err == nil {
		c.versionTable, c.sidecarTable = "readable_versions", "readable_sidecar"
	}
	return err
}

func (c *session) readFile(doc, part string) ([]byte, error) {
	path, err := c.docPath(doc, part)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(path)
	if !os.IsNotExist(err) || c.realRoot == "" {
		return body, err
	}
	dir, err := safePath(c.realRoot, doc)
	if err != nil {
		return nil, err
	}
	path, err = safePath(dir, part)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (c *session) selectVersions(columns string) string {
	return fmt.Sprintf("SELECT %s FROM %s WHERE data=? AND doc=? AND part=?", columns, c.versionTable)
}
