package session

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/home"

	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

func finishWorkspaceRootInput(source string, tree taskTree, baseline taskcopy.PortableState) (taskTree, error) {
	if err := taskcopy.MaterializePortableInput(source, tree.dir, baseline); err != nil {
		return tree, fmt.Errorf("preserve private task input: %w", err)
	}
	digest, err := taskcopy.WritePortableBaseline(tree.dir, baseline)
	if err != nil {
		return tree, fmt.Errorf("record original task input metadata: %w", err)
	}
	tree.portableBaselineDigest = digest
	return tree, nil
}

func taskWorkspaceHistoricalBaseline(tree taskTree) (taskcopy.PortableState, error) {
	if tree.portableBaselineDigest == "" {
		return taskcopy.PortableState{}, errors.New("this older private copy has no recorded source metadata baseline; its files are retained, but portable apply cannot safely infer their original metadata")
	}
	state, digest, err := taskcopy.ReadPortableBaseline(tree.dir)
	if err != nil {
		return state, fmt.Errorf("historical source metadata is unavailable; private copy retained: %w", err)
	}
	if digest != tree.portableBaselineDigest {
		return state, errors.New("historical source metadata changed; private copy retained")
	}
	return state, nil
}

func preserveFailedWorkspaceCreation(tree taskTree, cause error) error {
	root := home.Join("workspace-creation-failures")
	sum := sha256.Sum256([]byte(tree.dir + "\x00" + tree.uri))
	path := filepath.Join(root, hex.EncodeToString(sum[:])+".json")
	record := struct {
		Copy  *TaskCopyRecord
		Error string
	}{runCopyOf(tree), cause.Error()}
	journalErr := os.MkdirAll(root, 0700)
	if journalErr == nil {
		journalErr = writeJSONAtomic(path, record)
	}
	if journalErr != nil {
		return fmt.Errorf("%w; unused private input retained at %s (identity %s); could not save recovery receipt: %v", cause, tree.dir, tree.uri, journalErr)
	}
	return fmt.Errorf("%w; unused private input retained at %s (identity %s; recovery receipt %s)", cause, tree.dir, tree.uri, path)
}

func openPortableWorkspaceFamilyTree(tree taskTree, baseline taskcopy.PortableState) (taskTree, error) {
	var names []string
	for name := range baseline.Withheld {
		names = append(names, name)
	}
	sort.Strings(names)
	var exclusions strings.Builder
	for _, name := range names {
		if strings.ContainsAny(name, "\n\r") {
			return tree, errors.New("withheld input filename cannot safely be excluded from private history")
		}
		exclusions.WriteString("/" + strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[", " ", "\\ ", "#", "\\#", "!", "\\!").Replace(name) + "\n")
	}
	if problem := openFamilyRepositoryWithPolicy(tree.dir, names); problem != "" {
		return tree, errors.New(problem)
	}
	if len(names) > 0 {
		if err := os.WriteFile(filepath.Join(tree.dir, ".git", "info", "exclude"), []byte(exclusions.String()), 0600); err != nil {
			return tree, err
		}
	}
	return tree, nil
}

func checkTaskWorkspaceWithheldOutputs(tree taskTree) error {
	baseline, err := taskWorkspaceHistoricalBaseline(tree)
	if err != nil {
		return err
	}
	var names []string
	for name := range baseline.Withheld {
		names = append(names, name)
	}
	actual, err := taskcopy.InspectPortablePaths(tree.dir, names)
	if err != nil {
		return err
	}
	for name, expected := range baseline.Withheld {
		if actual.Entries[name] != expected {
			return fmt.Errorf("withheld input %q changed; private copy retained", name)
		}
	}
	current, err := taskcopy.InspectPortableState(tree.dir)
	if err != nil {
		return err
	}
	for name := range current.Withheld {
		if _, ok := baseline.Withheld[name]; !ok {
			return fmt.Errorf("new withheld output %q cannot be included in a reviewed snapshot; private copy retained", name)
		}
	}
	return nil
}
