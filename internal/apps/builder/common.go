package builder

import (
	"maps"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func commonKey(lib schema.CommonTypeLibrary, name string, record schema.RecordType) string {
	if record.Common != "" {
		return record.Common
	}
	for _, key := range slices.Sorted(maps.Keys(lib.Types)) {
		typ := lib.Types[key]
		if typeWordMatches(name, key) || slices.ContainsFunc(typ.Terms, func(term string) bool {
			return typeWordMatches(name, term)
		}) {
			return key
		}
	}
	return ""
}

func typeWordMatches(name, term string) bool {
	name = strings.ToLower(strings.ReplaceAll(name, "_", " "))
	term = strings.ToLower(strings.ReplaceAll(term, "_", " "))
	return name == term || name == term+"s" || name == term+"es" ||
		strings.HasSuffix(term, "y") && name == strings.TrimSuffix(term, "y")+"ies"
}

func (c Catalog) source(name string) string {
	if owner := c.Types[name].Owner; owner != "" {
		return owner
	}
	return c.Origins[name]
}

func (c Catalog) label(name string) string {
	if label := c.TypeLabels[name]; label != "" {
		return label
	}
	switch name {
	case "contact":
		return "Contacts"
	case "company":
		return "Companies"
	}
	if name != "" {
		return strings.ToUpper(name[:1]) + strings.ReplaceAll(name[1:], "_", " ")
	}
	return ""
}

func (c Catalog) reuseText(name string) string {
	line := "your existing " + c.label(name)
	if from := c.AppNames[c.source(name)]; from != "" {
		line += " from " + from
	}
	return line
}

func (c Catalog) reuseName(alias string, record schema.RecordType) string {
	if record.Common != "" {
		for name, typ := range c.Types {
			if typ.Common == record.Common {
				return name
			}
		}
	}
	return alias
}

func (j *job) reuseAccepted(alias string, record schema.RecordType, catalog Catalog) bool {
	if record.Common != "" {
		if slices.Contains(j.Accepted, "common:"+record.Common) {
			return true
		}
		name := catalog.reuseName(alias, record)
		return catalog.Types[name].Common == record.Common && slices.Contains(j.Accepted, name)
	}
	return slices.Contains(j.Accepted, alias)
}
