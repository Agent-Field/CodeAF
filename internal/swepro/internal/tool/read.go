// This file ports swe-pro/src/tool/read.ts:16-343 at commit 3b25a1a.
// LSP warming remains a host-service seam; the workspace-bound registry
// supplies permission checks, filesystem behavior, and nested instructions.
package tool

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

const (
	defaultReadLimit = 2000
	maxLineLength    = 2000
	maxReadBytes     = 50 * 1024
	sampleBytes      = 4096
)

const maxLineSuffix = "... (line truncated to 2000 chars)"

var supportedImageMIMEs = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/gif":  {},
	"image/webp": {},
}

var binaryExtensions = map[string]struct{}{
	".zip": {}, ".tar": {}, ".gz": {}, ".exe": {}, ".dll": {}, ".so": {},
	".class": {}, ".jar": {}, ".war": {}, ".7z": {}, ".doc": {}, ".docx": {},
	".xls": {}, ".xlsx": {}, ".ppt": {}, ".pptx": {}, ".odt": {}, ".ods": {},
	".odp": {}, ".bin": {}, ".dat": {}, ".obj": {}, ".o": {}, ".a": {},
	".lib": {}, ".wasm": {}, ".pyc": {}, ".pyo": {},
}

type readLinesResult struct {
	raw    []string
	count  int
	cut    bool
	more   bool
	offset int
}

type readMetadata struct {
	Preview   string   `json:"preview"`
	Truncated bool     `json:"truncated"`
	Loaded    []string `json:"loaded"`
}

func (r *Registry) executeRead(ctx context.Context, call steploop.ToolCall) (steploop.ToolResult, error) {
	var input readInput
	if err := decodeInput(call.Input, &input, "filePath"); err != nil {
		return steploop.ToolResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return steploop.ToolResult{}, err
	}

	offset := 1
	if input.Offset != nil && *input.Offset != 0 {
		offset = *input.Offset
	}
	limit := defaultReadLimit
	if input.Limit != nil {
		limit = *input.Limit
	}

	resolved, err := r.resolvePath(input.FilePath)
	if err != nil {
		return steploop.ToolResult{}, err
	}
	info, err := os.Stat(resolved)
	kind := "file"
	if err == nil && info.IsDir() {
		kind = "directory"
	}
	if askErr := r.askExternalDirectory(ctx, call, resolved, kind); askErr != nil {
		return steploop.ToolResult{}, askErr
	}
	if askErr := r.ask(ctx, call, "read", []string{resolved}, map[string]any{}); askErr != nil {
		return steploop.ToolResult{}, askErr
	}
	if errors.Is(err, os.ErrNotExist) {
		return steploop.ToolResult{}, readMissError(resolved)
	}
	if err != nil {
		return steploop.ToolResult{}, err
	}

	title, err := filepath.Rel(r.workDir, resolved)
	if err != nil {
		title = resolved
	}
	if info.IsDir() {
		return readDirectory(ctx, resolved, title, offset, limit)
	}

	loaded := r.instructionService().Resolve(
		steploop.ToolMessagesFromContext(ctx), resolved, call.MessageID,
	)
	loadedPaths := make([]string, 0, len(loaded))
	for _, item := range loaded {
		loadedPaths = append(loadedPaths, item.Filepath)
	}

	sample, err := readSample(resolved, info.Size(), sampleBytes)
	if err != nil {
		return steploop.ToolResult{}, err
	}
	mimeType := sniffAttachmentMIME(sample, attachmentMIME(resolved))
	if _, ok := supportedImageMIMEs[mimeType]; ok || mimeType == "application/pdf" {
		data, err := os.ReadFile(resolved)
		if err != nil {
			return steploop.ToolResult{}, err
		}
		message := "Image read successfully"
		if mimeType == "application/pdf" {
			message = "PDF read successfully"
		}
		attachments := []msgmodel.FilePart{{
			Type: "file",
			Mime: mimeType,
			URL:  "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data),
		}}
		return steploop.ToolResult{
			Title:       title,
			Output:      message,
			Attachments: &attachments,
			Metadata: rawMetadata(readMetadata{
				Preview:   message,
				Truncated: false,
				Loaded:    loadedPaths,
			}),
		}, nil
	}

	if isBinaryFile(resolved, sample) {
		return steploop.ToolResult{}, fmt.Errorf("Cannot read binary file: %s", resolved)
	}

	file, err := readLines(ctx, resolved, limit, offset)
	if err != nil {
		return steploop.ToolResult{}, err
	}
	if file.count < file.offset && !(file.count == 0 && file.offset == 1) {
		return steploop.ToolResult{}, fmt.Errorf(
			"Offset %d is out of range for this file (%d lines)",
			file.offset,
			file.count,
		)
	}

	var output strings.Builder
	output.WriteString("<path>")
	output.WriteString(resolved)
	output.WriteString("</path>\n<type>file</type>\n<content>\n")
	for i, line := range file.raw {
		if i > 0 {
			output.WriteByte('\n')
		}
		fmt.Fprintf(&output, "%d: %s", i+file.offset, line)
	}

	last := file.offset + len(file.raw) - 1
	next := last + 1
	truncated := file.more || file.cut
	switch {
	case file.cut:
		fmt.Fprintf(
			&output,
			"\n\n(Output capped at 50 KB. Showing lines %d-%d. Use offset=%d to continue.)",
			file.offset,
			last,
			next,
		)
	case file.more:
		fmt.Fprintf(
			&output,
			"\n\n(Showing lines %d-%d of %d. Use offset=%d to continue.)",
			file.offset,
			last,
			file.count,
			next,
		)
	default:
		fmt.Fprintf(&output, "\n\n(End of file - total %d lines)", file.count)
	}
	output.WriteString("\n</content>")
	if len(loaded) > 0 {
		output.WriteString("\n\n<system-reminder>\n")
		for index, item := range loaded {
			if index > 0 {
				output.WriteString("\n\n")
			}
			output.WriteString(item.Content)
		}
		output.WriteString("\n</system-reminder>")
	}

	previewLimit := len(file.raw)
	if previewLimit > 20 {
		previewLimit = 20
	}
	return steploop.ToolResult{
		Title:  title,
		Output: output.String(),
		Metadata: rawMetadata(readMetadata{
			Preview:   strings.Join(file.raw[:previewLimit], "\n"),
			Truncated: truncated,
			Loaded:    loadedPaths,
		}),
	}, nil
}

