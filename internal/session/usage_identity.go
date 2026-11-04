package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// usageIdentityCursor remembers only identities already read from this open
// ledger. Each append validates the generation under the same lock as its write,
// so queued live calls, take imports and other processes book an identity once.
// Rows without an identity remain distinct, as they were before identities.
type usageIdentityCursor struct {
	offset   int64
	seen     map[string]int64
	newline  bool
	ends     string
	modified time.Time
}

var errUsageLedgerReplaced = errors.New("usage ledger replaced during append")

func (c *usageIdentityCursor) append(file *os.File, raw []byte) (bool, error) {
	if err := filelock.Lock(file, true, false); err != nil {
		return false, err
	}
	defer filelock.Unlock(file)
	reader, err := usageIdentityReader(file)
	if err != nil {
		return false, err
	}
	defer reader.Close()
	if err := c.readTail(reader); err != nil {
		return false, err
	}
	var line struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &line); err != nil {
		return false, err
	}
	duplicate, err := c.duplicate(reader, line.ID)
	if err != nil {
		return false, err
	}
	if duplicate {
		raw = nil
	}
	position := c.offset
	// Preserve an unfinished old line and separate it from the next charge.
	// A complete EOF object was read above and needs only its missing newline.
	if c.newline {
		raw = append([]byte{'\n'}, raw...)
		position++
	}
	if len(raw) == 0 {
		return false, nil
	}
	if _, err := file.Write(raw); err != nil {
		return false, err
	}
	if line.ID != "" && !duplicate {
		c.seen[line.ID] = position
	}
	c.offset += int64(len(raw))
	c.newline = false
	return true, c.remember(reader)
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
		if row.ID != "" {
			c.seen[row.ID] = start + offset
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
	reader, err := os.Open(writer.Name())
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
	if json.Unmarshal(raw, &row) == nil && !row.At.IsZero() && row.ID == id {
		return true, nil
	}
	*c = usageIdentityCursor{}
	if err := c.readTail(file); err != nil {
		return false, err
	}
	_, exists = c.seen[id]
	return exists, nil
}
