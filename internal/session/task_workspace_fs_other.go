//go:build !linux && !darwin

package session

import (
	"errors"
	"os"
)

func workspaceApplicationMoveOut(root, relative, staging, held string) error {
	return errors.New("transactional portable apply requires Linux or Darwin")
}
func workspaceApplicationCreateDirectory(root, relative string, mode os.FileMode) error {
	return errors.New("transactional portable apply requires Linux or Darwin")
}
func workspaceApplicationRenameExclusive(source, root, relative string) error {
	return errors.New("transactional portable apply requires Linux or Darwin")
}
func workspaceApplicationRemove(root, relative string, directory bool) error {
	return errors.New("transactional portable apply requires Linux or Darwin")
}
func workspaceApplicationChmod(root, relative string, mode os.FileMode) error {
	return errors.New("transactional portable apply requires Linux or Darwin")
}

func workspaceApplicationDevice(path string) (uint64, error) {
	return 0, errors.New("transactional portable apply requires Linux or Darwin")
}

func workspaceApplicationSetSymlinkMode(root, relative string, mode os.FileMode) error {
	return errors.New("transactional portable apply requires Linux or Darwin")
}

func workspaceApplicationDirectoryIdentity(path string) (string, error) {
	return "", errors.New("transactional portable apply requires Linux or Darwin")
}
