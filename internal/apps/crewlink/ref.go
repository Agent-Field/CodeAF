package crewlink

import "strings"

// RecordRef is the crew item reference for one record.
func RecordRef(app, typ, id string) string {
	return "app:" + app + "/" + typ + "/" + id
}

// RecordHref opens the record in its own app tab.
func RecordHref(app, typ, id string) string {
	return "codeaf://apps/" + app + "/" + typ + "/" + id
}

// ParseRecordRef reads app:<app>/<type>/<id>.
func ParseRecordRef(ref string) (app, typ, id string, ok bool) {
	body, found := strings.CutPrefix(ref, "app:")
	if !found {
		return "", "", "", false
	}
	app, rest, found := strings.Cut(body, "/")
	if !found {
		return "", "", "", false
	}
	typ, id, found = strings.Cut(rest, "/")
	if !found || app == "" || typ == "" || id == "" || strings.Contains(id, "/") {
		return "", "", "", false
	}
	return app, typ, id, true
}
