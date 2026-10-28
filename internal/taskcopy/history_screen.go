package taskcopy

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/keys"
)

// writeScreenedFullHistory publishes only a validated candidate, retaining the
// preceding portable bundle if screening or extraction fails.
func writeScreenedFullHistory(tree, destination string) error {
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".screen-history-*")
	if err != nil {
		return err
	}
	staged := temporary.Name()
	defer os.Remove(staged)
	if err := temporary.Close(); err != nil {
		return err
	}
	if _, err := git(tree, "bundle", "create", staged, "HEAD"); err != nil {
		return errors.New("cannot create portable full history; local work is preserved")
	}
	if err := screenFullHistory(staged); err != nil {
		return err
	}
	return os.Rename(staged, destination)
}

// screenFullHistory judges every object in the exact self-contained bundle.
// A single batch stream covers blobs reached only by tags as well as complete
// history. Tree entries retain filename rules; raw objects retain content rules.
func screenFullHistory(bundle string) error {
	file, err := os.Open(bundle)
	if err != nil {
		return errors.New("cannot read portable full history; local work is preserved")
	}
	defer file.Close()
	header, err := readFullBundleHeader(file)
	if err != nil {
		return err
	}
	scratch, err := os.MkdirTemp("", "codeaf-screen-history-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	if err := historyGitCommand(scratch, "init", "--bare", "-q", "--object-format="+header.format).Run(); err != nil {
		return errFullHistoryContainer
	}
	if err := importFullBundlePack(file, scratch, header); err != nil {
		return err
	}
	kind, err := historyGitCommand(scratch, "cat-file", "-t", header.head).Output()
	if err != nil || string(kind) != "commit\n" {
		return errFullHistoryContainer
	}
	if err := historyGitCommand(scratch, "fsck", "--strict", "--no-reflogs", "--no-dangling", header.head).Run(); err != nil {
		return errFullHistoryContainer
	}
	scanner := keys.Scanner{}
	return screenHistoryObjects(scratch, &scanner)
}

var errFullHistoryContainer = errors.New("portable full history is not a supported complete bundle; local work is preserved")

type fullBundleHeader struct {
	head, format string
	offset       int64
}

// The full-history producer writes exactly HEAD, with no prerequisites or
// arbitrary references. Parse that original wire contract before any history import:
// cloning can silently discard references and is not a container validator.
func readFullBundleHeader(file *os.File) (fullBundleHeader, error) {
	input := bufio.NewReaderSize(file, 256)
	var consumed int64
	line := func() (string, error) {
		raw, err := input.ReadSlice('\n')
		consumed += int64(len(raw))
		if err != nil {
			return "", errFullHistoryContainer
		}
		return string(raw[:len(raw)-1]), nil
	}
	version, err := line()
	if err != nil {
		return fullBundleHeader{}, err
	}
	format := "sha1"
	switch version {
	case "# v2 git bundle":
	case "# v3 git bundle":
		capability, err := line()
		if err != nil {
			return fullBundleHeader{}, err
		}
		if capability != "@object-format=sha1" && capability != "@object-format=sha256" {
			return fullBundleHeader{}, errFullHistoryContainer
		}
		format = strings.TrimPrefix(capability, "@object-format=")
	default:
		return fullBundleHeader{}, errFullHistoryContainer
	}
	reference, err := line()
	if err != nil {
		return fullBundleHeader{}, err
	}
	head, err := fullBundleHead(reference, format)
	if err != nil {
		return fullBundleHeader{}, err
	}

	empty, err := line()
	if err != nil || empty != "" {
		return fullBundleHeader{}, errFullHistoryContainer
	}
	return fullBundleHeader{head: head, format: format, offset: consumed}, nil
}

func fullBundleHead(reference, format string) (string, error) {
	head, name, ok := strings.Cut(reference, " ")
	oid, err := hex.DecodeString(head)
	expected := 20
	if format == "sha256" {
		expected = 32
	}
	if !ok || name != "HEAD" || err != nil || len(oid) != expected || hex.EncodeToString(oid) != head {
		return "", errFullHistoryContainer
	}
	return head, nil
}

// index-pack validates and stores the original pack. It accepts trailing input
// but echoes it after its digest line, so require exactly that bounded line and
// EOF. Never capture or echo those extra bytes or the process's diagnostic text.
func importFullBundlePack(file *os.File, repository string, header fullBundleHeader) error {
	if _, err := file.Seek(header.offset, io.SeekStart); err != nil {
		return errFullHistoryContainer
	}
	cmd := historyGitCommand(repository, "index-pack", "--strict", "--stdin")
	cmd.Stdin = file
	output, err := cmd.StdoutPipe()
	if err != nil {
		return errFullHistoryContainer
	}
	defer output.Close()
	if err := cmd.Start(); err != nil {
		return errFullHistoryContainer
	}
	input := bufio.NewReaderSize(output, 256)
	line, readErr := input.ReadSlice('\n')
	valid := validPackReceipt(line, len(header.head))
	_, tailErr := input.ReadByte()
	if readErr != nil || !valid || !errors.Is(tailErr, io.EOF) {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if readErr != nil || !valid || !errors.Is(tailErr, io.EOF) || waitErr != nil {
		return errFullHistoryContainer
	}
	return nil
}

func validPackReceipt(line []byte, hexSize int) bool {
	text := string(line)
	if len(text) != len("pack\t")+hexSize+1 || !strings.HasPrefix(text, "pack\t") || !strings.HasSuffix(text, "\n") {
		return false
	}
	_, err := hex.DecodeString(text[len("pack\t") : len(text)-1])
	return err == nil
}

// Inspection stays in its owned object store even when inherited routing or
// configuration variables name another checkout. Executable lookup remains
// available for the installed process and failure regression tests.
func historyGitCommand(repository string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", repository}, args...)...)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_NO_REPLACE_OBJECTS=1", "GIT_OPTIONAL_LOCKS=0")
	return cmd
}

