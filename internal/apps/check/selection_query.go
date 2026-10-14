package check

import "regexp"

// selectedID matches a query template that reads one selected row's id.
var selectedID = regexp.MustCompile(`\{\{\s*\$?selection\.([a-zA-Z_][a-zA-Z0-9_-]*)\.id\s*\}\}`)

// unselectedList names the list whose selected id a query reads while that
// list has no single selection: the related-rows query of a master/detail
// screen ("device rows held by the selected person") has no value to filter on.
func unselectedList(query string, values map[string]any) (string, bool) {
	for _, match := range selectedID.FindAllStringSubmatch(query, -1) {
		if id, err := resolve("selection."+match[1]+".id", values); err == nil && id == "" {
			return match[1], true
		}
	}
	return "", false
}

func unselectedHint(list string) string {
	return `show this list only while one row is selected: put it inside a Panel with when "$selection.` + list + `.count == 1"`
}
