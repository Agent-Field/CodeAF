package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

const factoryOwnerFile = "factory-owner.json"

type factoryChatOwner struct {
	Step string `json:"step,omitempty"`
	Root string `json:"root"`
	Job  int    `json:"job"`
	Repo string `json:"repo"`
}

// Ownership lives beside the journal, not in a crew that can be replaced by a
// restart. Every round remains owned and deletable even when its links move.
func markFactoryChat(st *store.Store, it factory.Item, file string) error {
	if st == nil || it.ID <= 0 || strings.TrimSpace(file) == "" {
		return nil
	}
	data, err := json.Marshal(factoryChatOwner{Root: chatKey(st.Root()), Job: it.ID, Repo: it.Repo})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(filepath.Dir(file), factoryOwnerFile), data, 0o600)
}

func readFactoryOwner(file string) (factoryChatOwner, bool) {
	var o factoryChatOwner
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), factoryOwnerFile))
	if err != nil || json.Unmarshal(data, &o) != nil || o.Root == "" || o.Job <= 0 {
		return o, false
	}
	return o, true
}

// Legacy stage journals have a machine-generated first brief in the factory's
// own worktree. Ordinary chats, including ones in that worktree, do not match.
func legacyFactoryChat(file string, number int) bool {
	meta, err := session.LoadMeta(filepath.Dir(file))
	if err != nil || !strings.Contains(filepath.ToSlash(meta.Workspace), "/factory/work/") {
		return false
	}
	f, err := os.Open(file)
	if err != nil {
		return false
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 256*1024)
	for n := 0; scan.Scan() && n < 12; n++ {
		var row struct{ Type, Role, Content string }
		if json.Unmarshal(scan.Bytes(), &row) != nil || row.Type != "message" || row.Role != "user" {
			continue
		}
		lead, _, ok := strings.Cut(row.Content, ": ")
		if !ok || lead == "" || strings.ContainsAny(lead, " \n\t") {
			return false
		}
		expected := "You are the " + lead + " step of a codeaf run on issue #"
		if number > 0 {
			expected += strconv.Itoa(number) + "."
		}
		return strings.Contains(row.Content, "\n\n"+expected)
	}
	return false
}

func factoryOwnedConversation(file string) bool {
	if _, ok := readFactoryOwner(file); ok {
		return true
	}
	return legacyFactoryChat(file, 0)
}

func ownedJobChats(root string, it factory.Item, ownerRoots []string) []string {
	var out []string
	markers, _ := filepath.Glob(filepath.Join(root, "*", "*", factoryOwnerFile))
	for _, mark := range markers {
		file := filepath.Join(filepath.Dir(mark), "transcript.jsonl")
		o, ok := readFactoryOwner(file)
		if !ok || o.Job != it.ID || o.Repo != it.Repo {
			continue
		}
		match := len(ownerRoots) == 0
		for _, r := range ownerRoots {
			if chatKey(r) == o.Root || strings.HasPrefix(o.Root, filepath.Join(chatKey(r), "workers")+string(filepath.Separator)) {
				match = true
			}
		}
		if match {
			out = append(out, file)
		}
	}
	// Migrate only this job's exact worktree, and only generated stage briefs.
	work := ""
	if work == "" && len(ownerRoots) > 0 {
		work = filepath.Join(ownerRoots[0], "work", fmt.Sprintf("%s-%d", filepath.Base(it.Repo), it.Num))
	}
	if work != "" {
		metas, _ := filepath.Glob(filepath.Join(root, "*", "*", "meta.json"))
		for _, p := range metas {
			meta, err := session.LoadMeta(filepath.Dir(p))
			if err != nil || chatKey(meta.Workspace) != chatKey(work) {
				continue
			}
			file := filepath.Join(filepath.Dir(p), "transcript.jsonl")
			if legacyFactoryChat(file, it.Num) {
				out = append(out, file)
			}
		}
	}
	return out
}

func markFactoryStageChat(st *store.Store, it factory.Item, file string) error {
	if err := markFactoryChat(st, it, file); err != nil {
		return err
	}
	owner, ok := readFactoryOwner(file)
	if !ok {
		return nil
	}
	if it.Stream != nil && it.Stream.Cur >= 0 && it.Stream.Cur < len(it.Stream.Phases) {
		owner.Step = it.Stream.Phases[it.Stream.Cur].Name
	}
	data, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(filepath.Dir(file), factoryOwnerFile), data, 0600)
}
