package taskdoor

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

func childIdentity(pid int, work, record string) (string, string, error) {
	raw, err := exec.Command("ps", "-ww", "-p", strconv.Itoa(pid), "-o", "lstart=,stat=,command=").Output()
	if err != nil {
		return "", "", err
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 7 || strings.HasPrefix(fields[5], "Z") || !strings.Contains(string(raw), work) || !strings.Contains(string(raw), record) {
		return "", "", errors.New("the recorded process is not this task's child")
	}
	sum := sha256.Sum256([]byte(strings.Join(fields[6:], " ")))
	return strings.Join(fields[:5], " "), hex.EncodeToString(sum[:]), nil
}

func findAttemptChild(work, record string) (int, string, string, error) {
	raw, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return 0, "", "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.Contains(line, work) || !strings.Contains(line, record) {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		birth, hash, err := childIdentity(pid, work, record)
		if err == nil {
			return pid, birth, hash, nil
		}
	}
	return 0, "", "", errors.New("this task's child is no longer running")
}
