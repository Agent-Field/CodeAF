package taskdoor

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func childIdentity(pid int, work, record string) (string, string, error) {
	root := filepath.Join("/proc", strconv.Itoa(pid))
	raw, err := os.ReadFile(filepath.Join(root, "cmdline"))
	if err != nil {
		return "", "", err
	}
	workArg, recordArg := false, false
	for _, arg := range bytes.Split(raw, []byte{0}) {
		if bytes.Equal(arg, []byte(work)) {
			workArg = true
		}
		if bytes.Equal(arg, []byte(record)) {
			recordArg = true
		}
	}
	if !workArg {
		return "", "", errors.New("the recorded process does not name this task's work folder")
	}
	if !recordArg {
		return "", "", errors.New("the recorded process does not name this task's private record")
	}
	stat, err := os.ReadFile(filepath.Join(root, "stat"))
	if err != nil {
		return "", "", err
	}
	// The command name may contain spaces and parentheses. Fields after its
	// last ')' start at field 3; field 22 is the kernel's process birth tick.
	end := strings.LastIndexByte(string(stat), ')')
	if end < 0 {
		return "", "", errors.New("the child's process start is not readable")
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) <= 19 || fields[0] == "Z" {
		return "", "", os.ErrNotExist
	}
	sum := sha256.Sum256(raw)
	return fields[19], hex.EncodeToString(sum[:]), nil
}

func findAttemptChild(work, record string) (int, string, string, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, "", "", err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		birth, hash, err := childIdentity(pid, work, record)
		if err == nil {
			return pid, birth, hash, nil
		}
	}
	return 0, "", "", os.ErrNotExist
}
