package plandb

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CheckFiles returns the literal file-shaped words of a check command because
// #1604 showed that treating a future package or shell expression as a missing
// file can refuse valid work. The command is not evaluated.
func CheckFiles(check string) []string {
	var files []string
	for _, word := range strings.Fields(check) {
		word = strings.Trim(word, "'\"")
		word = strings.TrimRight(word, ";,)")
		word = strings.Trim(word, "'\"")
		if fileShaped(word) {
			files = append(files, word)
		}
	}
	return files
}

// NamedFiles returns file-shaped words from a task's prose because a title or
// work order may put backticks and sentence punctuation around a real name.
// CheckFiles keeps its narrower command parsing for the #1604 compatibility case.
func NamedFiles(text string) []string {
	var files []string
	for _, word := range strings.Fields(text) {
		for {
			prior := word
			word = strings.Trim(word, "'\"`()[]{}<>,;:!?")
			word = strings.TrimSuffix(word, ".")
			if word == prior {
				break
			}
		}
		if fileShaped(word) {
			files = append(files, word)
		}
	}
	return files
}

// PlaceholderSiblings finds the numbered names that make a missing trailing
// underscore a likely lost number, as #1573's issue_.go did. An existing file
// is intentional; siblings may be on disk or in a task's prose work order.
func PlaceholderSiblings(file, dir string, texts []string) []string {
	if !fileShaped(file) {
		return nil
	}
	base := path.Base(file)
	stem, ext, ok := fileStem(base)
	if !ok || !strings.HasSuffix(stem, "_") {
		return nil
	}
	if _, err := os.Stat(checkFilePath(dir, file)); err == nil || !os.IsNotExist(err) {
		return nil
	}
	siblings := make(map[string]bool)
	for _, text := range texts {
		for _, word := range NamedFiles(text) {
			candidate := path.Base(word)
			if numberedSibling(candidate, stem, ext, true) {
				siblings[candidate] = true
			}
		}
	}
	entries, err := os.ReadDir(checkFilePath(dir, path.Dir(file)))
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && numberedSibling(entry.Name(), stem, ext, false) {
				siblings[entry.Name()] = true
			}
		}
	}
	if len(siblings) == 0 {
		return nil
	}
	out := make([]string, 0, len(siblings))
	for sibling := range siblings {
		out = append(out, sibling)
	}
	sort.Strings(out)
	return out
}

// checkFilePath keeps absolute command operands absolute when a run checks
// its own working copy; relative operands are resolved under that copy.
func checkFilePath(dir, file string) string {
	if filepath.IsAbs(file) {
		return file
	}
	return filepath.Join(dir, filepath.FromSlash(file))
}

// fileShaped accepts only literal file names with a letter in the stem and a
// short extension. Shell syntax remains the shell's business, not the plan's.
func fileShaped(word string) bool {
	if word == "" || strings.HasPrefix(word, "-") || strings.ContainsAny(word, "*?[{$=") || strings.Contains(word, "://") || strings.Contains(word, "...") {
		return false
	}
	_, _, ok := fileStem(path.Base(word))
	return ok
}

func fileStem(base string) (string, string, bool) {
	dot := strings.LastIndexByte(base, '.')
	if dot <= 0 || dot == len(base)-1 {
		return "", "", false
	}
	stem, ext := base[:dot], base[dot+1:]
	letter := false
	for _, r := range stem {
		if unicode.IsLetter(r) {
			letter = true
			break
		}
	}
	if !letter || utf8.RuneCountInString(ext) > 10 {
		return "", "", false
	}
	for i, r := range ext {
		if i == 0 && !unicode.IsLetter(r) {
			return "", "", false
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return "", "", false
		}
	}
	return stem, ext, true
}

// numberedSibling accepts a template slot only from a work order; files on
// disk count as numbered siblings only when their suffix is digits.
func numberedSibling(candidate, stem, ext string, allowTemplate bool) bool {
	if !strings.HasPrefix(candidate, stem) || !strings.HasSuffix(candidate, "."+ext) {
		return false
	}
	number := strings.TrimSuffix(strings.TrimPrefix(candidate, stem), "."+ext)
	if allowTemplate && (number == "NN" || number == "XX" || number == "nn") {
		return true
	}
	if number == "" {
		return false
	}
	for _, r := range number {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
