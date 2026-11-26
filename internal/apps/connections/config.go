// Package connections executes operator-declared reads and effects against
// personal app sources. The profile owns endpoints and mappings, so app files
// and run arguments can select operations without choosing which host to trust.
package connections

import (
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
)

type Kind string

const (
	HTTP     Kind = "http"
	Postgres Kind = "postgres"
	MySQL    Kind = "mysql"
	Google   Kind = "google"
)

const CredentialPerson = "person"

type Config struct {
	GoogleFixture  bool             `json:"google_fixture,omitempty"`
	CredentialMode string           `json:"credential_mode,omitempty"`
	ID             string           `json:"id"`
	Kind           Kind             `json:"kind"`
	App            string           `json:"app"`
	Origin         string           `json:"origin,omitempty"`
	TestPath       string           `json:"test_path,omitempty"`
	Reads          map[string]Read  `json:"reads"`
	Writes         map[string]Write `json:"writes,omitempty"`
}

// Read maps fields on a bound app type to source columns or JSON properties.
// Match maps arguments to SQL equality predicates or HTTP query parameters.
type Read struct {
	GoogleIndex          bool              `json:"google_index,omitempty"`
	GoogleLinkIdentities bool              `json:"google_link_identities,omitempty"`
	GoogleQuery          string            `json:"google_query,omitempty"`
	Type                 string            `json:"type"`
	Table                string            `json:"table,omitempty"`
	Path                 string            `json:"path,omitempty"`
	Rows                 string            `json:"rows,omitempty"`
	Key                  string            `json:"key"`
	Version              string            `json:"version,omitempty"`
	Fields               map[string]string `json:"fields"`
	Match                map[string]string `json:"match,omitempty"`
	Residency            *ResidencyConfig  `json:"residency,omitempty"`
	OrderBy              string            `json:"order_by,omitempty"`
}

// ResidencyConfig admits a Personal fixed export to the type's existing
// audience. PostgreSQL must advance both columns for every edit or tombstone.
type ResidencyConfig struct {
	UpdatedAt       string                       `json:"updated_at"`
	Tombstone       string                       `json:"tombstone"`
	FreshForSeconds int                          `json:"fresh_for_seconds"`
	References      map[string]map[string]string `json:"references,omitempty"`
}

// Write names a read's binding and the subset of its fields that can change.
// HTTP writes map those fields, the source key and any version into a JSON
// object. Their optional response mapping selects only declared scalar values.
type Write struct {
	Read     string            `json:"read"`
	Fields   []string          `json:"fields"`
	Path     string            `json:"path,omitempty"`
	Response map[string]string `json:"response,omitempty"`
}

var alias = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

func fixedPath(path string) bool {
	u, err := url.Parse(path)
	return err == nil && len(path) <= 120 && strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") &&
		!strings.ContainsAny(path, "?#\\") && u.Host == "" && u.Scheme == "" && u.User == nil
}

