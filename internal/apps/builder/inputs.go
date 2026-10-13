package builder

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

var catalogOnce = sync.OnceValue(func() string {
	raw, err := spec.KitJSON()
	if err != nil {
		panic("kit catalog is embedded: " + err.Error())
	}
	return compact(raw)
})

// catalogText is kit.json, the only parts that exist, without its whitespace.
func catalogText() string { return catalogOnce() }

// schemaFor is the JSON Schema text of the file kinds a write call produces.
func schemaFor(urls ...string) string {
	var parts []string
	for _, url := range urls {
		raw, err := spec.SchemaJSON(url)
		if err != nil {
			panic("file schemas are embedded: " + err.Error())
		}
		parts = append(parts, compact(raw))
	}
	return strings.Join(parts, "\n")
}

func compact(raw []byte) string {
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		return string(raw)
	}
	return out.String()
}
