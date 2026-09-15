package main

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// channelRetention is the one place that decides how many disposable channel
// releases remain. The workflow asks this command which tags have expired.
const channelRetention = 40

type channelRelease struct {
	tag       string
	createdAt time.Time
}

func runPrune(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) != 2 || args[0] != "--channel" || (args[1] != "dev" && args[1] != "staging") {
		return usageErr("prune needs --channel dev or --channel staging")
	}
	channel := args[1]

	var releases []channelRelease
	scanner := bufio.NewScanner(stdin)
	line := 0
	for scanner.Scan() {
		line++
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) != 2 || strings.TrimSpace(fields[0]) == "" {
			return fmt.Errorf("release line %d must be <tag><tab><RFC3339 createdAt>", line)
		}
		createdAt, err := time.Parse(time.RFC3339, strings.TrimSpace(fields[1]))
		if err != nil {
			return fmt.Errorf("release line %d has an invalid createdAt: %w", line, err)
		}
		if tagKind(fields[0]) == channel {
			releases = append(releases, channelRelease{tag: fields[0], createdAt: createdAt})
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read releases: %w", err)
	}

	sort.Slice(releases, func(i, j int) bool {
		if releases[i].createdAt.Equal(releases[j].createdAt) {
			return releases[i].tag < releases[j].tag
		}
		return releases[i].createdAt.Before(releases[j].createdAt)
	})
	expired := len(releases) - channelRetention
	for i := 0; i < expired; i++ {
		fmt.Fprintln(stdout, releases[i].tag)
	}
	return nil
}