func (c Config) validate() error {
	if !alias.MatchString(c.ID) || (!alias.MatchString(c.App) && !cell.ValidID(c.App)) {
		return errors.New("a connection needs a local name and one app scope")
	}
	if c.CredentialMode != "" && c.CredentialMode != "shared" && c.CredentialMode != CredentialPerson {
		return errors.New("a connection credential mode is shared or person")
	}
	if c.GoogleFixture && c.Kind != Google {
		return ErrRejected
	}
	switch c.Kind {
	case HTTP:
		u, err := url.Parse(c.Origin)
		if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
			(u.Path != "" && u.Path != "/") || u.ForceQuery || strings.ContainsAny(c.Origin, "?#\\") {
			return errors.New("an HTTP connection needs an http or https origin without credentials or a path")
		}
		if u.Scheme == "http" && !slices.Contains([]string{"localhost", "127.0.0.1", "::1"}, u.Hostname()) {
			return errors.New("HTTP connections use HTTPS outside loopback")
		}
		if !fixedPath(c.TestPath) {
			return errors.New("an HTTP connection needs a fixed GET test path")
		}
	case Google:
		if c.Origin != "" || c.TestPath != "" || c.CredentialMode != CredentialPerson {
			return errors.New("Google Workspace needs a personal connection with Google's fixed endpoints")
		}
	case Postgres, MySQL:
		if c.Origin != "" || c.TestPath != "" {
			return errors.New("put the database connection string in the credential")
		}
	default:
		return errors.New("a connection kind is http, postgres, mysql or google")
	}
	if len(c.Reads) == 0 || len(c.Reads) > 32 || len(c.Writes) > 32 {
		return errors.New("a connection needs 1 to 32 reads and can have up to 32 writes")
	}
	for name, read := range c.Reads {
		if !alias.MatchString(name) || !alias.MatchString(read.Type) || !identifier.MatchString(read.Key) ||
			(read.Version != "" && (!identifier.MatchString(read.Version) || read.Version == read.Key)) ||
			len(read.Fields) == 0 || len(read.Fields) > 32 || len(read.Match) > 16 {
			return errors.New("a read needs a name, bound record type, key and field mapping")
		}
		if c.Kind == Google {
			if !slices.Contains([]string{"gmail", "attachments", "calendar", "drive", "transcripts"}, read.Table) ||
				read.Path != "" || read.Key != "id" || read.Version != "" && read.Version != "version" ||
				read.Rows != "" || read.OrderBy != "" || read.Residency != nil ||
				len(read.GoogleQuery) > maxCell || (read.GoogleIndex || read.GoogleQuery != "") && read.Table != "gmail" ||
				len(read.Match) > 1 {
				return errors.New("a Google read needs a known Workspace object and canonical field mapping")
			}
			for _, param := range read.Match {
				if param != "q" || read.Table != "gmail" {
					return errors.New("only Gmail accepts a search query")
				}
			}
		} else if read.GoogleQuery != "" || read.GoogleIndex || read.GoogleLinkIdentities {
			return ErrRejected
		} else if c.Kind == HTTP {
			if !fixedPath(read.Path) || read.Table != "" || read.OrderBy != "" || (read.Rows != "" && !identifier.MatchString(read.Rows)) {
				return errors.New("an HTTP read needs a fixed path and may name a JSON rows property")
			}
		} else if !validTable(read.Table) || read.Path != "" || read.Rows != "" ||
			(read.OrderBy != "" && !identifier.MatchString(read.OrderBy)) {
			return errors.New("a database read needs a table and optional sort column")
		}
		mapped := map[string]bool{read.Key: true, read.Version: true}
		if r := read.Residency; r != nil {
			if c.Kind != Postgres || c.CredentialMode == CredentialPerson || read.Version == "" || !identifier.MatchString(r.UpdatedAt) ||
				!identifier.MatchString(r.Tombstone) || mapped[r.UpdatedAt] || mapped[r.Tombstone] ||
				r.UpdatedAt == r.Tombstone || r.FreshForSeconds < 1 || r.FreshForSeconds > 3600 {
				return errors.New("residency needs a shared PostgreSQL credential, version, updated_at, tombstone and freshness")
			}
			mapped[r.UpdatedAt], mapped[r.Tombstone] = true, true
		}
		for field, column := range read.Fields {
			if c.Kind == Google && !slices.Contains(googleFields(read.Table, false), column) &&
				!slices.Contains(googleFields(read.Table, true), column) {
				return errors.New("the Google field is outside this mapping")
			}
			if !alias.MatchString(field) || field == "id" || field == "type" || !identifier.MatchString(column) || mapped[column] {
				return errors.New("a field mapping needs a distinct column outside the source key and version")
			}
			mapped[column] = true
		}
		if r := read.Residency; r != nil {
			for field, refs := range r.References {
				if read.Fields[field] == "" || len(refs) == 0 || len(refs) > maxRows ||
					read.Match[field] != "" || read.OrderBy == read.Fields[field] {
					return errors.New("a reference needs a mapped field and a bounded explicit crosswalk")
				}
				for key, id := range refs {
					if key == "" || len(key) > 1024 || !cell.ValidID(id) {
						return errors.New("a reference maps a source key to a canonical record ID")
					}
				}
			}
		}

		matched := map[string]bool{}
		for arg, target := range read.Match {
			if !alias.MatchString(arg) || !identifier.MatchString(target) || matched[target] || (c.Kind != HTTP && c.Kind != Google && read.Fields[arg] != target) {
				return errors.New("a read argument must map to a distinct query parameter or its mapped SQL field")
			}
			matched[target] = true
		}
		if read.OrderBy != "" && !slices.Contains(columnNames(read.Fields), read.OrderBy) {
			return errors.New("a database read may sort only by a mapped field")
		}
	}
	for name, write := range c.Writes {
		read, ok := c.Reads[write.Read]
		if !alias.MatchString(name) || !ok || len(write.Fields) == 0 || len(write.Fields) > len(read.Fields) {
			return errors.New("a write needs a name, read binding and writable fields")
		}
		if c.Kind == Google {
			if write.Path != "" || len(write.Response) != 0 || !googleOperation(read.Table, name) || name == "update-event" && read.Version == "" {
				return errors.New("choose a supported Google write for this read")
			}
		} else if c.Kind == HTTP {
			if !fixedPath(write.Path) || len(write.Response) > 16 {
				return errors.New("an HTTP write needs a fixed POST path and at most 16 response fields")
			}
			for label, field := range write.Response {
				if !alias.MatchString(label) || !identifier.MatchString(field) {
					return errors.New("an HTTP response mapping needs names and JSON properties")
				}
			}
		} else if read.Version == "" || write.Path != "" || len(write.Response) != 0 {
			return errors.New("a database write needs a version column")
		}
		seen := map[string]bool{}
		for _, field := range write.Fields {
			column := read.Fields[field]
			if c.Kind == Google && !slices.Contains(googleFields(read.Table, true), column) {
				return errors.New("the Google operation cannot set that field")
			}
			if column == "" || column == read.Key || column == read.Version || seen[column] {
				return errors.New("a write must name distinct mapped fields outside the source key and version")
			}
			seen[column] = true
		}
	}
	return nil
}

func validTable(table string) bool {
	parts := strings.Split(table, ".")
	if len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if !identifier.MatchString(part) {
			return false
		}
	}
	return true
}

func columnNames(mapping map[string]string) []string {
	out := make([]string, 0, len(mapping))
	for _, name := range mapping {
		out = append(out, name)
	}
	return out
}
