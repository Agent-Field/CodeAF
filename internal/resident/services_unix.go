//go:build !windows

package resident

func serviceHealthCommand(command string) ([]string, error) {
	return []string{"bash", "-lc", command}, nil
}
