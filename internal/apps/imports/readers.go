package imports

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type reader interface {
	Rows(context.Context) ([]string, [][]string, error)
}

func (s *Service) reader(scope svc.Scope, source bridge.ImportSourceSelection) (reader, error) {
	switch source.Kind {
	case "csv":
		return csvReader{source.File}, nil
	case "airtable", "hubspot":
		if s.all.Connectors == nil {
			return nil, bridge.Errorf(bridge.CodeConflict, "source connection is unavailable")
		}
		if source.Kind == "airtable" {
			return airtableReader{s.all.Connectors, scope, source}, nil
		}
		return hubspotReader{s.all.Connectors, scope, source}, nil
	default:
		return nil, invalid("unknown import source")
	}
}

// The door may return a tabular fake or the provider's records envelope.
// Decoding both here keeps source readers independent of adapter implementations.
func decodeTable(reply any, collection, nested string, properties []string) ([]string, [][]string, error) {
	data, err := json.Marshal(reply)
	if err != nil {
		return nil, nil, invalid("invalid source response")
	}
	var table struct {
		Columns []string   `json:"columns"`
		Rows    [][]string `json:"rows"`
	}
	if err := json.Unmarshal(data, &table); err == nil && table.Columns != nil {
		return table.Columns, table.Rows, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, nil, invalid("invalid source response")
	}
	raw, ok := envelope[collection]
	if !ok {
		return nil, nil, invalid("source response has no rows")
	}
	var objects []map[string]any
	if err := json.Unmarshal(raw, &objects); err != nil {
		return nil, nil, invalid("invalid source rows")
	}
	values := make([]map[string]any, len(objects))
	names := map[string]bool{}
	for i, object := range objects {
		values[i], _ = object[nested].(map[string]any)
		if values[i] == nil {
			values[i] = object
		}
		for key := range values[i] {
			names[key] = true
		}
	}
	columns := properties
	if columns == nil {
		columns = keys(names)
	}
	rows := make([][]string, len(values))
	for i, object := range values {
		rows[i] = make([]string, len(columns))
		for j, column := range columns {
			if object[column] != nil {
				rows[i][j] = fmt.Sprint(object[column])
			}
		}
	}
	if properties != nil {
		columns = hubspotColumns
	}
	return columns, rows, nil
}
func keys[T any](values map[string]T) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// Providers paginate independently of records.query. Follow their cursors through
// the same door; never silently treat the first page as the complete import.
func readPages(ctx context.Context, door svc.Connectors, scope svc.Scope, tool string, args map[string]any, collection, nested string, properties []string, cursor string) ([]string, [][]string, error) {
	columns := []string{}
	rows := [][]string{}
	seen := map[string]bool{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		reply, err := door.Call(ctx, scope, tool, args)
		if err != nil {
			return nil, nil, err
		}
		pageColumns, pageRows, err := decodeTable(reply, collection, nested, properties)
		if err != nil {
			return nil, nil, err
		}
		if err := validTable(pageColumns, pageRows); err != nil {
			return nil, nil, err
		}
		columns, rows = joinPage(columns, rows, pageColumns, pageRows)
		next, err := nextCursor(reply, cursor)
		if err != nil {
			return nil, nil, err
		}
		if next == "" {
			return columns, rows, nil
		}
		if seen[next] {
			return nil, nil, invalid("source repeated a pagination cursor")
		}
		seen[next] = true
		args[cursor] = next
	}
}
func nextCursor(reply any, cursor string) (string, error) {
	data, err := json.Marshal(reply)
	if err != nil {
		return "", err
	}
	var page struct {
		Offset string `json:"offset"`
		Paging struct {
			Next struct {
				After string `json:"after"`
			} `json:"next"`
		} `json:"paging"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return "", invalid("invalid source pagination")
	}
	if cursor == "offset" {
		return page.Offset, nil
	}
	return page.Paging.Next.After, nil
}
func joinPage(columns []string, rows [][]string, pageColumns []string, pageRows [][]string) ([]string, [][]string) {
	indexes := map[string]int{}
	for i, column := range columns {
		indexes[column] = i
	}
	for _, column := range pageColumns {
		if _, exists := indexes[column]; exists {
			continue
		}
		indexes[column] = len(columns)
		columns = append(columns, column)
		for i := range rows {
			rows[i] = append(rows[i], "")
		}
	}
	for _, row := range pageRows {
		mapped := make([]string, len(columns))
		for i, column := range pageColumns {
			mapped[indexes[column]] = row[i]
		}
		rows = append(rows, mapped)
	}
	return columns, rows
}
