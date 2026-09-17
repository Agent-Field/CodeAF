package update

import "strings"

// Plan is the executable and arguments a restored terminal restarts with.
type Plan struct {
	Path string
	Args []string
}

// RestartArgs keeps the original launch shape and points it at this transcript.
func RestartArgs(original []string, transcript string) []string {
	args := append([]string(nil), original...)
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return args
	}
	if len(args) == 0 {
		return []string{"chat", "--session", transcript}
	}
	for index, arg := range args {
		if arg == "--session" {
			if index+1 < len(args) {
				args[index+1] = transcript
				return args
			}
			return append(args, transcript)
		}
		if strings.HasPrefix(arg, "--session=") {
			args[index] = "--session=" + transcript
			return args
		}
	}
	return append(args, "--session", transcript)
}