func readMissError(path string) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("File not found: %s", path)
	}
	baseLower := strings.ToLower(base)
	items := make([]string, 0, 3)
	for _, entry := range entries {
		nameLower := strings.ToLower(entry.Name())
		if strings.Contains(nameLower, baseLower) || strings.Contains(baseLower, nameLower) {
			items = append(items, filepath.Join(dir, entry.Name()))
			if len(items) == 3 {
				break
			}
		}
	}
	if len(items) > 0 {
		return fmt.Errorf("File not found: %s\n\nDid you mean one of these?\n%s", path, strings.Join(items, "\n"))
	}
	return fmt.Errorf("File not found: %s", path)
}

func readDirectory(
	ctx context.Context,
	path string,
	title string,
	offset int,
	limit int,
) (steploop.ToolResult, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return steploop.ToolResult{}, err
	}
	items := make([]string, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return steploop.ToolResult{}, err
		}
		name := entry.Name()
		isDir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			if target, statErr := os.Stat(filepath.Join(path, name)); statErr == nil {
				isDir = target.IsDir()
			}
		}
		if isDir {
			name += "/"
		}
		items = append(items, name)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return jscompat.LocaleCompare(items[i], items[j]) < 0
	})

	start := offset - 1
	if start < 0 {
		start = 0
	}
	if start > len(items) {
		start = len(items)
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	sliced := items[start:end]
	truncated := start+len(sliced) < len(items)

	var output strings.Builder
	output.WriteString("<path>")
	output.WriteString(path)
	output.WriteString("</path>\n<type>directory</type>\n<entries>\n")
	output.WriteString(strings.Join(sliced, "\n"))
	if truncated {
		fmt.Fprintf(
			&output,
			"\n\n(Showing %d of %d entries. Use 'offset' parameter to read beyond entry %d)",
			len(sliced),
			len(items),
			offset+len(sliced),
		)
	} else {
		fmt.Fprintf(&output, "\n\n(%d entries)", len(items))
	}
	output.WriteString("\n</entries>")

	previewLimit := len(sliced)
	if previewLimit > 20 {
		previewLimit = 20
	}
	return steploop.ToolResult{
		Title:  title,
		Output: output.String(),
		Metadata: rawMetadata(readMetadata{
			Preview:   strings.Join(sliced[:previewLimit], "\n"),
			Truncated: truncated,
			Loaded:    []string{},
		}),
	}, nil
}

