package identity

import (
	"os"
	"path/filepath"
)

// ChatCount counts conversations with submitted work, rather than startup-only
// headers. It reads only session transcripts: workspace files are not chats and
// walking their trees would make a pairing refusal cost the size of every repo.
func ChatCount(home string) (int, error) {
	root := filepath.Join(home, "v3", "projects")
	projects, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	count := 0
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		sessions, err := os.ReadDir(filepath.Join(root, project.Name()))
		if err != nil {
			return 0, err
		}
		for _, session := range sessions {
			if !session.IsDir() {
				continue
			}
			dir := filepath.Join(root, project.Name(), session.Name())
			for _, name := range []string{filepath.Join(".cell", "transcript.jsonl"), "transcript.jsonl"} {
				used, err := noTurns(filepath.Join(dir, name))
				if os.IsNotExist(err) {
					continue
				}
				if err != nil {
					return 0, err
				}
				if used {
					count++
				}
				break
			}
		}
	}
	return count, nil
}
