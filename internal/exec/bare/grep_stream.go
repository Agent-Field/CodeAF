package bare

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	grepFileBytes      = 8 << 20
	grepLineBytes      = 64 << 10
	grepMatchCeiling   = 1000
	grepContextCeiling = 20
)

func grepSafetyDescription() string {
	return fmt.Sprintf(" Skips runtime output and files >%d MiB. Named files allow bounded log inspection.", grepFileBytes>>20)
}

// Read only the size observed at open, capped independently of file growth.
// One fixed-size line buffer suffices; a long line is drained and skipped,
// rather than accumulating arbitrary JSON/log output in memory.
func scanGrepFile(ctx context.Context, path string, visit func(int, string) error) (limited bool, err error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("not a regular file: %s", path)
	}
	size := info.Size()
	capped := size > grepFileBytes
	if capped {
		size = grepFileBytes
		limited = true
	}
	reader := bufio.NewReaderSize(io.LimitReader(file, size), grepLineBytes)
	head, _ := reader.Peek(min(8192, int(size)))
	if looksBinary(head) {
		return limited, nil
	}
	lineNumber := 0
	discarding := false
	for {
		if err := ctx.Err(); err != nil {
			return limited, err
		}
		line, readErr := reader.ReadSlice('\n')
		if errors.Is(readErr, bufio.ErrBufferFull) {
			discarding = true
			limited = true
			continue
		}
		// The byte cap may cut a line in half. Do not create a false regex
		// end-of-line match at that artificial boundary.
		if capped && errors.Is(readErr, io.EOF) && len(line) > 0 && line[len(line)-1] != '\n' {
			return true, nil
		}
		if len(line) > 0 || discarding {
			lineNumber++
			if !discarding {
				if err := visit(lineNumber, string(line)); err != nil {
					return limited, err
				}
			}
			discarding = false
		}
		if errors.Is(readErr, io.EOF) {
			return limited, nil
		}
		if readErr != nil {
			return limited, readErr
		}
	}
}

// exec.Cmd drains stderr into this writer without retaining unbounded errors.
type grepErrorBuffer struct{ data []byte }

func (b *grepErrorBuffer) Write(data []byte) (int, error) {
	n := len(data)
	if room := 4096 - len(b.data); room > 0 {
		b.data = append(b.data, data[:min(room, len(data))]...)
	}
	return n, nil
}
