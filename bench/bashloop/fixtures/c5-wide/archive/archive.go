// Package archive names the next file in a rotation.
package archive

import "strings"

// Ext is a path's extension, without the dot. A path with no extension
// answers "".
func Ext(path string) string {
	name := path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			name = path[i+1:]
			break
		}
	}
	dot := strings.Index(name, ".")
	if dot < 0 {
		return ""
	}
	return name[dot+1:]
}
