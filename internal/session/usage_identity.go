package session

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// usageIdentityCursor remembers only identities already read from this open
// ledger. Each append reads the new tail under the same lock as its write, so
// queued live calls, take imports and other processes all book an identity once.
// Rows without an identity remain distinct, as they were before identities.
type usageIdentityCursor struct {
	offset  int64
	seen    map[string]bool
	newline bool
}

func (c *usageIdentityCursor) append(file *os.File, raw []byte) (bool, error) {
	if err := filelock.Lock(file, true, false); err != nil {
		return false, err
	}
	defer filelock.Unlock(file)
	if err := c.readTail(file); err != nil {
		return false, err
	}
	var line struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &line); err != nil {
		return false, err
	}
	if line.ID != "" && c.seen[line.ID] {
		raw = nil
	}
	// Preserve an unfinished old line and separate it from the next charge.
	// A complete EOF object was read above and needs only its missing newline.
	if c.newline {
		raw = append([]byte{'\n'}, raw...)
	}
	if len(raw) == 0 {
		return false, nil
	}
	if _, err := file.Write(raw); err != nil {
		return false, err
	}
	if line.ID != "" {
		c.seen[line.ID] = true
	}
	c.offset += int64(len(raw))
	c.newline = false
	return true, nil
}

func (c *usageIdentityCursor) readTail(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if c.seen == nil || info.Size() < c.offset {
		c.offset = 0
		c.seen = make(map[string]bool)
	}
	size := info.Size() - c.offset
	if size == 0 {
		return nil
	}
	reader, err := os.Open(file.Name())
	if err != nil {
		return err
	}
	defer reader.Close()
	source, err := reader.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, source) {
		return errors.New("usage ledger replaced during append")
	}
	var last [1]byte
	if _, err := reader.ReadAt(last[:], info.Size()-1); err != nil {
		return err
	}
	c.newline = last[0] != '\n'
	// The added delimiter lets the ordinary reader recognize a complete EOF
	// object. An incomplete object stays invalid, without losing later charges.
	rows, _, err := scanUsage(io.MultiReader(io.NewSectionReader(reader, c.offset, size), strings.NewReader("\n")), time.Time{})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.ID != "" {
			c.seen[row.ID] = true
		}
	}
	c.offset += size
	return nil
}
