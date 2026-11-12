package resident

import "github.com/Agent-Field/codeaf/internal/shell"

func serviceHealthCommand(command string) ([]string, error) {
	bash, err := shell.Bash()
	return append([]string{bash}, shell.PipefailArgs(bash, []string{"-lc"}, command)...), err
}
