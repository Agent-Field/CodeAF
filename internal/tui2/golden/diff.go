package golden

import (
	"fmt"
	"strings"
	"unicode"
)

type diffKind uint8

const (
	diffEqual diffKind = iota
	diffDelete
	diffAdd
)

type diffLine struct {
	kind diffKind
	text string
}

func unifiedDiff(wantName, want, got string) string {
	wantLines := snapshotLines(want)
	gotLines := snapshotLines(got)
	operations := lineDiff(wantLines, gotLines)

	var diff strings.Builder
	fmt.Fprintf(&diff, "--- %s\n+++ rendered\n", wantName)
	fmt.Fprintf(&diff, "@@ -1,%d +1,%d @@\n", len(wantLines), len(gotLines))
	for _, operation := range operations {
		switch operation.kind {
		case diffEqual:
			diff.WriteByte(' ')
		case diffDelete:
			diff.WriteByte('-')
		case diffAdd:
			diff.WriteByte('+')
		}
		diff.WriteString(markInvisible(operation.text))
		diff.WriteByte('\n')
	}
	diff.WriteString("(spaces: ·, tabs: ⇥, escape: ␛)\n")
	return diff.String()
}

func snapshotLines(snapshot string) []string {
	if snapshot == "" {
		return nil
	}
	if strings.HasSuffix(snapshot, "\n") {
		snapshot = strings.TrimSuffix(snapshot, "\n")
	}
	return strings.Split(snapshot, "\n")
}

func lineDiff(want, got []string) []diffLine {
	columns := len(got) + 1
	lcs := make([]int, (len(want)+1)*columns)
	for wantIndex := len(want) - 1; wantIndex >= 0; wantIndex-- {
		for gotIndex := len(got) - 1; gotIndex >= 0; gotIndex-- {
			position := wantIndex*columns + gotIndex
			if want[wantIndex] == got[gotIndex] {
				lcs[position] = lcs[(wantIndex+1)*columns+gotIndex+1] + 1
			} else {
				deleteScore := lcs[(wantIndex+1)*columns+gotIndex]
				addScore := lcs[wantIndex*columns+gotIndex+1]
				if deleteScore >= addScore {
					lcs[position] = deleteScore
				} else {
					lcs[position] = addScore
				}
			}
		}
	}

	operations := make([]diffLine, 0, len(want)+len(got))
	wantIndex, gotIndex := 0, 0
	for wantIndex < len(want) && gotIndex < len(got) {
		if want[wantIndex] == got[gotIndex] {
			operations = append(operations, diffLine{kind: diffEqual, text: want[wantIndex]})
			wantIndex++
			gotIndex++
			continue
		}
		deleteScore := lcs[(wantIndex+1)*columns+gotIndex]
		addScore := lcs[wantIndex*columns+gotIndex+1]
		if deleteScore >= addScore {
			operations = append(operations, diffLine{kind: diffDelete, text: want[wantIndex]})
			wantIndex++
		} else {
			operations = append(operations, diffLine{kind: diffAdd, text: got[gotIndex]})
			gotIndex++
		}
	}
	for ; wantIndex < len(want); wantIndex++ {
		operations = append(operations, diffLine{kind: diffDelete, text: want[wantIndex]})
	}
	for ; gotIndex < len(got); gotIndex++ {
		operations = append(operations, diffLine{kind: diffAdd, text: got[gotIndex]})
	}
	return operations
}

func markInvisible(line string) string {
	if line == "" {
		return "∅"
	}
	var marked strings.Builder
	for _, character := range line {
		switch character {
		case ' ':
			marked.WriteRune('·')
		case '\t':
			marked.WriteRune('⇥')
		case '\x1b':
			marked.WriteRune('␛')
		default:
			if unicode.IsControl(character) {
				fmt.Fprintf(&marked, "\\u{%04X}", character)
			} else {
				marked.WriteRune(character)
			}
		}
	}
	return marked.String()
}

func firstDifference(want, got string) (line, column int) {
	wantLines := snapshotLines(want)
	gotLines := snapshotLines(got)
	commonLines := len(wantLines)
	if len(gotLines) < commonLines {
		commonLines = len(gotLines)
	}
	for lineIndex := 0; lineIndex < commonLines; lineIndex++ {
		if wantLines[lineIndex] == gotLines[lineIndex] {
			continue
		}
		wantRunes := []rune(wantLines[lineIndex])
		gotRunes := []rune(gotLines[lineIndex])
		commonColumns := len(wantRunes)
		if len(gotRunes) < commonColumns {
			commonColumns = len(gotRunes)
		}
		for columnIndex := 0; columnIndex < commonColumns; columnIndex++ {
			if wantRunes[columnIndex] != gotRunes[columnIndex] {
				return lineIndex + 1, columnIndex + 1
			}
		}
		return lineIndex + 1, commonColumns + 1
	}
	return commonLines + 1, 1
}
