package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

const collectionsSummary = `  aforge collections [list] [--json]    organize chat, work and file references`

const collectionsUsage = collectionsSummary + `
  aforge collections create <name> | rename <collection-id> <name>
  aforge collections show <collection-id>
  aforge collections add|remove <collection-id> <kind> <record-id>
  aforge collections find <kind> <record-id>
      kinds: collection, conversation, task, standing, artifact (a file path)
      tasks need --session <conversation-id>; all accept --db and --json
      organize references without moving files or starting work`

func runCollections(args []string) error { return runCollectionsTo(args, os.Stdout) }

func runCollectionsTo(args []string, output io.Writer) error {
	flags := commandFlags("collections")
	database := flags.String("db", home.Join("v3", "collections.db"), "collection database path; separate from learned memory")
	sessionID := flags.String("session", "", "owning conversation id for a task reference")
	asJSON := flags.Bool("json", false, "print structured records")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	rest := flags.Args()
	if len(rest) == 0 {
		rest = []string{"list"}
	}
	verb := rest[0]
	want := map[string]int{"list": 1, "create": 2, "rename": 3, "show": 2, "add": 4, "remove": 4, "find": 3}
	if n, ok := want[verb]; !ok || len(rest) != n {
		return fmt.Errorf("usage: %s", collectionsUsage)
	}
	if verb == "rename" || verb == "show" || verb == "add" || verb == "remove" {
		if err := (workspace.Ref{Kind: workspace.CollectionKind, ID: rest[1]}).Validate(); err != nil {
			return err
		}
	}
	if verb == "create" || verb == "rename" {
		if err := workspace.ValidateName(rest[len(rest)-1]); err != nil {
			return err
		}
	}
	var ref workspace.Ref
	if verb == "add" || verb == "remove" || verb == "find" {
		ref = workspace.Ref{Kind: workspace.Kind(rest[len(rest)-2]), ID: rest[len(rest)-1], SessionID: *sessionID}
		if ref.Kind == workspace.ArtifactKind {
			if ref.ID == "" {
				return fmt.Errorf("artifact reference needs a file path")
			}
			path, err := expandHome(ref.ID)
			if err != nil {
				return err
			}
			ref.ID, err = filepath.Abs(path)
			if err != nil {
				return err
			}
		}
		if err := ref.Validate(); err != nil {
			return err
		}
	} else if *sessionID != "" {
		return fmt.Errorf("--session belongs to a task reference in add, remove or find")
	}
	path, err := expandHome(*database)
	if err != nil {
		return err
	}
	store, err := workspace.Open(path)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx := context.Background()
	var result any
	switch verb {
	case "list":
		result, err = store.Collections(ctx)
	case "create":
		result, err = store.Create(ctx, rest[1])
	case "rename":
		err = store.Rename(ctx, rest[1], rest[2])
		result = workspace.Collection{ID: rest[1], Name: rest[2]}
	case "show":
		result, err = store.Members(ctx, rest[1])
	case "add":
		err = store.Add(ctx, rest[1], ref)
		result = ref
	case "remove":
		err = store.Remove(ctx, rest[1], ref)
		result = ref
	case "find":
		result, err = store.CollectionsFor(ctx, ref)
	}
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(output).Encode(result)
	}
	switch value := result.(type) {
	case workspace.Collection:
		_, err = fmt.Fprintf(output, "%s  %s\n", value.ID, value.Name)
	case []workspace.Collection:
		if len(value) == 0 {
			_, err = fmt.Fprintln(output, "No collections found. Create one with aforge collections create <name>.")
			return err
		}
		for _, c := range value {
			if _, err = fmt.Fprintf(output, "%s  %s\n", c.ID, c.Name); err != nil {
				return err
			}
		}
	case []workspace.Ref:
		if len(value) == 0 {
			_, err = fmt.Fprintln(output, "This collection has no references yet.")
			return err
		}
		for _, r := range value {
			if err = writeCollectionRef(output, r); err != nil {
				return err
			}
		}
	case workspace.Ref:
		if verb == "remove" {
			_, err = fmt.Fprintln(output, "Reference removed; the original record is unchanged.")
		} else {
			err = writeCollectionRef(output, value)
		}
	}
	return err
}

func writeCollectionRef(output io.Writer, ref workspace.Ref) error {
	var err error
	if ref.Kind == workspace.TaskKind {
		_, err = fmt.Fprintf(output, "task  %s  conversation %s\n", ref.ID, ref.SessionID)
	} else {
		_, err = fmt.Fprintf(output, "%s  %s\n", ref.Kind, ref.ID)
	}
	return err
}
