// Case-collision suppression — port of src/util/case-collision.ts:1-98
// (swe-pro 3b25a1a).
package util

import (
	"context"
	"fmt"
	"strings"
)

type CollisionResult struct {
	Collided   []string `json:"collided"`
	Suppressed []string `json:"suppressed"`
	Warning    string   `json:"warning,omitempty"`
}

func SuppressCaseCollisions(ctx context.Context, gitDir string) CollisionResult {
	result, _ := RunProcess(ctx, []string{"git", "ls-files"}, RunOptions{
		ProcessOptions: ProcessOptions{Cwd: gitDir},
		NoThrow:        true,
	})
	if result.Code != 0 {
		return CollisionResult{
			Collided: []string{}, Suppressed: []string{},
			Warning: fmt.Sprintf("git ls-files exited %d", result.Code),
		}
	}
	paths := []string{}
	for _, path := range strings.Split(string(result.Stdout), "\n") {
		if path != "" {
			paths = append(paths, path)
		}
	}
	order := []string{}
	groups := map[string][]string{}
	for _, path := range paths {
		key := jsLower(path)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], path)
	}
	collided := []string{}
	for _, key := range order {
		if len(groups[key]) > 1 {
			collided = append(collided, groups[key]...)
		}
	}
	if len(collided) == 0 {
		return CollisionResult{Collided: []string{}, Suppressed: []string{}}
	}
	suppressed := []string{}
	for start := 0; start < len(collided); start += 100 {
		end := min(start+100, len(collided))
		batch := collided[start:end]
		command := []string{"git", "update-index", "--skip-worktree", "--"}
		command = append(command, batch...)
		update, _ := RunProcess(ctx, command, RunOptions{
			ProcessOptions: ProcessOptions{Cwd: gitDir},
			NoThrow:        true,
		})
		if update.Code == 0 {
			suppressed = append(suppressed, batch...)
		}
	}
	return CollisionResult{Collided: collided, Suppressed: suppressed}
}

func jsLower(value string) string {
	// Default Unicode lowercasing plus the unconditional SpecialCasing entry
	// used by JS String#toLowerCase.
	value = strings.ReplaceAll(value, "\u0130", "i\u0307")
	return strings.ToLower(value)
}
