package builder

import (
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func (c Catalog) reuseSource(name string) string {
	if owner := c.Types[name].Owner; owner != "" {
		return owner
	}
	return c.Origins[name]
}

func (c Catalog) reuseText(name string) string {
	label := c.TypeLabels[name]
	if label == "" {
		switch name {
		case "contact":
			label = "Contacts"
		case "company":
			label = "Companies"
		default:
			label = strings.ReplaceAll(name, "_", " ")
		}
	}
	line := "your existing " + label
	if from := c.AppNames[c.reuseSource(name)]; from != "" {
		line += " from " + from
	}
	return line
}

func (j *job) reuseAccepted(name string, record schema.RecordType, catalog Catalog) bool {
	if record.Common == "" {
		return slices.Contains(j.Accepted, name)
	}
	if slices.Contains(j.Accepted, "common:"+record.Common) {
		return true
	}
	offer, ok := catalog.Types.OfferCommonType(name, record)
	return ok && offer.Existing != "" && slices.Contains(j.Accepted, offer.Existing)
}

func (j *job) acceptReuse(name string) {
	if !slices.Contains(j.Accepted, name) {
		j.Accepted = append(j.Accepted, name)
	}
}

// Reopening a kept version restores the labels for its shared records.
func (s *Jobs) describeKept(j *job) {
	catalog, _ := s.catalog(j.App)
	j.describe(catalog)
}
