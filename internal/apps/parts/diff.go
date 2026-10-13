package parts

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"strings"
)

func (s *Service) Diff(ctx context.Context, scope svc.Scope, p bridge.DiffPartParams) (bridge.DiffPartResult, error) {
	result := bridge.DiffPartResult{}
	if p.From < 1 || p.To < 1 {
		return result, invalid("diff versions must be positive")
	}
	c, err := s.open(ctx, scope)
	if err != nil {
		return result, err
	}
	defer c.close()
	d, err := c.readDoc(p.Doc)
	if err != nil {
		return result, err
	}
	from, err := c.get(ctx, bridge.GetPartParams{Doc: p.Doc, Part: p.Part, Version: p.From}, d.Surface)
	if err != nil {
		return result, err
	}
	to, err := c.get(ctx, bridge.GetPartParams{Doc: p.Doc, Part: p.Part, Version: p.To}, d.Surface)
	if err != nil {
		return result, err
	}
	result.FromText = from.Text
	result.ToText = to.Text
	result.Lines = lineDiff(from.Text, to.Text)
	return result, nil
}
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// Hirschberg's LCS keeps memory linear even for long document sections.
func lineDiff(from, to string) []bridge.DiffLine {
	a, b := splitLines(from), splitLines(to)
	common := lcs(a, b)
	lines := []bridge.DiffLine{}
	i, j := 0, 0
	for _, line := range common {
		for a[i] != line {
			lines = append(lines, bridge.DiffLine{Op: "-", Text: a[i]})
			i++
		}
		for b[j] != line {
			lines = append(lines, bridge.DiffLine{Op: "+", Text: b[j]})
			j++
		}
		lines = append(lines, bridge.DiffLine{Op: " ", Text: line})
		i++
		j++
	}
	for _, line := range a[i:] {
		lines = append(lines, bridge.DiffLine{Op: "-", Text: line})
	}
	for _, line := range b[j:] {
		lines = append(lines, bridge.DiffLine{Op: "+", Text: line})
	}
	return lines
}
func lcs(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	if len(a) == 1 {
		for _, v := range b {
			if v == a[0] {
				return []string{a[0]}
			}
		}
		return nil
	}
	middle := len(a) / 2
	left := lcsLengths(a[:middle], b, false)
	right := lcsLengths(a[middle:], b, true)
	best, split := -1, 0
	for i := 0; i <= len(b); i++ {
		if score := left[i] + right[len(b)-i]; score > best {
			best = score
			split = i
		}
	}
	return append(lcs(a[:middle], b[:split]), lcs(a[middle:], b[split:])...)
}
func lcsLengths(a, b []string, reverse bool) []int {
	row := make([]int, len(b)+1)
	for i := range a {
		prev := 0
		for j := range b {
			x, y := i, j
			if reverse {
				x = len(a) - 1 - i
				y = len(b) - 1 - j
			}
			old := row[j+1]
			if a[x] == b[y] {
				row[j+1] = prev + 1
			} else {
				row[j+1] = max(row[j+1], row[j])
			}
			prev = old
		}
	}
	return row
}
