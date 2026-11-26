package appwire

import (
	"fmt"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/apps/worksheet"
)

type WorksheetWindow struct {
	DataRecordParams
	Field   string `json:"field"`
	Row     int    `json:"row"`
	Column  int    `json:"column"`
	Rows    int    `json:"rows"`
	Columns int    `json:"columns"`
	Pin     bool   `json:"pin,omitempty"`
}

type WorksheetPage struct {
	Revision      string            `json:"revision"`
	Total         int               `json:"total"`
	Columns       []WorksheetColumn `json:"columns"`
	Rows          []WorksheetRow    `json:"rows"`
	ColumnIndexes []int             `json:"columnIndexes"`
}

type WorksheetColumn struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Type  string `json:"type"`
}

type WorksheetRow struct {
	Index int              `json:"index"`
	ID    string           `json:"id"`
	Cells []worksheet.Cell `json:"cells"`
}

func init() {
	methods["apps.worksheet_window"] = dataMethod("apps.worksheet_window", readWorksheet)
}

// Use the ordinary scoped record read so the page cannot see a value the
// requesting app or draft could not read from its bound field.
func readWorksheet(d Dataer, p WorksheetWindow) (*WorksheetPage, error) {
	if p.App == "" || p.ID == "" || p.Field == "" || p.Row < 0 || p.Row >= worksheet.MaxRows ||
		p.Column < 0 || p.Column >= worksheet.MaxColumns || p.Rows < 1 || p.Rows > worksheet.MaxRows ||
		p.Columns < 1 || p.Columns > worksheet.MaxColumns {
		return nil, Errorf(CodeBadParams, "Choose an app, worksheet field and cell range.")
	}
	record, err := d.DataGet(p.DataRecordParams)
	if err != nil {
		return nil, err
	}
	if record == nil || record.Row == nil {
		return nil, Errorf(CodeNotFound, "This worksheet is unavailable.")
	}
	source, _ := record.Row[p.Field].(string)
	sheet, err := worksheet.Open(source)
	if err != nil {
		return nil, Errorf(CodeBadParams, "%s", err)
	}
	rows, columns := sheet.Size()
	rows, columns = max(rows, 24), max(columns, 8)
	result := &WorksheetPage{
		Revision: fmt.Sprint(record.Row["_rev"]), Total: rows,
		Rows: []WorksheetRow{}, Columns: make([]WorksheetColumn, columns),
		ColumnIndexes: worksheetIndices(p.Column, p.Columns, columns, p.Pin),
	}
	for i := range result.Columns {
		result.Columns[i] = WorksheetColumn{ID: strconv.Itoa(i), Type: "text"}
	}
	for _, row := range worksheetIndices(p.Row, p.Rows, rows, p.Pin) {
		item := WorksheetRow{Index: row, ID: strconv.Itoa(row), Cells: make([]worksheet.Cell, len(result.ColumnIndexes))}
		for i, column := range result.ColumnIndexes {
			item.Cells[i] = sheet.Cell(row, column)
		}
		result.Rows = append(result.Rows, item)
	}
	return result, nil
}

func worksheetIndices(start, count, total int, pin bool) []int {
	indices := []int{}
	if pin && start > 0 {
		indices = append(indices, 0)
	}
	for i := start; i < min(start+count, total); i++ {
		indices = append(indices, i)
	}
	return indices
}
