// Package outputs turns scoped app record projections into document files.
package outputs

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

const (
	maxTextBytes = 2 << 20
	MaxFileBytes = 8 << 20
)

// Render needs only the template and already authorized rows. The strict text
// budget also covers expansion when the same field appears many times.
func Render(t schema.OutputTemplate, rows []map[string]any) ([]byte, error) {
	if len(rows) > schema.OutputRowsMax {
		return nil, fmt.Errorf("render at most %d rows", schema.OutputRowsMax)
	}
	remaining := maxTextBytes
	merge := func(text string, row map[string]any) (string, error) {
		if len(text) > remaining {
			return "", errors.New("output exceeds the text limit")
		}
		remaining -= len(text)
		return schema.MergeOutput(text, func(name string) (string, error) {
			value := row[name]
			if value == nil {
				return "", nil
			}
			s, ok := value.(string)
			if !ok {
				raw, err := json.Marshal(value)
				if err != nil {
					return "", err
				}
				s = string(raw)
			}
			if len(s) > remaining {
				return "", errors.New("output exceeds the text limit")
			}
			remaining -= len(s)
			return s, nil
		})
	}
	var buf bytes.Buffer
	if t.Format == "csv" {
		if len(t.Columns) == 0 {
			return nil, errors.New("CSV output needs columns")
		}
		w := csv.NewWriter(&buf)
		cells := make([]string, len(t.Columns))
		for i, column := range t.Columns {
			cells[i] = csvCell(column.Label)
		}
		if err := w.Write(cells); err != nil {
			return nil, err
		}
		for _, row := range rows {
			for i, column := range t.Columns {
				text, err := merge(column.Value, row)
				if err != nil {
					return nil, err
				}
				cells[i] = csvCell(text)
			}
			if err := w.Write(cells); err != nil {
				return nil, err
			}
		}
		w.Flush()
		return checkedFile(&buf, w.Error())
	}

	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		text, err := merge(t.Body, row)
		if err != nil {
			return nil, err
		}
		lines = append(lines, strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"))
	}
	var err error
	switch t.Format {
	case "pdf":
		err = writePDF(&buf, t.Label, lines)
	case "docx":
		err = writeDOCX(&buf, t.Label, lines)
	case "pptx":
		err = writePPTX(&buf, t.Label, lines)
	default:
		err = fmt.Errorf("unknown output format %q", t.Format)
	}
	return checkedFile(&buf, err)
}

func checkedFile(buf *bytes.Buffer, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	if buf.Len() > MaxFileBytes {
		return nil, errors.New("output exceeds the file size limit")
	}
	return buf.Bytes(), nil
}

// Spreadsheet apps treat leading formula characters and tabs as executable
// expressions. Import numeric literals normally and neutralize other cells.
func csvCell(text string) string {
	trimmed := strings.TrimLeftFunc(text, unicode.IsSpace)
	if strings.HasPrefix(text, "\t") || strings.HasPrefix(text, "\r") || strings.HasPrefix(text, "\n") {
		return "'" + text
	}
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		if _, err := strconv.ParseFloat(trimmed, 64); err != nil {
			return "'" + text
		}
	}
	return text
}
