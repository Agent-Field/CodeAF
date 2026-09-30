package identity

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// predecessorsFile lists, one id per line, the identities this one replaced by
// rotation, oldest first. A device that is paired later shows it to a computer
// still on an old identity, which is how that computer knows it may follow.
const predecessorsFile = "predecessors"

// MaxPredecessors bounds the list: a person who rotates more often than this
// has no computer still on the oldest ones.
const MaxPredecessors = 8

var idShape = regexp.MustCompile(`^id_[0-9a-f]{32}$`)

// ValidID says whether s has the shape of an identity id.
func ValidID(s string) bool { return idShape.MatchString(s) }

// Predecessors answers the identities the one under home replaced, or none.
func Predecessors(home string) []string {
	raw, err := os.ReadFile(filepath.Join(home, predecessorsFile))
	if err != nil {
		return nil
	}
	var ids []string
	for _, line := range strings.Fields(string(raw)) {
		if ValidID(line) {
			ids = append(ids, line)
		}
	}
	return ids
}

// RecordPredecessor adds id to the list, once, keeping the newest
// MaxPredecessors. Repeating it changes nothing, so a crashed rotation can
// call it again.
func RecordPredecessor(home, id string) error {
	if !ValidID(id) {
		return errors.New("identity: not an identity id")
	}
	ids := Predecessors(home)
	if slices.Contains(ids, id) {
		return nil
	}
	ids = append(ids, id)
	ids = ids[max(0, len(ids)-MaxPredecessors):]
	tmp, err := privateTemp(home, []byte(strings.Join(ids, "\n")+"\n"))
	if err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(home, predecessorsFile))
}
