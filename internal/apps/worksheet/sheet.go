// Package worksheet evaluates the tab-separated cells stored in a worksheet record.
package worksheet

import (
	"encoding/csv"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const (
	MaxRows    = 500
	MaxColumns = 32
	MaxBytes   = 1 << 20
)

type Cell struct {
	Value   any    `json:"value"`
	Formula string `json:"formula,omitempty"`
	Error   string `json:"error,omitempty"`
}

type address struct{ row, column int }

type Sheet struct {
	rows   [][]string
	width  int
	cache  map[address]Cell
	active map[address]bool
	work   int
}

func Open(source string) (*Sheet, error) {
	if len(source) > MaxBytes {
		return nil, errors.New("The worksheet is too large.")
	}
	r := csv.NewReader(strings.NewReader(source))
	r.Comma, r.FieldsPerRecord = '\t', -1
	s := &Sheet{cache: map[address]Cell{}, active: map[address]bool{}}
	for {
		row, err := r.Read()
		if errors.Is(err, io.EOF) {
			return s, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read worksheet: %w", err)
		}
		if len(s.rows) >= MaxRows || len(row) > MaxColumns {
			return nil, fmt.Errorf("a worksheet takes up to %d rows and %d columns", MaxRows, MaxColumns)
		}
		s.rows = append(s.rows, row)
		s.width = max(s.width, len(row))
	}
}

func (s *Sheet) Size() (int, int) { return len(s.rows), s.width }

func (s *Sheet) RawRow(row int) []string { return append([]string(nil), s.rows[row]...) }

func (s *Sheet) Cell(row, column int) Cell {
	if row < 0 || column < 0 || row >= len(s.rows) || column >= len(s.rows[row]) {
		return Cell{}
	}
	input := s.rows[row][column]
	if !strings.HasPrefix(input, "=") {
		return Cell{Value: literal(input)}
	}
	p := address{row, column}
	if cell, ok := s.cache[p]; ok {
		return cell
	}
	cell := Cell{Formula: input}
	if s.active[p] {
		cell.Error = "Circular cell reference"
		return cell
	}
	if len(s.active) >= 64 || s.work >= 40_000 || len(input) > 512 {
		cell.Error = "Formula is too complex"
		return cell
	}
	s.work++
	s.active[p] = true
	value, err := s.formula(strings.TrimSpace(input[1:]))
	delete(s.active, p)
	if err != nil {
		cell.Error = err.Error()
	} else {
		cell.Value = value
	}
	s.cache[p] = cell
	return cell
}

var decimal = regexp.MustCompile(`^[+-]?(?:\d+(?:\.\d*)?|\.\d+)$`)
var references = regexp.MustCompile(`^([A-Z]{1,2})([1-9][0-9]{0,2})$`)
var sumRange = regexp.MustCompile(`(?i)^SUM\(\s*([A-Z]+[1-9][0-9]*)\s*:\s*([A-Z]+[1-9][0-9]*)\s*\)$`)

func literal(input string) any {
	if input == "" {
		return nil
	}
	if decimal.MatchString(input) {
		if value, err := strconv.ParseFloat(input, 64); err == nil {
			return value
		}
	}
	switch strings.ToLower(input) {
	case "true":
		return true
	case "false":
		return false
	}
	return input
}

func position(ref string) (address, error) {
	match := references.FindStringSubmatch(strings.ToUpper(ref))
	if match == nil {
		return address{}, fmt.Errorf("unknown cell reference %s", ref)
	}
	column := 0
	for _, letter := range match[1] {
		column = column*26 + int(letter-'A'+1)
	}
	row, _ := strconv.Atoi(match[2])
	if row > MaxRows || column > MaxColumns {
		return address{}, errors.New("Cell reference is outside the worksheet")
	}
	return address{row - 1, column - 1}, nil
}

func (s *Sheet) formula(source string) (any, error) {
	if match := sumRange.FindStringSubmatch(source); match != nil {
		a, err := position(match[1])
		if err != nil {
			return nil, err
		}
		b, err := position(match[2])
		if err != nil {
			return nil, err
		}
		var total float64
		for row := min(a.row, b.row); row <= max(a.row, b.row); row++ {
			for column := min(a.column, b.column); column <= max(a.column, b.column); column++ {
				s.work++
				if s.work >= 40_000 {
					return nil, errors.New("Formula range is too complex")
				}
				cell := s.Cell(row, column)
				if cell.Error != "" {
					return nil, errors.New(cell.Error)
				}
				if value, ok := cell.Value.(float64); ok {
					total += value
				}
			}
		}
		return finite(total)
	}
	tree, err := parser.ParseExpr(source)
	if err != nil {
		return nil, errors.New("Invalid formula")
	}
	return s.eval(tree)
}

func (s *Sheet) eval(node ast.Expr) (any, error) {
	switch x := node.(type) {
	case *ast.ParenExpr:
		return s.eval(x.X)
	case *ast.BasicLit:
		switch x.Kind {
		case token.INT, token.FLOAT:
			if number, err := strconv.ParseFloat(x.Value, 64); err == nil {
				return finite(number)
			}
		case token.STRING:
			return strconv.Unquote(x.Value)
		}
	case *ast.Ident:
		p, err := position(x.Name)
		if err != nil {
			return nil, err
		}
		cell := s.Cell(p.row, p.column)
		if cell.Error != "" {
			return nil, errors.New(cell.Error)
		}
		return cell.Value, nil
	case *ast.UnaryExpr:
		n, err := s.number(x.X)
		if err != nil {
			return nil, err
		}
		if x.Op == token.ADD {
			return n, nil
		}
		if x.Op == token.SUB {
			return -n, nil
		}
	case *ast.BinaryExpr:
		a, err := s.number(x.X)
		if err != nil {
			return nil, err
		}
		b, err := s.number(x.Y)
		if err != nil {
			return nil, err
		}
		switch x.Op {
		case token.ADD:
			return finite(a + b)
		case token.SUB:
			return finite(a - b)
		case token.MUL:
			return finite(a * b)
		case token.QUO:
			return finite(a / b)
		case token.REM:
			return finite(math.Mod(a, b))
		}
	}
	return nil, errors.New("Unsupported formula")
}

func (s *Sheet) number(node ast.Expr) (float64, error) {
	value, err := s.eval(node)
	if err != nil {
		return 0, err
	}
	if value == nil {
		return 0, nil
	}
	n, ok := value.(float64)
	if !ok {
		return 0, errors.New("Use a number in this formula")
	}
	return n, nil
}

func finite(n float64) (float64, error) {
	if math.IsInf(n, 0) || math.IsNaN(n) {
		return 0, errors.New("Formula result is outside the supported range")
	}
	return n, nil
}
