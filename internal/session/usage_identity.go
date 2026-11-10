package session

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

// UsageLedgerLockSuffix keeps remote append and reconstruction on one process-shared lock.
const UsageLedgerLockSuffix = ".remote-lock"

// UsageExplicitID returns the explicit call identity on line when one is set:
// CallID, a remote-event key, or ID.
func UsageExplicitID(line UsageLine) string {
	if id := strings.TrimSpace(line.CallID); id != "" {
		return id
	}
	if ev := strings.TrimSpace(line.RemoteEvent); ev != "" {
		return "remote:" + ev
	}
	if id := strings.TrimSpace(line.ID); id != "" {
		return id
	}
	return ""
}

// UsageFingerprint hashes the eight call facts when both At and Session are present.
func UsageFingerprint(line UsageLine) string {
	session := strings.TrimSpace(line.Session)
	if line.At.IsZero() || session == "" {
		return ""
	}
	raw, _ := json.Marshal([]any{
		line.At.UTC().Format(time.RFC3339Nano),
		session,
		line.Model,
		line.Role,
		line.Calls,
		line.Input,
		line.Output,
		line.USD,
	})
	sum := sha256.Sum256(raw)
	return "call:" + hex.EncodeToString(sum[:])
}

// UsageCallID identifies the original worker row independently of the computer,
// folder or parent that later carries it. Older worker rows need no rewrite:
// the task transport computes the same identity from their original facts.
func UsageCallID(line UsageLine) string {
	if id := UsageExplicitID(line); id != "" {
		return id
	}
	return UsageFingerprint(line)
}

// usageIdentityCursor remembers only identities already read from this open
// ledger. Each append validates the generation under the same lock as its write,
// so queued live calls, take imports and other processes book an identity once.
// Rows without an identity remain distinct, as they were before identities.
type usageIdentityCursor struct {
	buffer   []byte
	offset   int64
	seen     map[string]int64
	newline  bool
	ends     string
	modified time.Time
}

var errUsageLedgerReplaced = errors.New("usage ledger replaced during append")

// append holds one ledger generation stable for a bounded batch. Identities
// supplied by the recorder avoid decoding the rows it just encoded.
func (c *usageIdentityCursor) append(file *os.File, rows []usageWrite) (int, error) {
	if err := lockUsageIdentityWriter(file); err != nil {
		return 0, err
	}
	defer unlockUsageIdentityWriter(file)
	reader, err := usageIdentityReader(file)
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	if err := lockUsageIdentityReader(reader); err != nil {
		return 0, err
	}
	defer unlockUsageIdentityReader(reader)
	if err := c.readTail(reader); err != nil {
		return 0, err
	}
	raw := c.buffer[:0]
	if c.newline {
		raw = append(raw, '\n')
	}
	pending := make(map[string]int64, len(rows))
	written := 0
	for _, row := range rows {
		if row.line == nil {
			continue
		}
		_, duplicate := pending[row.id]
		if !duplicate {
			duplicate, err = c.duplicate(reader, row.id)
			if err != nil {
				return 0, err
			}
		}
		if duplicate {
			continue
		}
		if row.id != "" {
			pending[row.id] = c.offset + int64(len(raw))
		}
		raw = append(raw, row.line...)
		written++
	}
	if len(raw) == 0 {
		return 0, nil
	}
	if _, err := file.Write(raw); err != nil {
		return 0, err
	}
	for id, position := range pending {
		c.seen[id] = position
	}
	c.offset += int64(len(raw))
	c.newline = false
	c.buffer = raw[:0]
	return written, c.remember(reader)
}

func (c *usageIdentityCursor) readTail(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	source, err := os.Stat(file.Name())
	if err != nil {
		return err
	}
	if !os.SameFile(info, source) {
		return errUsageLedgerReplaced
	}
	// External growth can include an interior restore. Neither sampled ends
	// nor a cached positive row can prove that a missing identity is absent.
	// Only our unchanged generation is trusted; external changes rebuild it.
	valid := c.seen != nil && info.Size() == c.offset && info.ModTime().Equal(c.modified)
	if valid {
		ends, err := usagePrefixEnds(file, c.offset)
		if err != nil {
			return err
		}
		valid = ends == c.ends
	}
	if !valid {
		*c = usageIdentityCursor{seen: make(map[string]int64)}
	}
	size := info.Size() - c.offset
	if size == 0 {
		return nil
	}
	var last [1]byte
	if _, err := file.ReadAt(last[:], info.Size()-1); err != nil {
		return err
	}
	c.newline = last[0] != '\n'
	// The added delimiter lets the ordinary reader recognize a complete EOF
	// object. An incomplete object stays invalid, without losing later charges.
	start := c.offset
	_, err = scanUsageLines(io.MultiReader(io.NewSectionReader(file, c.offset, size), strings.NewReader("\n")), -1, func(offset int64, row UsageLine) error {
		if id := row.ChargeID(); id != "" {
			c.seen[id] = start + offset
		}
		return nil
	})
	if err != nil {
		return err
	}
	c.offset += size
	return c.remember(file)
}

// remember validates the cached prefix on the next append at a fixed read cost.
// Same-size modifications also invalidate it even if both ends were preserved.
func (c *usageIdentityCursor) remember(file *os.File) error {
	ends, err := usagePrefixEnds(file, c.offset)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	c.ends, c.modified = ends, info.ModTime()
	return nil
}

// usageIdentityReader keeps the append descriptor write-only, so opening a
// blocked sink retains its ordinary semantics. Both descriptors must name the
// same inode before any cached identity can suppress this writer's charge.
func usageIdentityReader(writer *os.File) (*os.File, error) {
	reader, err := fsreplace.Open(writer.Name())
	if err != nil {
		return nil, err
	}
	info, err := writer.Stat()
	if err == nil {
		var source os.FileInfo
		source, err = reader.Stat()
		if err == nil && !os.SameFile(info, source) {
			err = errUsageLedgerReplaced
		}
	}
	if err != nil {
		reader.Close()
		return nil, err
	}
	return reader, nil
}

// A sampled prefix detects ordinary restores cheaply. Before suppressing a
// charge, also validate its actual cached row: an interior rewrite followed by
// growth can preserve both sampled ends. A stale position rebuilds the index.
func (c *usageIdentityCursor) duplicate(file *os.File, id string) (bool, error) {
	position, exists := c.seen[id]
	if !exists {
		return false, nil
	}
	buffer := bufio.NewReader(io.NewSectionReader(file, position, usageLineBytes))
	raw, err := buffer.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	var row UsageLine
	if json.Unmarshal(raw, &row) == nil && !row.At.IsZero() && row.ChargeID() == id {
		return true, nil
	}
	*c = usageIdentityCursor{}
	if err := c.readTail(file); err != nil {
		return false, err
	}
	_, exists = c.seen[id]
	return exists, nil
}