func screenCarriedHistory(dest string) error {
	prepared, err := readRecord(dest)
	if err != nil {
		return err
	}
	if prepared.FullHistory {
		return screenFullHistory(filepath.Join(dest, bundleName))
	}
	return nil
}

// No object bytes are materialized or echoed. On malformed framing, scanner
// errors or findings, stop the child and refuse the bundle without modifying it.
func screenHistoryObjects(repository string, scanner *keys.Scanner) error {
	cmd := historyGitCommand(repository, "cat-file", "--batch", "--batch-all-objects")
	output, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	defer output.Close()
	if err := cmd.Start(); err != nil {
		return err
	}
	scanErr := screenHistoryBatch(bufio.NewReaderSize(output, 64<<10), scanner)
	if scanErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if scanErr != nil {
		return scanErr
	}
	if waitErr != nil {
		return fmt.Errorf("cannot read portable history for secret screening: %w", waitErr)
	}
	return nil
}

type historyHeader struct {
	kind     string
	size     int64
	oidBytes int
}

func readHistoryHeader(input *bufio.Reader) (historyHeader, error) {
	line, err := input.ReadSlice('\n')
	if errors.Is(err, io.EOF) && len(line) == 0 {
		return historyHeader{}, io.EOF
	}
	if err != nil {
		return historyHeader{}, errors.New("cannot read portable history object header")
	}
	fields := strings.Fields(string(line))
	if len(fields) != 3 {
		return historyHeader{}, errors.New("malformed portable history object header")
	}
	oid, err := hex.DecodeString(fields[0])
	if err != nil || (len(oid) != 20 && len(oid) != 32) {
		return historyHeader{}, errors.New("malformed portable history object identity")
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || size < 0 {
		return historyHeader{}, errors.New("malformed portable history object size")
	}
	return historyHeader{kind: fields[1], size: size, oidBytes: len(oid)}, nil
}

func screenHistoryBatch(input *bufio.Reader, scanner *keys.Scanner) error {
	for {
		header, err := readHistoryHeader(input)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		object := &io.LimitedReader{R: input, N: header.size}
		if err := screenHistoryObject(object, header.kind, header.oidBytes, scanner); err != nil {
			return err
		}
		if object.N != 0 {
			return errors.New("truncated portable history object")
		}
		if separator, err := input.ReadByte(); err != nil || separator != '\n' {
			return errors.New("malformed portable history object separator")
		}
	}
}

func screenHistoryObject(object io.Reader, kind string, oidBytes int, scanner *keys.Scanner) error {
	switch kind {
	case "tree":
		return screenHistoryTree(object, oidBytes, scanner)
	case "blob", "commit", "tag":
		return historyFinding(scanner, "history-object", object)
	default:
		return errors.New("unsupported portable history object type")
	}
}

func screenHistoryTree(object io.Reader, oidBytes int, scanner *keys.Scanner) error {
	input := bufio.NewReaderSize(object, 64<<10)
	for {
		entry, err := input.ReadSlice(0)
		if errors.Is(err, io.EOF) && len(entry) == 0 {
			return nil
		}
		if err != nil {
			return errors.New("malformed or oversized portable history tree entry")
		}
		mode, name, ok := strings.Cut(string(entry[:len(entry)-1]), " ")
		if !ok || !filepath.IsLocal(name) || filepath.Base(name) != name || name == "." {
			return errors.New("unsafe portable history tree entry")
		}
		if mode != "40000" && mode != "160000" {
			if err := historyFinding(scanner, name, strings.NewReader("")); err != nil {
				return err
			}
		}
		if _, err := io.CopyN(io.Discard, input, int64(oidBytes)); err != nil {
			return errors.New("truncated portable history tree entry")
		}
	}
}

func historyFinding(scanner *keys.Scanner, name string, input io.Reader) error {
	finding, err := scanner.Reader(name, input)
	if err != nil {
		return fmt.Errorf("cannot screen portable history: %w", err)
	}
	if finding.Rule != "" {
		return fmt.Errorf("portable history contains secret material (%s); local work is preserved but this copy cannot be transferred", finding.Rule)
	}
	return nil
}
