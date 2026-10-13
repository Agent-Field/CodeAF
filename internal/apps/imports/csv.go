package imports

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"io"
	"strings"
)

type csvReader struct{ file string }

func (r csvReader) Rows(ctx context.Context) ([]string, [][]string, error) {
	data, err := base64.StdEncoding.DecodeString(r.file)
	if err != nil {
		return nil, nil, invalid("CSV file must be base64 encoded")
	}
	csv := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\ufeff")))
	columns, err := csv.Read()
	if err != nil {
		return nil, nil, invalid("CSV needs a header row")
	}
	rows := [][]string{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		row, err := csv.Read()
		if err == io.EOF {
			return columns, rows, nil
		}
		if err != nil {
			return nil, nil, invalid("invalid CSV: " + err.Error())
		}
		rows = append(rows, row)
	}
}