func rawMetadata(value any) msgmodel.RawObject {
	data, err := jscompat.Stringify(value)
	if err != nil {
		panic(err)
	}
	return msgmodel.RawObject(data)
}

func readSample(path string, fileSize int64, size int) ([]byte, error) {
	if fileSize == 0 {
		return []byte{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if fileSize < int64(size) {
		size = int(fileSize)
	}
	out := make([]byte, size)
	n, err := io.ReadFull(file, out)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	return out[:n], nil
}

func isBinaryFile(path string, sample []byte) bool {
	if _, ok := binaryExtensions[strings.ToLower(filepath.Ext(path))]; ok {
		return true
	}
	if len(sample) == 0 {
		return false
	}
	nonPrintable := 0
	for _, value := range sample {
		if value == 0 {
			return true
		}
		if value < 9 || (value > 13 && value < 32) {
			nonPrintable++
		}
	}
	return float64(nonPrintable)/float64(len(sample)) > 0.3
}

func sniffAttachmentMIME(data []byte, fallback string) string {
	switch {
	case bytes.HasPrefix(data, []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}):
		return "image/png"
	case bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg"
	case bytes.HasPrefix(data, []byte{0x47, 0x49, 0x46, 0x38}):
		return "image/gif"
	case bytes.HasPrefix(data, []byte{0x42, 0x4d}):
		return "image/bmp"
	case bytes.HasPrefix(data, []byte{0x25, 0x50, 0x44, 0x46, 0x2d}):
		return "application/pdf"
	case len(data) >= 12 &&
		bytes.Equal(data[:4], []byte{0x52, 0x49, 0x46, 0x46}) &&
		bytes.Equal(data[8:12], []byte{0x57, 0x45, 0x42, 0x50}):
		return "image/webp"
	default:
		return fallback
	}
}

func attachmentMIME(path string) string {
	value := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if semi := strings.IndexByte(value, ';'); semi >= 0 {
		value = value[:semi]
	}
	if value == "" {
		return "application/octet-stream"
	}
	return value
}

func readLines(ctx context.Context, path string, limit int, offset int) (readLinesResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return readLinesResult{}, err
	}
	defer file.Close()

	result := readLinesResult{raw: []string{}, offset: offset}
	start := offset - 1
	reader := bufio.NewReader(file)
	for {
		if err := ctx.Err(); err != nil {
			return readLinesResult{}, err
		}
		data, readErr := reader.ReadBytes('\n')
		if len(data) == 0 && errors.Is(readErr, io.EOF) {
			break
		}
		if len(data) > 0 && data[len(data)-1] == '\n' {
			data = data[:len(data)-1]
			if len(data) > 0 && data[len(data)-1] == '\r' {
				data = data[:len(data)-1]
			}
		}
		text := strings.ToValidUTF8(string(data), "\uFFFD")
		result.count++
		if result.count > start {
			if len(result.raw) >= limit {
				result.more = true
			} else {
				line := truncateJSString(text, maxLineLength)
				size := len([]byte(line))
				if len(result.raw) > 0 {
					size++
				}
				if readBytesLength(result.raw)+size > maxReadBytes {
					result.cut = true
					result.more = true
					break
				}
				result.raw = append(result.raw, line)
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return readLinesResult{}, readErr
			}
			break
		}
	}
	return result, nil
}

func readBytesLength(lines []string) int {
	total := 0
	for i, line := range lines {
		total += len([]byte(line))
		if i > 0 {
			total++
		}
	}
	return total
}

func truncateJSString(value string, limit int) string {
	units := utf16.Encode([]rune(value))
	if len(units) <= limit {
		return value
	}
	units = units[:limit]
	runes := utf16.Decode(units)
	out := string(runes)
	if len(runes) > 0 && runes[len(runes)-1] == utf8.RuneError && units[len(units)-1] >= 0xd800 && units[len(units)-1] <= 0xdbff {
		// JS substring can leave an unpaired high surrogate. Buffer.byteLength
		// and later UTF-8 output encode it as U+FFFD.
		out = string(runes[:len(runes)-1]) + "\uFFFD"
	}
	return out + maxLineSuffix
}
